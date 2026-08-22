package ui

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

type benchmarkGeometry struct {
	width          int
	height         int
	topHeight      int
	tableHeight    int
	controlsHeight int
	statusHeight   int
	controlsWidth  int
	statusWidth    int
	stacked        bool
}

type benchmarkSortColumn int

const (
	benchmarkSortNone benchmarkSortColumn = iota
	benchmarkSortRank
	benchmarkSortModel
	benchmarkSortEffort
	benchmarkSortTask
	benchmarkSortResult
	benchmarkSortTime
	benchmarkSortTokens
	benchmarkSortCost
)

type benchmarkColumn struct {
	sort  benchmarkSortColumn
	title string
	x     int
	width int
}

type benchmarkControlSegment struct {
	text    string
	button  footerButtonID
	enabled bool
	active  bool
}

const benchmarkDetailCopyLabel = "[ (C) COPY ]"

func layoutBenchmarkArea(width, height int) benchmarkGeometry {
	width = max(width, 1)
	height = max(height, 1)
	// Keep three status rows available for runtime state, current trial detail,
	// and evaluator limits while still giving the matrix most vertical space.
	topHeight := min(5, height)
	controlsWidth := min(max(width*3/5, 38), max(width-1, 1))
	statusWidth := max(width-controlsWidth-1, 1)
	// A framed status panel needs enough room for useful content. Once that is no
	// longer true, stack it below the controls instead of letting Lip Gloss grow
	// the row beyond the terminal width.
	if statusWidth < 12 {
		controlsHeight := min(5, height)
		statusHeight := 0
		if height-controlsHeight >= 10 {
			statusHeight = 5
		}
		return benchmarkGeometry{
			width: width, height: height, topHeight: controlsHeight + statusHeight,
			tableHeight:    max(height-controlsHeight-statusHeight, 0),
			controlsHeight: controlsHeight, statusHeight: statusHeight,
			controlsWidth: width, statusWidth: width, stacked: true,
		}
	}
	return benchmarkGeometry{
		width: width, height: height, topHeight: topHeight,
		tableHeight:    max(height-topHeight, 0),
		controlsHeight: topHeight, statusHeight: topHeight,
		controlsWidth: controlsWidth, statusWidth: statusWidth,
	}
}

func (m Model) renderBenchmarkArea(width, height int, colors palette) string {
	width, height = max(width, 1), max(height, 1)
	if height < 3 {
		lines := []string{fitTableCell("BENCHMARK // TERMINAL TOO SHORT", width)}
		for len(lines) < height {
			lines = append(lines, strings.Repeat(" ", width))
		}
		return colors.dimmed().Render(strings.Join(lines, "\n"))
	}
	if m.benchmarkScopeOpen {
		return m.renderBenchmarkScope(width, height, colors)
	}
	if m.benchmarkDetail != nil {
		return m.renderBenchmarkDetail(width, height, colors)
	}
	layout := layoutBenchmarkArea(width, height)
	controls := m.renderBenchmarkControls(layout.controlsWidth, layout.controlsHeight, colors)
	top := controls
	if layout.statusHeight > 0 {
		status := m.renderBenchmarkStatus(layout.statusWidth, layout.statusHeight, colors)
		if layout.stacked {
			top = lipgloss.JoinVertical(lipgloss.Left, controls, status)
		} else {
			top = lipgloss.JoinHorizontal(lipgloss.Top, controls, " ", status)
		}
	}
	view := top
	if layout.tableHeight >= 3 {
		view = lipgloss.JoinVertical(lipgloss.Left, top, m.renderBenchmarkTable(width, layout.tableHeight, colors))
	}
	if padding := height - lipgloss.Height(view); padding > 0 {
		view += strings.Repeat("\n", padding)
	}
	return view
}

func (m Model) renderBenchmarkDetail(width, height int, colors palette) string {
	width, height = max(width, 1), max(height, 1)
	if m.benchmarkDetail == nil {
		return ""
	}
	innerWidth := max(width-4, 1)
	bodyHeight := max(height-2, 1)
	lines := m.benchmarkDetailLines(innerWidth, colors)
	maximum := max(len(lines)-bodyHeight, 0)
	scroll := min(max(m.benchmarkDetailScroll, 0), maximum)
	end := min(scroll+bodyHeight, len(lines))
	visible := lines[scroll:end]
	for len(visible) < bodyHeight {
		visible = append(visible, strings.Repeat(" ", innerWidth))
	}
	detailState := "RUN DETAIL"
	if m.benchmarkDetailActive {
		detailState = "LIVE RUN DETAIL // IN PROGRESS"
	}
	title := fmt.Sprintf("%s // BENCHMARK-ONLY // LINES %d-%d/%d // ESC BACK", detailState, min(scroll+1, len(lines)), end, len(lines))
	return frameSizedWithTitleAction(width, bodyHeight, ansi.Truncate(title, max(innerWidth-4, 1), ""), m.renderBenchmarkDetailCopy(colors), strings.Join(visible, "\n"), colors.primary, colors)
}

func (m Model) benchmarkDetailLines(width int, colors palette) []string {
	result, ok := m.benchmarkDetailResult()
	if !ok {
		return nil
	}
	model := result.DisplayName
	if model == "" {
		model = result.Model
	}
	if result.ActualModel != "" && result.ActualModel != result.Model {
		model += " → " + result.ActualModel
	}
	outcome := "FAIL"
	outcomeStyle := lipgloss.NewStyle().Bold(true).Foreground(colors.danger)
	if result.Stopped {
		outcome = "STOPPED"
		outcomeStyle = lipgloss.NewStyle().Bold(true).Foreground(colors.warning)
	} else if m.benchmarkDetailActive {
		outcome = "IN PROGRESS"
		outcomeStyle = lipgloss.NewStyle().Bold(true).Foreground(colors.accent)
	} else if result.Correct {
		outcome = "PASS"
		outcomeStyle = lipgloss.NewStyle().Bold(true).Foreground(colors.primary)
	}
	if result.Provider == "digbench" && !m.benchmarkDetailActive && !result.Stopped {
		if result.Correct {
			outcome = "WIN"
		} else if result.Failure != "" {
			outcome = "INCOMPLETE"
		} else {
			outcome = "LOSS"
		}
	}
	lines := []string{
		outcomeStyle.Render(fitTableCell("RESULT // "+outcome, width)),
		colors.label().Render(fitTableCell("MODEL // "+model+" // EFFORT // "+strings.ToUpper(result.Effort), width)),
		colors.label().Render(fitTableCell("TASK // "+result.TaskName+" // TIME // "+formatBenchmarkDuration(result.Duration), width)),
	}
	if result.Provider == "digbench" {
		level := fmt.Sprintf("%d", result.CurrentLevel)
		if result.MaxLevel > 0 {
			level = fmt.Sprintf("%d/%d", result.CurrentLevel, result.MaxLevel)
		}
		lines = append(lines, colors.label().Render(fitTableCell(fmt.Sprintf(
			"DIGBENCH // LEVEL %s // BEATEN %d // STEPS %d // STATUS %s",
			level, result.LevelsBeaten, result.Steps, result.GameStatus,
		), width)))
	}
	usage := "TOKENS // N/A"
	if result.UsageKnown {
		usage = "TOKENS // " + benchmarkUsageDetail(result)
	}
	lines = append(lines, colors.dimmed().Render(fitTableCell(usage, width)))
	cost := "API EQ // N/A"
	if result.CostKnown {
		cost = fmt.Sprintf("API EQ // ~$%.4f", result.CostUSD)
	} else if result.CostIssue != "" {
		cost += " // " + result.CostIssue
	}
	lines = append(lines, colors.dimmed().Render(fitTableCell(cost, width)))
	if result.Failure != "" {
		label := "FAILURE // "
		if result.Stopped {
			label = "STOP ISSUE // "
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(colors.danger).Render(fitTableCell(label+result.Failure, width)))
	}
	lines = append(lines, colors.dimmed().Render(strings.Repeat("─", width)))
	if len(result.Interactions) == 0 {
		lines = append(lines,
			colors.label().Render(fitTableCell("BENCHMARK TRANSCRIPT // UNAVAILABLE", width)),
			colors.dimmed().Render(fitTableCell("This result predates detail capture or was supplied by demo data.", width)),
		)
		return lines
	}
	for index, interaction := range result.Interactions {
		if index > 0 {
			lines = append(lines, colors.dimmed().Render(strings.Repeat("─", width)))
		}
		heading := fmt.Sprintf("%s // +%s", strings.ToUpper(string(interaction.Kind)), formatBenchmarkDuration(interaction.Elapsed))
		headingStyle := colors.label()
		if interaction.Kind == codex.BenchmarkInteractionVerifier && !result.Correct {
			headingStyle = lipgloss.NewStyle().Bold(true).Foreground(colors.danger)
		}
		lines = append(lines, headingStyle.Render(fitTableCell(heading, width)))
		contentStyle := lipgloss.NewStyle().Foreground(colors.primary)
		if interaction.Kind == codex.BenchmarkInteractionPolicy {
			contentStyle = lipgloss.NewStyle().Foreground(colors.warning)
		}
		for _, line := range benchmarkDetailWrap(interaction.Content, width) {
			lines = append(lines, contentStyle.Render(fitTableCell(line, width)))
		}
	}
	return lines
}

func (m Model) benchmarkDetailResult() (codex.BenchmarkResult, bool) {
	if m.benchmarkDetail == nil {
		return codex.BenchmarkResult{}, false
	}
	result := *m.benchmarkDetail
	if m.benchmarkDetailActive {
		if active := m.currentBenchmarkActive(); active != nil && benchmarkRunKey(*active) == benchmarkRunKey(result) {
			result = *active
		}
	}
	return result, true
}

func (m Model) renderBenchmarkDetailCopy(colors palette) string {
	style := lipgloss.NewStyle().Foreground(colors.dim).Background(colors.background)
	if m.hoveredButton == footerButtonBenchmarkCopy {
		style = style.Bold(true).Foreground(colors.accent)
	}
	if m.flashedButton == footerButtonBenchmarkCopy {
		style = style.Bold(true).Foreground(colors.background).Background(colors.primary)
	}
	return style.Render(benchmarkDetailCopyLabel)
}

func (m Model) benchmarkDetailClipboardText() string {
	result, ok := m.benchmarkDetailResult()
	if !ok {
		return ""
	}
	status := "FAIL"
	if result.Stopped {
		status = "STOPPED"
	} else if m.benchmarkDetailActive {
		status = "IN PROGRESS"
	} else if result.Correct {
		status = "PASS"
	}
	if result.Provider == "digbench" && !m.benchmarkDetailActive && !result.Stopped {
		if result.Correct {
			status = "WIN"
		} else if result.Failure != "" {
			status = "INCOMPLETE"
		} else {
			status = "LOSS"
		}
	}
	model := result.DisplayName
	if model == "" {
		model = result.Model
	}

	var output strings.Builder
	output.WriteString("CODEXOMETER BENCHMARK RUN DETAIL\n")
	output.WriteString("TRANSCRIPT: BENCHMARK-ONLY\n")
	fmt.Fprintf(&output, "RESULT: %s\n", status)
	fmt.Fprintf(&output, "MODEL: %s\n", model)
	if result.ActualModel != "" && result.ActualModel != result.Model {
		fmt.Fprintf(&output, "ACTUAL MODEL: %s\n", result.ActualModel)
	}
	fmt.Fprintf(&output, "EFFORT: %s\n", strings.ToUpper(result.Effort))
	fmt.Fprintf(&output, "TASK: %s\n", result.TaskName)
	fmt.Fprintf(&output, "TIME: %s\n", formatBenchmarkDuration(result.Duration))
	if result.Provider == "digbench" {
		level := fmt.Sprintf("%d", result.CurrentLevel)
		if result.MaxLevel > 0 {
			level = fmt.Sprintf("%d/%d", result.CurrentLevel, result.MaxLevel)
		}
		fmt.Fprintf(&output, "DIGBENCH: LEVEL %s // BEATEN %d // STEPS %d // STATUS %s\n",
			level, result.LevelsBeaten, result.Steps, result.GameStatus)
	}
	if result.UsageKnown {
		fmt.Fprintf(&output, "TOKENS: %s\n", benchmarkUsageDetail(result))
	} else {
		output.WriteString("TOKENS: N/A\n")
	}
	if result.CostKnown {
		fmt.Fprintf(&output, "API EQ: ~$%.4f\n", result.CostUSD)
	} else if result.CostIssue != "" {
		fmt.Fprintf(&output, "API EQ: N/A // %s\n", result.CostIssue)
	} else {
		output.WriteString("API EQ: N/A\n")
	}
	if result.Failure != "" {
		label := "FAILURE"
		if result.Stopped {
			label = "STOP ISSUE"
		}
		fmt.Fprintf(&output, "%s: %s\n", label, result.Failure)
	}
	if len(result.Interactions) == 0 {
		output.WriteString("\nBENCHMARK TRANSCRIPT: UNAVAILABLE\n")
		return output.String()
	}
	for _, interaction := range result.Interactions {
		fmt.Fprintf(&output, "\n[%s +%s]\n", strings.ToUpper(string(interaction.Kind)), formatBenchmarkDuration(interaction.Elapsed))
		if interaction.Content == "" {
			output.WriteString("(empty)\n")
			continue
		}
		output.WriteString(interaction.Content)
		if !strings.HasSuffix(interaction.Content, "\n") {
			output.WriteByte('\n')
		}
	}
	return output.String()
}

func benchmarkUsageDetail(result codex.BenchmarkResult) string {
	detail := fmt.Sprintf("TOTAL %d // INPUT %d // CACHED %d // OUTPUT %d // REASONING %d",
		result.Usage.TotalTokens, result.Usage.InputTokens, result.Usage.CachedInputTokens,
		result.Usage.OutputTokens, result.Usage.ReasoningOutputTokens)
	if result.UsageSource != "" {
		detail += " // SOURCE " + string(result.UsageSource)
	}
	return detail
}

func benchmarkDetailWrap(content string, width int) []string {
	width = max(width, 1)
	content = strings.ReplaceAll(content, "\t", "    ")
	if content == "" {
		return []string{"(empty)"}
	}
	wrapped := ansi.Wrap(content, width, " \t")
	lines := strings.Split(wrapped, "\n")
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], width, "")
	}
	return lines
}

func (m Model) benchmarkDetailPageSize() int {
	layout := m.dashboardLayout()
	return max(layout.meterHeight-2, 1)
}

func (m Model) benchmarkDetailMaximumScroll() int {
	if m.benchmarkDetail == nil {
		return 0
	}
	layout := m.dashboardLayout()
	lines := m.benchmarkDetailLines(max(layout.contentWidth-4, 1), paletteFor(m.theme))
	return max(len(lines)-max(layout.meterHeight-2, 1), 0)
}

type benchmarkScopeItemKind int

const (
	benchmarkScopeDone benchmarkScopeItemKind = iota
	benchmarkScopeAllModels
	benchmarkScopeModel
	benchmarkScopeAllEfforts
	benchmarkScopeEffort
	benchmarkScopeAllGames
	benchmarkScopeGame
)

type benchmarkScopeItem struct {
	kind     benchmarkScopeItemKind
	value    string
	label    string
	efforts  []string
	selected bool
}

func (m Model) renderBenchmarkScope(width, height int, colors palette) string {
	width, height = max(width, 1), max(height, 1)
	innerWidth := max(width-4, 1)
	bodyHeight := max(height-2, 1)
	items := m.benchmarkScopeItems()
	selectedEfforts := stringSetUI(m.benchmarkScope.Efforts)
	start := min(max(m.benchmarkScopeScroll, 0), max(len(items)-bodyHeight, 0))
	end := min(start+bodyHeight, len(items))
	lines := make([]string, 0, bodyHeight)
	for index := start; index < end; index++ {
		lines = append(lines, m.renderBenchmarkScopeItem(items[index], index, innerWidth, colors, selectedEfforts))
	}
	for len(lines) < bodyHeight {
		lines = append(lines, strings.Repeat(" ", innerWidth))
	}
	title := fmt.Sprintf("BENCHMARK SCOPE // %d MODELS // %d EFFORTS // %d PAIRS",
		len(m.benchmarkScope.Models), len(m.benchmarkScope.Efforts), m.benchmarkCombinations)
	if m.benchmarkSelectedTaskExternal() {
		title += fmt.Sprintf(" // %d/%d GAMES", len(m.benchmarkScope.Games), len(m.benchmarkPlan.Games))
	}
	title += " // SPACE TOGGLE // ESC DONE"
	return frameSized(width, bodyHeight, ansi.Truncate(title, max(innerWidth-4, 1), ""), strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) renderBenchmarkScopeItem(item benchmarkScopeItem, index, width int, colors palette, selectedEfforts map[string]bool) string {
	label := item.label
	if len(item.efforts) > 0 {
		label += " // " + strings.ToUpper(strings.Join(item.efforts, ", "))
	}
	if index == m.benchmarkScopeCursor || index == m.benchmarkScopeHover {
		style := lipgloss.NewStyle().Bold(true).Foreground(colors.background).Background(colors.accent)
		return style.Render(fitTableCell(label, width))
	}

	if item.kind == benchmarkScopeModel && len(item.efforts) > 0 {
		modelStyle := colors.dimmed()
		if item.selected {
			modelStyle = lipgloss.NewStyle().Foreground(colors.primary)
		}
		line := modelStyle.Render(item.label) + colors.dimmed().Render(" // ")
		for effortIndex, effort := range item.efforts {
			if effortIndex > 0 {
				line += colors.dimmed().Render(", ")
			}
			effortStyle := colors.dimmed()
			if item.selected && selectedEfforts[effort] {
				effortStyle = lipgloss.NewStyle().Foreground(colors.primary)
			}
			line += effortStyle.Render(strings.ToUpper(effort))
		}
		return fitTableCell(line, width)
	}

	style := colors.dimmed()
	if item.selected {
		style = lipgloss.NewStyle().Foreground(colors.primary)
	}
	if item.kind == benchmarkScopeDone || item.kind == benchmarkScopeAllModels || item.kind == benchmarkScopeAllEfforts || item.kind == benchmarkScopeAllGames {
		style = style.Bold(true).Foreground(colors.accent)
	}
	return style.Render(fitTableCell(label, width))
}

func (m Model) benchmarkScopeItems() []benchmarkScopeItem {
	allModels := len(m.benchmarkPlan.Models) > 0 && len(m.benchmarkScope.Models) == len(m.benchmarkPlan.Models)
	allEfforts := len(m.benchmarkPlan.Efforts) > 0 && len(m.benchmarkScope.Efforts) == len(m.benchmarkPlan.Efforts)
	items := []benchmarkScopeItem{{
		kind: benchmarkScopeDone, label: "[ (D) DONE ] // RETURN TO BENCHMARK", selected: true,
	}, {
		kind: benchmarkScopeAllModels, selected: allModels,
		label: scopeCheckLabel(allModels) + " MODELS // " + scopeAllAction(allModels),
	}}
	selectedModels := stringSetUI(m.benchmarkScope.Models)
	for _, model := range m.benchmarkPlan.Models {
		selected := selectedModels[model.Model]
		label := "  " + scopeCheckLabel(selected) + " " + model.DisplayName
		items = append(items, benchmarkScopeItem{
			kind: benchmarkScopeModel, value: model.Model, label: label,
			efforts: append([]string(nil), model.Efforts...), selected: selected,
		})
	}
	items = append(items, benchmarkScopeItem{
		kind: benchmarkScopeAllEfforts, selected: allEfforts,
		label: scopeCheckLabel(allEfforts) + " REASONING LEVELS // " + scopeAllAction(allEfforts),
	})
	selectedEfforts := stringSetUI(m.benchmarkScope.Efforts)
	for _, effort := range m.benchmarkPlan.Efforts {
		selected := selectedEfforts[effort]
		items = append(items, benchmarkScopeItem{
			kind: benchmarkScopeEffort, value: effort, selected: selected,
			label: "  " + scopeCheckLabel(selected) + " " + strings.ToUpper(effort),
		})
	}
	if m.benchmarkSelectedTaskExternal() {
		allGames := len(m.benchmarkPlan.Games) > 0 && len(m.benchmarkScope.Games) == len(m.benchmarkPlan.Games)
		items = append(items, benchmarkScopeItem{
			kind: benchmarkScopeAllGames, selected: allGames,
			label: scopeCheckLabel(allGames) + " DIGBENCH GAMES // " + scopeAllAction(allGames),
		})
		selectedGames := stringSetUI(m.benchmarkScope.Games)
		for _, game := range m.benchmarkPlan.Games {
			selected := selectedGames[game]
			items = append(items, benchmarkScopeItem{
				kind: benchmarkScopeGame, value: game, selected: selected,
				label: "  " + scopeCheckLabel(selected) + " " + strings.ToUpper(game),
			})
		}
	}
	return items
}

func scopeCheckLabel(selected bool) string {
	if selected {
		return "[x]"
	}
	return "[ ]"
}

func scopeAllAction(selected bool) string {
	if selected {
		return "CLEAR ALL"
	}
	return "CHECK ALL"
}

func stringSetUI(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func (m *Model) openBenchmarkScope() {
	if m.benchmarkRunActive() || len(m.benchmarkPlan.Models) == 0 {
		return
	}
	m.benchmarkScopeOpen = true
	m.benchmarkScopeHover = -1
	m.benchmarkScopeCursor = min(max(m.benchmarkScopeCursor, 0), max(len(m.benchmarkScopeItems())-1, 0))
	m.revealBenchmarkScopeCursor()
}

func (m *Model) closeBenchmarkScope() {
	m.benchmarkScopeOpen = false
	m.benchmarkScopeHover = -1
}

func (m *Model) moveBenchmarkScopeCursor(direction int) {
	items := m.benchmarkScopeItems()
	if len(items) == 0 {
		return
	}
	m.benchmarkScopeCursor = min(max(m.benchmarkScopeCursor+direction, 0), len(items)-1)
	m.benchmarkScopeHover = -1
	m.revealBenchmarkScopeCursor()
}

func (m *Model) revealBenchmarkScopeCursor() {
	pageSize := m.benchmarkScopePageSize()
	if m.benchmarkScopeCursor < m.benchmarkScopeScroll {
		m.benchmarkScopeScroll = m.benchmarkScopeCursor
	} else if m.benchmarkScopeCursor >= m.benchmarkScopeScroll+pageSize {
		m.benchmarkScopeScroll = m.benchmarkScopeCursor - pageSize + 1
	}
	m.benchmarkScopeScroll = min(max(m.benchmarkScopeScroll, 0), max(len(m.benchmarkScopeItems())-pageSize, 0))
}

func (m Model) benchmarkScopePageSize() int {
	return max(m.dashboardLayout().meterHeight-2, 1)
}

func (m *Model) toggleBenchmarkScopeCursor() {
	items := m.benchmarkScopeItems()
	if m.benchmarkScopeCursor < 0 || m.benchmarkScopeCursor >= len(items) {
		return
	}
	item := items[m.benchmarkScopeCursor]
	switch item.kind {
	case benchmarkScopeDone:
		m.closeBenchmarkScope()
		return
	case benchmarkScopeAllModels:
		if item.selected {
			m.benchmarkScope.Models = nil
		} else {
			m.benchmarkScope.Models = m.benchmarkPlan.AllScope().Models
		}
	case benchmarkScopeModel:
		m.benchmarkScope.Models = toggleScopeValue(m.benchmarkScope.Models, item.value)
	case benchmarkScopeAllEfforts:
		if item.selected {
			m.benchmarkScope.Efforts = nil
		} else {
			m.benchmarkScope.Efforts = m.benchmarkPlan.AllScope().Efforts
		}
	case benchmarkScopeEffort:
		m.benchmarkScope.Efforts = toggleScopeValue(m.benchmarkScope.Efforts, item.value)
	case benchmarkScopeAllGames:
		if item.selected {
			m.benchmarkScope.Games = nil
		} else {
			m.benchmarkScope.Games = m.benchmarkPlan.AllScope().Games
		}
	case benchmarkScopeGame:
		m.benchmarkScope.Games = toggleScopeValue(m.benchmarkScope.Games, item.value)
	}
	m.benchmarkCombinations = m.benchmarkPlan.CombinationCount(m.benchmarkScope)
	m.benchmarkAllArmed = false
	m.benchmarkSelectedArmed = false
}

func toggleScopeValue(values []string, value string) []string {
	for index, existing := range values {
		if existing == value {
			return append(append([]string(nil), values[:index]...), values[index+1:]...)
		}
	}
	return append(append([]string(nil), values...), value)
}

func (m Model) benchmarkScopeItemAt(x, y int) (int, bool) {
	if m.meterView != viewBenchmark || !m.benchmarkScopeOpen || x < 0 || y < 0 {
		return 0, false
	}
	dashboard := m.dashboardLayout()
	localY := y - dashboard.meterY - 1
	if x < 4 || x >= dashboard.contentWidth || localY < 0 || localY >= m.benchmarkScopePageSize() {
		return 0, false
	}
	index := m.benchmarkScopeScroll + localY
	if index < 0 || index >= len(m.benchmarkScopeItems()) {
		return 0, false
	}
	return index, true
}

func (m Model) renderBenchmarkControls(width, height int, colors palette) string {
	innerWidth := max(width-4, 1)
	lines := make([]string, 0, max(height-2, 0))
	for _, segments := range m.benchmarkVisibleControlLines(innerWidth, height) {
		lines = append(lines, m.renderBenchmarkSegments(segments, innerWidth, colors))
	}
	return frameSized(width, max(height-2, 1), "BENCHMARK CONTROLS", strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) benchmarkVisibleControlLines(width, height int) [][]benchmarkControlSegment {
	lines := m.benchmarkControlLines(width)
	capacity := max(height-2, 0)
	if capacity >= len(lines) {
		return lines
	}
	if capacity == 2 && len(lines) >= 3 {
		if m.benchmarkRunActive() {
			return [][]benchmarkControlSegment{lines[0], lines[2]}
		}
		return lines[:2]
	}
	return lines[:min(len(lines), capacity)]
}

func (m Model) renderBenchmarkSegments(segments []benchmarkControlSegment, width int, colors palette) string {
	parts := make([]string, 0, len(segments))
	used := 0
	for _, segment := range segments {
		style := colors.dimmed()
		if segment.button == footerButtonNone {
			style = lipgloss.NewStyle().Foreground(colors.primary)
		} else if segment.enabled {
			style = lipgloss.NewStyle().Foreground(colors.primary)
			if m.hoveredButton == segment.button {
				style = style.Bold(true).Foreground(colors.accent)
			}
			if m.flashedButton == segment.button {
				style = style.Bold(true).Foreground(colors.background).Background(colors.primary)
			}
		}
		if segment.active {
			style = lipgloss.NewStyle().Bold(true).Foreground(colors.background).Background(colors.primary)
		}
		parts = append(parts, style.Render(segment.text))
		used += lipgloss.Width(segment.text)
	}
	if len(parts) > 1 {
		used += len(parts) - 1
	}
	return strings.Join(parts, " ") + strings.Repeat(" ", max(width-used, 0))
}

func (m Model) benchmarkControlLines(width int) [][]benchmarkControlSegment {
	tasks := m.benchmarkTasks()
	allTasks := m.benchmarkRunAllTasks()
	selected := codex.BenchmarkTask{Name: "NO TASKS"}
	if len(tasks) > 0 {
		selected = tasks[m.benchmarkSelectedTask%len(tasks)]
	}
	running := m.benchmarkRunActive()
	selectorMiddleWidth := 0
	for _, task := range tasks {
		selectorMiddleWidth = max(selectorMiddleWidth, lipgloss.Width("TASK // "+task.Name))
	}
	selectorMiddleWidth = min(selectorMiddleWidth, max(width-8, 1))
	selector := []benchmarkControlSegment{{text: ansi.Truncate(selected.Name, width, ""), enabled: true}}
	if width >= 9 {
		selector = []benchmarkControlSegment{
			{text: "[◀]", button: footerButtonBenchmarkPrevious, enabled: !running},
			{text: fitTableCell("TASK // "+selected.Name, selectorMiddleWidth), enabled: true},
			{text: "[▶]", button: footerButtonBenchmarkNext, enabled: !running},
		}
	}

	selectedLabel := "[ (B) RUN SELECTED ]"
	allTurns := m.benchmarkCombinations * len(allTasks)
	allLabel := fmt.Sprintf("[ (A) RUN ALL // %d ]", allTurns)
	scopeLabel := fmt.Sprintf("[ (S) SCOPE // %d ]", m.benchmarkCombinations)
	stopLabel := "[ (X) STOP ]"
	if m.benchmarkPlanning {
		allLabel = "[ DISCOVERING TURNS… ]"
	}
	if selected.External && m.benchmarkCombinations != 1 {
		selectedLabel = "[ SELECT 1 PAIR IN SCOPE ]"
	} else if selected.External && len(m.benchmarkScope.Games) == 0 {
		selectedLabel = "[ SELECT DIGBENCH GAMES ]"
	} else if selected.External {
		selectedLabel = fmt.Sprintf("[ (B) RUN DIGBENCH // %d ]", len(m.benchmarkScope.Games))
		if m.benchmarkSelectedArmed {
			selectedLabel = fmt.Sprintf("[ CONFIRM // %d REMOTE SESSIONS ]", len(m.benchmarkScope.Games))
		}
	}
	if m.benchmarkAllArmed {
		allLabel = fmt.Sprintf("[ CONFIRM // %d TURNS ]", allTurns)
	}
	if lipgloss.Width(selectedLabel)+lipgloss.Width(allLabel)+1 > width {
		selectedLabel = "[B:RUN]"
		if selected.External {
			selectedLabel = fmt.Sprintf("[B:DIG %d]", len(m.benchmarkScope.Games))
			if m.benchmarkCombinations != 1 || len(m.benchmarkScope.Games) == 0 {
				selectedLabel = "[B:SCOPE]"
			} else if m.benchmarkSelectedArmed {
				selectedLabel = fmt.Sprintf("[B:CONFIRM %d]", len(m.benchmarkScope.Games))
			}
		}
		allLabel = fmt.Sprintf("[A:SUITE %d]", allTurns)
		if m.benchmarkPlanning {
			allLabel = "[A:WAIT]"
		}
		if m.benchmarkAllArmed {
			allLabel = fmt.Sprintf("[A:CONFIRM %d]", allTurns)
		}
	}
	if lipgloss.Width(selectedLabel)+lipgloss.Width(allLabel)+1 > width {
		selectedLabel = "[RUN]"
		if selected.External {
			selectedLabel = fmt.Sprintf("[D:%d]", len(m.benchmarkScope.Games))
			if m.benchmarkCombinations != 1 || len(m.benchmarkScope.Games) == 0 {
				selectedLabel = "[SCOPE]"
			} else if m.benchmarkSelectedArmed {
				selectedLabel = fmt.Sprintf("[D:%d?]", len(m.benchmarkScope.Games))
			}
		}
		allLabel = fmt.Sprintf("[ALL:%d]", allTurns)
		if m.benchmarkPlanning {
			allLabel = "[A:…]"
		}
		if m.benchmarkAllArmed {
			allLabel = fmt.Sprintf("[A:%d?]", allTurns)
		}
	}
	if lipgloss.Width(scopeLabel)+lipgloss.Width(stopLabel)+1 > width {
		scopeLabel = fmt.Sprintf("[S:SCOPE %d]", m.benchmarkCombinations)
		stopLabel = "[X:STOP]"
	}
	if lipgloss.Width(scopeLabel)+lipgloss.Width(stopLabel)+1 > width {
		scopeLabel = fmt.Sprintf("[SCOPE:%d]", m.benchmarkCombinations)
		stopLabel = "[STOP]"
	}
	primary := []benchmarkControlSegment{
		{text: selectedLabel, button: footerButtonBenchmarkSelected, enabled: m.benchmarkCanRunSelected() && len(tasks) > 0},
		{text: allLabel, button: footerButtonBenchmarkAll, enabled: benchmarkRunAllAvailable(running, m.benchmarkCombinations, len(allTasks))},
	}
	secondary := []benchmarkControlSegment{
		{text: scopeLabel, button: footerButtonBenchmarkScope, enabled: !running && len(m.benchmarkPlan.Models) > 0},
		{text: stopLabel, button: footerButtonBenchmarkStop, enabled: m.benchmarkState == benchmarkRunning},
	}
	if running {
		secondary[0], secondary[1] = secondary[1], secondary[0]
	}
	for benchmarkSegmentsWidth(primary) > width && len(primary) > 1 {
		primary = primary[:len(primary)-1]
	}
	for benchmarkSegmentsWidth(secondary) > width && len(secondary) > 1 {
		secondary = secondary[:len(secondary)-1]
	}
	return [][]benchmarkControlSegment{selector, primary, secondary}
}

func benchmarkSegmentsWidth(segments []benchmarkControlSegment) int {
	width := max(len(segments)-1, 0)
	for _, segment := range segments {
		width += lipgloss.Width(segment.text)
	}
	return width
}

func (m Model) benchmarkFilterLine(width int) []benchmarkControlSegment {
	controls := []benchmarkControlSegment{
		{text: "SHOW //", enabled: true},
		{text: "[ ALL ]", button: footerButtonBenchmarkFilterAll, enabled: true, active: m.benchmarkFilter == benchmarkFilterAll},
		{text: "[ PASS ]", button: footerButtonBenchmarkFilterPass, enabled: true, active: m.benchmarkFilter == benchmarkFilterPass},
		{text: "[ FAIL ]", button: footerButtonBenchmarkFilterFail, enabled: true, active: m.benchmarkFilter == benchmarkFilterFail},
		{text: " RANK //", enabled: true},
		{text: "[ COST ]", button: footerButtonBenchmarkRankCost, enabled: true, active: m.benchmarkRankMode == benchmarkRankCost},
		{text: "[ BAL ]", button: footerButtonBenchmarkRankBalanced, enabled: true, active: m.benchmarkRankMode == benchmarkRankBalanced},
		{text: "[ SPEED ]", button: footerButtonBenchmarkRankSpeed, enabled: true, active: m.benchmarkRankMode == benchmarkRankSpeed},
	}
	if benchmarkSegmentsWidth(controls) > width {
		controls = []benchmarkControlSegment{
			{text: "SHOW", enabled: true},
			{text: "[ALL]", button: footerButtonBenchmarkFilterAll, enabled: true, active: m.benchmarkFilter == benchmarkFilterAll},
			{text: "[PASS]", button: footerButtonBenchmarkFilterPass, enabled: true, active: m.benchmarkFilter == benchmarkFilterPass},
			{text: "[FAIL]", button: footerButtonBenchmarkFilterFail, enabled: true, active: m.benchmarkFilter == benchmarkFilterFail},
			{text: "RANK", enabled: true},
			{text: "[C]", button: footerButtonBenchmarkRankCost, enabled: true, active: m.benchmarkRankMode == benchmarkRankCost},
			{text: "[B]", button: footerButtonBenchmarkRankBalanced, enabled: true, active: m.benchmarkRankMode == benchmarkRankBalanced},
			{text: "[S]", button: footerButtonBenchmarkRankSpeed, enabled: true, active: m.benchmarkRankMode == benchmarkRankSpeed},
		}
	}
	if benchmarkSegmentsWidth(controls) > width {
		controls = []benchmarkControlSegment{
			{text: "[A]", button: footerButtonBenchmarkFilterAll, enabled: true, active: m.benchmarkFilter == benchmarkFilterAll},
			{text: "[P]", button: footerButtonBenchmarkFilterPass, enabled: true, active: m.benchmarkFilter == benchmarkFilterPass},
			{text: "[F]", button: footerButtonBenchmarkFilterFail, enabled: true, active: m.benchmarkFilter == benchmarkFilterFail},
			{text: "[C]", button: footerButtonBenchmarkRankCost, enabled: true, active: m.benchmarkRankMode == benchmarkRankCost},
			{text: "[B]", button: footerButtonBenchmarkRankBalanced, enabled: true, active: m.benchmarkRankMode == benchmarkRankBalanced},
			{text: "[S]", button: footerButtonBenchmarkRankSpeed, enabled: true, active: m.benchmarkRankMode == benchmarkRankSpeed},
		}
	}
	for benchmarkSegmentsWidth(controls) > width && len(controls) > 0 {
		controls = controls[:len(controls)-1]
	}
	return controls
}

func benchmarkRunAllAvailable(running bool, combinations, taskCount int) bool {
	return !running && combinations > 0 && taskCount > 0
}

func (m Model) renderBenchmarkStatus(width, height int, colors palette) string {
	state := "READY"
	detail := fmt.Sprintf("USES QUOTA // %d SELECTED MODEL + EFFORT PAIRS", m.benchmarkCombinations)
	if m.benchmarkPlanning {
		detail = "DISCOVERING VISIBLE MODELS + EFFORTS"
	}
	color := colors.primary
	switch m.benchmarkState {
	case benchmarkRunning:
		state = fmt.Sprintf("RUNNING %d/%d", m.benchmarkCompleted, m.benchmarkTotal)
		if m.benchmarkTotal == 0 {
			state = "DISCOVERING MODELS"
		}
		detail = strings.TrimSpace(m.benchmarkCurrentTask + " // " + m.benchmarkCurrentModel + " // " + strings.ToUpper(m.benchmarkCurrentEffort))
		if detail == "//" || detail == "" {
			detail = "QUERYING LOCAL CODEX APP-SERVER"
		}
		color = colors.accent
	case benchmarkStopping:
		state = fmt.Sprintf("STOPPING // %d/%d COMPLETE", m.benchmarkCompleted, m.benchmarkTotal)
		detail = "INTERRUPTING CURRENT BENCHMARK TRIAL"
		color = colors.warning
	case benchmarkStopped:
		state = fmt.Sprintf("STOPPED // %d/%d COMPLETE", m.benchmarkCompleted, m.benchmarkTotal)
		detail = "COMPLETED RESULTS RETAINED // PRESS B TO RUN AGAIN"
		if issue := latestBenchmarkStopIssue(m.benchmarkResults); issue != "" {
			detail = "STOP ISSUE // " + issue
		}
		color = colors.warning
	case benchmarkFinished:
		passed := benchmarkPassCount(m.benchmarkResults)
		state = fmt.Sprintf("COMPLETE // %d/%d PASS", passed, len(m.benchmarkResults))
		detail = "PRESS B OR CLICK TO RUN AGAIN"
		if failure := latestBenchmarkFailure(m.benchmarkResults); failure != "" {
			detail = "LAST FAIL // " + failure
		} else if issue := latestBenchmarkMeasurementIssue(m.benchmarkResults); issue != "" {
			detail = "LAST N/A // " + issue
		}
		if m.benchmarkError != "" {
			state = "BENCHMARK FAULT"
			detail = m.benchmarkError
			color = colors.danger
		}
	}
	if m.benchmarkError != "" && !m.benchmarkRunActive() {
		state = "BENCHMARK FAULT"
		detail = m.benchmarkError
		color = colors.danger
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(color).Render(ansi.Truncate(state, max(width-4, 1), "")),
		colors.dimmed().Render(ansi.Truncate(detail, max(width-4, 1), "")),
	}
	if height >= 5 {
		boundary := "HERMETIC STARLARK // BOUNDED STEPS PER CASE"
		if m.benchmarkSelectedTaskExternal() || strings.HasPrefix(m.benchmarkCurrentTask, "DIGBENCH") {
			boundary = "EXTERNAL DIGBENCH // PERSISTED REMOTE SESSION // RANDOM SEED"
		}
		lines = append(lines, colors.dimmed().Render(ansi.Truncate(boundary, max(width-4, 1), "")))
	}
	lines = lines[:min(len(lines), max(height-2, 0))]
	return frameSized(width, max(height-2, 1), "ALGORITHM TRIAL", strings.Join(lines, "\n"), color, colors)
}

func (m Model) benchmarkSelectedTaskExternal() bool {
	tasks := m.benchmarkTasks()
	return len(tasks) > 0 && tasks[m.benchmarkSelectedTask%len(tasks)].External
}

func latestBenchmarkFailure(results []codex.BenchmarkResult) string {
	for index := len(results) - 1; index >= 0; index-- {
		if !results[index].Correct && results[index].Failure != "" {
			return results[index].Failure
		}
	}
	return ""
}

func latestBenchmarkStopIssue(results []codex.BenchmarkResult) string {
	for index := len(results) - 1; index >= 0; index-- {
		if results[index].Stopped && results[index].Failure != "" {
			return results[index].Failure
		}
	}
	return ""
}

func latestBenchmarkMeasurementIssue(results []codex.BenchmarkResult) string {
	for index := len(results) - 1; index >= 0; index-- {
		if results[index].UsageIssue != "" {
			return results[index].UsageIssue
		}
		if results[index].CostIssue != "" {
			return results[index].CostIssue
		}
	}
	return ""
}

func (m Model) renderBenchmarkTable(width, height int, colors palette) string {
	innerWidth := max(width-4, 1)
	bodyHeight := max(height-2, 1)
	visibleResults := filterBenchmarkResults(m.benchmarkResults, m.benchmarkFilter)
	allTableResults := m.benchmarkTableResults()
	columns := benchmarkTableColumns(innerWidth, allTableResults, m.activeBenchmarkKey())
	rows, _, clipped := m.benchmarkVisibleRows(width, height)
	lines := []string{m.renderBenchmarkSegments(m.benchmarkFilterLine(innerWidth), innerWidth, colors)}
	if bodyHeight > 1 {
		lines = append(lines, m.renderBenchmarkHeader(columns, colors))
	}
	if bodyHeight > 2 {
		lines = append(lines, colors.dimmed().Render(strings.Repeat("─", innerWidth)))
	}
	if clipped {
		ordered := m.orderedBenchmarkResults()
		start, end, _ := benchmarkVisibleResultRange(len(ordered), max(bodyHeight-len(lines), 0), m.benchmarkScroll)
		lines = append(lines, colors.dimmed().Render(fitTableCell(
			fmt.Sprintf("ROWS %d-%d/%d // ↑ ↓ SELECT // ENTER OR CLICK DETAILS", start+1, end, len(ordered)), innerWidth,
		)))
	}
	for _, row := range rows {
		style := colors.dimmed()
		if row.active {
			style = lipgloss.NewStyle().Bold(true).Foreground(colors.accent)
		} else if row.stopped {
			style = lipgloss.NewStyle().Bold(true).Foreground(colors.warning)
		} else if row.pass {
			style = lipgloss.NewStyle().Foreground(colors.primary)
		} else {
			style = lipgloss.NewStyle().Foreground(colors.danger)
		}
		key := benchmarkRunKey(row.result)
		if m.benchmarkSelectedRun == key || (m.benchmarkRunHovered && m.benchmarkHoveredRun == key) {
			style = style.Bold(true).Foreground(colors.background).Background(colors.accent)
		}
		lines = append(lines, style.Render(row.text))
	}
	if len(visibleResults) == 0 && m.benchmarkActive == nil && len(lines) < bodyHeight {
		message := "RUN SELECTED OR RUN ALL TO BEGIN // THIS CONSUMES CODEX QUOTA"
		if m.benchmarkRunActive() {
			message = "WAITING FOR FIRST RESULT"
		} else if len(m.benchmarkResults) > 0 {
			message = "NO RESULTS MATCH THE ACTIVE FILTER"
		}
		lines = append(lines, colors.dimmed().Render(fitTableCell(message, innerWidth)))
	}
	for len(lines) < bodyHeight {
		lines = append(lines, strings.Repeat(" ", innerWidth))
	}
	if len(lines) > bodyHeight {
		lines = lines[:bodyHeight]
	}
	title := "RESULT MATRIX // STANDARD API-EQUIVALENT USD"
	return frameSized(width, max(height-2, 1), ansi.Truncate(title, max(innerWidth-4, 1), ""), strings.Join(lines, "\n"), colors.primary, colors)
}

type benchmarkTableRow struct {
	text    string
	pass    bool
	active  bool
	stopped bool
	result  codex.BenchmarkResult
}

func (m Model) orderedBenchmarkResults() []codex.BenchmarkResult {
	visible := filterBenchmarkResults(m.benchmarkResults, m.benchmarkFilter)
	rankings := benchmarkRankings(m.benchmarkResults, m.benchmarkRankMode)
	ordered := sortedBenchmarkResults(visible, m.benchmarkSort, m.benchmarkSortDescending, rankings)
	if active := m.currentBenchmarkActive(); active != nil {
		ordered = append(ordered, *active)
	}
	return ordered
}

func (m Model) benchmarkTableResults() []codex.BenchmarkResult {
	results := slices.Clone(m.benchmarkResults)
	if active := m.currentBenchmarkActive(); active != nil {
		results = append(results, *active)
	}
	return results
}

func (m Model) currentBenchmarkActive() *codex.BenchmarkResult {
	if m.benchmarkActive == nil {
		return nil
	}
	active := *m.benchmarkActive
	if !m.benchmarkActiveSince.IsZero() {
		active.Duration = max(time.Since(m.benchmarkActiveSince), 0)
	}
	return &active
}

func (m Model) activeBenchmarkKey() string {
	if m.benchmarkActive == nil {
		return ""
	}
	return benchmarkRunKey(*m.benchmarkActive)
}

func benchmarkVisibleResultRange(total, available, scroll int) (start, end int, banner bool) {
	available = max(available, 0)
	if total > available && available >= 2 {
		pageSize := available - 1
		maximumScroll := max(total-pageSize, 0)
		scroll = min(max(scroll, 0), maximumScroll)
		end = total - scroll
		start = max(end-pageSize, 0)
		return start, end, true
	}
	end = total
	start = max(end-available, 0)
	return start, end, false
}

// benchmarkVisibleRows is shared by rendering and hit testing so only rows
// actually drawn on screen can open a detail view.
func (m Model) benchmarkVisibleRows(width, height int) (rows []benchmarkTableRow, firstBodyLine int, banner bool) {
	innerWidth := max(width-4, 1)
	bodyHeight := max(height-2, 1)
	firstBodyLine = 1
	if bodyHeight > 1 {
		firstBodyLine++
	}
	if bodyHeight > 2 {
		firstBodyLine++
	}
	ordered := m.orderedBenchmarkResults()
	start, end, banner := benchmarkVisibleResultRange(len(ordered), max(bodyHeight-firstBodyLine, 0), m.benchmarkScroll)
	if banner {
		firstBodyLine++
	}
	rankings := benchmarkRankings(m.benchmarkResults, m.benchmarkRankMode)
	activeKey := m.activeBenchmarkKey()
	columns := benchmarkTableColumns(innerWidth, m.benchmarkTableResults(), activeKey)
	return benchmarkTableRows(columns, ordered[start:end], rankings, activeKey), firstBodyLine, banner
}

func benchmarkRunKey(result codex.BenchmarkResult) string {
	model := result.Model
	if model == "" {
		model = result.DisplayName
	}
	task := string(result.TaskID)
	if task == "" {
		task = result.TaskName
	}
	return strings.ToLower(model) + "\x00" + strings.ToLower(result.Effort) + "\x00" + strings.ToLower(task)
}

func benchmarkTableColumns(width int, results []codex.BenchmarkResult, activeKeys ...string) []benchmarkColumn {
	titles := []string{"RANK", "MODEL", "EFFORT", "TASK", "RESULT", "TIME", "TOKENS", "API EQ"}
	sorts := []benchmarkSortColumn{benchmarkSortRank, benchmarkSortModel, benchmarkSortEffort, benchmarkSortTask, benchmarkSortResult, benchmarkSortTime, benchmarkSortTokens, benchmarkSortCost}
	widths := make([]int, len(titles))
	idealWidths := make([]int, len(titles))
	for index, title := range titles {
		// Reserve brackets and a sort arrow even before this column is active.
		widths[index] = lipgloss.Width(title) + 3
		idealWidths[index] = widths[index]
	}
	combinations := make(map[string]struct{})
	for _, result := range results {
		combinations[benchmarkCombinationKey(result)] = struct{}{}
		active := len(activeKeys) > 0 && activeKeys[0] != "" && activeKeys[0] == benchmarkRunKey(result)
		for index, value := range benchmarkResultValues(result, nil, active) {
			idealWidths[index] = max(idealWidths[index], lipgloss.Width(value))
		}
	}
	if len(combinations) > 0 {
		idealWidths[0] = max(idealWidths[0], lipgloss.Width(fmt.Sprintf("#%d", len(combinations))))
	}

	separatorWidth := len(widths) - 1
	cellBudget := max(width-separatorWidth, len(widths))
	for sumInts(widths) > cellBudget {
		widest := -1
		for index, columnWidth := range widths {
			if columnWidth > 1 && (widest < 0 || columnWidth > widths[widest]) {
				widest = index
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
	}
	for sumInts(widths) < cellBudget {
		grewTowardContent := false
		for index := range widths {
			if sumInts(widths) >= cellBudget {
				break
			}
			if widths[index] < idealWidths[index] {
				widths[index]++
				grewTowardContent = true
			}
		}
		if !grewTowardContent {
			break
		}
	}
	for index := 0; sumInts(widths) < cellBudget; index++ {
		widths[index%len(widths)]++
	}

	columns := make([]benchmarkColumn, 0, len(widths))
	x := 0
	for index := range widths {
		columns = append(columns, benchmarkColumn{sort: sorts[index], title: titles[index], x: x, width: widths[index]})
		x += widths[index] + 1
	}
	return columns
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func formatBenchmarkColumns(columns []benchmarkColumn, values ...string) string {
	parts := make([]string, 0, len(columns))
	for index, column := range columns {
		value := ""
		if index < len(values) {
			value = values[index]
		}
		parts = append(parts, fitTableCell(value, column.width))
	}
	return strings.Join(parts, " ")
}

func (m Model) renderBenchmarkHeader(columns []benchmarkColumn, colors palette) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		label := column.title
		if m.benchmarkSort == column.sort {
			if m.benchmarkSortDescending {
				label += "▼"
			} else {
				label += "▲"
			}
		}
		label = "[" + label + "]"
		style := lipgloss.NewStyle().Foreground(colors.primary).Background(colors.background)
		if m.benchmarkSortHovered && m.benchmarkHoveredSort == column.sort {
			style = style.Bold(true).Foreground(colors.accent)
		}
		if m.benchmarkSort == column.sort {
			style = style.Bold(true).Foreground(colors.background).Background(colors.primary)
		}
		parts = append(parts, style.Render(fitTableCell(label, column.width)))
	}
	return strings.Join(parts, colors.dimmed().Render(" "))
}

func benchmarkTableRows(columns []benchmarkColumn, results []codex.BenchmarkResult, rankings map[string]int, activeKeys ...string) []benchmarkTableRow {
	rows := make([]benchmarkTableRow, 0, len(results))
	for _, result := range results {
		active := len(activeKeys) > 0 && activeKeys[0] != "" && activeKeys[0] == benchmarkRunKey(result)
		rows = append(rows, benchmarkTableRow{
			text:    formatBenchmarkColumns(columns, benchmarkResultValues(result, rankings, active)...),
			pass:    result.Correct,
			active:  active,
			stopped: result.Stopped,
			result:  result,
		})
	}
	return rows
}

func benchmarkResultValues(result codex.BenchmarkResult, rankings map[string]int, active ...bool) []string {
	inProgress := len(active) > 0 && active[0]
	outcome := "FAIL"
	if inProgress {
		outcome = "IN PROGRESS"
	} else if result.Stopped {
		outcome = "STOPPED"
	} else if result.Correct {
		outcome = "PASS"
	}
	if result.Provider == "digbench" && !inProgress && !result.Stopped {
		if result.Correct {
			outcome = "WIN"
		} else if result.Failure != "" {
			outcome = "INCOMPLETE"
		} else {
			outcome = "LOSS"
		}
	}
	cost := "N/A"
	if result.CostKnown {
		cost = fmt.Sprintf("~$%.4f", result.CostUSD)
	}
	tokens := "N/A"
	if result.UsageKnown {
		tokens = formatCompactTokens(result.Usage.TotalTokens)
	}
	model := result.DisplayName
	if result.ActualModel != "" && result.ActualModel != result.Model {
		model += "→" + result.ActualModel
	}
	rank := "—"
	if value := rankings[benchmarkCombinationKey(result)]; result.Provider == "" && !inProgress && !result.Stopped && value > 0 {
		rank = fmt.Sprintf("#%d", value)
	}
	return []string{
		rank,
		model,
		strings.ToUpper(result.Effort),
		result.TaskName,
		outcome,
		formatBenchmarkDuration(result.Duration),
		tokens,
		cost,
	}
}

func sortedBenchmarkResults(results []codex.BenchmarkResult, column benchmarkSortColumn, descending bool, suppliedRankings ...map[string]int) []codex.BenchmarkResult {
	ordered := slices.Clone(results)
	if column == benchmarkSortNone {
		return ordered
	}
	var rankings map[string]int
	if column == benchmarkSortRank {
		if len(suppliedRankings) > 0 {
			rankings = suppliedRankings[0]
		} else {
			rankings = benchmarkRankings(results)
		}
	}
	sort.SliceStable(ordered, func(left, right int) bool {
		if column == benchmarkSortRank {
			comparison := compareInt(rankings[benchmarkCombinationKey(ordered[left])], rankings[benchmarkCombinationKey(ordered[right])])
			if descending {
				return comparison > 0
			}
			return comparison < 0
		}
		if column == benchmarkSortCost && ordered[left].CostKnown != ordered[right].CostKnown {
			return ordered[left].CostKnown
		}
		if column == benchmarkSortTokens && ordered[left].UsageKnown != ordered[right].UsageKnown {
			return ordered[left].UsageKnown
		}
		comparison := compareBenchmarkResults(ordered[left], ordered[right], column)
		if descending {
			return comparison > 0
		}
		return comparison < 0
	})
	return ordered
}

type benchmarkRankSummary struct {
	key           string
	passes        int
	failures      int
	costComplete  bool
	cost          float64
	duration      time.Duration
	costRank      int
	timeRank      int
	weightedScore int
}

func benchmarkCombinationKey(result codex.BenchmarkResult) string {
	model := result.Model
	if model == "" {
		model = result.DisplayName
	}
	return strings.ToLower(model) + "\x00" + strings.ToLower(result.Effort)
}

// benchmarkRankings compares model/effort combinations across all completed
// rows currently in the matrix. Correctness dominates a weighted blend of the
// independent cost and elapsed-time ranks.
func benchmarkRankings(results []codex.BenchmarkResult, modes ...benchmarkRankMode) map[string]int {
	mode := benchmarkRankBalanced
	if len(modes) > 0 {
		mode = modes[0]
	}
	byKey := make(map[string]*benchmarkRankSummary)
	for _, result := range results {
		if result.Stopped {
			continue
		}
		key := benchmarkCombinationKey(result)
		summary := byKey[key]
		if summary == nil {
			summary = &benchmarkRankSummary{key: key, costComplete: true}
			byKey[key] = summary
		}
		if result.Correct {
			summary.passes++
		} else {
			summary.failures++
		}
		duration := max(result.Duration, time.Duration(0))
		if duration > time.Duration(math.MaxInt64)-summary.duration {
			summary.duration = time.Duration(math.MaxInt64)
		} else {
			summary.duration += duration
		}
		if result.CostKnown && result.CostUSD >= 0 && !math.IsNaN(result.CostUSD) && !math.IsInf(result.CostUSD, 0) && !math.IsInf(summary.cost+result.CostUSD, 0) {
			summary.cost += result.CostUSD
		} else {
			summary.costComplete = false
		}
	}
	summaries := make([]benchmarkRankSummary, 0, len(byKey))
	for _, summary := range byKey {
		summaries = append(summaries, *summary)
	}
	costWeight, timeWeight := benchmarkRankWeights(mode)
	for _, tier := range benchmarkCorrectnessTiers(summaries) {
		costRanks := benchmarkCostRanks(tier)
		timeRanks := benchmarkTimeRanks(tier)
		for index := range summaries {
			if summaries[index].passes != tier[0].passes || summaries[index].failures != tier[0].failures {
				continue
			}
			summaries[index].costRank = costRanks[summaries[index].key]
			summaries[index].timeRank = timeRanks[summaries[index].key]
			summaries[index].weightedScore = summaries[index].costRank*costWeight + summaries[index].timeRank*timeWeight
		}
	}
	sort.Slice(summaries, func(left, right int) bool {
		if comparison := compareBenchmarkRankSummaries(summaries[left], summaries[right]); comparison != 0 {
			return comparison < 0
		}
		return summaries[left].key < summaries[right].key
	})
	rankings := make(map[string]int, len(summaries))
	rank := 0
	for index, summary := range summaries {
		if index == 0 || compareBenchmarkRankSummaries(summaries[index-1], summary) != 0 {
			rank = index + 1
		}
		rankings[summary.key] = rank
	}
	return rankings
}

type benchmarkCorrectnessTier struct {
	passes   int
	failures int
}

func benchmarkCorrectnessTiers(summaries []benchmarkRankSummary) [][]benchmarkRankSummary {
	byTier := make(map[benchmarkCorrectnessTier][]benchmarkRankSummary)
	for _, summary := range summaries {
		key := benchmarkCorrectnessTier{passes: summary.passes, failures: summary.failures}
		byTier[key] = append(byTier[key], summary)
	}
	tiers := make([][]benchmarkRankSummary, 0, len(byTier))
	for _, tier := range byTier {
		tiers = append(tiers, tier)
	}
	return tiers
}

func benchmarkRankWeights(mode benchmarkRankMode) (cost, elapsed int) {
	switch mode {
	case benchmarkRankCost:
		return 3, 1
	case benchmarkRankSpeed:
		return 1, 3
	default:
		return 1, 1
	}
}

func benchmarkCostRanks(summaries []benchmarkRankSummary) map[string]int {
	ordered := slices.Clone(summaries)
	sort.Slice(ordered, func(left, right int) bool {
		if comparison := compareBenchmarkCosts(ordered[left], ordered[right]); comparison != 0 {
			return comparison < 0
		}
		return ordered[left].key < ordered[right].key
	})
	return benchmarkAxisRanks(ordered, compareBenchmarkCosts)
}

func compareBenchmarkCosts(left, right benchmarkRankSummary) int {
	if left.costComplete != right.costComplete {
		return compareInt(boolInt(right.costComplete), boolInt(left.costComplete))
	}
	if !left.costComplete || left.cost == right.cost {
		return 0
	}
	if left.cost < right.cost {
		return -1
	}
	return 1
}

func benchmarkTimeRanks(summaries []benchmarkRankSummary) map[string]int {
	ordered := slices.Clone(summaries)
	sort.Slice(ordered, func(left, right int) bool {
		if comparison := compareInt64(int64(ordered[left].duration), int64(ordered[right].duration)); comparison != 0 {
			return comparison < 0
		}
		return ordered[left].key < ordered[right].key
	})
	return benchmarkAxisRanks(ordered, func(left, right benchmarkRankSummary) int {
		return compareInt64(int64(left.duration), int64(right.duration))
	})
}

func benchmarkAxisRanks(ordered []benchmarkRankSummary, compare func(benchmarkRankSummary, benchmarkRankSummary) int) map[string]int {
	ranks := make(map[string]int, len(ordered))
	rank := 0
	for index, summary := range ordered {
		if index == 0 || compare(ordered[index-1], summary) != 0 {
			rank = index + 1
		}
		ranks[summary.key] = rank
	}
	return ranks
}

func compareBenchmarkRankSummaries(left, right benchmarkRankSummary) int {
	if left.passes != right.passes {
		return compareInt(right.passes, left.passes)
	}
	if left.failures != right.failures {
		return compareInt(left.failures, right.failures)
	}
	return compareInt(left.weightedScore, right.weightedScore)
}

func compareBenchmarkResults(left, right codex.BenchmarkResult, column benchmarkSortColumn) int {
	switch column {
	case benchmarkSortModel:
		return strings.Compare(strings.ToLower(left.DisplayName), strings.ToLower(right.DisplayName))
	case benchmarkSortEffort:
		return compareInt(benchmarkEffortRank(left.Effort), benchmarkEffortRank(right.Effort))
	case benchmarkSortTask:
		return strings.Compare(strings.ToLower(left.TaskName), strings.ToLower(right.TaskName))
	case benchmarkSortResult:
		return compareInt(boolInt(left.Correct), boolInt(right.Correct))
	case benchmarkSortTime:
		return compareInt64(int64(left.Duration), int64(right.Duration))
	case benchmarkSortTokens:
		return compareInt64(left.Usage.TotalTokens, right.Usage.TotalTokens)
	case benchmarkSortCost:
		if left.CostUSD < right.CostUSD {
			return -1
		}
		if left.CostUSD > right.CostUSD {
			return 1
		}
	}
	return 0
}

func benchmarkEffortRank(effort string) int {
	for index, name := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
		if strings.EqualFold(effort, name) {
			return index
		}
	}
	return 100
}

func compareInt(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareInt64(left, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func filterBenchmarkResults(results []codex.BenchmarkResult, filter benchmarkResultFilter) []codex.BenchmarkResult {
	if filter == benchmarkFilterAll {
		return slices.Clone(results)
	}
	filtered := make([]codex.BenchmarkResult, 0, len(results))
	for _, result := range results {
		if (filter == benchmarkFilterPass && result.Correct) || (filter == benchmarkFilterFail && !result.Correct && !result.Stopped) {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func fitTableCell(value string, width int) string {
	value = ansi.Truncate(value, max(width, 0), "")
	return value + strings.Repeat(" ", max(width-lipgloss.Width(value), 0))
}

func formatBenchmarkDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	if duration < time.Minute {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(duration/time.Minute), int(duration/time.Second)%60)
}

func benchmarkPassCount(results []codex.BenchmarkResult) int {
	passed := 0
	for _, result := range results {
		if result.Correct {
			passed++
		}
	}
	return passed
}

func (m Model) benchmarkButtonAt(x, y int) footerButtonID {
	if m.loading && len(m.snapshot.Meters()) == 0 {
		return footerButtonNone
	}
	if m.benchmarkScopeOpen {
		return footerButtonNone
	}
	if m.benchmarkDetail != nil {
		return m.benchmarkDetailCopyAt(x, y)
	}
	dashboard := m.dashboardLayout()
	layout := layoutBenchmarkArea(dashboard.contentWidth, dashboard.meterHeight)
	localX, localY := x-2, y-dashboard.meterY
	if localX < 0 || localY < 0 || localY >= layout.height || layout.height < 3 {
		return footerButtonNone
	}
	if localX < layout.controlsWidth && localY < layout.controlsHeight {
		for line, segments := range m.benchmarkVisibleControlLines(max(layout.controlsWidth-4, 1), layout.controlsHeight) {
			if localY == line+1 {
				return benchmarkSegmentButtonAt(localX, segments)
			}
		}
	}
	if layout.tableHeight >= 3 && localX < layout.width && localY == layout.topHeight+1 {
		return benchmarkSegmentButtonAt(localX, m.benchmarkFilterLine(max(layout.width-4, 1)))
	}
	return footerButtonNone
}

func (m Model) benchmarkDetailCopyAt(x, y int) footerButtonID {
	if m.meterView != viewBenchmark || m.benchmarkDetail == nil || x < 0 || y < 0 {
		return footerButtonNone
	}
	dashboard := m.dashboardLayout()
	labelWidth := lipgloss.Width(benchmarkDetailCopyLabel)
	if dashboard.contentWidth < labelWidth+8 || y != dashboard.meterY {
		return footerButtonNone
	}
	localX := x - 2
	start := dashboard.contentWidth - labelWidth - 2
	if localX >= start && localX < start+labelWidth {
		return footerButtonBenchmarkCopy
	}
	return footerButtonNone
}

func benchmarkSegmentButtonAt(localX int, segments []benchmarkControlSegment) footerButtonID {
	segmentX := 2
	for _, segment := range segments {
		segmentWidth := lipgloss.Width(segment.text)
		if segment.button != footerButtonNone && segment.enabled && localX >= segmentX && localX < segmentX+segmentWidth {
			return segment.button
		}
		segmentX += segmentWidth + 1
	}
	return footerButtonNone
}

func (m Model) benchmarkHeaderAt(x, y int) (benchmarkSortColumn, bool) {
	if m.meterView != viewBenchmark || m.benchmarkDetail != nil || x < 0 || y < 0 {
		return benchmarkSortNone, false
	}
	dashboard := m.dashboardLayout()
	geometry := layoutBenchmarkArea(dashboard.contentWidth, dashboard.meterHeight)
	if geometry.tableHeight <= 3 {
		return benchmarkSortNone, false
	}
	tableY := dashboard.meterY + geometry.topHeight
	if y != tableY+2 {
		return benchmarkSortNone, false
	}
	localX := x - 4
	for _, column := range benchmarkTableColumns(max(dashboard.contentWidth-4, 1), m.benchmarkTableResults(), m.activeBenchmarkKey()) {
		if localX >= column.x && localX < column.x+column.width {
			return column.sort, true
		}
	}
	return benchmarkSortNone, false
}

func (m Model) benchmarkRunAt(x, y int) (benchmarkTableRow, bool) {
	if m.meterView != viewBenchmark || m.benchmarkDetail != nil || x < 0 || y < 0 {
		return benchmarkTableRow{}, false
	}
	dashboard := m.dashboardLayout()
	geometry := layoutBenchmarkArea(dashboard.contentWidth, dashboard.meterHeight)
	if geometry.tableHeight < 4 {
		return benchmarkTableRow{}, false
	}
	tableY := dashboard.meterY + geometry.topHeight
	if x < 4 || x >= 2+dashboard.contentWidth-2 {
		return benchmarkTableRow{}, false
	}
	rows, firstBodyLine, _ := m.benchmarkVisibleRows(dashboard.contentWidth, geometry.tableHeight)
	index := y - (tableY + 1 + firstBodyLine)
	if index < 0 || index >= len(rows) {
		return benchmarkTableRow{}, false
	}
	return rows[index], true
}

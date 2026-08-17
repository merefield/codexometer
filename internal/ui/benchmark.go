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
		// Drop the decorative spacer before dropping an actionable row.
		return [][]benchmarkControlSegment{lines[0], lines[2]}
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
	selected := codex.BenchmarkTask{Name: "NO TASKS"}
	if len(tasks) > 0 {
		selected = tasks[m.benchmarkSelectedTask%len(tasks)]
	}
	running := m.benchmarkState == benchmarkRunning
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
	allTurns := m.benchmarkCombinations * len(tasks)
	allLabel := fmt.Sprintf("[ (A) RUN ALL // %d ]", allTurns)
	if m.benchmarkPlanning {
		allLabel = "[ DISCOVERING TURNS… ]"
	}
	if m.benchmarkAllArmed {
		allLabel = fmt.Sprintf("[ CONFIRM // %d TURNS ]", allTurns)
	}
	if lipgloss.Width(selectedLabel)+lipgloss.Width(allLabel)+1 > width {
		selectedLabel = "[B:RUN]"
		allLabel = fmt.Sprintf("[A:SUITE %d]", allTurns)
		if m.benchmarkPlanning {
			allLabel = "[A:WAIT]"
		}
		if m.benchmarkAllArmed {
			allLabel = fmt.Sprintf("[A:CONFIRM %d]", allTurns)
		}
	}
	if lipgloss.Width(selectedLabel)+lipgloss.Width(allLabel)+1 > width {
		selectedLabel = "[B]"
		allLabel = fmt.Sprintf("[A:%d]", allTurns)
		if m.benchmarkPlanning {
			allLabel = "[A:…]"
		}
		if m.benchmarkAllArmed {
			allLabel = fmt.Sprintf("[A:%d?]", allTurns)
		}
	}
	run := []benchmarkControlSegment{
		{text: selectedLabel, button: footerButtonBenchmarkSelected, enabled: !running && len(tasks) > 0},
		{text: allLabel, button: footerButtonBenchmarkAll, enabled: benchmarkRunAllAvailable(running, m.benchmarkCombinations, len(tasks))},
	}
	if benchmarkSegmentsWidth(run) > width {
		run = run[:1]
	}
	return [][]benchmarkControlSegment{selector, nil, run}
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
	detail := "USES QUOTA // EVERY VISIBLE MODEL + EFFORT"
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
	if m.benchmarkError != "" && m.benchmarkState != benchmarkRunning {
		state = "BENCHMARK FAULT"
		detail = m.benchmarkError
		color = colors.danger
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(color).Render(ansi.Truncate(state, max(width-4, 1), "")),
		colors.dimmed().Render(ansi.Truncate(detail, max(width-4, 1), "")),
	}
	if height >= 5 {
		lines = append(lines, colors.dimmed().Render(ansi.Truncate("HERMETIC STARLARK // BOUNDED STEPS PER CASE", max(width-4, 1), "")))
	}
	lines = lines[:min(len(lines), max(height-2, 0))]
	return frameSized(width, max(height-2, 1), "ALGORITHM TRIAL", strings.Join(lines, "\n"), color, colors)
}

func latestBenchmarkFailure(results []codex.BenchmarkResult) string {
	for index := len(results) - 1; index >= 0; index-- {
		if !results[index].Correct && results[index].Failure != "" {
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
	rankings := benchmarkRankings(m.benchmarkResults, m.benchmarkRankMode)
	columns := benchmarkTableColumns(innerWidth, m.benchmarkResults)
	rows := benchmarkTableRows(columns, sortedBenchmarkResults(visibleResults, m.benchmarkSort, m.benchmarkSortDescending, rankings), rankings)
	lines := []string{m.renderBenchmarkSegments(m.benchmarkFilterLine(innerWidth), innerWidth, colors)}
	if bodyHeight > 1 {
		lines = append(lines, m.renderBenchmarkHeader(columns, colors))
	}
	if bodyHeight > 2 {
		lines = append(lines, colors.dimmed().Render(strings.Repeat("─", innerWidth)))
	}
	available := max(bodyHeight-len(lines), 0)
	if len(rows) > available && available >= 2 {
		pageSize := available - 1
		maximumScroll := max(len(rows)-pageSize, 0)
		scroll := min(max(m.benchmarkScroll, 0), maximumScroll)
		end := len(rows) - scroll
		start := max(end-pageSize, 0)
		lines = append(lines, colors.dimmed().Render(fitTableCell(
			fmt.Sprintf("ROWS %d-%d/%d // PGUP PGDN // MOUSE WHEEL", start+1, end, len(rows)), innerWidth,
		)))
		rows = rows[start:end]
	} else if len(rows) > available {
		rows = rows[len(rows)-available:]
	}
	for _, row := range rows {
		style := colors.dimmed()
		if row.pass {
			style = lipgloss.NewStyle().Foreground(colors.primary)
		} else {
			style = lipgloss.NewStyle().Foreground(colors.danger)
		}
		lines = append(lines, style.Render(row.text))
	}
	if len(visibleResults) == 0 && len(lines) < bodyHeight {
		message := "RUN SELECTED OR RUN ALL TO BEGIN // THIS CONSUMES CODEX QUOTA"
		if m.benchmarkState == benchmarkRunning {
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
	text string
	pass bool
}

func benchmarkTableColumns(width int, results []codex.BenchmarkResult) []benchmarkColumn {
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
		for index, value := range benchmarkResultValues(result, nil) {
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

func benchmarkTableRows(columns []benchmarkColumn, results []codex.BenchmarkResult, rankings map[string]int) []benchmarkTableRow {
	rows := make([]benchmarkTableRow, 0, len(results))
	for _, result := range results {
		rows = append(rows, benchmarkTableRow{
			text: formatBenchmarkColumns(columns, benchmarkResultValues(result, rankings)...),
			pass: result.Correct,
		})
	}
	return rows
}

func benchmarkResultValues(result codex.BenchmarkResult, rankings map[string]int) []string {
	outcome := "FAIL"
	if result.Correct {
		outcome = "PASS"
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
	if value := rankings[benchmarkCombinationKey(result)]; value > 0 {
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
		if (filter == benchmarkFilterPass && result.Correct) || (filter == benchmarkFilterFail && !result.Correct) {
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
	if m.meterView != viewBenchmark || x < 0 || y < 0 {
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
	for _, column := range benchmarkTableColumns(max(dashboard.contentWidth-4, 1), m.benchmarkResults) {
		if localX >= column.x && localX < column.x+column.width {
			return column.sort, true
		}
	}
	return benchmarkSortNone, false
}

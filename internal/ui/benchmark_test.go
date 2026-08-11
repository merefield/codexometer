package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

type benchmarkStubFetcher struct {
	stubFetcher
	results []codex.BenchmarkResult
}

func (f benchmarkStubFetcher) BenchmarkCombinationCount(context.Context) (int, error) { return 2, nil }

func (f benchmarkStubFetcher) RunBenchmarkSuite(ctx context.Context, tasks []codex.BenchmarkTaskID, emit func(codex.BenchmarkEvent)) {
	emit(codex.BenchmarkEvent{Total: len(f.results), Combinations: 2})
	for index := range f.results {
		select {
		case <-ctx.Done():
			emit(codex.BenchmarkEvent{Done: true, Err: ctx.Err()})
			return
		default:
		}
		result := f.results[index]
		if len(tasks) > 0 && result.TaskID == "" {
			result.TaskID, result.TaskName = tasks[0], "MERGE RANGES"
		}
		emit(codex.BenchmarkEvent{
			Total: len(f.results), Completed: index + 1, Combinations: 2,
			CurrentTaskID: result.TaskID, CurrentTask: result.TaskName,
			CurrentModel: result.DisplayName, CurrentEffort: result.Effort, Result: &result,
		})
	}
	emit(codex.BenchmarkEvent{Total: len(f.results), Completed: len(f.results), Done: true})
}

func TestBenchmarkViewRendersResponsiveResultsTable(t *testing.T) {
	model := Model{
		width: 100, height: 30, meterStyle: styleBenchmark,
		benchmarkState: benchmarkFinished,
		benchmarkResults: []codex.BenchmarkResult{
			{TaskName: "MERGE RANGES", Model: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", Effort: "high", Correct: true, Duration: 12500 * time.Millisecond, Usage: codex.BenchmarkUsage{TotalTokens: 4321}, CostKnown: true, CostUSD: 0.0412},
			{TaskName: "LRU CACHE", Model: "future", DisplayName: "Future Model", Effort: "low", Correct: false, Duration: 2 * time.Minute, Usage: codex.BenchmarkUsage{TotalTokens: 999}, Failure: "wrong result"},
		},
	}
	output := ansi.Strip(model.renderBenchmarkArea(96, 19, paletteFor(themeHacker)))
	for _, want := range []string{"(B) RUN SELECTED", "MERGE RANGES", "SHOW //", "HERMETIC STARLARK", "1/2 PASS", "PASS", "FAIL", "4.3K", "~$0.0412", "N/A"} {
		if !strings.Contains(output, want) {
			t.Fatalf("benchmark view missing %q:\n%s", want, output)
		}
	}
	if got := lipgloss.Width(output); got != 96 {
		t.Fatalf("benchmark width = %d, want 96", got)
	}
	if got := lipgloss.Height(output); got != 19 {
		t.Fatalf("benchmark height = %d, want 19", got)
	}
	controls := ansi.Strip(model.renderBenchmarkControls(58, 5, paletteFor(themeHacker)))
	lines := strings.Split(controls, "\n")
	selectorLine, runLine := -1, -1
	for index, line := range lines {
		if strings.Contains(line, "TASK //") {
			selectorLine = index
		}
		if strings.Contains(line, "(B) RUN SELECTED") {
			runLine = index
		}
	}
	if selectorLine < 0 || runLine != selectorLine+2 || strings.Trim(lines[selectorLine+1], " │") != "" {
		t.Fatalf("controls do not contain a blank row between Task and Run:\n%s", controls)
	}
}

func TestBenchmarkHotkeyRunsSuiteAndCollectsEvents(t *testing.T) {
	result := codex.BenchmarkResult{Model: "model", DisplayName: "Model", Effort: "medium", Correct: true}
	fetcher := benchmarkStubFetcher{
		stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()},
		results:     []codex.BenchmarkResult{result},
	}
	model := New(fetcher, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.meterStyle = styleBenchmark

	updated, command := model.Update(key('b'))
	model = updated.(Model)
	if model.benchmarkState != benchmarkRunning || command == nil || model.flashedButton != footerButtonBenchmarkSelected {
		t.Fatalf("benchmark did not start: state=%d command=%v", model.benchmarkState, command != nil)
	}

	rawCommandMessage := command()
	batch, ok := rawCommandMessage.(tea.BatchMsg)
	if !ok {
		t.Fatalf("hotkey command returned %T, want tea.BatchMsg", rawCommandMessage)
	}
	var message benchmarkEventMsg
	found := false
	for _, child := range batch {
		if candidate, ok := child().(benchmarkEventMsg); ok {
			message, found = candidate, true
			break
		}
	}
	if !found {
		t.Fatal("hotkey batch did not contain benchmark launch command")
	}
	for model.benchmarkState == benchmarkRunning {
		updated, command = model.Update(message)
		model = updated.(Model)
		if model.benchmarkState == benchmarkRunning {
			message = command().(benchmarkEventMsg)
		}
	}
	if len(model.benchmarkResults) != 1 || !model.benchmarkResults[0].Correct || model.benchmarkCompleted != 1 {
		t.Fatalf("benchmark results not collected: %#v", model.benchmarkResults)
	}
}

func TestBenchmarkButtonSupportsHoverAndClick(t *testing.T) {
	model := New(benchmarkStubFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width, model.height = 100, 30
	model.meterStyle = styleBenchmark
	x, y := benchmarkControlCoordinates(t, model, footerButtonBenchmarkSelected)
	mouse := tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || model.hoveredButton != footerButtonBenchmarkSelected {
		t.Fatal("benchmark button hover was not recorded")
	}
	mouse.Action = tea.MouseActionPress
	mouse.Button = tea.MouseButtonLeft
	updated, command = model.Update(mouse)
	model = updated.(Model)
	if command == nil || model.benchmarkState != benchmarkRunning {
		t.Fatal("benchmark button click did not start suite")
	}
}

func TestBenchmarkTableClipsOlderRows(t *testing.T) {
	model := Model{benchmarkState: benchmarkFinished}
	for index := 0; index < 20; index++ {
		model.benchmarkResults = append(model.benchmarkResults, codex.BenchmarkResult{
			Model: "model", DisplayName: "Model", Effort: "low", Correct: index%2 == 0,
		})
	}
	output := ansi.Strip(model.renderBenchmarkTable(70, 8, paletteFor(themeBlueSteel)))
	if !strings.Contains(output, "ROWS 19-20/20") || lipgloss.Height(output) != 8 {
		t.Fatalf("clipped table is invalid:\n%s", output)
	}
	model.benchmarkScroll = 4
	output = ansi.Strip(model.renderBenchmarkTable(70, 8, paletteFor(themeBlueSteel)))
	if !strings.Contains(output, "ROWS 15-16/20") {
		t.Fatalf("scrolled table did not expose older rows:\n%s", output)
	}
}

func TestBenchmarkPageKeysAndMouseWheelScrollResults(t *testing.T) {
	model := Model{width: 90, height: 28, meterStyle: styleBenchmark, benchmarkState: benchmarkFinished}
	for index := 0; index < 30; index++ {
		model.benchmarkResults = append(model.benchmarkResults, codex.BenchmarkResult{DisplayName: "Model", Effort: "low"})
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(Model)
	if command != nil || model.benchmarkScroll == 0 {
		t.Fatalf("Page Up did not scroll: offset=%d command=%v", model.benchmarkScroll, command != nil)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(Model)
	if model.benchmarkScroll != 0 {
		t.Fatalf("Page Down offset = %d, want 0", model.benchmarkScroll)
	}
	updated, _ = model.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	model = updated.(Model)
	if model.benchmarkScroll != 3 {
		t.Fatalf("mouse-wheel offset = %d, want 3", model.benchmarkScroll)
	}
}

func TestBenchmarkHeadingButtonsSortAndReverse(t *testing.T) {
	model := Model{
		width: 100, height: 30, meterStyle: styleBenchmark, benchmarkState: benchmarkFinished,
		benchmarkResults: []codex.BenchmarkResult{
			{DisplayName: "Zulu", Effort: "low", Duration: 2 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 200}},
			{DisplayName: "Alpha", Effort: "high", Correct: true, Duration: time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 100}},
		},
	}
	dashboard := model.dashboardLayout()
	geometry := layoutBenchmarkArea(dashboard.contentWidth, dashboard.meterHeight)
	headerX, headerY := renderedTextCoordinates(t, model, "[MODEL]")
	mouse := tea.MouseMsg{
		X:      headerX,
		Y:      headerY,
		Action: tea.MouseActionMotion,
	}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || !model.benchmarkSortHovered || model.benchmarkHoveredSort != benchmarkSortModel {
		t.Fatal("model heading did not highlight on hover")
	}
	mouse.Button, mouse.Action = tea.MouseButtonLeft, tea.MouseActionPress
	updated, command = model.Update(mouse)
	model = updated.(Model)
	if command != nil || model.benchmarkSort != benchmarkSortModel || model.benchmarkSortDescending {
		t.Fatalf("first heading click did not select ascending model sort: %#v", model)
	}
	ordered := sortedBenchmarkResults(model.benchmarkResults, model.benchmarkSort, model.benchmarkSortDescending)
	if ordered[0].DisplayName != "Alpha" {
		t.Fatalf("ascending model order = %q first, want Alpha", ordered[0].DisplayName)
	}
	updated, _ = model.Update(mouse)
	model = updated.(Model)
	ordered = sortedBenchmarkResults(model.benchmarkResults, model.benchmarkSort, model.benchmarkSortDescending)
	if !model.benchmarkSortDescending || ordered[0].DisplayName != "Zulu" {
		t.Fatalf("second heading click did not reverse model sort: descending=%v first=%q", model.benchmarkSortDescending, ordered[0].DisplayName)
	}
	view := ansi.Strip(model.renderBenchmarkTable(dashboard.contentWidth, geometry.tableHeight, paletteFor(themeHacker)))
	if !strings.Contains(view, "[MODEL▼]") || !strings.Contains(view, "[EFFORT]") {
		t.Fatalf("heading buttons or sort direction missing:\n%s", view)
	}
}

func TestBenchmarkRenderedClickSurfacesMatchHitTestingAcrossSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{
		{40, 16}, {40, 24}, {45, 24}, {60, 24}, {80, 24}, {100, 30}, {160, 45},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			model := Model{
				snapshot: codex.DemoSnapshot(), width: size.width, height: size.height,
				meterStyle: styleBenchmark, benchmarkCombinations: 3,
			}
			dashboard := model.dashboardLayout()
			geometry := layoutBenchmarkArea(dashboard.contentWidth, dashboard.meterHeight)
			for _, segments := range model.benchmarkControlLines(max(geometry.controlsWidth-4, 1)) {
				for _, segment := range segments {
					if segment.button == footerButtonNone || !segment.enabled {
						continue
					}
					x, y := renderedTextStart(t, model, segment.text)
					for offset := 0; offset < lipgloss.Width(segment.text); offset++ {
						if got := model.benchmarkButtonAt(x+offset, y); got != segment.button {
							t.Errorf("rendered %q cell %d hit button %d, want %d", segment.text, offset, got, segment.button)
						}
					}
				}
			}
			for _, segment := range model.benchmarkFilterLine(max(dashboard.contentWidth-4, 1)) {
				if segment.button == footerButtonNone || !segment.enabled {
					continue
				}
				x, y := renderedTextStart(t, model, segment.text)
				for offset := 0; offset < lipgloss.Width(segment.text); offset++ {
					if got := model.benchmarkButtonAt(x+offset, y); got != segment.button {
						t.Errorf("rendered matrix filter %q cell %d hit button %d, want %d", segment.text, offset, got, segment.button)
					}
				}
			}

			tableTitleX, tableTitleY := renderedTextStart(t, model, "RESULT MATRIX")
			_ = tableTitleX
			headerY := tableTitleY + 2
			if geometry.tableHeight <= 3 {
				if got, ok := model.benchmarkHeaderAt(4, headerY); ok || got != benchmarkSortNone {
					t.Errorf("non-rendered compact heading exposed a click surface: (%d,%v)", got, ok)
				}
				return
			}
			columns := benchmarkTableColumns(max(dashboard.contentWidth-4, 1), model.benchmarkResults)
			for _, column := range columns {
				for offset := 0; offset < column.width; offset++ {
					x := 4 + column.x + offset
					if got, ok := model.benchmarkHeaderAt(x, headerY); !ok || got != column.sort {
						t.Errorf("rendered heading %q cell %d at %d,%d hit (%d,%v)", column.title, offset, x, headerY, got, ok)
					}
				}
			}
		})
	}
}

func TestBenchmarkSortUsesReasoningOrderAndMetrics(t *testing.T) {
	results := []codex.BenchmarkResult{
		{DisplayName: "B", Effort: "xhigh", TaskName: "SHORTEST PATH", Duration: 3 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 300}, CostKnown: true, CostUSD: 0.3},
		{DisplayName: "A", Effort: "low", TaskName: "LRU CACHE", Correct: true, Duration: time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 100}, CostKnown: true, CostUSD: 0.1},
		{DisplayName: "C", Effort: "medium", TaskName: "MERGE RANGES", Duration: 2 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 200}},
	}
	checks := []struct {
		column benchmarkSortColumn
		want   string
	}{
		{benchmarkSortEffort, "A"},
		{benchmarkSortTask, "A"},
		{benchmarkSortResult, "B"},
		{benchmarkSortTime, "A"},
		{benchmarkSortTokens, "A"},
		{benchmarkSortCost, "A"},
	}
	for _, check := range checks {
		ordered := sortedBenchmarkResults(results, check.column, false)
		if ordered[0].DisplayName != check.want {
			t.Errorf("column %d sorted %q first, want %q", check.column, ordered[0].DisplayName, check.want)
		}
	}
	ordered := sortedBenchmarkResults(results, benchmarkSortCost, true)
	if !ordered[0].CostKnown || ordered[len(ordered)-1].CostKnown {
		t.Fatal("descending cost sort did not leave N/A values last")
	}
}

func TestBenchmarkColumnsRespondToHeadingsValuesAndAvailableWidth(t *testing.T) {
	results := []codex.BenchmarkResult{{
		Model: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", Effort: "ultra", TaskName: "SHORTEST PATH",
		Correct: true, Duration: 12*time.Minute + 34*time.Second,
		Usage: codex.BenchmarkUsage{TotalTokens: 12_345_678}, CostKnown: true, CostUSD: 12345.6789,
	}}
	columns := benchmarkTableColumns(96, results)
	if len(columns) != 7 {
		t.Fatalf("column count = %d, want 7", len(columns))
	}
	used := len(columns) - 1
	for _, column := range columns {
		used += column.width
		minimumHeadingWidth := lipgloss.Width(column.title) + 3
		if column.width < minimumHeadingWidth {
			t.Errorf("%s width = %d, want at least %d for brackets and sort arrow", column.title, column.width, minimumHeadingWidth)
		}
	}
	if used != 96 {
		t.Fatalf("responsive columns use %d cells, want 96", used)
	}
	if columns[0].width > 24 {
		t.Fatalf("model column consumed %d cells despite short model values", columns[0].width)
	}
	values := benchmarkResultValues(results[0])
	for index, value := range values {
		if lipgloss.Width(value) > columns[index].width {
			t.Errorf("column %s width %d truncates value %q despite ample space", columns[index].title, columns[index].width, value)
		}
	}

	model := Model{benchmarkResults: results, benchmarkSort: benchmarkSortResult}
	header := ansi.Strip(model.renderBenchmarkHeader(columns, paletteFor(themeHacker)))
	if !strings.Contains(header, "[RESULT▲]") || !strings.Contains(header, "[MODEL]") {
		t.Fatalf("full headings were truncated despite ample space: %q", header)
	}

	narrow := benchmarkTableColumns(42, results)
	used = len(narrow) - 1
	for _, column := range narrow {
		used += column.width
	}
	if used != 42 {
		t.Fatalf("narrow columns use %d cells, want 42", used)
	}
}

func TestBenchmarkTaskSelectorRunAllGuardAndExactTurnCount(t *testing.T) {
	model := New(benchmarkStubFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width, model.height = 100, 30
	model.meterStyle = styleBenchmark
	model.benchmarkCombinations = 33

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if model.benchmarkSelectedTask != 1 {
		t.Fatalf("right arrow selected task %d, want 1", model.benchmarkSelectedTask)
	}
	controls := ansi.Strip(model.renderBenchmarkControls(60, 8, paletteFor(themeHacker)))
	if !strings.Contains(controls, "LRU CACHE") || !strings.Contains(controls, "132") {
		t.Fatalf("selector or exact all-turn count missing:\n%s", controls)
	}

	updated, command := model.Update(key('a'))
	model = updated.(Model)
	if !model.benchmarkAllArmed || model.benchmarkState == benchmarkRunning || command == nil {
		t.Fatal("first Run All press did not arm confirmation without running")
	}
	controls = ansi.Strip(model.renderBenchmarkControls(60, 8, paletteFor(themeHacker)))
	if !strings.Contains(controls, "CONFIRM") || !strings.Contains(controls, "132") {
		t.Fatalf("confirmation label missing exact turn count:\n%s", controls)
	}
	updated, command = model.Update(key('a'))
	model = updated.(Model)
	if model.benchmarkAllArmed || model.benchmarkState != benchmarkRunning || command == nil {
		t.Fatal("second Run All press did not launch the suite")
	}
}

func TestBenchmarkRunAllAvailabilityRequiresTasksAndCombinations(t *testing.T) {
	for _, test := range []struct {
		name         string
		running      bool
		combinations int
		tasks        int
		want         bool
	}{
		{name: "available", combinations: 2, tasks: 4, want: true},
		{name: "no tasks", combinations: 2},
		{name: "no combinations", tasks: 4},
		{name: "already running", running: true, combinations: 2, tasks: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := benchmarkRunAllAvailable(test.running, test.combinations, test.tasks); got != test.want {
				t.Fatalf("benchmarkRunAllAvailable(%v, %d, %d) = %v, want %v", test.running, test.combinations, test.tasks, got, test.want)
			}
		})
	}
}

func TestBenchmarkPassFailFilterButtonsAndHotkey(t *testing.T) {
	model := Model{
		width: 100, height: 30, meterStyle: styleBenchmark, benchmarkState: benchmarkFinished,
		benchmarkResults: []codex.BenchmarkResult{
			{DisplayName: "Passing", TaskName: "LRU CACHE", Correct: true},
			{DisplayName: "Failing", TaskName: "SHORTEST PATH", Correct: false},
		},
	}
	x, y := benchmarkControlCoordinates(t, model, footerButtonBenchmarkFilterFail)
	updated, command := model.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = updated.(Model)
	if command == nil || model.benchmarkFilter != benchmarkFilterFail {
		t.Fatal("FAIL filter button did not activate")
	}
	visible := filterBenchmarkResults(model.benchmarkResults, model.benchmarkFilter)
	if len(visible) != 1 || visible[0].DisplayName != "Failing" {
		t.Fatalf("FAIL filter results = %#v", visible)
	}
	updated, _ = model.Update(key('f'))
	model = updated.(Model)
	if model.benchmarkFilter != benchmarkFilterAll {
		t.Fatalf("filter hotkey cycled to %d, want ALL", model.benchmarkFilter)
	}
}

func benchmarkControlCoordinates(t *testing.T, model Model, target footerButtonID) (int, int) {
	t.Helper()
	dashboard := model.dashboardLayout()
	geometry := layoutBenchmarkArea(dashboard.contentWidth, dashboard.meterHeight)
	for _, segments := range model.benchmarkControlLines(max(geometry.controlsWidth-4, 1)) {
		for _, segment := range segments {
			if segment.button == target {
				return renderedTextCoordinates(t, model, segment.text)
			}
		}
	}
	for _, segment := range model.benchmarkFilterLine(max(dashboard.contentWidth-4, 1)) {
		if segment.button == target {
			return renderedTextCoordinates(t, model, segment.text)
		}
	}
	t.Fatalf("benchmark control %d not found", target)
	return 0, 0
}

func renderedTextCoordinates(t *testing.T, model Model, text string) (int, int) {
	t.Helper()
	x, y := renderedTextStart(t, model, text)
	return x + max(lipgloss.Width(text)/2, 0), y
}

func renderedTextStart(t *testing.T, model Model, text string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(model.View()), "\n") {
		if byteX := strings.Index(line, text); byteX >= 0 {
			return lipgloss.Width(line[:byteX]), y
		}
	}
	t.Fatalf("rendered text %q not found", text)
	return 0, 0
}

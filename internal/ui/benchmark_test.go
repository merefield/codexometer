package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

type benchmarkStubFetcher struct {
	stubFetcher
	results []codex.BenchmarkResult
}

type benchmarkCaptureFetcher struct {
	stubFetcher
	tasks chan []codex.BenchmarkTaskID
}

type benchmarkScopedCaptureFetcher struct {
	stubFetcher
	plan   codex.BenchmarkPlan
	scopes chan codex.BenchmarkScope
}

func (f benchmarkScopedCaptureFetcher) BenchmarkCombinationCount(context.Context) (int, error) {
	return f.plan.CombinationCount(f.plan.AllScope()), nil
}

func (f benchmarkScopedCaptureFetcher) BenchmarkPlan(context.Context) (codex.BenchmarkPlan, error) {
	return f.plan, nil
}

func (f benchmarkScopedCaptureFetcher) RunBenchmarkSuite(ctx context.Context, tasks []codex.BenchmarkTaskID, emit func(codex.BenchmarkEvent)) {
	f.RunBenchmarkSuiteScoped(ctx, tasks, f.plan.AllScope(), emit)
}

func (f benchmarkScopedCaptureFetcher) RunBenchmarkSuiteScoped(_ context.Context, _ []codex.BenchmarkTaskID, scope codex.BenchmarkScope, emit func(codex.BenchmarkEvent)) {
	f.scopes <- scope
	emit(codex.BenchmarkEvent{Done: true, Combinations: f.plan.CombinationCount(scope)})
}

func (f benchmarkCaptureFetcher) BenchmarkCombinationCount(context.Context) (int, error) {
	return 1, nil
}

func (f benchmarkCaptureFetcher) RunBenchmarkSuite(_ context.Context, tasks []codex.BenchmarkTaskID, emit func(codex.BenchmarkEvent)) {
	f.tasks <- append([]codex.BenchmarkTaskID(nil), tasks...)
	emit(codex.BenchmarkEvent{Total: len(tasks), Completed: len(tasks), Done: true, Combinations: 1})
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
		width: 100, height: 30, meterView: viewBenchmark,
		benchmarkState: benchmarkFinished,
		benchmarkResults: []codex.BenchmarkResult{
			{TaskName: "MERGE RANGES", Model: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", Effort: "high", Correct: true, Duration: 12500 * time.Millisecond, Usage: codex.BenchmarkUsage{TotalTokens: 4321}, UsageKnown: true, CostKnown: true, CostUSD: 0.0412},
			{TaskName: "LRU CACHE", Model: "future", DisplayName: "Future Model", Effort: "low", Correct: false, Duration: 2 * time.Minute, Usage: codex.BenchmarkUsage{TotalTokens: 999}, UsageKnown: true, Failure: "wrong result"},
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
	if selectorLine < 0 || runLine != selectorLine+1 || !strings.Contains(lines[selectorLine+2], "(S) SCOPE") || !strings.Contains(lines[selectorLine+2], "(X) STOP") {
		t.Fatalf("controls do not contain stable Run, Scope, and Stop rows:\n%s", controls)
	}
}

func TestBenchmarkRowClickOpensScrollableBenchmarkOnlyDetail(t *testing.T) {
	result := codex.BenchmarkResult{
		TaskID: "merge-ranges", TaskName: "MERGE RANGES", Model: "detail-model", DisplayName: "Detail Model", Effort: "high",
		Correct: true, Duration: 12500 * time.Millisecond,
		Usage:      codex.BenchmarkUsage{TotalTokens: 4321, InputTokens: 3000, CachedInputTokens: 1000, OutputTokens: 1321, ReasoningOutputTokens: 500},
		UsageKnown: true, UsageSource: codex.BenchmarkUsageRawResponses, CostKnown: true, CostUSD: 0.0412,
		Interactions: []codex.BenchmarkInteraction{
			{Kind: codex.BenchmarkInteractionPrompt, Content: "Solve the benchmark-only prompt."},
			{Elapsed: time.Second, Kind: codex.BenchmarkInteractionResponse, Content: strings.Repeat("{\"code\":\"safe benchmark response\"}\n", 40)},
			{Elapsed: 2 * time.Second, Kind: codex.BenchmarkInteractionVerifier, Content: "Submission passed the deterministic verifier."},
		},
	}
	model := Model{
		snapshot: codex.DemoSnapshot(), width: 100, height: 30, meterView: viewBenchmark,
		benchmarkState: benchmarkFinished, benchmarkResults: []codex.BenchmarkResult{result},
	}
	x, y := benchmarkRunCoordinates(t, model, benchmarkRunKey(result))
	updated, command := model.Update(tea.MouseMotionMsg{X: x, Y: y})
	model = updated.(Model)
	if command != nil || !model.benchmarkRunHovered || model.benchmarkHoveredRun != benchmarkRunKey(result) {
		t.Fatal("benchmark row hover was not recorded")
	}
	updated, command = model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	if command != nil || model.benchmarkDetail == nil {
		t.Fatal("benchmark row click did not open its detail")
	}
	output := ansi.Strip(model.render())
	for _, want := range []string{"RUN DETAIL", benchmarkDetailCloseLabel, benchmarkDetailCopyLabel, "RESULT // PASS", "Detail Model", "TOTAL 4321", "PROMPT //", "Solve the benchmark-only prompt", "RESPONSE //", "safe benchmark response"} {
		if !strings.Contains(output, want) {
			t.Fatalf("benchmark detail missing %q:\n%s", want, output)
		}
	}
	if got := lipgloss.Width(output); got != model.width {
		t.Fatalf("benchmark detail width = %d, want %d", got, model.width)
	}
	if got := lipgloss.Height(output); got != model.height {
		t.Fatalf("benchmark detail height = %d, want %d", got, model.height)
	}
	copyX, copyY := renderedTextStart(t, model, benchmarkDetailCopyLabel)
	closeX, closeY := renderedTextStart(t, model, benchmarkDetailCloseLabel)
	if closeY != model.dashboardLayout().meterY {
		t.Fatalf("Close control y = %d, want top frame y = %d", closeY, model.dashboardLayout().meterY)
	}
	if copyY != model.dashboardLayout().meterY+model.dashboardLayout().meterHeight-1 {
		t.Fatalf("Copy control y = %d, want bottom frame y = %d", copyY, model.dashboardLayout().meterY+model.dashboardLayout().meterHeight-1)
	}
	updated, command = model.Update(tea.MouseMotionMsg{X: copyX, Y: copyY})
	model = updated.(Model)
	if command != nil || model.hoveredButton != footerButtonBenchmarkCopy {
		t.Fatal("benchmark detail Copy hover was not recorded")
	}
	updated, command = model.Update(tea.MouseClickMsg{X: copyX, Y: copyY, Button: tea.MouseLeft})
	model = updated.(Model)
	if command == nil || model.flashedButton != footerButtonBenchmarkCopy {
		t.Fatal("benchmark detail Copy click did not issue a clipboard command")
	}
	clipboard := model.benchmarkDetailClipboardText()
	for _, want := range []string{"CODEXOMETER BENCHMARK RUN DETAIL", "RESULT: PASS", "MODEL: Detail Model", "[PROMPT +0.0s]", "Solve the benchmark-only prompt.", "[VERIFIER +2.0s]", "Submission passed the deterministic verifier."} {
		if !strings.Contains(clipboard, want) {
			t.Fatalf("full clipboard export missing %q:\n%s", want, clipboard)
		}
	}
	if ansi.Strip(clipboard) != clipboard || strings.Contains(clipboard, "╭") {
		t.Fatalf("clipboard export contains terminal styling or frame decoration:\n%q", clipboard)
	}
	updated, _ = model.Update(specialKey(tea.KeyPgDown))
	model = updated.(Model)
	if model.benchmarkDetailScroll == 0 {
		t.Fatal("Page Down did not scroll a long benchmark detail")
	}
	updated, command = model.Update(tea.MouseClickMsg{X: closeX, Y: closeY, Button: tea.MouseLeft})
	model = updated.(Model)
	if command == nil || model.benchmarkDetail != nil || model.benchmarkSelectedRun != benchmarkRunKey(result) {
		t.Fatal("Close control did not return to the selected matrix row")
	}
	model.openBenchmarkDetail(result)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)
	if command == nil || model.benchmarkDetail != nil {
		t.Fatal("X did not close the benchmark detail")
	}
	model.openBenchmarkDetail(result)
	updated, command = model.Update(specialKey(tea.KeyEscape))
	model = updated.(Model)
	if command != nil || model.benchmarkDetail != nil || model.benchmarkSelectedRun != benchmarkRunKey(result) {
		t.Fatal("Escape did not return to the selected matrix row")
	}
}

func TestBenchmarkDetailCopyShortcutExportsContentOutsideViewport(t *testing.T) {
	result := codex.BenchmarkResult{
		TaskName: "LONG DETAIL", Model: "model", DisplayName: "Model", Effort: "medium", Correct: true,
		Interactions: []codex.BenchmarkInteraction{
			{Kind: codex.BenchmarkInteractionPrompt, Content: strings.Repeat("prompt line\n", 50)},
			{Elapsed: time.Second, Kind: codex.BenchmarkInteractionVerifier, Content: "final offscreen verifier"},
		},
	}
	model := Model{snapshot: codex.DemoSnapshot(), width: 80, height: 20, meterView: viewBenchmark, benchmarkDetail: &result}
	if strings.Contains(ansi.Strip(model.render()), "final offscreen verifier") {
		t.Fatal("test fixture verifier unexpectedly fits in the visible viewport")
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	model = updated.(Model)
	if command == nil || model.flashedButton != footerButtonBenchmarkCopy {
		t.Fatal("c did not activate benchmark detail Copy")
	}
	if clipboard := model.benchmarkDetailClipboardText(); !strings.Contains(clipboard, "final offscreen verifier") {
		t.Fatalf("clipboard omitted content outside viewport:\n%s", clipboard)
	}
}

func TestBenchmarkDetailOmitsEmptyUsageSource(t *testing.T) {
	result := codex.BenchmarkResult{
		TaskName: "DEMO", Model: "model", DisplayName: "Model", Effort: "low", Correct: true,
		UsageKnown: true, Usage: codex.BenchmarkUsage{TotalTokens: 100, InputTokens: 70, OutputTokens: 30},
	}
	model := Model{benchmarkDetail: &result}
	screen := ansi.Strip(strings.Join(model.benchmarkDetailLines(200, paletteFor(themeHacker)), "\n"))
	clipboard := model.benchmarkDetailClipboardText()
	if strings.Contains(screen, "SOURCE") || strings.Contains(clipboard, "SOURCE") {
		t.Fatalf("empty usage source left a dangling label:\nscreen: %s\nclipboard: %s", screen, clipboard)
	}

	result.UsageSource = codex.BenchmarkUsageCumulative
	screen = ansi.Strip(strings.Join(model.benchmarkDetailLines(200, paletteFor(themeHacker)), "\n"))
	clipboard = model.benchmarkDetailClipboardText()
	if !strings.Contains(screen, "SOURCE cumulative") || !strings.Contains(clipboard, "SOURCE cumulative") {
		t.Fatalf("known usage source was omitted:\nscreen: %s\nclipboard: %s", screen, clipboard)
	}
}

func TestInProgressBenchmarkRowOpensAndUpdatesLiveDetail(t *testing.T) {
	active := codex.BenchmarkResult{
		TaskID: "merge-ranges", TaskName: "MERGE RANGES", Model: "live-model", DisplayName: "Live Model", Effort: "medium",
		Interactions: []codex.BenchmarkInteraction{{Kind: codex.BenchmarkInteractionPrompt, Content: "Live benchmark prompt"}},
	}
	model := Model{
		snapshot: codex.DemoSnapshot(), width: 100, height: 30, meterView: viewBenchmark,
		benchmarkState: benchmarkRunning, benchmarkActive: &active, benchmarkActiveSince: time.Now().Add(-2 * time.Second),
	}
	table := ansi.Strip(model.renderBenchmarkArea(96, 19, paletteFor(themeHacker)))
	if !strings.Contains(table, "IN PROGRESS") || !strings.Contains(table, "Live Model") {
		t.Fatalf("active benchmark row is missing:\n%s", table)
	}
	x, y := benchmarkRunCoordinates(t, model, benchmarkRunKey(active))
	updated, command := model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	if command != nil || model.benchmarkDetail == nil || !model.benchmarkDetailActive {
		t.Fatal("active benchmark row did not open a live detail")
	}
	detail := ansi.Strip(model.render())
	if !strings.Contains(detail, "LIVE RUN DETAIL") || !strings.Contains(detail, "RESULT // IN PROGRESS") || !strings.Contains(detail, "Live benchmark prompt") {
		t.Fatalf("live detail is incomplete:\n%s", detail)
	}

	progress := active
	progress.Duration = 3 * time.Second
	progress.Interactions = append(progress.Interactions, codex.BenchmarkInteraction{
		Elapsed: 3 * time.Second, Kind: codex.BenchmarkInteractionResponse, Content: "{\"code\":\"live response\"}",
	})
	updated, _ = model.Update(benchmarkEventMsg{ok: true, event: codex.BenchmarkEvent{Active: &progress, Total: 1}})
	model = updated.(Model)
	if model.benchmarkDetail == nil || !strings.Contains(model.benchmarkDetail.Interactions[1].Content, "live response") {
		t.Fatal("active event did not update the open detail")
	}

	result := progress
	result.Correct = true
	result.Interactions = append(result.Interactions, codex.BenchmarkInteraction{
		Elapsed: 4 * time.Second, Kind: codex.BenchmarkInteractionVerifier, Content: "Submission passed the deterministic verifier.",
	})
	updated, _ = model.Update(benchmarkEventMsg{ok: true, event: codex.BenchmarkEvent{Result: &result, Total: 1, Completed: 1}})
	model = updated.(Model)
	if model.benchmarkActive != nil || model.benchmarkDetailActive || model.benchmarkDetail == nil || !model.benchmarkDetail.Correct {
		t.Fatal("completed result did not replace the live row and finalize its open detail")
	}
	if output := ansi.Strip(model.render()); !strings.Contains(output, "RESULT // PASS") || !strings.Contains(output, "VERIFIER //") {
		t.Fatalf("finalized live detail is incomplete:\n%s", output)
	}
}

func TestBenchmarkKeyboardSelectsAndOpensResultRow(t *testing.T) {
	results := []codex.BenchmarkResult{
		{TaskName: "FIRST", Model: "first", DisplayName: "First", Effort: "low"},
		{TaskName: "SECOND", Model: "second", DisplayName: "Second", Effort: "high", Correct: true},
	}
	model := Model{width: 100, height: 30, meterView: viewBenchmark, benchmarkState: benchmarkFinished, benchmarkResults: results}
	updated, _ := model.Update(specialKey(tea.KeyUp))
	model = updated.(Model)
	if model.benchmarkSelectedRun != benchmarkRunKey(results[1]) {
		t.Fatalf("Up selected %q, want newest visible row", model.benchmarkSelectedRun)
	}
	updated, command := model.Update(specialKey(tea.KeyEnter))
	model = updated.(Model)
	if command != nil || model.benchmarkDetail == nil || model.benchmarkDetail.DisplayName != "Second" {
		t.Fatal("Enter did not open the keyboard-selected benchmark detail")
	}
}

func TestBenchmarkAreaHonorsEveryAllocatedHeight(t *testing.T) {
	model := Model{benchmarkState: benchmarkFinished}
	for height := 1; height <= 12; height++ {
		t.Run(fmt.Sprintf("height-%d", height), func(t *testing.T) {
			const width = 76
			output := ansi.Strip(model.renderBenchmarkArea(width, height, paletteFor(themeHacker)))
			if got := lipgloss.Height(output); got != height {
				t.Fatalf("benchmark height = %d, want %d:\n%s", got, height, output)
			}
			if got := lipgloss.Width(output); got != width {
				t.Fatalf("benchmark width = %d, want %d:\n%s", got, width, output)
			}
			geometry := layoutBenchmarkArea(width, height)
			if geometry.tableHeight < 3 && strings.Contains(output, "RESULT MATRIX") {
				t.Fatalf("table rendered in an allocation too short for its frame:\n%s", output)
			}
			if height == 4 && !strings.Contains(output, "RUN SELECTED") && !strings.Contains(output, "B:RUN") {
				t.Fatalf("compact controls dropped an action before their spacer:\n%s", output)
			}
		})
	}
}

func TestBenchmarkControlsStayWithinNarrowAllocations(t *testing.T) {
	model := Model{benchmarkCombinations: 12}
	for width := 5; width <= 44; width++ {
		output := ansi.Strip(model.renderBenchmarkControls(width, 5, paletteFor(themeHacker)))
		if got := lipgloss.Width(output); got != width {
			t.Fatalf("width %d rendered as %d:\n%s", width, got, output)
		}
	}
}

func TestVeryShortBenchmarkAreaExposesNoHiddenMouseTargets(t *testing.T) {
	for _, height := range []int{8, 9, 10} {
		model := Model{
			snapshot: codex.DemoSnapshot(), width: 40, height: height,
			meterView: viewBenchmark,
		}
		dashboard := model.dashboardLayout()
		if dashboard.meterHeight >= 3 {
			t.Fatalf("test height %d produced meter height %d", height, dashboard.meterHeight)
		}
		for y := dashboard.meterY; y < dashboard.footerY; y++ {
			for x := 0; x < model.width; x++ {
				if button := model.benchmarkButtonAt(x, y); button != footerButtonNone {
					t.Fatalf("height %d exposed hidden button %d at %d,%d", height, button, x, y)
				}
				if column, ok := model.benchmarkHeaderAt(x, y); ok || column != benchmarkSortNone {
					t.Fatalf("height %d exposed hidden heading %d at %d,%d", height, column, x, y)
				}
			}
		}
	}
}

func TestBenchmarkStatusExplainsUnavailableMeasurements(t *testing.T) {
	model := Model{
		benchmarkState: benchmarkFinished,
		benchmarkResults: []codex.BenchmarkResult{{
			Correct: true, UsageIssue: "matching usage event was not observed",
		}},
	}
	output := ansi.Strip(model.renderBenchmarkStatus(60, 5, paletteFor(themeHacker)))
	if !strings.Contains(output, "LAST N/A // matching usage event was not observed") {
		t.Fatalf("benchmark status did not explain unavailable telemetry:\n%s", output)
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
	model.meterView = viewBenchmark

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
	model.meterView = viewBenchmark
	x, y := benchmarkControlCoordinates(t, model, footerButtonBenchmarkSelected)
	mouse := tea.MouseMotionMsg{X: x, Y: y}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || model.hoveredButton != footerButtonBenchmarkSelected {
		t.Fatal("benchmark button hover was not recorded")
	}
	mouse.Button = tea.MouseLeft
	updated, command = model.Update(tea.MouseClickMsg(mouse))
	model = updated.(Model)
	if command == nil || model.benchmarkState != benchmarkRunning {
		t.Fatal("benchmark button click did not start suite")
	}
}

func TestBenchmarkStopCancelsAndRetainsStoppedCurrentRow(t *testing.T) {
	cancelled := false
	active := codex.BenchmarkResult{
		TaskID: codex.BenchmarkMergeRanges, TaskName: "MERGE RANGES", Model: "model", DisplayName: "Model", Effort: "high",
		Interactions: []codex.BenchmarkInteraction{{Kind: codex.BenchmarkInteractionPrompt, Content: "prompt"}},
	}
	model := Model{
		snapshot: codex.DemoSnapshot(), width: 100, height: 30, meterView: viewBenchmark,
		benchmarkState: benchmarkRunning, benchmarkTotal: 3, benchmarkCompleted: 1,
		benchmarkCombinations: 1, benchmarkActive: &active, benchmarkCancel: func() { cancelled = true },
	}
	controls := ansi.Strip(model.renderBenchmarkControls(58, 5, paletteFor(themeHacker)))
	if !strings.Contains(controls, "(X) STOP") {
		t.Fatalf("running controls omitted Stop:\n%s", controls)
	}
	x, y := benchmarkControlCoordinates(t, model, footerButtonBenchmarkStop)
	updated, command := model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	if command == nil || !cancelled || model.benchmarkState != benchmarkStopping {
		t.Fatalf("Stop did not request cancellation: state=%d cancelled=%v", model.benchmarkState, cancelled)
	}

	stopped := active
	stopped.Stopped = true
	stopped.Interactions = append(stopped.Interactions, codex.BenchmarkInteraction{Kind: codex.BenchmarkInteractionVerifier, Content: "Benchmark stopped before completion."})
	updated, _ = model.Update(benchmarkEventMsg{ok: true, event: codex.BenchmarkEvent{Total: 3, Completed: 1, Result: &stopped, Stopped: true}})
	model = updated.(Model)
	updated, _ = model.Update(benchmarkEventMsg{ok: true, event: codex.BenchmarkEvent{Total: 3, Completed: 1, Stopped: true, Done: true}})
	model = updated.(Model)
	if model.benchmarkState != benchmarkStopped || len(model.benchmarkResults) != 1 || !model.benchmarkResults[0].Stopped {
		t.Fatalf("stopped result was not retained: state=%d results=%#v", model.benchmarkState, model.benchmarkResults)
	}
	output := ansi.Strip(model.renderBenchmarkArea(96, 19, paletteFor(themeHacker)))
	if !strings.Contains(output, "STOPPED // 1/3 COMPLETE") || !strings.Contains(output, "STOPPED") {
		t.Fatalf("stopped state is not visible:\n%s", output)
	}
	model.openBenchmarkDetail(stopped)
	detail := ansi.Strip(model.renderBenchmarkArea(96, 19, paletteFor(themeHacker)))
	if !strings.Contains(detail, "RESULT // STOPPED") || !strings.Contains(model.benchmarkDetailClipboardText(), "RESULT: STOPPED") {
		t.Fatalf("stopped detail/export is not explicit:\n%s", detail)
	}
}

func TestBenchmarkScopeScreenSelectsModelsAndEffortsForRun(t *testing.T) {
	plan := codex.BenchmarkPlan{
		Models: []codex.BenchmarkModelOption{
			{Model: "model-a", DisplayName: "Model A", Efforts: []string{"low", "high"}},
			{Model: "model-b", DisplayName: "Model B", Efforts: []string{"low"}},
		},
		Efforts: []string{"low", "high"},
	}
	scopes := make(chan codex.BenchmarkScope, 1)
	fetcher := benchmarkScopedCaptureFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}, plan: plan, scopes: scopes}
	model := New(fetcher, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width, model.height = 100, 30
	model.meterView = viewBenchmark
	updated, _ := model.Update(benchmarkPlanMsg{plan: plan, combinations: 3})
	model = updated.(Model)

	updated, command := model.Update(key('s'))
	model = updated.(Model)
	if command == nil || !model.benchmarkScopeOpen || model.benchmarkCombinations != 3 {
		t.Fatal("Scope hotkey did not open the all-selected scope screen")
	}
	screen := ansi.Strip(model.renderBenchmarkArea(96, 19, paletteFor(themeHacker)))
	for _, want := range []string{"BENCHMARK SCOPE", "(D) DONE", "[x] MODELS // CLEAR ALL", "Model A", "Model B", "[x] REASONING LEVELS // CLEAR ALL", "LOW", "HIGH"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("scope screen missing %q:\n%s", want, screen)
		}
	}

	clickScopeItem := func(index int) {
		t.Helper()
		x, y := benchmarkScopeCoordinates(t, model, index)
		updated, _ = model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		model = updated.(Model)
	}
	clickScopeItem(1) // Clear All models.
	if model.benchmarkCombinations != 0 || len(model.benchmarkScope.Models) != 0 || model.benchmarkPlanNeeded() {
		t.Fatalf("model Clear All did not clear scope: %#v", model.benchmarkScope)
	}
	clickScopeItem(1) // Check All models.
	if model.benchmarkCombinations != 3 || len(model.benchmarkScope.Models) != 2 {
		t.Fatalf("model Check All did not restore scope: %#v", model.benchmarkScope)
	}
	clickScopeItem(3) // Model B off: Model A low/high remain.
	if model.benchmarkCombinations != 2 || len(model.benchmarkScope.Models) != 1 || model.benchmarkScope.Models[0] != "model-a" {
		t.Fatalf("model checkbox did not narrow scope: %#v", model.benchmarkScope)
	}
	colors := paletteFor(themeHacker)
	modelARow := model.renderBenchmarkScopeItem(model.benchmarkScopeItems()[2], 2, 90, colors, stringSetUI(model.benchmarkScope.Efforts))
	if !strings.Contains(modelARow, lipgloss.NewStyle().Foreground(colors.primary).Render("HIGH")) {
		t.Fatalf("selected reasoning level was not active in model row: %q", modelARow)
	}
	clickScopeItem(6) // High off: Model A/low remains.
	if model.benchmarkCombinations != 1 || slices.Contains(model.benchmarkScope.Efforts, "high") {
		t.Fatalf("effort checkbox did not narrow scope: %#v", model.benchmarkScope)
	}
	modelARow = model.renderBenchmarkScopeItem(model.benchmarkScopeItems()[2], 2, 90, colors, stringSetUI(model.benchmarkScope.Efforts))
	if !strings.Contains(modelARow, colors.dimmed().Render("HIGH")) || strings.Contains(modelARow, lipgloss.NewStyle().Foreground(colors.primary).Render("HIGH")) {
		t.Fatalf("out-of-scope reasoning level was not dimmed in model row: %q", modelARow)
	}
	model.benchmarkScopeCursor = 4
	updated, _ = model.Update(specialKey(tea.KeySpace)) // Check All reasoning levels.
	model = updated.(Model)
	if model.benchmarkCombinations != 2 || len(model.benchmarkScope.Efforts) != 2 {
		t.Fatalf("reasoning Check All did not restore efforts: %#v", model.benchmarkScope)
	}
	clickScopeItem(6) // Keep the final launched scope to low only.

	clickScopeItem(0)
	if model.benchmarkScopeOpen {
		t.Fatal("Done did not close Scope")
	}
	updated, command = model.Update(key('b'))
	model = updated.(Model)
	if command == nil || model.benchmarkState != benchmarkRunning {
		t.Fatal("scoped benchmark did not start")
	}
	rawCommand := command()
	batch, ok := rawCommand.(tea.BatchMsg)
	if !ok {
		t.Fatalf("scoped run command returned %T, want tea.BatchMsg", rawCommand)
	}
	for _, child := range batch {
		if _, ok := child().(benchmarkEventMsg); ok {
			break
		}
	}
	got := <-scopes
	if len(got.Models) != 1 || got.Models[0] != "model-a" || len(got.Efforts) != 1 || got.Efforts[0] != "low" {
		t.Fatalf("runner received scope %#v", got)
	}
}

func TestBenchmarkScopeAreaHonorsAllocatedSize(t *testing.T) {
	plan := codex.BenchmarkPlan{
		Models:  []codex.BenchmarkModelOption{{Model: "model", DisplayName: "Model", Efforts: []string{"low", "high"}}},
		Efforts: []string{"low", "high"},
	}
	for _, size := range []struct{ width, height int }{{20, 3}, {40, 8}, {76, 12}, {120, 24}} {
		model := Model{benchmarkPlan: plan, benchmarkScope: plan.AllScope(), benchmarkScopeOpen: true}
		output := ansi.Strip(model.renderBenchmarkArea(size.width, size.height, paletteFor(themeHacker)))
		if got := lipgloss.Width(output); got != size.width {
			t.Errorf("scope width at %dx%d = %d", size.width, size.height, got)
		}
		if got := lipgloss.Height(output); got != size.height {
			t.Errorf("scope height at %dx%d = %d", size.width, size.height, got)
		}
	}
}

func TestDigBenchTaskRequiresOnePairAndIsExcludedFromRunAll(t *testing.T) {
	client := codex.Client{DigBenchToken: "secret", DigBenchGames: []string{"P-1", "P-2"}}
	model := Model{
		benchmarkRunner: client, benchmarkScopedRunner: client, benchmarkCombinations: 2,
		benchmarkPlan:  codex.BenchmarkPlan{Games: []string{"P-1", "P-2"}},
		benchmarkScope: codex.BenchmarkScope{Games: []string{"P-1", "P-2"}},
	}
	tasks := model.benchmarkTasks()
	model.benchmarkSelectedTask = len(tasks) - 1
	if !tasks[model.benchmarkSelectedTask].External || model.benchmarkCanRunSelected() {
		t.Fatalf("DigBench should be visible but disabled with two pairs: %#v", tasks)
	}
	model.benchmarkCombinations = 1
	if !model.benchmarkCanRunSelected() {
		t.Fatal("DigBench was not enabled with exactly one selected pair")
	}
	for _, task := range model.benchmarkRunAllTasks() {
		if task.External {
			t.Fatalf("external task included in Run All: %#v", task)
		}
	}
}

func TestDigBenchScopeFiltersDiscoveredGamesAndConfirmsRemoteSessions(t *testing.T) {
	plan := codex.BenchmarkPlan{
		Models:  []codex.BenchmarkModelOption{{Model: "model", DisplayName: "Model", Efforts: []string{"high"}}},
		Efforts: []string{"high"}, Games: []string{"P-1", "P-2", "P-3"},
	}
	client := codex.Client{DigBenchToken: "secret", DigBenchGames: plan.Games}
	model := Model{
		benchmarkRunner: client, benchmarkScopedRunner: client, benchmarkPlan: plan, benchmarkScope: plan.AllScope(),
		benchmarkCombinations: 1, benchmarkSelectedTask: len(client.BenchmarkTasks()) - 1, meterView: viewBenchmark,
	}
	items := model.benchmarkScopeItems()
	var p2 int
	for index, item := range items {
		if item.kind == benchmarkScopeGame && item.value == "P-2" {
			p2 = index
		}
	}
	if p2 == 0 {
		t.Fatalf("discovered games missing from Scope: %#v", items)
	}
	model.benchmarkScopeCursor = p2
	model.toggleBenchmarkScopeCursor()
	if len(model.benchmarkScope.Games) != 2 || slices.Contains(model.benchmarkScope.Games, "P-2") {
		t.Fatalf("game checkbox did not narrow scope: %#v", model.benchmarkScope.Games)
	}
	updated, command := model.Update(key('b'))
	model = updated.(Model)
	if !model.benchmarkSelectedArmed || model.benchmarkState == benchmarkRunning || command == nil {
		t.Fatal("DigBench did not require confirmation before creating remote sessions")
	}
	updated, command = model.Update(key('b'))
	model = updated.(Model)
	if model.benchmarkSelectedArmed || model.benchmarkState != benchmarkRunning || model.benchmarkTotal != 2 || command == nil {
		t.Fatalf("confirmed DigBench run did not start selected games: state=%d total=%d", model.benchmarkState, model.benchmarkTotal)
	}
}

func TestDigBenchDetailShowsProgressAndTranscript(t *testing.T) {
	result := codex.BenchmarkResult{
		TaskID: codex.BenchmarkDigBench, TaskName: "DIGBENCH P-1", Provider: "digbench",
		Model: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", Effort: "high", Correct: true,
		CurrentLevel: 14, LevelsBeaten: 14, MaxLevel: 14, Steps: 553, GameStatus: "completed",
		Interactions: []codex.BenchmarkInteraction{
			{Kind: codex.BenchmarkInteractionMove, Content: "move_right"},
			{Kind: codex.BenchmarkInteractionState, Content: `{"observation":"WIN"}`},
		},
	}
	model := Model{benchmarkDetail: &result}
	output := ansi.Strip(strings.Join(model.benchmarkDetailLines(100, paletteFor(themeHacker)), "\n"))
	for _, want := range []string{"RESULT // WIN", "LEVEL 14/14", "BEATEN 14", "STEPS 553", "MOVE //", "move_right", `"observation":"WIN"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("DigBench detail missing %q:\n%s", want, output)
		}
	}
	if copied := model.benchmarkDetailClipboardText(); !strings.Contains(copied, "RESULT: WIN") || !strings.Contains(copied, "DIGBENCH: LEVEL 14/14") {
		t.Fatalf("DigBench clipboard text missing progress:\n%s", copied)
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
	model := Model{width: 90, height: 28, meterView: viewBenchmark, benchmarkState: benchmarkFinished}
	for index := 0; index < 30; index++ {
		model.benchmarkResults = append(model.benchmarkResults, codex.BenchmarkResult{DisplayName: "Model", Effort: "low"})
	}
	updated, command := model.Update(specialKey(tea.KeyPgUp))
	model = updated.(Model)
	if command != nil || model.benchmarkScroll == 0 {
		t.Fatalf("Page Up did not scroll: offset=%d command=%v", model.benchmarkScroll, command != nil)
	}
	updated, _ = model.Update(specialKey(tea.KeyPgDown))
	model = updated.(Model)
	if model.benchmarkScroll != 0 {
		t.Fatalf("Page Down offset = %d, want 0", model.benchmarkScroll)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if model.benchmarkScroll != 3 {
		t.Fatalf("mouse-wheel offset = %d, want 3", model.benchmarkScroll)
	}
}

func TestBenchmarkHeadingButtonsSortAndReverse(t *testing.T) {
	model := Model{
		width: 100, height: 30, meterView: viewBenchmark, benchmarkState: benchmarkFinished,
		benchmarkResults: []codex.BenchmarkResult{
			{DisplayName: "Zulu", Effort: "low", Duration: 2 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 200}},
			{DisplayName: "Alpha", Effort: "high", Correct: true, Duration: time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 100}},
		},
	}
	dashboard := model.dashboardLayout()
	geometry := layoutBenchmarkArea(dashboard.contentWidth, dashboard.meterHeight)
	headerX, headerY := renderedTextCoordinates(t, model, "[MODEL]")
	mouse := tea.MouseMotionMsg{
		X: headerX,
		Y: headerY,
	}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || !model.benchmarkSortHovered || model.benchmarkHoveredSort != benchmarkSortModel {
		t.Fatal("model heading did not highlight on hover")
	}
	mouse.Button = tea.MouseLeft
	click := tea.MouseClickMsg(mouse)
	updated, command = model.Update(click)
	model = updated.(Model)
	if command != nil || model.benchmarkSort != benchmarkSortModel || model.benchmarkSortDescending {
		t.Fatalf("first heading click did not select ascending model sort: %#v", model)
	}
	ordered := sortedBenchmarkResults(model.benchmarkResults, model.benchmarkSort, model.benchmarkSortDescending)
	if ordered[0].DisplayName != "Alpha" {
		t.Fatalf("ascending model order = %q first, want Alpha", ordered[0].DisplayName)
	}
	updated, _ = model.Update(click)
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
				meterView: viewBenchmark, benchmarkCombinations: 3,
			}
			dashboard := model.dashboardLayout()
			geometry := layoutBenchmarkArea(dashboard.contentWidth, dashboard.meterHeight)
			for _, segments := range model.benchmarkVisibleControlLines(max(geometry.controlsWidth-4, 1), geometry.controlsHeight) {
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
			if geometry.tableHeight >= 3 {
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
			}

			if geometry.tableHeight < 3 {
				if got, ok := model.benchmarkHeaderAt(4, dashboard.meterY+geometry.topHeight+2); ok || got != benchmarkSortNone {
					t.Errorf("hidden compact table exposed a heading click surface: (%d,%v)", got, ok)
				}
				return
			}
			tableTitleX, tableTitleY := renderedTextStart(t, model, "RESULT MATRIX")
			_ = tableTitleX
			headerY := tableTitleY + 2
			if geometry.tableHeight == 3 {
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
		{DisplayName: "B", Effort: "xhigh", TaskName: "SHORTEST PATH", Duration: 3 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 300}, UsageKnown: true, CostKnown: true, CostUSD: 0.3},
		{DisplayName: "A", Effort: "low", TaskName: "LRU CACHE", Correct: true, Duration: time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 100}, UsageKnown: true, CostKnown: true, CostUSD: 0.1},
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
	ordered = sortedBenchmarkResults(results, benchmarkSortTokens, true)
	if !ordered[0].UsageKnown || ordered[len(ordered)-1].UsageKnown {
		t.Fatal("descending token sort did not leave N/A values last")
	}
}

func TestBenchmarkRankingPrioritizesCorrectnessThenMeasuredEfficiency(t *testing.T) {
	results := []codex.BenchmarkResult{
		{Model: "sol", DisplayName: "Sol", Effort: "medium", TaskName: "A", Correct: true, Duration: 4 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 400}, UsageKnown: true, CostUSD: 0.04, CostKnown: true},
		{Model: "sol", DisplayName: "Sol", Effort: "medium", TaskName: "B", Correct: true, Duration: 6 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 600}, UsageKnown: true, CostUSD: 0.06, CostKnown: true},
		{Model: "terra", DisplayName: "Terra", Effort: "low", TaskName: "A", Correct: true, Duration: 3 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 350}, UsageKnown: true, CostUSD: 0.03, CostKnown: true},
		{Model: "terra", DisplayName: "Terra", Effort: "low", TaskName: "B", Correct: true, Duration: 5 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 550}, UsageKnown: true, CostUSD: 0.05, CostKnown: true},
		{Model: "future", DisplayName: "Future", Effort: "high", TaskName: "A", Correct: true, Duration: 2 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 200}, UsageKnown: true},
		{Model: "future", DisplayName: "Future", Effort: "high", TaskName: "B", Correct: true, Duration: 2 * time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 200}, UsageKnown: true},
		{Model: "luna", DisplayName: "Luna", Effort: "low", TaskName: "A", Correct: true, Duration: time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 100}, UsageKnown: true, CostUSD: 0.01, CostKnown: true},
		{Model: "luna", DisplayName: "Luna", Effort: "low", TaskName: "B", Duration: time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 100}, UsageKnown: true, CostUSD: 0.01, CostKnown: true},
	}
	rankings := benchmarkRankings(results)
	for key, want := range map[string]int{
		"terra\x00low":   1,
		"future\x00high": 2,
		"sol\x00medium":  3,
		"luna\x00low":    4,
	} {
		if got := rankings[key]; got != want {
			t.Errorf("rank %q = %d, want %d; all=%v", key, got, want, rankings)
		}
	}
	values := benchmarkResultValues(results[0], rankings)
	if values[0] != "#3" {
		t.Fatalf("rank display = %q, want #3", values[0])
	}
	ordered := sortedBenchmarkResults(results, benchmarkSortRank, false, rankings)
	if ordered[0].DisplayName != "Terra" || ordered[len(ordered)-1].DisplayName != "Luna" {
		t.Fatalf("rank sort order starts/ends %q/%q", ordered[0].DisplayName, ordered[len(ordered)-1].DisplayName)
	}
	overridden := map[string]int{
		"terra\x00low":   4,
		"luna\x00low":    1,
		"future\x00high": 2,
		"sol\x00medium":  3,
	}
	ordered = sortedBenchmarkResults(results, benchmarkSortRank, false, overridden)
	if ordered[0].DisplayName != "Luna" || ordered[len(ordered)-1].DisplayName != "Terra" {
		t.Fatalf("supplied rank sort order starts/ends %q/%q; supplied rankings were not reused", ordered[0].DisplayName, ordered[len(ordered)-1].DisplayName)
	}
	tied := benchmarkRankings([]codex.BenchmarkResult{
		{Model: "a", Effort: "low", Correct: true, Duration: time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 1}, UsageKnown: true, CostKnown: true},
		{Model: "b", Effort: "low", Correct: true, Duration: time.Second, Usage: codex.BenchmarkUsage{TotalTokens: 999_999}, UsageKnown: true, CostKnown: true},
	})
	if tied["a\x00low"] != 1 || tied["b\x00low"] != 1 {
		t.Fatalf("cost/time tie ranks = %v, want both #1 despite different token totals", tied)
	}
}

func TestBenchmarkRankWeightingSwitchesBetweenCostAndSpeed(t *testing.T) {
	results := []codex.BenchmarkResult{
		{Model: "cheap", Effort: "low", Correct: true, Duration: 30 * time.Second, CostKnown: true, CostUSD: 0.01},
		{Model: "middle", Effort: "low", Correct: true, Duration: 20 * time.Second, CostKnown: true, CostUSD: 0.02},
		{Model: "fast", Effort: "low", Correct: true, Duration: 10 * time.Second, CostKnown: true, CostUSD: 0.03},
	}
	cost := benchmarkRankings(results, benchmarkRankCost)
	balanced := benchmarkRankings(results, benchmarkRankBalanced)
	speed := benchmarkRankings(results, benchmarkRankSpeed)
	if cost["cheap\x00low"] != 1 || cost["fast\x00low"] != 3 {
		t.Fatalf("cost-weighted ranks = %v", cost)
	}
	if balanced["cheap\x00low"] != 1 || balanced["middle\x00low"] != 1 || balanced["fast\x00low"] != 1 {
		t.Fatalf("balanced symmetric ranks = %v; want a three-way tie", balanced)
	}
	if speed["fast\x00low"] != 1 || speed["cheap\x00low"] != 3 {
		t.Fatalf("speed-weighted ranks = %v", speed)
	}
}

func TestBenchmarkEfficiencyRanksAreIsolatedByCorrectnessTier(t *testing.T) {
	peers := []codex.BenchmarkResult{
		{Model: "cheap", Effort: "low", Correct: true, Duration: 30 * time.Second, CostKnown: true, CostUSD: 0.01},
		{Model: "fast", Effort: "low", Correct: true, Duration: 10 * time.Second, CostKnown: true, CostUSD: 0.03},
	}
	baseline := benchmarkRankings(peers, benchmarkRankBalanced)
	withFailures := append(slices.Clone(peers),
		codex.BenchmarkResult{Model: "failed-cost", Effort: "low", Duration: 40 * time.Second, CostKnown: true, CostUSD: 0.02},
		codex.BenchmarkResult{Model: "failed-time", Effort: "low", Duration: 20 * time.Second, CostKnown: true, CostUSD: 0.04},
	)
	got := benchmarkRankings(withFailures, benchmarkRankBalanced)
	for _, key := range []string{"cheap\x00low", "fast\x00low"} {
		if got[key] != baseline[key] {
			t.Errorf("lower-correctness combinations changed peer rank %q from %d to %d", key, baseline[key], got[key])
		}
	}
}

func TestBenchmarkResultValuesDistinguishUnknownFromObservedZero(t *testing.T) {
	unknown := benchmarkResultValues(codex.BenchmarkResult{}, nil)
	if unknown[6] != "N/A" || unknown[7] != "N/A" {
		t.Fatalf("unknown measurements = tokens %q, cost %q; want N/A", unknown[6], unknown[7])
	}
	observedZero := benchmarkResultValues(codex.BenchmarkResult{UsageKnown: true, CostKnown: true}, nil)
	if observedZero[6] != "0" || observedZero[7] != "~$0.0000" {
		t.Fatalf("observed zero measurements = tokens %q, cost %q", observedZero[6], observedZero[7])
	}
}

func TestBenchmarkColumnsRespondToHeadingsValuesAndAvailableWidth(t *testing.T) {
	results := []codex.BenchmarkResult{{
		Model: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", Effort: "ultra", TaskName: "SHORTEST PATH",
		Correct: true, Duration: 12*time.Minute + 34*time.Second,
		Usage: codex.BenchmarkUsage{TotalTokens: 12_345_678}, UsageKnown: true, CostKnown: true, CostUSD: 12345.6789,
	}}
	columns := benchmarkTableColumns(96, results)
	if len(columns) != 8 {
		t.Fatalf("column count = %d, want 8", len(columns))
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
	if columns[1].width > 24 {
		t.Fatalf("model column consumed %d cells despite short model values", columns[1].width)
	}
	values := benchmarkResultValues(results[0], benchmarkRankings(results))
	for index, value := range values {
		if lipgloss.Width(value) > columns[index].width {
			t.Errorf("column %s width %d truncates value %q despite ample space", columns[index].title, columns[index].width, value)
		}
	}

	model := Model{benchmarkResults: results, benchmarkSort: benchmarkSortResult}
	header := ansi.Strip(model.renderBenchmarkHeader(columns, paletteFor(themeHacker)))
	if !strings.Contains(header, "[RESULT▲]") || !strings.Contains(header, "[RANK]") || !strings.Contains(header, "[MODEL]") {
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
	model.meterView = viewBenchmark
	model.benchmarkCombinations = 33

	updated, _ := model.Update(specialKey(tea.KeyRight))
	model = updated.(Model)
	if model.benchmarkSelectedTask != 1 {
		t.Fatalf("right arrow selected task %d, want 1", model.benchmarkSelectedTask)
	}
	controls := ansi.Strip(model.renderBenchmarkControls(60, 8, paletteFor(themeHacker)))
	if !strings.Contains(controls, "LRU CACHE") || !strings.Contains(controls, "231") {
		t.Fatalf("selector or exact all-turn count missing:\n%s", controls)
	}

	updated, command := model.Update(key('a'))
	model = updated.(Model)
	if !model.benchmarkAllArmed || model.benchmarkState == benchmarkRunning || command == nil {
		t.Fatal("first Run All press did not arm confirmation without running")
	}
	controls = ansi.Strip(model.renderBenchmarkControls(60, 8, paletteFor(themeHacker)))
	if !strings.Contains(controls, "CONFIRM") || !strings.Contains(controls, "231") {
		t.Fatalf("confirmation label missing exact turn count:\n%s", controls)
	}
	updated, command = model.Update(key('a'))
	model = updated.(Model)
	if model.benchmarkAllArmed || model.benchmarkState != benchmarkRunning || command == nil {
		t.Fatal("second Run All press did not launch all tasks")
	}
}

func TestBenchmarkSelectorAnchorsButtonsToLongestTaskName(t *testing.T) {
	model := Model{}
	buttonX := func(segments []benchmarkControlSegment, target footerButtonID) int {
		x := 0
		for _, segment := range segments {
			if segment.button == target {
				return x
			}
			x += lipgloss.Width(segment.text) + 1
		}
		return -1
	}
	first := model.benchmarkControlLines(60)[0]
	previousX, nextX := buttonX(first, footerButtonBenchmarkPrevious), buttonX(first, footerButtonBenchmarkNext)
	for range model.benchmarkTasks() {
		model.selectBenchmarkTask(1)
		selector := model.benchmarkControlLines(60)[0]
		if got := buttonX(selector, footerButtonBenchmarkPrevious); got != previousX {
			t.Fatalf("previous button moved from %d to %d", previousX, got)
		}
		if got := buttonX(selector, footerButtonBenchmarkNext); got != nextX {
			t.Fatalf("next button moved from %d to %d", nextX, got)
		}
	}

	clickable := Model{snapshot: codex.DemoSnapshot(), width: 100, height: 30, meterView: viewBenchmark}
	nextX, nextY := benchmarkControlCoordinates(t, clickable, footerButtonBenchmarkNext)
	for range clickable.benchmarkTasks() {
		clickable.selectBenchmarkTask(1)
		if got := clickable.footerButtonAt(nextX, nextY); got != footerButtonBenchmarkNext {
			t.Fatalf("anchored next-button coordinate hit %d after selecting task %d", got, clickable.benchmarkSelectedTask)
		}
	}
}

func TestBenchmarkRunAllLaunchesUnifiedCatalog(t *testing.T) {
	captured := make(chan []codex.BenchmarkTaskID, 1)
	fetcher := benchmarkCaptureFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}, tasks: captured}
	model := New(fetcher, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.meterView = viewBenchmark
	model.benchmarkCombinations = 1

	model, _ = model.activateFooterButton(footerButtonBenchmarkAll)
	model, command := model.activateFooterButton(footerButtonBenchmarkAll)
	if command == nil || model.benchmarkState != benchmarkRunning {
		t.Fatal("confirmed Run All did not launch")
	}
	_ = command()
	got := <-captured
	wantTasks := codex.BenchmarkTasks()
	want := make([]codex.BenchmarkTaskID, 0, len(wantTasks))
	for _, task := range wantTasks {
		want = append(want, task.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("launched tasks = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("launched tasks = %v, want %v", got, want)
		}
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
		width: 100, height: 30, meterView: viewBenchmark, benchmarkState: benchmarkFinished,
		benchmarkResults: []codex.BenchmarkResult{
			{DisplayName: "Passing", TaskName: "LRU CACHE", Correct: true},
			{DisplayName: "Failing", TaskName: "SHORTEST PATH", Correct: false},
		},
	}
	x, y := benchmarkControlCoordinates(t, model, footerButtonBenchmarkFilterFail)
	updated, command := model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
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

func TestBenchmarkRankWeightButtonsAndHotkeyRecomputeImmediately(t *testing.T) {
	model := Model{
		width: 100, height: 30, snapshot: codex.DemoSnapshot(), meterView: viewBenchmark,
		benchmarkRankMode: benchmarkRankBalanced,
		benchmarkResults: []codex.BenchmarkResult{
			{Model: "cheap", DisplayName: "Cheap", Effort: "low", Correct: true, Duration: 30 * time.Second, CostKnown: true, CostUSD: 0.01},
			{Model: "fast", DisplayName: "Fast", Effort: "low", Correct: true, Duration: 10 * time.Second, CostKnown: true, CostUSD: 0.03},
		},
	}
	x, y := benchmarkControlCoordinates(t, model, footerButtonBenchmarkRankCost)
	updated, command := model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	if command == nil || model.benchmarkRankMode != benchmarkRankCost || model.flashedButton != footerButtonBenchmarkRankCost {
		t.Fatalf("Cost rank button did not activate and flash: mode=%d flash=%d", model.benchmarkRankMode, model.flashedButton)
	}
	if ranks := benchmarkRankings(model.benchmarkResults, model.benchmarkRankMode); ranks["cheap\x00low"] != 1 {
		t.Fatalf("Cost selection did not immediately rank Cheap first: %v", ranks)
	}

	updated, command = model.Update(key('w'))
	model = updated.(Model)
	if command == nil || model.benchmarkRankMode != benchmarkRankBalanced || model.flashedButton != footerButtonBenchmarkRankBalanced {
		t.Fatalf("weight hotkey did not cycle from Cost to Balanced: mode=%d flash=%d", model.benchmarkRankMode, model.flashedButton)
	}
	updated, command = model.Update(key('w'))
	model = updated.(Model)
	if command == nil || model.benchmarkRankMode != benchmarkRankSpeed || model.flashedButton != footerButtonBenchmarkRankSpeed {
		t.Fatalf("weight hotkey did not cycle from Balanced to Speed: mode=%d flash=%d", model.benchmarkRankMode, model.flashedButton)
	}
	if ranks := benchmarkRankings(model.benchmarkResults, model.benchmarkRankMode); ranks["fast\x00low"] != 1 {
		t.Fatalf("Speed selection did not immediately rank Fast first: %v", ranks)
	}

	x, y = benchmarkControlCoordinates(t, model, footerButtonBenchmarkRankBalanced)
	updated, _ = model.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.benchmarkRankMode != benchmarkRankBalanced {
		t.Fatalf("Balanced rank button selected mode %d", model.benchmarkRankMode)
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

func benchmarkRunCoordinates(t *testing.T, model Model, key string) (int, int) {
	t.Helper()
	for y := 0; y < model.height; y++ {
		for x := 0; x < model.width; x++ {
			if row, ok := model.benchmarkRunAt(x, y); ok && benchmarkRunKey(row.result) == key {
				return x, y
			}
		}
	}
	t.Fatalf("benchmark run %q was not clickable", key)
	return 0, 0
}

func benchmarkScopeCoordinates(t *testing.T, model Model, index int) (int, int) {
	t.Helper()
	for y := 0; y < model.height; y++ {
		for x := 0; x < model.width; x++ {
			if got, ok := model.benchmarkScopeItemAt(x, y); ok && got == index {
				return x, y
			}
		}
	}
	t.Fatalf("benchmark scope item %d has no click coordinates", index)
	return 0, 0
}

func renderedTextCoordinates(t *testing.T, model Model, text string) (int, int) {
	t.Helper()
	x, y := renderedTextStart(t, model, text)
	return x + max(lipgloss.Width(text)/2, 0), y
}

func renderedTextStart(t *testing.T, model Model, text string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(model.render()), "\n") {
		if byteX := strings.Index(line, text); byteX >= 0 {
			return lipgloss.Width(line[:byteX]), y
		}
	}
	t.Fatalf("rendered text %q not found", text)
	return 0, 0
}

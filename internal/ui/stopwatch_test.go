package ui

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func TestStopwatchGoSampleAndStopLifecycle(t *testing.T) {
	baseline := int64(1_000)
	model := New(stubLiveFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.meterStyle = styleStopwatch

	updated, command := model.Update(key('s'))
	model = updated.(Model)
	if command == nil || model.stopwatchState != stopwatchStarting || model.flashedButton != footerButtonStopwatchGo {
		t.Fatalf("Go did not arm stopwatch: state=%d flash=%d command=%v", model.stopwatchState, model.flashedButton, command)
	}
	flashed := model.renderStopwatchButton(14, 6, "(S)TART", footerButtonStopwatchGo, model.stopwatchGoEnabled(), paletteFor(themeHacker))
	wantFlash := lipgloss.NewStyle().Bold(true).Foreground(paletteFor(themeHacker).background).Background(paletteFor(themeHacker).primary).Render("(S)TART")
	if !strings.Contains(flashed, wantFlash) {
		t.Fatal("Start hotkey did not visibly pulse after entering the starting state")
	}
	sequence := model.stopwatchRequest
	startedAt := time.Unix(100, 0)
	updated, command = model.Update(stopwatchFetchedMsg{
		kind: stopwatchFetchStart, sequence: sequence, usage: usageWithTokens(baseline), at: startedAt,
	})
	model = updated.(Model)
	if model.stopwatchState != stopwatchRunning || model.stopwatchBaseline != baseline || command != nil {
		t.Fatalf("baseline was not established: %#v", model)
	}

	model.stopwatchRequest++
	updated, _ = model.Update(stopwatchFetchedMsg{
		kind: stopwatchFetchSample, sequence: model.stopwatchRequest,
		usage: usageWithTokens(1_250), at: startedAt.Add(29 * time.Second),
	})
	model = updated.(Model)
	if model.stopwatchLatest != 1_250 || len(model.stopwatchSamples) != 0 {
		t.Fatalf("live read should update the readout without prematurely adding a graph bucket: %#v", model.stopwatchSamples)
	}
	model.stopwatchFetchActive = true
	updated, _ = model.Update(secondMsg(startedAt.Add(30 * time.Second)))
	model = updated.(Model)
	model.stopwatchFetchActive = false
	if len(model.stopwatchSamples) != 1 || model.stopwatchSamples[0].intervalTokens != 250 {
		t.Fatalf("30-second graph bucket was not recorded: %#v", model.stopwatchSamples)
	}

	updated, command = model.Update(key('p'))
	model = updated.(Model)
	if command == nil || model.stopwatchState != stopwatchStopping || model.flashedButton != footerButtonStopwatchStop {
		t.Fatalf("Stop did not request final sync: state=%d flash=%d command=%v", model.stopwatchState, model.flashedButton, command)
	}
	stopSequence := model.stopwatchRequest
	updated, _ = model.Update(stopwatchFetchedMsg{
		kind: stopwatchFetchStop, sequence: stopSequence,
		usage: usageWithTokens(1_400), at: startedAt.Add(45 * time.Second),
	})
	model = updated.(Model)
	if model.stopwatchState != stopwatchStopped || model.stopwatchLatest-model.stopwatchBaseline != 400 {
		t.Fatalf("final usage was not recorded: state=%d total=%d", model.stopwatchState, model.stopwatchLatest-model.stopwatchBaseline)
	}
	if len(model.stopwatchSamples) != 1 {
		t.Fatalf("partial final interval was incorrectly plotted as a 30-second bucket: %#v", model.stopwatchSamples)
	}
}

func TestStopwatchTickSamplesOnlyWhileRunningAndIgnoresStaleResults(t *testing.T) {
	model := New(stubLiveFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}}, time.Minute)
	model.meterStyle = styleStopwatch
	updated, command := model.Update(secondMsg(time.Now()))
	model = updated.(Model)
	if command == nil || model.stopwatchFetchActive {
		t.Fatal("idle stopwatch did not retain only the global clock tick")
	}

	model.stopwatchState = stopwatchRunning
	model.stopwatchRequest = 4
	model.stopwatchNextSample = time.Now().Add(time.Minute)
	updated, command = model.Update(secondMsg(time.Now()))
	model = updated.(Model)
	if command == nil || !model.stopwatchFetchActive || model.stopwatchRequest != 5 {
		t.Fatalf("running clock tick did not request live telemetry: %#v", model)
	}
	updated, _ = model.Update(stopwatchFetchedMsg{
		kind: stopwatchFetchSample, sequence: 4, usage: usageWithTokens(999), at: time.Now(),
	})
	model = updated.(Model)
	if model.stopwatchLatest != 0 || !model.stopwatchFetchActive {
		t.Fatal("stale response changed stopwatch state")
	}
}

func TestStopwatchSurfacesUnavailableAndFailedFinalUsage(t *testing.T) {
	model := Model{meterStyle: styleStopwatch, stopwatchState: stopwatchStarting, stopwatchRequest: 1}
	updated, _ := model.Update(stopwatchFetchedMsg{
		kind: stopwatchFetchStart, sequence: 1, err: errors.New("local telemetry unavailable"), at: time.Now(),
	})
	model = updated.(Model)
	if model.stopwatchState != stopwatchStopped || !strings.Contains(model.stopwatchError, "local telemetry unavailable") {
		t.Fatalf("missing usage was not surfaced: %#v", model)
	}

	model.stopwatchState = stopwatchStopping
	model.stopwatchRequest = 2
	updated, _ = model.Update(stopwatchFetchedMsg{
		kind: stopwatchFetchStop, sequence: 2, err: errors.New("offline"), at: time.Now(),
	})
	model = updated.(Model)
	if model.stopwatchState != stopwatchStopped || model.stopwatchError != "offline" {
		t.Fatalf("failed final sync was not surfaced: %#v", model)
	}
}

func TestStopwatchFetchUsesConfiguredLocalSource(t *testing.T) {
	want := codex.LiveUsageSnapshot{TotalTokens: 321, SessionCount: 2}
	model := New(stubLiveFetcher{usage: want}, time.Minute)
	message := model.stopwatchFetch(stopwatchFetchStart, 7)().(stopwatchFetchedMsg)
	if message.err != nil || message.sequence != 7 || message.usage != want {
		t.Fatalf("live source result = %#v", message)
	}

	missing := New(stubFetcher{}, time.Minute)
	message = missing.stopwatchFetch(stopwatchFetchStart, 8)().(stopwatchFetchedMsg)
	if message.err == nil || !strings.Contains(message.err.Error(), "local Codex session telemetry") {
		t.Fatalf("missing live source error = %v", message.err)
	}
}

func TestStopwatchStopUsesFreshLocalSourceWhenAvailable(t *testing.T) {
	fetcher := &stubFreshLiveFetcher{
		stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()},
		ordinary:    codex.LiveUsageSnapshot{TotalTokens: 100},
		fresh:       codex.LiveUsageSnapshot{TotalTokens: 175},
	}
	model := New(fetcher, time.Minute)
	message := model.stopwatchFetch(stopwatchFetchStop, 9)().(stopwatchFetchedMsg)
	if message.err != nil || message.usage.TotalTokens != 175 || fetcher.freshCalls != 1 || fetcher.ordinaryCalls != 0 {
		t.Fatalf("final read did not use fresh discovery: message=%#v fetcher=%#v", message, fetcher)
	}
}

func TestStopwatchViewIsResponsiveAndGraphAutoScales(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}, {200, 60}} {
		model := Model{
			snapshot: codex.DemoSnapshot(), width: size.width, height: size.height,
			meterStyle: styleStopwatch, stopwatchState: stopwatchRunning,
			stopwatchBaseline: 1_000, stopwatchLatest: 7_250,
			stopwatchStartedAt: time.Now().Add(-time.Minute),
			stopwatchSamples:   []stopwatchSample{{intervalTokens: 250}, {intervalTokens: 6_000}},
		}
		output := model.View()
		plain := ansi.Strip(output)
		for _, want := range []string{"SESSION READOUT", "6,250 TOKENS", "ELAPSED", "(S)TART", "STO(P)", "LOCAL TOKEN BARS", "AUTO 0-10K", "█", "░"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%dx%d stopwatch missing %q:\n%s", size.width, size.height, want, plain)
			}
		}
		if lipgloss.Width(output) > size.width || lipgloss.Height(output) > size.height {
			t.Errorf("stopwatch overflowed %dx%d: got %dx%d", size.width, size.height, lipgloss.Width(output), lipgloss.Height(output))
		}
	}
}

func TestStopwatchRecordingLampPulsesBetweenDarkAndBrightRed(t *testing.T) {
	colors := paletteFor(themeHacker)
	model := Model{stopwatchState: stopwatchRunning, stopwatchStartedAt: time.Now()}
	model.phase = 0
	bright := model.renderStopwatchReadout(60, 8, colors)
	model.phase = 1
	dark := model.renderStopwatchReadout(60, 8, colors)
	wantBright := lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render("RECORDING ●")
	wantDark := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7A2633")).Render("RECORDING ●")
	if !strings.Contains(bright, wantBright) || !strings.Contains(dark, wantDark) || ansi.Strip(bright) != ansi.Strip(dark) {
		t.Fatal("recording lamp did not pulse red intensity while keeping a stable label")
	}
}

func TestStopwatchLargeButtonsAreClickableAcrossTheirBoxes(t *testing.T) {
	model := Model{
		snapshot: codex.DemoSnapshot(), width: 100, height: 36,
		meterStyle: styleStopwatch, stopwatchState: stopwatchIdle,
	}
	colors := paletteFor(model.theme)
	dashboard := model.dashboardLayout()
	geometry := layoutStopwatchArea(dashboard.contentWidth, dashboard.meterHeight)
	area := model.renderStopwatchArea(dashboard.contentWidth, dashboard.meterHeight, colors)
	if area.goRect != geometry.goRect || area.stopRect != geometry.stopRect {
		t.Fatalf("rendered controls diverged from pure layout: rendered=%#v layout=%#v", area, geometry)
	}
	originX := 2
	originY := dashboard.meterY

	for _, test := range []struct {
		rect stopwatchRect
		want footerButtonID
	}{{geometry.goRect, footerButtonStopwatchGo}, {geometry.stopRect, footerButtonStopwatchStop}} {
		// Hit an otherwise blank spot inside the large box, not merely its label.
		x := originX + test.rect.x + 1
		y := originY + test.rect.y + 1
		if got := model.stopwatchButtonAt(x, y); got != test.want {
			t.Errorf("button hit = %d, want %d at %d,%d", got, test.want, x, y)
		}
	}
	hoverX := originX + geometry.goRect.x + 1
	hoverY := originY + geometry.goRect.y + 1
	updated, command := model.Update(tea.MouseMsg{X: hoverX, Y: hoverY, Action: tea.MouseActionMotion})
	model = updated.(Model)
	if command != nil || model.hoveredButton != footerButtonStopwatchGo {
		t.Fatal("hovering the Go box did not select it")
	}
	hovered := model.renderStopwatchButton(14, 6, "(S)TART", footerButtonStopwatchGo, true, colors)
	wantHover := lipgloss.NewStyle().Bold(true).Foreground(colors.accent).Background(colors.background).Render("(S)TART")
	if !strings.Contains(hovered, wantHover) {
		t.Fatal("hovering the Go box did not highlight its label")
	}

	updated, command = model.Update(tea.MouseMsg{
		X: originX + geometry.goRect.x + 1, Y: originY + geometry.goRect.y + 1,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if command == nil || model.stopwatchState != stopwatchStarting {
		t.Fatal("clicking the Go box did not start the stopwatch")
	}
}

func TestStopwatchGraphIntroducesBarsOnRightAndScrollsThemLeft(t *testing.T) {
	model := Model{stopwatchState: stopwatchRunning, stopwatchSamples: []stopwatchSample{{intervalTokens: 10}}}
	first := ansi.Strip(model.renderStopwatchGraph(40, 10, paletteFor(themeHacker)))
	firstX := graphRightmostBar(first)
	if firstX < 0 {
		t.Fatalf("first graph has no bar:\n%s", first)
	}

	model.stopwatchSamples = append(model.stopwatchSamples, stopwatchSample{intervalTokens: 10})
	second := ansi.Strip(model.renderStopwatchGraph(40, 10, paletteFor(themeHacker)))
	line := graphBarLine(second)
	if !strings.Contains(line, "█ █") || graphRightmostBar(second) != firstX {
		t.Fatalf("new bar did not enter on the right while the old bar shifted left:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestStopwatchGraphUsesVerticalGranularBars(t *testing.T) {
	model := Model{
		stopwatchState:   stopwatchRunning,
		stopwatchSamples: []stopwatchSample{{intervalTokens: 25}, {intervalTokens: 100}},
	}
	graph := ansi.Strip(model.renderStopwatchGraph(40, 12, paletteFor(themeHacker)))
	if strings.Count(graph, "█") <= len(model.stopwatchSamples) {
		t.Fatalf("bars did not extend vertically across multiple rows:\n%s", graph)
	}
	if !strings.Contains(graph, "░") {
		t.Fatalf("bars did not use dim granular cells above their values:\n%s", graph)
	}
}

func TestStopwatchFormattingAndScaleHelpers(t *testing.T) {
	for input, want := range map[int64]string{0: "0", 999: "999", 1_000: "1,000", -1_234_567: "-1,234,567", math.MinInt64: "-9,223,372,036,854,775,808"} {
		if got := formatTokens(input); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[int64]int64{0: 1, 1: 1, 11: 20, 201: 500, 6_000: 10_000} {
		if got := niceTokenCeiling(input); got != want {
			t.Errorf("niceTokenCeiling(%d) = %d, want %d", input, got, want)
		}
	}
	if got := formatCompactTokens(10_000); got != "10K" {
		t.Fatalf("compact tokens = %q", got)
	}
	if got := formatElapsed(time.Hour + 2*time.Minute + 3*time.Second); got != "01:02:03" {
		t.Fatalf("elapsed = %q", got)
	}
}

type stubLiveFetcher struct {
	stubFetcher
	usage codex.LiveUsageSnapshot
	err   error
}

type stubFreshLiveFetcher struct {
	stubFetcher
	ordinary      codex.LiveUsageSnapshot
	fresh         codex.LiveUsageSnapshot
	ordinaryCalls int
	freshCalls    int
}

func (f *stubFreshLiveFetcher) FetchTokenUsage(context.Context) (codex.LiveUsageSnapshot, error) {
	f.ordinaryCalls++
	return f.ordinary, nil
}

func (f *stubFreshLiveFetcher) FetchTokenUsageFresh(context.Context) (codex.LiveUsageSnapshot, error) {
	f.freshCalls++
	return f.fresh, nil
}

func (f stubLiveFetcher) FetchTokenUsage(context.Context) (codex.LiveUsageSnapshot, error) {
	return f.usage, f.err
}

func usageWithTokens(tokens int64) codex.LiveUsageSnapshot {
	return codex.LiveUsageSnapshot{TotalTokens: tokens, LastActivity: time.Now(), SessionCount: 1}
}

func graphRightmostBar(graph string) int {
	rightmost := -1
	for index, char := range []rune(graphBarLine(graph)) {
		if char == '█' {
			rightmost = index
		}
	}
	return rightmost
}

func graphBarLine(graph string) string {
	for _, line := range strings.Split(graph, "\n") {
		if strings.Contains(line, "█") {
			return line
		}
	}
	return ""
}

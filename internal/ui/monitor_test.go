package ui

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorGoSampleAndStopLifecycle(t *testing.T) {
	baseline := int64(1_000)
	model := New(stubLiveFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.meterView = viewMonitor

	updated, command := model.Update(key('s'))
	model = updated.(Model)
	if command == nil || model.monitorState != monitorStarting || model.flashedButton != footerButtonMonitorGo {
		t.Fatalf("Go did not arm monitor: state=%d flash=%d command=%v", model.monitorState, model.flashedButton, command)
	}
	flashed := model.renderMonitorButton(14, 6, "(S)TART", footerButtonMonitorGo, model.monitorGoEnabled(), paletteFor(themeHacker))
	wantFlash := lipgloss.NewStyle().Bold(true).Foreground(paletteFor(themeHacker).background).Background(paletteFor(themeHacker).primary).Render("(S)TART")
	if !strings.Contains(flashed, wantFlash) {
		t.Fatal("Start hotkey did not visibly pulse after entering the starting state")
	}
	sequence := model.monitorRequest
	startedAt := time.Unix(100, 0)
	updated, command = model.Update(monitorFetchedMsg{
		kind: monitorFetchStart, sequence: sequence, usage: usageWithTokens(baseline), at: startedAt,
	})
	model = updated.(Model)
	if model.monitorState != monitorRunning || model.monitorBaseline != baseline || command != nil {
		t.Fatalf("baseline was not established: %#v", model)
	}

	model.monitorRequest++
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: model.monitorRequest,
		usage: usageWithTokens(1_250), at: startedAt.Add(29 * time.Second),
	})
	model = updated.(Model)
	if model.monitorLatest != 1_250 || len(model.monitorSamples) != 0 {
		t.Fatalf("live read should update the readout without prematurely adding a graph bucket: %#v", model.monitorSamples)
	}
	updated, command = model.Update(secondMsg(startedAt.Add(30 * time.Second)))
	model = updated.(Model)
	if command == nil || !model.monitorBoundaryDue || !model.monitorFetchActive {
		t.Fatal("sample boundary did not request fresh telemetry")
	}
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchBoundary, sequence: model.monitorRequest,
		usage: usageWithTokens(1_250), at: startedAt.Add(30*time.Second + 100*time.Millisecond),
	})
	model = updated.(Model)
	if len(model.monitorSamples) != 1 || model.monitorSamples[0].intervalTokens != 250 {
		t.Fatalf("30-second graph bucket was not recorded: %#v", model.monitorSamples)
	}

	updated, command = model.Update(key('p'))
	model = updated.(Model)
	if command == nil || model.monitorState != monitorStopping || model.flashedButton != footerButtonMonitorStop {
		t.Fatalf("Stop did not request final sync: state=%d flash=%d command=%v", model.monitorState, model.flashedButton, command)
	}
	stopSequence := model.monitorRequest
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchStop, sequence: stopSequence,
		usage: usageWithTokens(1_400), at: startedAt.Add(45 * time.Second),
	})
	model = updated.(Model)
	if model.monitorState != monitorStopped || model.monitorLatest-model.monitorBaseline != 400 {
		t.Fatalf("final usage was not recorded: state=%d total=%d", model.monitorState, model.monitorLatest-model.monitorBaseline)
	}
	if len(model.monitorSamples) != 1 {
		t.Fatalf("partial final interval was incorrectly plotted as a 30-second bucket: %#v", model.monitorSamples)
	}
}

func TestMonitorTickSamplesOnlyWhileRunningAndIgnoresStaleResults(t *testing.T) {
	model := New(stubLiveFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}}, time.Minute)
	model.meterView = viewMonitor
	updated, command := model.Update(secondMsg(time.Now()))
	model = updated.(Model)
	if command == nil || model.monitorFetchActive {
		t.Fatal("idle monitor did not retain only the global clock tick")
	}

	model.monitorState = monitorRunning
	model.monitorRequest = 4
	model.monitorNextSample = time.Now().Add(time.Minute)
	updated, command = model.Update(secondMsg(time.Now()))
	model = updated.(Model)
	if command == nil || !model.monitorFetchActive || model.monitorRequest != 5 {
		t.Fatalf("running clock tick did not request live telemetry: %#v", model)
	}
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: 4, usage: usageWithTokens(999), at: time.Now(),
	})
	model = updated.(Model)
	if model.monitorLatest != 0 || !model.monitorFetchActive {
		t.Fatal("stale response changed monitor state")
	}
}

func TestMonitorTracksRootSessionsAndSamplesEveryGraphOnOneTick(t *testing.T) {
	startedAt := time.Unix(1_000, 0)
	model := Model{monitorState: monitorStarting, monitorRequest: 1}
	started := codex.LiveUsageSnapshot{
		TotalTokens: 300, SessionCount: 2,
		Sessions: []codex.LiveUsageSession{
			{ID: "root-alpha-uuid", WorkingDirectory: "/work/alpha", TotalTokens: 100, AgentCount: 2, Active: true},
			{ID: "root-bravo-uuid", WorkingDirectory: "/work/bravo", TotalTokens: 200, Active: true},
		},
	}
	updated, _ := model.Update(monitorFetchedMsg{
		kind: monitorFetchStart, sequence: 1, usage: started, at: startedAt,
	})
	model = updated.(Model)
	if len(model.monitorSessionData) != 2 || model.monitorSessions != 2 {
		t.Fatalf("root session baselines were not established: %#v", model.monitorSessionData)
	}

	model.monitorRequest++
	usage := codex.LiveUsageSnapshot{
		TotalTokens: 400, SessionCount: 2,
		Sessions: []codex.LiveUsageSession{
			{ID: "root-alpha-uuid", WorkingDirectory: "/work/alpha", TotalTokens: 160, AgentCount: 2, Active: true},
			{ID: "root-bravo-uuid", WorkingDirectory: "/work/bravo", TotalTokens: 240, Active: true},
		},
	}
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: model.monitorRequest, usage: usage, at: startedAt.Add(29 * time.Second),
	})
	model = updated.(Model)
	updated, command := model.Update(secondMsg(startedAt.Add(30 * time.Second)))
	model = updated.(Model)
	if command == nil || !model.monitorFetchActive {
		t.Fatal("shared graph boundary did not request fresh telemetry")
	}
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchBoundary, sequence: model.monitorRequest, usage: usage, at: startedAt.Add(30 * time.Second),
	})
	model = updated.(Model)
	if len(model.monitorSessionData[0].samples) != 1 || len(model.monitorSessionData[1].samples) != 1 {
		t.Fatalf("session graphs were not sampled together: %#v", model.monitorSessionData)
	}
	if model.monitorSessionData[0].samples[0].intervalTokens != 60 || model.monitorSessionData[1].samples[0].intervalTokens != 40 {
		t.Fatalf("per-root sample deltas were not isolated: %#v", model.monitorSessionData)
	}
}

func TestMonitorTracksCompactModelCallOutputAndTTFTStatsSinceStart(t *testing.T) {
	startedAt := time.Unix(1_400, 0)
	model := Model{monitorState: monitorStarting, monitorRequest: 1}
	updated, _ := model.Update(monitorFetchedMsg{
		kind: monitorFetchStart, sequence: 1, at: startedAt,
		usage: codex.LiveUsageSnapshot{TotalTokens: 100, SessionCount: 1, Sessions: []codex.LiveUsageSession{{
			ID: "alpha", TotalTokens: 100, Active: true,
			ModelCalls:  []codex.LiveModelCall{{Sequence: 1, At: startedAt.Add(-time.Second), OutputTokens: 9_999, OutputAvailable: true}},
			TurnTimings: []codex.LiveTurnTiming{{Sequence: 2, At: startedAt.Add(-time.Second), TimeToFirstToken: 20 * time.Second, Available: true}},
		}}},
	})
	model = updated.(Model)
	session := model.monitorSessionData[0]
	if session.modelCalls != 0 || session.latestTTFTOK {
		t.Fatalf("pre-Start response telemetry was not baselined: %#v", session)
	}

	model.monitorRequest++
	latestAt := startedAt.Add(52 * time.Second)
	usage := codex.LiveUsageSnapshot{TotalTokens: 200, SessionCount: 1, Sessions: []codex.LiveUsageSession{{
		ID: "alpha", TotalTokens: 200, Active: true,
		ModelCalls: []codex.LiveModelCall{
			{Sequence: 1, At: startedAt.Add(-time.Second), OutputTokens: 9_999, OutputAvailable: true},
			{Sequence: 3, At: startedAt.Add(40 * time.Second), OutputTokens: 2_013, OutputAvailable: true},
			{Sequence: 4, At: latestAt, OutputTokens: 842, OutputAvailable: true},
		},
		TurnTimings: []codex.LiveTurnTiming{
			{Sequence: 2, At: startedAt.Add(-time.Second), TimeToFirstToken: 20 * time.Second, Available: true},
			{Sequence: 5, At: startedAt.Add(45 * time.Second), TimeToFirstToken: 11_600 * time.Millisecond, Available: true},
			{Sequence: 6, At: latestAt, TimeToFirstToken: 2_400 * time.Millisecond, Available: true},
		},
	}}}
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: model.monitorRequest, at: startedAt.Add(time.Minute), usage: usage,
	})
	model = updated.(Model)
	session = model.monitorSessionData[0]
	if session.modelCalls != 2 || session.latestOutput != 842 || session.peakOutput != 2_013 {
		t.Fatalf("model-call output stats = %#v", session)
	}
	if !session.latestTTFTOK || !session.peakTTFTOK || session.latestTTFT != 2_400*time.Millisecond || session.peakTTFT != 11_600*time.Millisecond {
		t.Fatalf("TTFT stats = %#v", session)
	}
	if got := formatMonitorCallActivity(session, startedAt.Add(time.Minute)); got != "CALLS 2 // LAST 8S AGO" {
		t.Fatalf("call activity = %q", got)
	}
	if got := formatMonitorTTFT(session); got != "TTFT 2.4S // PEAK 11.6S" {
		t.Fatalf("TTFT readout = %q", got)
	}
	if got := formatMonitorOutput(session); got != "LAST OUT 842 // PEAK 2,013" {
		t.Fatalf("output readout = %q", got)
	}
	partial := session
	applyMonitorSessionTelemetry(&partial, codex.LiveUsageSession{
		ModelCalls:  []codex.LiveModelCall{{Sequence: 7, At: startedAt.Add(55 * time.Second)}},
		TurnTimings: []codex.LiveTurnTiming{{Sequence: 8, At: startedAt.Add(56 * time.Second)}},
	}, time.Time{})
	if partial.modelCalls != 3 || partial.latestOutputOK || !partial.peakOutputOK {
		t.Fatalf("partial output stats = %#v", partial)
	}
	if partial.latestTTFTOK || !partial.peakTTFTOK {
		t.Fatalf("partial TTFT stats = %#v", partial)
	}
	if got := formatMonitorTTFT(partial); got != "TTFT N/A // PEAK 11.6S" {
		t.Fatalf("partial TTFT readout = %q", got)
	}
	if got := formatMonitorOutput(partial); got != "LAST OUT N/A // PEAK 2,013" {
		t.Fatalf("partial output readout = %q", got)
	}
	card := ansi.Strip(model.renderMonitorSessionMetrics(64, 12, session, "", paletteFor(themeHacker)))
	for _, want := range []string{"CALLS 2", "TTFT 2.4S // PEAK 11.6S", "LAST OUT 842 // PEAK 2,013"} {
		if !strings.Contains(card, want) {
			t.Errorf("session card missing %q:\n%s", want, card)
		}
	}

	model.monitorRequest++
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: model.monitorRequest, at: startedAt.Add(61 * time.Second), usage: usage,
	})
	model = updated.(Model)
	if model.monitorSessionData[0].modelCalls != 2 {
		t.Fatal("replayed response telemetry was counted twice")
	}

	legacy := monitorSession{}
	if got := formatMonitorTTFT(legacy); got != "TTFT N/A // PEAK N/A" {
		t.Fatalf("legacy TTFT readout = %q", got)
	}
	if got := formatMonitorOutput(legacy); got != "LAST OUT N/A // PEAK N/A" {
		t.Fatalf("legacy output readout = %q", got)
	}
}

func TestMonitorAttributesObservedQuotaMovementByLocalTokenShare(t *testing.T) {
	startedAt := time.Unix(1_500, 0)
	reset := startedAt.Add(time.Hour).Unix()
	model := Model{monitorState: monitorStarting, monitorRequest: 1}
	updated, _ := model.Update(monitorFetchedMsg{
		kind: monitorFetchStart, sequence: 1, at: startedAt,
		quota: monitorQuotaSnapshot(20, reset),
		usage: codex.LiveUsageSnapshot{TotalTokens: 300, SessionCount: 2, Sessions: []codex.LiveUsageSession{
			{ID: "alpha", TotalTokens: 100, Active: true},
			{ID: "bravo", TotalTokens: 200, Active: true},
		}},
	})
	model = updated.(Model)
	model.monitorRequest++
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: model.monitorRequest, at: startedAt.Add(time.Minute),
		usage: codex.LiveUsageSnapshot{TotalTokens: 400, SessionCount: 2, Sessions: []codex.LiveUsageSession{
			{ID: "alpha", TotalTokens: 160, Active: true},
			{ID: "bravo", TotalTokens: 240, Active: true},
		}},
	})
	model = updated.(Model)
	updated, _ = model.Update(fetchedMsg{snapshot: monitorQuotaSnapshot(23, reset)})
	model = updated.(Model)

	if got := model.monitorQuotaReadout(); got != "ACCOUNT QUOTA Δ // 5 HOURS +3PP" {
		t.Fatalf("quota companion readout = %q", got)
	}
	alpha := model.monitorSessionData[model.monitorSessionIndex("alpha")]
	bravo := model.monitorSessionData[model.monitorSessionIndex("bravo")]
	alphaView := ansi.Strip(model.renderMonitorSessionMetrics(52, 8, alpha, "", paletteFor(themeHacker)))
	bravoView := ansi.Strip(model.renderMonitorSessionMetrics(52, 8, bravo, "", paletteFor(themeHacker)))
	for view, wants := range map[string][]string{
		alphaView: {"60 TOKENS // 60% LOCAL", "EST LOCAL-ONLY 5H ~2PP"},
		bravoView: {"40 TOKENS // 40% LOCAL", "EST LOCAL-ONLY 5H ~1PP"},
	} {
		for _, want := range wants {
			if !strings.Contains(view, want) {
				t.Errorf("session estimate missing %q:\n%s", want, view)
			}
		}
	}
	model.monitorQuotaWindows[0].latestUsed = 21
	if got := model.monitorSessionQuotaEstimate(0.4); got != "EST LOCAL-ONLY 5H <1PP" {
		t.Fatalf("sub-point estimate = %q", got)
	}
	model.monitorQuotaWindows[0].latestUsed = 20
	if got := model.monitorSessionQuotaEstimate(0.4); got != "EST LOCAL-ONLY 5H // NO INTEGER Δ" {
		t.Fatalf("zero integer movement = %q", got)
	}
}

func TestMonitorDoesNotEstimateAcrossAQuotaReset(t *testing.T) {
	startedAt := time.Unix(1_700, 0)
	model := Model{monitorState: monitorStarting, monitorRequest: 1}
	updated, _ := model.Update(monitorFetchedMsg{
		kind: monitorFetchStart, sequence: 1, at: startedAt,
		quota: monitorQuotaSnapshot(80, startedAt.Add(time.Minute).Unix()),
		usage: codex.LiveUsageSnapshot{TotalTokens: 100, SessionCount: 1, Sessions: []codex.LiveUsageSession{
			{ID: "alpha", TotalTokens: 100, Active: true},
		}},
	})
	model = updated.(Model)
	model.monitorRequest++
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: model.monitorRequest, at: startedAt.Add(2 * time.Minute),
		usage: codex.LiveUsageSnapshot{TotalTokens: 150, SessionCount: 1, Sessions: []codex.LiveUsageSession{
			{ID: "alpha", TotalTokens: 150, Active: true},
		}},
	})
	model = updated.(Model)
	updated, _ = model.Update(fetchedMsg{snapshot: monitorQuotaSnapshot(2, startedAt.Add(6*time.Hour).Unix())})
	model = updated.(Model)

	if got := model.monitorQuotaReadout(); !strings.Contains(got, "5 HOURS RESET") {
		t.Fatalf("resetting quota readout = %q", got)
	}
	estimate := model.monitorSessionQuotaEstimate(1)
	if estimate != "EST LOCAL-ONLY 5H // RESET" {
		t.Fatalf("reset-crossing estimate = %q", estimate)
	}
}

func TestMonitorSuppressesStaleAndMissingQuotaEstimates(t *testing.T) {
	startedAt := time.Unix(1_800, 0)
	reset := startedAt.Add(time.Hour).Unix()
	model := Model{monitorState: monitorRunning, monitorStartedAt: startedAt, monitorLatest: 200, monitorBaseline: 100}
	model.startMonitorQuotaSnapshot(monitorQuotaSnapshot(20, reset))
	model.monitorQuotaWindows[0].latestUsed = 22

	updated, _ := model.Update(fetchedMsg{err: errors.New("quota offline")})
	model = updated.(Model)
	if got := model.monitorSessionQuotaEstimate(1); got != "EST LOCAL-ONLY 5H // STALE" {
		t.Fatalf("failed-refresh estimate = %q", got)
	}
	if got := model.monitorQuotaReadout(); got != "ACCOUNT QUOTA Δ // 5 HOURS STALE" {
		t.Fatalf("failed-refresh account readout = %q", got)
	}

	updated, _ = model.Update(fetchedMsg{snapshot: codex.Snapshot{}})
	model = updated.(Model)
	if !model.monitorQuotaWindows[0].stale {
		t.Fatal("omitted quota window was not marked stale")
	}
	if got := model.monitorSessionQuotaEstimate(1); got != "EST LOCAL-ONLY 5H // STALE" {
		t.Fatalf("missing-window estimate = %q", got)
	}

	model.monitorQuotaWindows[0].stale = false
	model.monitorState = monitorStopping
	model.monitorRequest = 4
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchStop, sequence: 4, usage: codex.LiveUsageSnapshot{TotalTokens: 200},
		quotaErr: errors.New("final quota offline"), at: startedAt.Add(time.Minute),
	})
	model = updated.(Model)
	if got := model.monitorSessionQuotaEstimate(1); got != "EST LOCAL-ONLY 5H // STALE" {
		t.Fatalf("failed-final estimate = %q", got)
	}
}

func TestMonitorQuotaReadsBracketTheLocalTokenInterval(t *testing.T) {
	fetcher := &orderedMonitorFetcher{
		quota: monitorQuotaSnapshot(20, time.Now().Add(time.Hour).Unix()),
		usage: codex.LiveUsageSnapshot{TotalTokens: 100},
	}
	model := New(fetcher, time.Minute)
	if message := model.monitorFetch(monitorFetchStart, 1)().(monitorFetchedMsg); message.err != nil {
		t.Fatal(message.err)
	}
	if want := []string{"quota", "usage"}; !reflect.DeepEqual(fetcher.calls, want) {
		t.Fatalf("Start reads = %v; want %v", fetcher.calls, want)
	}

	fetcher.calls = nil
	if message := model.monitorFetch(monitorFetchStop, 2)().(monitorFetchedMsg); message.err != nil {
		t.Fatal(message.err)
	}
	if want := []string{"usage-fresh", "quota"}; !reflect.DeepEqual(fetcher.calls, want) {
		t.Fatalf("Stop reads = %v; want %v", fetcher.calls, want)
	}
}

func TestMonitorAddsANewlyDetectedRootWithoutLosingItsFirstUsage(t *testing.T) {
	startedAt := time.Unix(2_000, 0)
	model := Model{monitorState: monitorStarting, monitorRequest: 1}
	updated, _ := model.Update(monitorFetchedMsg{
		kind: monitorFetchStart, sequence: 1, at: startedAt,
		usage: codex.LiveUsageSnapshot{TotalTokens: 100, SessionCount: 1, Sessions: []codex.LiveUsageSession{
			{ID: "existing", TotalTokens: 100, Active: true},
		}},
	})
	model = updated.(Model)
	model.monitorRequest++
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: model.monitorRequest, at: startedAt.Add(time.Second),
		usage: codex.LiveUsageSnapshot{TotalTokens: 150, SessionCount: 2, Sessions: []codex.LiveUsageSession{
			{ID: "existing", TotalTokens: 110, Active: true},
			{ID: "new-root", StartedAt: startedAt.Add(500 * time.Millisecond), TotalTokens: 40, Active: true},
		}},
	})
	model = updated.(Model)
	index := model.monitorSessionIndex("new-root")
	if index < 0 || model.monitorSessionData[index].baseline != 0 || model.monitorSessionData[index].latest != 40 {
		t.Fatalf("new root's first observed usage was lost: %#v", model.monitorSessionData)
	}
	if got := model.monitorSessionElapsed(model.monitorSessionData[index], startedAt.Add(time.Second)); got != 500*time.Millisecond {
		t.Fatalf("new root elapsed time = %s; want its own 500ms lifetime", got)
	}
	model.captureMonitorSamples(startedAt.Add(time.Second))
	if sample := model.monitorSessionData[index].samples[0]; sample.duration != 500*time.Millisecond || sample.intervalTokens != 40 {
		t.Fatalf("new root partial first sample = %#v; want 500ms and 40 tokens", sample)
	}
}

func TestMonitorWaitsForAFreshBoundaryReadWhenOrdinaryFetchIsInFlight(t *testing.T) {
	startedAt := time.Unix(3_000, 0)
	model := Model{
		monitorState: monitorRunning, monitorStartedAt: startedAt,
		monitorNextSample: startedAt.Add(30 * time.Second), monitorFetchActive: true,
		monitorRequest: 5,
	}
	updated, _ := model.Update(secondMsg(startedAt.Add(30 * time.Second)))
	model = updated.(Model)
	if !model.monitorBoundaryDue || len(model.monitorSamples) != 0 {
		t.Fatal("boundary was closed from stale telemetry")
	}

	updated, command := model.Update(monitorFetchedMsg{
		kind: monitorFetchSample, sequence: 5, usage: usageWithTokens(100), at: startedAt.Add(30*time.Second + time.Millisecond),
	})
	model = updated.(Model)
	if command == nil || model.monitorRequest != 6 || !model.monitorFetchActive || len(model.monitorSamples) != 0 {
		t.Fatal("ordinary result did not trigger a dedicated boundary read")
	}
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchBoundary, sequence: 6, usage: usageWithTokens(125), at: startedAt.Add(30*time.Second + 2*time.Millisecond),
	})
	model = updated.(Model)
	if len(model.monitorSamples) != 1 || model.monitorSamples[0].intervalTokens != 125 || model.monitorBoundaryDue {
		t.Fatalf("fresh boundary result did not close the bucket: %#v", model.monitorSamples)
	}
}

func TestMonitorRejectsBackwardsSessionBoundaryAndRetriesNextTick(t *testing.T) {
	startedAt := time.Unix(3_100, 0)
	model := Model{
		monitorState: monitorRunning, monitorStartedAt: startedAt,
		monitorLatest: 100, monitorGraphStart: 100,
		monitorNextSample: startedAt.Add(time.Minute), monitorBoundaryDue: true,
		monitorFetchActive: true, monitorRequest: 7,
		monitorSessionData: []monitorSession{{
			id: "root", baseline: 100, latest: 100, graphStart: 100,
			startedAt: startedAt, active: true, displayed: true,
		}},
	}

	updated, command := model.Update(monitorFetchedMsg{
		kind: monitorFetchBoundary, sequence: 7, at: startedAt.Add(30 * time.Second),
		usage: codex.LiveUsageSnapshot{TotalTokens: 110, Sessions: []codex.LiveUsageSession{{
			ID: "root", TotalTokens: 90, Active: true,
		}}},
	})
	model = updated.(Model)
	if command != nil || len(model.monitorSamples) != 0 || !model.monitorBoundaryDue {
		t.Fatalf("invalid boundary closed its bucket: due=%t samples=%#v command=%v", model.monitorBoundaryDue, model.monitorSamples, command)
	}
	if model.monitorLatest != 100 || model.monitorSessionData[0].latest != 100 || model.monitorError == "" {
		t.Fatalf("invalid boundary mutated accepted telemetry: %#v", model)
	}

	updated, command = model.Update(secondMsg(startedAt.Add(31 * time.Second)))
	model = updated.(Model)
	if command == nil || !model.monitorFetchActive || model.monitorRequest != 8 || !model.monitorBoundaryDue {
		t.Fatalf("rejected boundary was not retried on the next tick: %#v", model)
	}
}

func TestMonitorRendersOneMetricsAndGraphPairPerRootSession(t *testing.T) {
	model := Model{
		monitorState: monitorRunning, monitorStartedAt: time.Now().Add(-time.Minute),
		monitorSessionData: []monitorSession{
			{id: "019d-alpha-a1b2c", workingDirectory: "/work/alpha", latest: 150, agentCount: 2, active: true, displayed: true, samples: []monitorSample{{intervalTokens: 50}}},
			{id: "019d-bravo-d4e5f", workingDirectory: "/work/bravo", latest: 75, active: true, displayed: true, samples: []monitorSample{{intervalTokens: 25}}},
		},
	}
	view := ansi.Strip(model.renderMonitorSessions(116, 24, paletteFor(themeHacker)))
	for _, want := range []string{"A1B2C // ALPHA", "D4E5F // BRAVO", "ROOT + 2 AGENTS", "DIR // alpha", "DIR // bravo"} {
		if !strings.Contains(view, want) {
			t.Errorf("per-root monitor view missing %q:\n%s", want, view)
		}
	}
	if strings.Count(view, "30 SEC TOKEN BARS") != 2 {
		t.Fatalf("wanted one graph beside each root metrics box:\n%s", view)
	}
}

func TestMonitorSessionRowsStayInsideResponsiveRectangle(t *testing.T) {
	sessions := []monitorSession{
		{id: "root-a", active: true, displayed: true, samples: []monitorSample{{intervalTokens: 10}}},
		{id: "root-b", active: true, displayed: true, samples: []monitorSample{{intervalTokens: 20}}},
		{id: "root-c", active: true, displayed: true, samples: []monitorSample{{intervalTokens: 30}}},
		{id: "root-d", active: true, displayed: true, samples: []monitorSample{{intervalTokens: 40}}},
	}
	for _, size := range []struct{ width, height int }{{20, 6}, {40, 9}, {80, 12}, {116, 24}} {
		model := Model{monitorState: monitorRunning, monitorSessionData: sessions}
		view := model.renderMonitorSessions(size.width, size.height, paletteFor(themeHacker))
		if lipgloss.Width(view) > size.width || lipgloss.Height(view) > size.height {
			t.Errorf("session rows overflowed %dx%d: got %dx%d\n%s", size.width, size.height, lipgloss.Width(view), lipgloss.Height(view), ansi.Strip(view))
		}
	}
	model := Model{monitorState: monitorRunning, monitorSessionData: sessions}
	firstPage := ansi.Strip(model.renderMonitorSessions(80, 9, paletteFor(themeHacker)))
	if !strings.Contains(firstPage, "ROWS 1-3/4") || strings.Contains(firstPage, "OOT-D") {
		t.Fatalf("first constrained page did not expose navigation state:\n%s", firstPage)
	}
	model.monitorScroll = 1
	secondPage := ansi.Strip(model.renderMonitorSessions(80, 9, paletteFor(themeHacker)))
	if !strings.Contains(secondPage, "ROWS 2-4/4") || !strings.Contains(secondPage, "OOT-D") || strings.Contains(secondPage, "OOT-A") {
		t.Fatalf("scrolled constrained page did not reveal the final root:\n%s", secondPage)
	}
}

func TestMonitorSessionPagesRespondToKeyboardAndMouse(t *testing.T) {
	model := Model{
		meterView: viewMonitor, width: 84, height: 24,
		monitorSessionData: []monitorSession{
			{id: "a", displayed: true}, {id: "b", displayed: true},
			{id: "c", displayed: true}, {id: "d", displayed: true},
		},
	}
	wantLastPage := max(model.visibleMonitorSessionCount()-model.monitorPageSize(), 0)
	updated, command := model.Update(specialKey(tea.KeyPgDown))
	model = updated.(Model)
	if command != nil || model.monitorScroll != wantLastPage {
		t.Fatalf("Page Down monitor scroll = %d; want %d", model.monitorScroll, wantLastPage)
	}
	updated, _ = model.Update(specialKey(tea.KeyPgUp))
	model = updated.(Model)
	if model.monitorScroll != 0 {
		t.Fatalf("Page Up monitor scroll = %d; want 0", model.monitorScroll)
	}
	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.monitorScroll != 1 {
		t.Fatalf("mouse wheel monitor scroll = %d; want 1", model.monitorScroll)
	}
}

func TestMonitorSurfacesUnavailableAndFailedFinalUsage(t *testing.T) {
	model := Model{meterView: viewMonitor, monitorState: monitorStarting, monitorRequest: 1}
	updated, _ := model.Update(monitorFetchedMsg{
		kind: monitorFetchStart, sequence: 1, err: errors.New("local telemetry unavailable"), at: time.Now(),
	})
	model = updated.(Model)
	if model.monitorState != monitorStopped || !strings.Contains(model.monitorError, "local telemetry unavailable") {
		t.Fatalf("missing usage was not surfaced: %#v", model)
	}

	model.monitorState = monitorStopping
	model.monitorRequest = 2
	updated, _ = model.Update(monitorFetchedMsg{
		kind: monitorFetchStop, sequence: 2, err: errors.New("offline"), at: time.Now(),
	})
	model = updated.(Model)
	if model.monitorState != monitorStopped || model.monitorError != "offline" {
		t.Fatalf("failed final sync was not surfaced: %#v", model)
	}
}

func TestMonitorFetchUsesConfiguredLocalSource(t *testing.T) {
	want := codex.LiveUsageSnapshot{TotalTokens: 321, SessionCount: 2}
	model := New(stubLiveFetcher{usage: want}, time.Minute)
	message := model.monitorFetch(monitorFetchStart, 7)().(monitorFetchedMsg)
	if message.err != nil || message.sequence != 7 || !reflect.DeepEqual(message.usage, want) {
		t.Fatalf("live source result = %#v", message)
	}

	missing := New(stubFetcher{}, time.Minute)
	message = missing.monitorFetch(monitorFetchStart, 8)().(monitorFetchedMsg)
	if message.err == nil || !strings.Contains(message.err.Error(), "local Codex session telemetry") {
		t.Fatalf("missing live source error = %v", message.err)
	}
}

func TestMonitorStopUsesFreshLocalSourceWhenAvailable(t *testing.T) {
	fetcher := &stubFreshLiveFetcher{
		stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()},
		ordinary:    codex.LiveUsageSnapshot{TotalTokens: 100},
		fresh:       codex.LiveUsageSnapshot{TotalTokens: 175},
	}
	model := New(fetcher, time.Minute)
	message := model.monitorFetch(monitorFetchStop, 9)().(monitorFetchedMsg)
	if message.err != nil || message.usage.TotalTokens != 175 || fetcher.freshCalls != 1 || fetcher.ordinaryCalls != 0 {
		t.Fatalf("final read did not use fresh discovery: message=%#v fetcher=%#v", message, fetcher)
	}
}

func TestMonitorViewIsResponsiveAndGraphAutoScales(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}, {200, 60}} {
		model := Model{
			snapshot: codex.DemoSnapshot(), width: size.width, height: size.height,
			meterView: viewMonitor, monitorState: monitorRunning,
			monitorBaseline: 1_000, monitorLatest: 7_250,
			monitorStartedAt: time.Now().Add(-time.Minute),
			monitorSamples:   []monitorSample{{intervalTokens: 250}, {intervalTokens: 6_000}},
		}
		output := model.render()
		plain := ansi.Strip(output)
		for _, want := range []string{"MONITOR READOUT", "6,250 TOKENS", "(S)TART", "STO(P)", "LOCAL TOKEN BARS", "AUTO 0-10K", "█", "░"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%dx%d monitor missing %q:\n%s", size.width, size.height, want, plain)
			}
		}
		if size.height >= 40 && !strings.Contains(plain, "ELAPSED") {
			t.Errorf("%dx%d monitor omitted elapsed telemetry despite available height", size.width, size.height)
		}
		if lipgloss.Width(output) > size.width || lipgloss.Height(output) > size.height {
			t.Errorf("monitor overflowed %dx%d: got %dx%d", size.width, size.height, lipgloss.Width(output), lipgloss.Height(output))
		}
	}
}

func TestMonitorRecordingLampPulsesBetweenDarkAndBrightRed(t *testing.T) {
	colors := paletteFor(themeHacker)
	model := Model{monitorState: monitorRunning, monitorStartedAt: time.Now()}
	model.phase = 0
	bright := model.renderMonitorReadout(60, 8, colors)
	model.phase = 1
	dark := model.renderMonitorReadout(60, 8, colors)
	wantBright := lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render("RECORDING ●")
	wantDark := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7A2633")).Render("RECORDING ●")
	if !strings.Contains(bright, wantBright) || !strings.Contains(dark, wantDark) || ansi.Strip(bright) != ansi.Strip(dark) {
		t.Fatal("recording lamp did not pulse red intensity while keeping a stable label")
	}
}

func TestMonitorSessionProminentlyDistinguishesAttentionReason(t *testing.T) {
	colors := paletteFor(themeHacker)
	model := Model{monitorState: monitorRunning, monitorStartedAt: time.Now().Add(-time.Minute)}
	base := monitorSession{
		id: "019d-attention", workingDirectory: "/work/attention", startedAt: time.Now().Add(-time.Minute),
		baseline: 100, latest: 250, active: true,
	}
	for _, test := range []struct {
		attention codex.SessionAttention
		badge     string
		status    string
	}{
		{codex.SessionAttentionInput, "INPUT NEEDED", "INPUT // ROOT"},
		{codex.SessionAttentionApproval, "APPROVAL NEEDED", "APPROVAL // ROOT"},
		{codex.SessionAttentionCheck, "CHECK SESSION", "CHECK // ROOT"},
	} {
		session := base
		session.attention = test.attention
		output := model.renderMonitorSessionMetrics(42, 10, session, "", colors)
		plain := ansi.Strip(output)
		if !strings.Contains(plain, "● "+test.badge) || !strings.Contains(plain, test.status) {
			t.Fatalf("attention session was not distinctly labelled:\n%s", plain)
		}
		wantBadge := lipgloss.NewStyle().Bold(true).Foreground(colors.background).Background(colors.warning).Render(" ● " + test.badge + " ")
		if !strings.Contains(output, wantBadge) {
			t.Fatalf("%s label was not highlighted with the warning treatment", test.badge)
		}
	}
}

func TestMonitorLargeButtonsAreClickableAcrossTheirBoxes(t *testing.T) {
	model := Model{
		snapshot: codex.DemoSnapshot(), width: 100, height: 36,
		meterView: viewMonitor, monitorState: monitorIdle,
	}
	colors := paletteFor(model.theme)
	dashboard := model.dashboardLayout()
	geometry := layoutMonitorArea(dashboard.contentWidth, dashboard.meterHeight)
	area := model.renderMonitorArea(dashboard.contentWidth, dashboard.meterHeight, colors)
	if area.goRect != geometry.goRect || area.stopRect != geometry.stopRect {
		t.Fatalf("rendered controls diverged from pure layout: rendered=%#v layout=%#v", area, geometry)
	}
	originX := 2
	originY := dashboard.meterY

	for _, test := range []struct {
		rect monitorRect
		want footerButtonID
	}{{geometry.goRect, footerButtonMonitorGo}, {geometry.stopRect, footerButtonNone}} {
		// Hit an otherwise blank spot inside the large box, not merely its label.
		x := originX + test.rect.x + 1
		y := originY + test.rect.y + 1
		if got := model.monitorButtonAt(x, y); got != test.want {
			t.Errorf("button hit = %d, want %d at %d,%d", got, test.want, x, y)
		}
	}
	model.monitorState = monitorRunning
	stopX := originX + geometry.stopRect.x + 1
	stopY := originY + geometry.stopRect.y + 1
	hoverX := originX + geometry.goRect.x + 1
	hoverY := originY + geometry.goRect.y + 1
	if got := model.monitorButtonAt(stopX, stopY); got != footerButtonMonitorStop {
		t.Errorf("enabled Stop button hit = %d, want %d", got, footerButtonMonitorStop)
	}
	if got := model.monitorButtonAt(hoverX, hoverY); got != footerButtonNone {
		t.Errorf("disabled Start button hit = %d, want none", got)
	}
	model.monitorState = monitorIdle
	updated, command := model.Update(tea.MouseMotionMsg{X: hoverX, Y: hoverY})
	model = updated.(Model)
	if command != nil || model.hoveredButton != footerButtonMonitorGo {
		t.Fatal("hovering the Go box did not select it")
	}
	hovered := model.renderMonitorButton(14, 6, "(S)TART", footerButtonMonitorGo, true, colors)
	wantHover := lipgloss.NewStyle().Bold(true).Foreground(colors.accent).Background(colors.background).Render("(S)TART")
	if !strings.Contains(hovered, wantHover) {
		t.Fatal("hovering the Go box did not highlight its label")
	}

	updated, command = model.Update(tea.MouseClickMsg{
		X: originX + geometry.goRect.x + 1, Y: originY + geometry.goRect.y + 1,
		Button: tea.MouseLeft,
	})
	model = updated.(Model)
	if command == nil || model.monitorState != monitorStarting {
		t.Fatal("clicking the Go box did not start the monitor")
	}
}

func TestMonitorHeaderComponentsHonorAllocatedDimensions(t *testing.T) {
	model := Model{snapshot: codex.DemoSnapshot(), meterView: viewMonitor, monitorState: monitorIdle}
	colors := paletteFor(themeHacker)
	for _, size := range []struct{ width, height int }{{36, 10}, {56, 12}, {96, 18}, {156, 30}} {
		geometry := layoutMonitorArea(size.width, size.height)
		readout := model.renderMonitorReadout(geometry.readoutWidth, geometry.topHeight, colors)
		if gotWidth, gotHeight := lipgloss.Width(readout), lipgloss.Height(readout); gotWidth != geometry.readoutWidth || gotHeight != geometry.topHeight {
			t.Errorf("%dx%d readout rendered %dx%d, want %dx%d", size.width, size.height, gotWidth, gotHeight, geometry.readoutWidth, geometry.topHeight)
		}

		for index, width := range geometry.buttonWidths {
			button := model.renderMonitorButton(width, geometry.topHeight, "BUTTON", footerButtonMonitorGo, true, colors)
			if gotWidth, gotHeight := lipgloss.Width(button), lipgloss.Height(button); gotWidth != width || gotHeight != geometry.topHeight {
				t.Errorf("%dx%d button %d rendered %dx%d, want %dx%d", size.width, size.height, index, gotWidth, gotHeight, width, geometry.topHeight)
			}
		}
	}
}

func TestMonitorButtonBoxesMatchEnabledHitSurfacesAcrossSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{{40, 16}, {40, 24}, {60, 24}, {100, 36}, {160, 45}} {
		for _, state := range []monitorState{monitorIdle, monitorRunning} {
			model := Model{
				snapshot: codex.DemoSnapshot(), width: size.width, height: size.height,
				meterView: viewMonitor, monitorState: state,
			}
			dashboard := model.dashboardLayout()
			geometry := layoutMonitorArea(dashboard.contentWidth, dashboard.meterHeight)
			for _, button := range []struct {
				rect    monitorRect
				id      footerButtonID
				enabled bool
			}{
				{geometry.goRect, footerButtonMonitorGo, model.monitorGoEnabled()},
				{geometry.stopRect, footerButtonMonitorStop, model.monitorStopEnabled()},
			} {
				want := footerButtonNone
				if button.enabled {
					want = button.id
				}
				for localY := button.rect.y; localY < button.rect.y+button.rect.height; localY++ {
					for localX := button.rect.x; localX < button.rect.x+button.rect.width; localX++ {
						got := model.monitorButtonAt(2+localX, dashboard.meterY+localY)
						if got != want {
							t.Errorf("%dx%d state %d button %d cell %d,%d hit %d, want %d", size.width, size.height, state, button.id, localX, localY, got, want)
						}
					}
				}
			}
		}
	}
}

func TestMonitorGraphIntroducesBarsOnRightAndScrollsThemLeft(t *testing.T) {
	model := Model{monitorState: monitorRunning, monitorSamples: []monitorSample{{intervalTokens: 10}}}
	first := ansi.Strip(model.renderMonitorGraph(40, 10, paletteFor(themeHacker)))
	firstX := graphRightmostBar(first)
	if firstX < 0 {
		t.Fatalf("first graph has no bar:\n%s", first)
	}

	model.monitorSamples = append(model.monitorSamples, monitorSample{intervalTokens: 10})
	second := ansi.Strip(model.renderMonitorGraph(40, 10, paletteFor(themeHacker)))
	line := graphBarLine(second)
	if !strings.Contains(line, "█ █") || graphRightmostBar(second) != firstX {
		t.Fatalf("new bar did not enter on the right while the old bar shifted left:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestMonitorGraphUsesVerticalGranularBars(t *testing.T) {
	model := Model{
		monitorState:   monitorRunning,
		monitorSamples: []monitorSample{{intervalTokens: 25}, {intervalTokens: 100}},
	}
	graph := ansi.Strip(model.renderMonitorGraph(40, 12, paletteFor(themeHacker)))
	if strings.Count(graph, "█") <= len(model.monitorSamples) {
		t.Fatalf("bars did not extend vertically across multiple rows:\n%s", graph)
	}
	if !strings.Contains(graph, "░") {
		t.Fatalf("bars did not use dim granular cells above their values:\n%s", graph)
	}
}

func TestMonitorFormattingAndScaleHelpers(t *testing.T) {
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

type orderedMonitorFetcher struct {
	calls []string
	quota codex.Snapshot
	usage codex.LiveUsageSnapshot
}

func (f *orderedMonitorFetcher) Fetch(context.Context) (codex.Snapshot, error) {
	f.calls = append(f.calls, "quota")
	return f.quota, nil
}

func (f *orderedMonitorFetcher) FetchTokenUsage(context.Context) (codex.LiveUsageSnapshot, error) {
	f.calls = append(f.calls, "usage")
	return f.usage, nil
}

func (f *orderedMonitorFetcher) FetchTokenUsageFresh(context.Context) (codex.LiveUsageSnapshot, error) {
	f.calls = append(f.calls, "usage-fresh")
	return f.usage, nil
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

func monitorQuotaSnapshot(used int, reset int64) codex.Snapshot {
	duration := int64(300)
	return codex.Snapshot{RateLimits: codex.RateLimitSnapshot{
		Primary: &codex.Window{UsedPercent: used, WindowDurationMins: &duration, ResetsAt: &reset},
	}}
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

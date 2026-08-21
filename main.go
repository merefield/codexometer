package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/ui"
	"github.com/merefield/codexometer/internal/version"
)

type demoFetcher struct {
	mu             sync.Mutex
	snapshot       codex.Snapshot
	pendingQuota   int
	suppressUsage  bool
	lifetimeTokens int64
	apiEqUSD       float64
	apiEqCalls     int64
	eventSequence  uint64
	alphaCalls     []codex.LiveModelCall
	alphaTurns     []codex.LiveTurnTiming
	bravoCalls     []codex.LiveModelCall
	bravoTurns     []codex.LiveTurnTiming
}

func (d *demoFetcher) Fetch(context.Context) (codex.Snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.snapshot.RateLimits.Primary == nil {
		d.snapshot = codex.DemoSnapshot()
		d.snapshot.AccountFingerprint = "demo-account"
	}
	if d.pendingQuota > 0 {
		d.snapshot.RateLimits.Primary.UsedPercent = min(97, d.snapshot.RateLimits.Primary.UsedPercent+d.pendingQuota)
		d.snapshot.RateLimits.Secondary.UsedPercent = min(97, d.snapshot.RateLimits.Secondary.UsedPercent+d.pendingQuota)
		d.pendingQuota = 0
	}
	d.suppressUsage = true
	d.snapshot.FetchedAt = time.Now()
	return cloneDemoSnapshot(d.snapshot), nil
}

func cloneDemoSnapshot(snapshot codex.Snapshot) codex.Snapshot {
	cloned := snapshot
	if snapshot.RateLimits.Primary != nil {
		primary := *snapshot.RateLimits.Primary
		cloned.RateLimits.Primary = &primary
	}
	if snapshot.RateLimits.Secondary != nil {
		secondary := *snapshot.RateLimits.Secondary
		cloned.RateLimits.Secondary = &secondary
	}
	if snapshot.RateLimitResetCredits != nil {
		credits := *snapshot.RateLimitResetCredits
		cloned.RateLimitResetCredits = &credits
	}
	return cloned
}

func (d *demoFetcher) FetchTokenUsage(context.Context) (codex.LiveUsageSnapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lifetimeTokens == 0 {
		d.lifetimeTokens = 100_000
		d.suppressUsage = false
	} else if d.suppressUsage {
		d.suppressUsage = false
	} else {
		d.lifetimeTokens += 1_234
		d.apiEqUSD += 0.18
		d.apiEqCalls++
		d.pendingQuota += 5
		now := time.Now()
		d.eventSequence++
		call := codex.LiveModelCall{
			Sequence: d.eventSequence, At: now, Model: "gpt-5.6-terra",
			OutputTokens: 420 + int64(d.eventSequence%5)*137, OutputAvailable: true,
			APIEqUSD: 0.18, APIEqKnown: true,
		}
		d.eventSequence++
		timing := codex.LiveTurnTiming{Sequence: d.eventSequence, At: now, TimeToFirstToken: time.Duration(900+d.eventSequence%7*350) * time.Millisecond, Available: true}
		if d.eventSequence/2%2 == 0 {
			d.alphaCalls = append(d.alphaCalls, call)
			d.alphaTurns = append(d.alphaTurns, timing)
		} else {
			d.bravoCalls = append(d.bravoCalls, call)
			d.bravoTurns = append(d.bravoTurns, timing)
		}
	}
	alphaTokens := d.lifetimeTokens * 3 / 5
	return codex.LiveUsageSnapshot{
		TotalTokens: d.lifetimeTokens, APIEqUSD: d.apiEqUSD, APIEqPricedCalls: d.apiEqCalls,
		LastActivity: time.Now(), SessionCount: 2,
		Sessions: []codex.LiveUsageSession{
			{ID: "019d-demo-a1b2c", WorkingDirectory: "/projects/alpha", TotalTokens: alphaTokens, LastActivity: time.Now(), AgentCount: 2, Active: true,
				Attention: codex.SessionAttentionApproval, ModelCalls: append([]codex.LiveModelCall(nil), d.alphaCalls...), TurnTimings: append([]codex.LiveTurnTiming(nil), d.alphaTurns...)},
			{ID: "019d-demo-d4e5f", WorkingDirectory: "/projects/bravo", TotalTokens: d.lifetimeTokens - alphaTokens, LastActivity: time.Now(), Active: true,
				Attention: codex.SessionAttentionInput, ModelCalls: append([]codex.LiveModelCall(nil), d.bravoCalls...), TurnTimings: append([]codex.LiveTurnTiming(nil), d.bravoTurns...)},
		},
	}, nil
}

func (d *demoFetcher) BenchmarkCombinationCount(context.Context) (int, error) { return 2, nil }

func (d *demoFetcher) RunBenchmarkSuite(ctx context.Context, tasks []codex.BenchmarkTaskID, emit func(codex.BenchmarkEvent)) {
	d.RunBenchmarkSuiteScoped(ctx, tasks, d.demoBenchmarkPlan().AllScope(), emit)
}

func (d *demoFetcher) BenchmarkPlan(context.Context) (codex.BenchmarkPlan, error) {
	return d.demoBenchmarkPlan(), nil
}

func (d *demoFetcher) demoBenchmarkPlan() codex.BenchmarkPlan {
	return codex.BenchmarkPlan{
		Models: []codex.BenchmarkModelOption{
			{Model: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", Efforts: []string{"medium"}},
			{Model: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", Efforts: []string{"low"}},
		},
		Efforts: []string{"low", "medium"},
	}
}

func (d *demoFetcher) RunBenchmarkSuiteScoped(ctx context.Context, tasks []codex.BenchmarkTaskID, scope codex.BenchmarkScope, emit func(codex.BenchmarkEvent)) {
	baseResults := []codex.BenchmarkResult{
		{Model: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", Effort: "medium", ActualModel: "gpt-5.6-sol", Correct: true, Duration: 12_400 * time.Millisecond, Usage: codex.BenchmarkUsage{TotalTokens: 4_820}, UsageKnown: true, CostUSD: 0.0412, CostKnown: true, Interactions: demoBenchmarkInteractions(true)},
		{Model: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", Effort: "low", ActualModel: "gpt-5.6-terra", Correct: false, Duration: 7_900 * time.Millisecond, Usage: codex.BenchmarkUsage{TotalTokens: 2_210}, UsageKnown: true, CostUSD: 0.0091, CostKnown: true, Failure: "case 17 returned the wrong answer", Interactions: demoBenchmarkInteractions(false)},
	}
	models, efforts := make(map[string]bool), make(map[string]bool)
	for _, model := range scope.Models {
		models[model] = true
	}
	for _, effort := range scope.Efforts {
		efforts[effort] = true
	}
	if len(tasks) == 0 {
		tasks = []codex.BenchmarkTaskID{codex.BenchmarkMergeRanges}
	}
	var results []codex.BenchmarkResult
	for _, taskID := range tasks {
		for _, task := range codex.BenchmarkTasks() {
			if task.ID != taskID {
				continue
			}
			for _, base := range baseResults {
				if !models[base.Model] || !efforts[base.Effort] {
					continue
				}
				result := base
				result.TaskID, result.TaskName = task.ID, task.Name
				results = append(results, result)
			}
		}
	}
	emit(codex.BenchmarkEvent{Total: len(results), Combinations: 2})
	for index := range results {
		select {
		case <-ctx.Done():
			emit(codex.BenchmarkEvent{Total: len(results), Completed: index, Done: true, Err: ctx.Err()})
			return
		default:
		}
		emit(codex.BenchmarkEvent{Total: len(results), Completed: index, CurrentModel: results[index].DisplayName, CurrentEffort: results[index].Effort})
		result := results[index]
		emit(codex.BenchmarkEvent{Total: len(results), Completed: index + 1, Combinations: 2, CurrentTaskID: result.TaskID, CurrentTask: result.TaskName, CurrentModel: result.DisplayName, CurrentEffort: result.Effort, Result: &result})
	}
	emit(codex.BenchmarkEvent{Total: len(results), Completed: len(results), Done: true})
}

func demoBenchmarkInteractions(passed bool) []codex.BenchmarkInteraction {
	verdict := "case 17 returned the wrong answer"
	if passed {
		verdict = "Submission passed the deterministic verifier."
	}
	return []codex.BenchmarkInteraction{
		{Kind: codex.BenchmarkInteractionPrompt, Content: "Implement the selected deterministic benchmark as one Starlark function and return a JSON object containing its complete source."},
		{Elapsed: 7 * time.Second, Kind: codex.BenchmarkInteractionResponse, Content: "{\"code\":\"def solve(items):\\n    return items\\n\"}"},
		{Elapsed: 8 * time.Second, Kind: codex.BenchmarkInteractionVerifier, Content: verdict},
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, defaultDependencies()))
}

type dependencies struct {
	checkAuth func(context.Context, string) (codex.Snapshot, error)
	startUI   func(ui.Fetcher, time.Duration, bool) error
}

func defaultDependencies() dependencies {
	return dependencies{
		checkAuth: func(ctx context.Context, binary string) (codex.Snapshot, error) {
			return (codex.Client{Binary: binary}).Fetch(ctx)
		},
		startUI: startUI,
	}
}

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	flags := flag.NewFlagSet("codexometer", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		codexPath    = flags.String("codex", "codex", "path to the Codex CLI")
		refresh      = flags.Duration("refresh", time.Minute, "quota refresh interval")
		demo         = flags.Bool("demo", false, "show the UI with simulated quota data")
		inline       = flags.Bool("inline", false, "render inline instead of using the alternate screen")
		checkAuth    = flags.Bool("check-auth", false, "verify access to the current Codex login and exit")
		printVersion bool
	)
	flags.BoolVar(&printVersion, "version", false, "print the version and exit")
	flags.BoolVar(&printVersion, "v", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if printVersion {
		fmt.Fprintln(stdout, "codexometer "+version.Current())
		return 0
	}
	if *checkAuth {
		snapshot, err := deps.checkAuth(context.Background(), *codexPath)
		if err != nil {
			fmt.Fprintln(stderr, "Codex auth check failed:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Codex auth OK // %d limit meter(s) online\n", len(snapshot.Meters()))
		return 0
	}

	client := codex.Client{Binary: *codexPath}
	if liveUsage, err := codex.NewLiveUsageReader(""); err == nil {
		client.LiveUsage = liveUsage
	}
	var fetcher ui.Fetcher = client
	if *demo {
		fetcher = &demoFetcher{}
	}

	if err := deps.startUI(fetcher, *refresh, *inline); err != nil {
		fmt.Fprintln(stderr, "codexometer:", err)
		return 1
	}
	return 0
}

func startUI(fetcher ui.Fetcher, refresh time.Duration, inline bool) error {
	model := ui.New(fetcher, refresh)
	if store, storeErr := ui.NewDefaultPreferenceStore(); storeErr == nil {
		model = ui.NewWithPreferences(fetcher, refresh, store)
	}
	model.SetInline(inline)
	_, err := tea.NewProgram(model).Run()
	return err
}

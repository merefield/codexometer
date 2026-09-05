package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/digbench"
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
		CodexStatusKnown: true, CodexUp: true, CodexWorking: true,
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
	checkAuth         func(context.Context, string) (codex.Snapshot, error)
	listDigBenchGames func(context.Context, string) ([]string, error)
	runDigBench       func(context.Context, string, string, string, codex.DigBenchOptions) (codex.DigBenchResult, error)
	startUI           func(ui.Fetcher, time.Duration, bool, bool) error
}

func defaultDependencies() dependencies {
	return dependencies{
		checkAuth: func(ctx context.Context, binary string) (codex.Snapshot, error) {
			return (codex.Client{Binary: binary}).Fetch(ctx)
		},
		listDigBenchGames: func(ctx context.Context, token string) ([]string, error) {
			response, err := (digbench.Client{Token: token}).ListGames(ctx)
			return response.Games, err
		},
		runDigBench: func(ctx context.Context, binary, token, apiKey string, options codex.DigBenchOptions) (codex.DigBenchResult, error) {
			return (codex.Client{Binary: binary, BenchmarkAPIKey: apiKey}).RunDigBench(ctx, digbench.Client{Token: token}, options)
		},
		startUI: startUI,
	}
}

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	flags := flag.NewFlagSet("codexometer", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		codexPath       = flags.String("codex", "codex", "path to the Codex CLI")
		refresh         = flags.Duration("refresh", time.Minute, "quota refresh interval")
		demo            = flags.Bool("demo", false, "show the UI with simulated quota data")
		inline          = flags.Bool("inline", false, "render inline instead of using the alternate screen")
		alwaysShowReset = flags.Bool("always-show-reset", false, "show available quota reset credits below 80% consumption")
		checkAuth       = flags.Bool("check-auth", false, "verify access to the current Codex login and exit")
		digBenchGame    = flags.String("digbench-game", "", "run one experimental DigBench game and exit")
		digBenchModel   = flags.String("digbench-model", "gpt-5.6-sol", "Codex model for --digbench-game")
		digBenchEffort  = flags.String("digbench-effort", "high", "reasoning effort for --digbench-game")
		digBenchTimeout = flags.Duration("digbench-timeout", codex.DefaultDigBenchTimeout, "hard limit for --digbench-game")
		printVersion    bool
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
	// Capture credentials for the benchmark components, then remove them before
	// any path that can launch Codex. This keeps auth checks and quota reads on
	// the user's prevailing Codex login instead of leaking or implicitly using a
	// benchmarking credential through the child environment.
	benchmarkAPIKey, benchmarkAPIKeySource, err := takeBenchmarkAPIKey()
	if err != nil {
		fmt.Fprintln(stderr, "codexometer:", err)
		return 1
	}
	digBenchToken, err := takeDigBenchToken()
	if err != nil {
		fmt.Fprintln(stderr, "codexometer:", err)
		return 1
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
	if strings.TrimSpace(*digBenchGame) != "" {
		if *demo {
			fmt.Fprintln(stderr, "codexometer: --digbench-game cannot be combined with --demo")
			return 2
		}
		if digBenchToken == "" {
			fmt.Fprintln(stderr, "codexometer: DIGBENCH_API_TOKEN is required for --digbench-game")
			return 1
		}
		if deps.runDigBench == nil {
			fmt.Fprintln(stderr, "codexometer: DigBench runner unavailable")
			return 1
		}
		billing := "the prevailing Codex login and quota"
		if benchmarkAPIKey != "" {
			billing = "API-key usage-based billing from " + benchmarkAPIKeySource
		}
		fmt.Fprintf(stderr, "Starting DigBench %s with %s/%s; this creates a persisted remote session and uses %s.\n",
			strings.TrimSpace(*digBenchGame), strings.TrimSpace(*digBenchModel), strings.TrimSpace(*digBenchEffort), billing)
		result, err := deps.runDigBench(context.Background(), *codexPath, digBenchToken, benchmarkAPIKey, codex.DigBenchOptions{
			Game: strings.TrimSpace(*digBenchGame), Model: strings.TrimSpace(*digBenchModel),
			Effort: strings.TrimSpace(*digBenchEffort), Timeout: *digBenchTimeout, ClientVersion: version.Current(),
			Progress: func(progress codex.DigBenchProgress) {
				fmt.Fprintln(stderr, formatDigBenchProgress(progress))
			},
		})
		if err != nil {
			fmt.Fprintln(stderr, "codexometer: DigBench run failed:", err)
			return 1
		}
		fmt.Fprintln(stdout, formatDigBenchResult(result))
		if result.Failure != "" {
			return 1
		}
		return 0
	}

	var digBenchGames []string
	if digBenchToken != "" && deps.listDigBenchGames != nil {
		discoveryCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		digBenchGames, err = deps.listDigBenchGames(discoveryCtx, digBenchToken)
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "codexometer: DigBench game discovery unavailable:", err)
			digBenchGames = nil
		}
	}
	digBenchGames = normalizeDigBenchGames(digBenchGames)
	client := codex.Client{Binary: *codexPath, BenchmarkAPIKey: benchmarkAPIKey, DigBenchToken: digBenchToken, DigBenchGames: digBenchGames}
	if liveUsage, err := codex.NewLiveUsageReader(""); err == nil {
		client.LiveUsage = liveUsage
	}
	var fetcher ui.Fetcher = client
	if *demo {
		fetcher = &demoFetcher{}
	}

	if err := deps.startUI(fetcher, *refresh, *inline, *alwaysShowReset); err != nil {
		fmt.Fprintln(stderr, "codexometer:", err)
		return 1
	}
	return 0
}

func formatDigBenchProgress(progress codex.DigBenchProgress) string {
	label := "DIGBENCH PROGRESS"
	switch progress.Phase {
	case codex.DigBenchProgressSession:
		label = "DIGBENCH SESSION CREATED"
	case codex.DigBenchProgressTurn:
		label = "CODEX TURN STARTED"
	case codex.DigBenchProgressHeartbeat:
		label = "DIGBENCH WORKING"
	}
	level := fmt.Sprintf("%d", progress.Level)
	if progress.MaxLevel > 0 {
		level = fmt.Sprintf("%d/%d", progress.Level, progress.MaxLevel)
	}
	parts := []string{label, "LEVEL " + level, fmt.Sprintf("BEATEN %d", progress.LevelsBeaten), fmt.Sprintf("STEPS %d", progress.Steps)}
	if progress.Status != "" {
		parts = append(parts, "STATUS "+progress.Status)
	}
	if progress.ActualModel != "" && progress.Phase == codex.DigBenchProgressTurn {
		parts = append(parts, "MODEL "+progress.ActualModel)
	}
	parts = append(parts, "ELAPSED "+progress.Elapsed.Round(100*time.Millisecond).String())
	return strings.Join(parts, " // ")
}

func takeBenchmarkAPIKey() (key, source string, err error) {
	for _, candidate := range []string{"CODEXOMETER_BENCHMARK_API_KEY", "OPENAI_API_KEY"} {
		value := strings.TrimSpace(os.Getenv(candidate))
		if unsetErr := os.Unsetenv(candidate); unsetErr != nil {
			return "", "", fmt.Errorf("remove %s from child environment: %w", candidate, unsetErr)
		}
		if key == "" && value != "" {
			key, source = value, candidate
		}
	}
	return key, source, nil
}

func takeDigBenchToken() (string, error) {
	token := strings.TrimSpace(os.Getenv("DIGBENCH_API_TOKEN"))
	if err := os.Unsetenv("DIGBENCH_API_TOKEN"); err != nil {
		return "", fmt.Errorf("remove DIGBENCH_API_TOKEN from child environment: %w", err)
	}
	return token, nil
}

func normalizeDigBenchGames(games []string) []string {
	seen := make(map[string]bool, len(games))
	normalized := make([]string, 0, len(games))
	for _, game := range games {
		game = strings.TrimSpace(game)
		if game != "" && !containsTerminalControl(game) && !seen[game] {
			seen[game] = true
			normalized = append(normalized, game)
		}
	}
	return normalized
}

func containsTerminalControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) {
			return true
		}
	}
	return false
}

func formatDigBenchResult(result codex.DigBenchResult) string {
	outcome := "LOSS"
	if result.Won {
		outcome = "WIN"
	} else if result.Failure != "" {
		outcome = "INCOMPLETE"
	}
	levels := fmt.Sprintf("%d", result.LevelsBeaten)
	if result.MaxLevel > 0 {
		levels = fmt.Sprintf("%d/%d", result.LevelsBeaten, result.MaxLevel)
	}
	tokens := "N/A"
	if result.UsageKnown {
		tokens = fmt.Sprintf("%d", result.Usage.TotalTokens)
	}
	cost := "N/A"
	if result.CostKnown {
		cost = fmt.Sprintf("$%.4f", result.CostUSD)
	}
	line := fmt.Sprintf("DIGBENCH %s // %s // LEVELS %s // STEPS %d // %s/%s // %s // TOKENS %s // API EQ %s",
		result.Game, outcome, levels, result.Steps, result.DisplayName, result.Effort,
		result.Duration.Round(time.Millisecond), tokens, cost)
	if result.ActualModel != "" && result.ActualModel != result.Model {
		line += " // SERVED " + result.ActualModel
	}
	if result.Seed != nil {
		line += fmt.Sprintf(" // SEED %d", *result.Seed)
	}
	if result.FrameworkVersion != "" {
		line += " // ENGINE " + result.FrameworkVersion
	}
	if result.Failure != "" {
		line += " // " + result.Failure
	} else if !result.UsageKnown && result.UsageIssue != "" {
		line += " // TELEMETRY " + result.UsageIssue
	} else if !result.CostKnown && result.CostIssue != "" {
		line += " // API EQ " + result.CostIssue
	}
	return line
}

func startUI(fetcher ui.Fetcher, refresh time.Duration, inline, alwaysShowReset bool) error {
	model := ui.New(fetcher, refresh)
	if store, storeErr := ui.NewDefaultPreferenceStore(); storeErr == nil {
		model = ui.NewWithPreferences(fetcher, refresh, store)
	}
	model.SetInline(inline)
	model.SetAlwaysShowReset(alwaysShowReset)
	_, err := tea.NewProgram(model).Run()
	return err
}

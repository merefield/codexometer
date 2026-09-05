package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/ui"
	"github.com/merefield/codexometer/internal/version"
)

func TestRunVersion(t *testing.T) {
	for _, flag := range []string{"-v", "--version"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{flag}, &stdout, &stderr, dependencies{})
		expected := "codexometer " + version.Current() + "\n"
		if code != 0 || stdout.String() != expected || stderr.Len() != 0 {
			t.Errorf("flag=%s code=%d stdout=%q stderr=%q", flag, code, stdout.String(), stderr.String())
		}
	}
}

func TestRunRejectsInvalidFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--not-a-flag"}, &stdout, &stderr, dependencies{})
	if code != 2 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunAuthCheckSuccessAndFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := dependencies{
		checkAuth: func(context.Context, string) (codex.Snapshot, error) {
			return codex.DemoSnapshot(), nil
		},
	}
	code := run([]string{"--check-auth", "--codex", "/custom/codex"}, &stdout, &stderr, deps)
	if code != 0 || !strings.Contains(stdout.String(), "2 limit meter(s) online") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	deps.checkAuth = func(context.Context, string) (codex.Snapshot, error) {
		return codex.Snapshot{}, errors.New("logged out")
	}
	code = run([]string{"--check-auth"}, &stdout, &stderr, deps)
	if code != 1 || !strings.Contains(stderr.String(), "logged out") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunAuthCheckScrubsBenchmarkCredentialsBeforeLaunchingCodex(t *testing.T) {
	t.Setenv("DIGBENCH_API_TOKEN", "digbench-secret")
	t.Setenv("CODEXOMETER_BENCHMARK_API_KEY", "benchmark-secret")
	t.Setenv("OPENAI_API_KEY", "fallback-secret")
	var stdout, stderr bytes.Buffer
	deps := dependencies{
		checkAuth: func(context.Context, string) (codex.Snapshot, error) {
			for _, name := range []string{"DIGBENCH_API_TOKEN", "CODEXOMETER_BENCHMARK_API_KEY", "OPENAI_API_KEY"} {
				if value := os.Getenv(name); value != "" {
					t.Fatalf("%s remained in the auth-check child environment", name)
				}
			}
			return codex.DemoSnapshot(), nil
		},
	}

	if code := run([]string{"--check-auth"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunStartsDemoWithSelectedOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	deps := dependencies{
		startUI: func(fetcher ui.Fetcher, refresh time.Duration, inline, alwaysShowReset bool) error {
			called = true
			if refresh != 30*time.Second || !inline || !alwaysShowReset {
				t.Fatalf("refresh=%s inline=%v", refresh, inline)
			}
			usageFetcher, ok := fetcher.(ui.TokenUsageFetcher)
			if !ok {
				t.Fatal("demo fetcher does not provide live token usage")
			}
			first, err := usageFetcher.FetchTokenUsage(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := fetcher.Fetch(context.Background())
			if err != nil || len(snapshot.Meters()) != 2 {
				t.Fatalf("demo fetch returned %#v, %v", snapshot, err)
			}
			stable, err := usageFetcher.FetchTokenUsage(context.Background())
			if err != nil || !sameDemoAccounting(first, stable) {
				t.Fatalf("initial demo bracket was unstable: first=%#v second=%#v err=%v", first, stable, err)
			}
			advanced, err := usageFetcher.FetchTokenUsage(context.Background())
			if err != nil || advanced.TotalTokens <= stable.TotalTokens || advanced.APIEqUSD <= 0 {
				t.Fatalf("demo activity did not advance: before=%#v after=%#v err=%v", stable, advanced, err)
			}
			refreshed, err := fetcher.Fetch(context.Background())
			if err != nil || refreshed.Meters()[0].Window.UsedPercent-snapshot.Meters()[0].Window.UsedPercent != 5 {
				t.Fatalf("demo quota did not advance: first=%#v second=%#v err=%v", snapshot, refreshed, err)
			}
			second, err := usageFetcher.FetchTokenUsage(context.Background())
			if err != nil || !sameDemoAccounting(advanced, second) || second.APIEqPricedCalls != 1 {
				t.Fatalf("second demo bracket was unstable: first=%#v second=%#v err=%v", advanced, second, err)
			}
			return nil
		},
	}
	code := run([]string{"--demo", "--inline", "--refresh", "30s", "--always-show-reset"}, &stdout, &stderr, deps)
	if code != 0 || !called {
		t.Fatalf("code=%d called=%v stderr=%q", code, called, stderr.String())
	}
}

func sameDemoAccounting(left, right codex.LiveUsageSnapshot) bool {
	return left.APIEqUSD == right.APIEqUSD && left.APIEqPricedCalls == right.APIEqPricedCalls &&
		left.APIEqUnpricedCalls == right.APIEqUnpricedCalls &&
		left.APIEqPendingCalls == right.APIEqPendingCalls
}

func TestRunReportsUIError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := dependencies{
		startUI: func(ui.Fetcher, time.Duration, bool, bool) error { return errors.New("terminal unavailable") },
	}
	code := run(nil, &stdout, &stderr, deps)
	if code != 1 || !strings.Contains(stderr.String(), "terminal unavailable") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunDigBenchUsesExplicitSingleGameOptions(t *testing.T) {
	t.Setenv("DIGBENCH_API_TOKEN", "secret")
	var stdout, stderr bytes.Buffer
	called := false
	deps := dependencies{
		runDigBench: func(_ context.Context, binary, token, apiKey string, options codex.DigBenchOptions) (codex.DigBenchResult, error) {
			called = true
			if value := os.Getenv("DIGBENCH_API_TOKEN"); value != "" {
				t.Fatalf("DigBench token remained in child environment: %q", value)
			}
			if binary != "/custom/codex" || token != "secret" || apiKey != "" || options.Game != "P-3" || options.Model != "gpt-5.6-terra" || options.Effort != "medium" || options.Timeout != 2*time.Minute {
				t.Fatalf("binary=%q token=%q apiKeyPresent=%v options=%#v", binary, token, apiKey != "", options)
			}
			if options.Progress == nil {
				t.Fatal("DigBench progress callback was not configured")
			}
			options.Progress(codex.DigBenchProgress{
				Phase: codex.DigBenchProgressUpdate, Level: 2, MaxLevel: 4,
				LevelsBeaten: 1, Steps: 9, Status: "in_progress", Elapsed: 1250 * time.Millisecond,
			})
			return codex.DigBenchResult{
				Game: "P-3", Won: true, Status: "completed", LevelsBeaten: 4, MaxLevel: 4, Steps: 17,
				DisplayName: "GPT-5.6 Terra", Effort: "medium", Duration: 3 * time.Second,
				UsageKnown: true, Usage: codex.BenchmarkUsage{TotalTokens: 1234}, CostKnown: true, CostUSD: 0.0123,
			}, nil
		},
	}
	code := run([]string{
		"--digbench-game", "P-3", "--digbench-model", "gpt-5.6-terra", "--digbench-effort", "medium",
		"--digbench-timeout", "2m", "--codex", "/custom/codex",
	}, &stdout, &stderr, deps)
	if code != 0 || !called || !strings.Contains(stdout.String(), "DIGBENCH P-3 // WIN // LEVELS 4/4") ||
		!strings.Contains(stderr.String(), "Starting DigBench P-3 with gpt-5.6-terra/medium") ||
		!strings.Contains(stderr.String(), "DIGBENCH PROGRESS // LEVEL 2/4 // BEATEN 1 // STEPS 9 // STATUS in_progress // ELAPSED 1.3s") {
		t.Fatalf("code=%d called=%v stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
	}
}

func TestFormatDigBenchProgressMilestones(t *testing.T) {
	tests := []struct {
		progress codex.DigBenchProgress
		want     string
	}{
		{progress: codex.DigBenchProgress{Phase: codex.DigBenchProgressSession, Level: 1, MaxLevel: 3}, want: "DIGBENCH SESSION CREATED // LEVEL 1/3"},
		{progress: codex.DigBenchProgress{Phase: codex.DigBenchProgressTurn, Level: 1, ActualModel: "gpt-5.6-sol"}, want: "CODEX TURN STARTED // LEVEL 1 // BEATEN 0 // STEPS 0 // MODEL gpt-5.6-sol"},
		{progress: codex.DigBenchProgress{Phase: codex.DigBenchProgressHeartbeat, Elapsed: 15 * time.Second}, want: "DIGBENCH WORKING // LEVEL 0 // BEATEN 0 // STEPS 0 // ELAPSED 15s"},
	}
	for _, test := range tests {
		if got := formatDigBenchProgress(test.progress); !strings.Contains(got, test.want) {
			t.Errorf("formatDigBenchProgress(%#v) = %q, want containing %q", test.progress, got, test.want)
		}
	}
}

func TestRunDigBenchPrefersDedicatedBenchmarkAPIKey(t *testing.T) {
	t.Setenv("DIGBENCH_API_TOKEN", "digbench-secret")
	t.Setenv("CODEXOMETER_BENCHMARK_API_KEY", "benchmark-secret")
	t.Setenv("OPENAI_API_KEY", "fallback-secret")
	var stdout, stderr bytes.Buffer
	deps := dependencies{
		runDigBench: func(_ context.Context, _, _, apiKey string, _ codex.DigBenchOptions) (codex.DigBenchResult, error) {
			if apiKey != "benchmark-secret" {
				t.Fatalf("benchmark API key preference was not honored")
			}
			if os.Getenv("CODEXOMETER_BENCHMARK_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != "" {
				t.Fatal("benchmark API key remained in the inherited environment")
			}
			return codex.DigBenchResult{Game: "P-1", Won: true}, nil
		},
	}
	if code := run([]string{"--digbench-game", "P-1"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "API-key usage-based billing from CODEXOMETER_BENCHMARK_API_KEY") {
		t.Fatalf("missing API billing notice: %q", stderr.String())
	}
}

func TestRunPassesOpenAIAPIKeyToUIBenchmarks(t *testing.T) {
	t.Setenv("CODEXOMETER_BENCHMARK_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	var stdout, stderr bytes.Buffer
	deps := dependencies{
		startUI: func(fetcher ui.Fetcher, _ time.Duration, _ bool, _ bool) error {
			client, ok := fetcher.(codex.Client)
			if !ok || client.BenchmarkAPIKey != "openai-secret" {
				t.Fatalf("benchmark API key was not attached to Codex client")
			}
			if os.Getenv("OPENAI_API_KEY") != "" {
				t.Fatal("OPENAI_API_KEY remained in the inherited environment")
			}
			return nil
		},
	}
	if code := run(nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunPassesDigBenchTokenToUIWithoutLeavingItInEnvironment(t *testing.T) {
	t.Setenv("DIGBENCH_API_TOKEN", "digbench-secret")
	var stdout, stderr bytes.Buffer
	deps := dependencies{
		listDigBenchGames: func(_ context.Context, token string) ([]string, error) {
			if token != "digbench-secret" {
				t.Fatalf("game discovery token = %q", token)
			}
			return []string{"P-2", "P-1", "P-2"}, nil
		},
		startUI: func(fetcher ui.Fetcher, _ time.Duration, _ bool, _ bool) error {
			client, ok := fetcher.(codex.Client)
			if !ok || client.DigBenchToken != "digbench-secret" {
				t.Fatalf("DigBench token was not attached to Codex client")
			}
			if len(client.DigBenchGames) != 2 || client.DigBenchGames[0] != "P-2" || client.DigBenchGames[1] != "P-1" {
				t.Fatalf("DigBench games were not normalized: %#v", client.DigBenchGames)
			}
			if os.Getenv("DIGBENCH_API_TOKEN") != "" {
				t.Fatal("DIGBENCH_API_TOKEN remained in the inherited environment")
			}
			return nil
		},
	}
	if code := run(nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestNormalizeDigBenchGamesRejectsTerminalControls(t *testing.T) {
	games := normalizeDigBenchGames([]string{
		" P-1 ", "P-1", "P-2\nFORGED", "P-3\x1b]52;c;payload\a", "P-4\u0085FORGED", "P-5",
	})
	want := []string{"P-1", "P-5"}
	if len(games) != len(want) || games[0] != want[0] || games[1] != want[1] {
		t.Fatalf("normalized games = %#v, want %#v", games, want)
	}
}

func TestRunDigBenchRequiresToken(t *testing.T) {
	t.Setenv("DIGBENCH_API_TOKEN", "")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--digbench-game", "P-1"}, &stdout, &stderr, dependencies{})
	if code != 1 || !strings.Contains(stderr.String(), "DIGBENCH_API_TOKEN is required") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDefaultDependenciesAreConfigured(t *testing.T) {
	deps := defaultDependencies()
	if deps.checkAuth == nil || deps.listDigBenchGames == nil || deps.runDigBench == nil || deps.startUI == nil {
		t.Fatal("default dependencies are incomplete")
	}
}

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

func TestRunStartsDemoWithSelectedOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	deps := dependencies{
		startUI: func(fetcher ui.Fetcher, refresh time.Duration, inline bool) error {
			called = true
			if refresh != 30*time.Second || !inline {
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
	code := run([]string{"--demo", "--inline", "--refresh", "30s"}, &stdout, &stderr, deps)
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
		startUI: func(ui.Fetcher, time.Duration, bool) error { return errors.New("terminal unavailable") },
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
		runDigBench: func(_ context.Context, binary, token string, options codex.DigBenchOptions) (codex.DigBenchResult, error) {
			called = true
			if value := os.Getenv("DIGBENCH_API_TOKEN"); value != "" {
				t.Fatalf("DigBench token remained in child environment: %q", value)
			}
			if binary != "/custom/codex" || token != "secret" || options.Game != "P-3" || options.Model != "gpt-5.6-terra" || options.Effort != "medium" || options.Timeout != 2*time.Minute {
				t.Fatalf("binary=%q token=%q options=%#v", binary, token, options)
			}
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
	if code != 0 || !called || !strings.Contains(stdout.String(), "DIGBENCH P-3 // WIN // LEVELS 4/4") || !strings.Contains(stderr.String(), "persisted remote session") {
		t.Fatalf("code=%d called=%v stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
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
	if deps.checkAuth == nil || deps.runDigBench == nil || deps.startUI == nil {
		t.Fatal("default dependencies are incomplete")
	}
}

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/ui"
)

func TestRunVersion(t *testing.T) {
	for _, flag := range []string{"-v", "--version"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{flag}, &stdout, &stderr, dependencies{})
		if code != 0 || stdout.String() != "codexometer v0.6.0\n" || stderr.Len() != 0 {
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
			snapshot, err := fetcher.Fetch(context.Background())
			if err != nil || len(snapshot.Meters()) != 2 {
				t.Fatalf("demo fetch returned %#v, %v", snapshot, err)
			}
			usageFetcher, ok := fetcher.(ui.TokenUsageFetcher)
			if !ok {
				t.Fatal("demo fetcher does not provide live token usage")
			}
			first, err := usageFetcher.FetchTokenUsage(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			second, err := usageFetcher.FetchTokenUsage(context.Background())
			if err != nil || second.TotalTokens <= first.TotalTokens {
				t.Fatalf("demo token usage did not advance: first=%#v second=%#v err=%v", first, second, err)
			}
			return nil
		},
	}
	code := run([]string{"--demo", "--inline", "--refresh", "30s"}, &stdout, &stderr, deps)
	if code != 0 || !called {
		t.Fatalf("code=%d called=%v stderr=%q", code, called, stderr.String())
	}
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

func TestDefaultDependenciesAreConfigured(t *testing.T) {
	deps := defaultDependencies()
	if deps.checkAuth == nil || deps.startUI == nil {
		t.Fatal("default dependencies are incomplete")
	}
}

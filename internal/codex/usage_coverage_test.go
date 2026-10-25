package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUsageCoverageDetectsFileOnlyConfigurationAndPrefersEffectiveState(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(project, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, ".codex", "config.toml")
	r := &LiveUsageReader{SessionsRoot: filepath.Join(home, "sessions")}
	sessions := []LiveUsageSession{{ID: "thread", WorkingDirectory: project, Active: true}}
	for _, config := range []string{"[features]\nasync_compaction = true\n", "[features.async_compaction]\nenabled = true\n", "[profiles.experimental.features]\nasync_compaction = true\n"} {
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		r.localFeatureChecks = nil
		if got := r.usageCoverage(context.Background(), sessions, nil); !strings.Contains(got, "background compaction") {
			t.Fatal("file-only risk ignored", got)
		}
		if got := r.usageCoverage(context.Background(), sessions, map[string]string{"thread": ""}); got != "" {
			t.Fatal("local setting overrode effective server state", got)
		}
	}
	if err := os.WriteFile(path, []byte("[features]\nasync_compaction = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r.localFeatureChecks = nil
	if got := r.usageCoverage(context.Background(), sessions, nil); got != "" {
		t.Fatal("disabled flag blocked accounting", got)
	}
}

func TestUsageCoverageKeepsKnownRiskAcrossDisconnectAndIncludesChildren(t *testing.T) {
	home := t.TempDir()
	r := &LiveUsageReader{SessionsRoot: filepath.Join(home, "sessions"), files: map[string]*rolloutCursor{"child": {threadID: "child", workingDirectory: home, lastModified: time.Now()}}}
	sessions := []LiveUsageSession{{ID: "root", WorkingDirectory: home, Active: true}}
	for _, state := range []map[string]string{{"child": asyncCompactionCoverage}, nil} {
		if got := r.usageCoverage(context.Background(), sessions, state); !strings.Contains(got, "child") || !strings.Contains(got, "background compaction") {
			t.Fatal("child coverage vanished", got)
		}
	}
	if got := r.usageCoverage(context.Background(), sessions, map[string]string{"child": ""}); got != "" {
		t.Fatal("verified recovery retained stale risk", got)
	}
}

func TestUsageCoverageRejectsUnreadableConfigurationAndPartialDiscovery(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	r := &LiveUsageReader{SessionsRoot: filepath.Join(home, "sessions")}
	if err := os.WriteFile(path, []byte("malformed = ["), 0600); err != nil {
		t.Fatal(err)
	}
	sessions := []LiveUsageSession{{ID: "thread", Active: true}}
	if got := r.usageCoverage(context.Background(), sessions, nil); !strings.Contains(got, "could not be verified") {
		t.Fatal("invalid configuration silently accepted", got)
	}
	r.discoveryCoverageIssue = "compressed rollout history could not be verified"
	if got := r.usageCoverage(context.Background(), nil, nil); got != r.discoveryCoverageIssue {
		t.Fatal("partial discovery hidden", got)
	}
}

func TestUsageCoverageRetainsInactiveSessionRiskUntilVerifiedRecovery(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(map[bool]string{false: "root", true: "old folded child"}[child], func(t *testing.T) {
			home := t.TempDir()
			r := &LiveUsageReader{SessionsRoot: filepath.Join(home, "sessions")}
			sessions := []LiveUsageSession{{ID: "root", WorkingDirectory: home, Active: true, TotalTokens: 1100}}
			id := "root"
			if child {
				id = "child"
				r.files = map[string]*rolloutCursor{"child": {threadID: id, parentThreadID: "root", workingDirectory: home, lastModified: time.Now().Add(-48 * time.Hour), observedTokens: 1100}}
			}
			if got := r.usageCoverage(context.Background(), sessions, map[string]string{id: asyncCompactionCoverage}); !strings.Contains(got, "background compaction") {
				t.Fatal("known risk missing", got)
			}
			// An idle, previously observed row remains visible, but loses Active once
			// daemon/writer signals disappear. That is not verified feature recovery.
			sessions[0].Active = false
			for range 2 {
				got := r.usageCoverage(context.Background(), sessions, nil)
				if !strings.Contains(got, id) || !strings.Contains(got, "background compaction") {
					t.Fatal("inactive/disconnected row hid cached risk", got)
				}
			}
			if got := r.usageCoverage(context.Background(), sessions, map[string]string{id: ""}); got != "" {
				t.Fatal("verified recovery did not clear inactive risk", got)
			}
			if got := r.usageCoverage(context.Background(), sessions, nil); got != "" {
				t.Fatal("recovered risk reappeared", got)
			}
		})
	}
}

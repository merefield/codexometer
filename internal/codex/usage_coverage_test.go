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

func TestUsageCoverageIgnoresNonDirectoryOptionalConfigAndKeepsScanning(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "working directory", true: "ancestor directory"}[nested], func(t *testing.T) {
			home := t.TempDir()
			project := filepath.Join(home, "workspace")
			cwd := project
			if nested {
				cwd = filepath.Join(project, "nested")
			}
			if err := os.MkdirAll(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(project, ".codex")
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				t.Fatal(err)
			}
			r := &LiveUsageReader{SessionsRoot: filepath.Join(home, "sessions")}
			sessions := []LiveUsageSession{{ID: "thread", WorkingDirectory: cwd, Active: true}}
			if got := r.usageCoverage(context.Background(), sessions, nil); got != "" {
				t.Fatal("a .codex file was mistaken for inaccessible optional configuration", got)
			}
			// A blocked path is absence, not permission to stop scanning: a real
			// config in a higher ancestor must still qualify coverage.
			ancestor := filepath.Join(home, ".codex")
			if err := os.Mkdir(ancestor, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ancestor, "config.toml"), []byte("[features]\nasync_compaction = true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if got := r.usageCoverage(context.Background(), sessions, nil); !strings.Contains(got, "background compaction") {
				t.Fatal("non-directory path hid a real ancestor configuration", got)
			}
		})
	}
}

func TestUsageCoverageDoesNotCacheNonDirectoryOptionalConfig(t *testing.T) {
	home := t.TempDir()
	marker := filepath.Join(home, ".codex")
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	r := &LiveUsageReader{}
	path := filepath.Join(marker, "config.toml")
	if enabled, issue := r.localAsyncCompaction(path, "thread"); enabled || issue != "" {
		t.Fatal("non-directory optional config should be absent", enabled, issue)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(marker, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[features]\nasync_compaction = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if enabled, issue := r.localAsyncCompaction(path, "thread"); !enabled || issue != "" {
		t.Fatal("real config added later was hidden by cached absence", enabled, issue)
	}
}

func TestUsageCoveragePermissionDeniedConfigStillBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[features]\nasync_compaction = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	file, err := os.Open(path)
	if err == nil {
		file.Close()
		t.Skip("current user/OS can read despite mode bits; cannot exercise denied read")
	}
	if !os.IsPermission(err) {
		t.Fatal("expected an actual permission failure", err)
	}
	r := &LiveUsageReader{}
	if _, issue := r.localAsyncCompaction(path, "thread"); issue != "background usage configuration could not be read" {
		t.Fatal("permission failure was incorrectly treated as optional absence", issue)
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

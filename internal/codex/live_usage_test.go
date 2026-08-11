package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveUsageReaderBaselinesAndConsumesAppendedTelemetry(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "baseline")
	writeRollout(t, path, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}

	initial, err := reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if initial.TotalTokens != 0 {
		t.Fatalf("historical baseline counted as new usage: %#v", initial)
	}

	appendRollout(t, path,
		`{"timestamp":"ignored","type":"response_item","payload":{"content":"private text is ignored"}}`+"\n"+
			tokenCountLine(now, 150)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 50 || usage.LastActivity.IsZero() || usage.SessionCount != 1 {
		t.Fatalf("unexpected first live delta: %#v", usage)
	}

	appendRollout(t, path, tokenCountLine(now.Add(time.Second), 150)+"\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 50 {
		t.Fatalf("repeated cumulative total changed usage: %#v, %v", usage, err)
	}

	partial := tokenCountLine(now.Add(2*time.Second), 225)
	appendRollout(t, path, partial)
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 50 {
		t.Fatalf("partial JSONL record was consumed: %#v, %v", usage, err)
	}
	appendRollout(t, path, "\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 125 {
		t.Fatalf("completed JSONL record was not consumed: %#v, %v", usage, err)
	}
}

func TestLiveUsageReaderHandlesNewOldAndResetSessions(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	baselinePath := testRolloutPath(t, home, now.Add(-time.Hour), "known")
	writeRollout(t, baselinePath, tokenCountLine(now.Add(-time.Hour), 500)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	newPath := testRolloutPath(t, home, now.Add(2*time.Second), "new")
	writeRollout(t, newPath, tokenCountLine(now.Add(2*time.Second), 40)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 40 {
		t.Fatalf("new session usage = %#v, %v; want 40", usage, err)
	}

	oldPath := testRolloutPath(t, home, now.Add(-2*time.Hour), "resumed")
	writeRollout(t, oldPath, tokenCountLine(now.Add(-2*time.Hour), 900)+"\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 40 {
		t.Fatalf("late-discovered old baseline counted historical usage: %#v, %v", usage, err)
	}
	appendRollout(t, oldPath, tokenCountLine(now.Add(3*time.Second), 925)+"\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 65 {
		t.Fatalf("resumed session delta = %#v, %v; want 65", usage, err)
	}

	appendRollout(t, baselinePath, tokenCountLine(now.Add(4*time.Second), 10)+"\n")
	appendRollout(t, baselinePath, tokenCountLine(now.Add(5*time.Second), 35)+"\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 90 {
		t.Fatalf("counter reset was mishandled: %#v, %v", usage, err)
	}
}

func TestFreshLiveUsageReadNoticesAResumedDormantSession(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	created := now.Add(-72 * time.Hour)
	path := testRolloutPath(t, home, created, "dormant")
	writeRollout(t, path, tokenCountLine(created, 1_000)+"\n")
	if err := os.Chtimes(path, created, created); err != nil {
		t.Fatal(err)
	}
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	appendRollout(t, path, tokenCountLine(now, 1_075)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 0 {
		t.Fatalf("ordinary read unexpectedly rediscovered dormant session: %#v, %v", usage, err)
	}
	usage, err = reader.FetchTokenUsageFresh(context.Background())
	if err != nil || usage.TotalTokens != 75 {
		t.Fatalf("fresh read resumed dormant session usage = %#v, %v; want 75", usage, err)
	}
}

func TestLiveUsageReaderExpiresInactiveSessionCount(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "inactive")
	writeRollout(t, path, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path, tokenCountLine(now, 125)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.SessionCount != 1 {
		t.Fatalf("active session count = %#v, %v; want one", usage, err)
	}

	inactiveAt := now.Add(-6 * time.Minute)
	if err := os.Chtimes(path, inactiveAt, inactiveAt); err != nil {
		t.Fatal(err)
	}
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.SessionCount != 0 {
		t.Fatalf("inactive session count = %#v, %v; want zero", usage, err)
	}
}

func TestLiveUsageReaderKeepsNewestActivityAcrossEvents(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := testRolloutPath(t, home, now.Add(-time.Hour), "activity-order")
	writeRollout(t, path, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path, tokenCountLine(now, 150)+"\n"+tokenCountLine(now.Add(-time.Minute), 175)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || !usage.LastActivity.Equal(now) {
		t.Fatalf("last activity = %s, %v; want newest %s", usage.LastActivity, err, now)
	}
}

func TestLiveUsageReaderContinuesPastUnreadableCursor(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "readable")
	writeRollout(t, path, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	badPath := filepath.Join(home, "unreadable-rollout")
	if err := os.Mkdir(badPath, 0o700); err != nil {
		t.Fatal(err)
	}
	probe := &rolloutCursor{lastModified: now}
	if err := reader.consume(badPath, probe); err == nil {
		t.Skip("platform permits directory cursor reads")
	}
	reader.files[badPath] = &rolloutCursor{lastModified: now}
	appendRollout(t, path, tokenCountLine(now, 150)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 50 {
		t.Fatalf("readable telemetry was lost beside bad cursor: %#v, %v", usage, err)
	}
}

func TestLiveUsageReaderIgnoresMalformedAndNullTokenEvents(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "malformed")
	writeRollout(t, path, "{not json token_count}\n"+
		`{"type":"event_msg","payload":{"type":"token_count","info":null}}`+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 0 {
		t.Fatalf("malformed events affected usage: %#v, %v", usage, err)
	}
}

func TestLatestTokenTotalSearchesBackwardAcrossLargeRecords(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now, "large-tail")
	writeRollout(t, path, tokenCountLine(now, 4321)+"\n"+
		`{"type":"response_item","payload":{"text":"`+strings.Repeat("x", 130_000)+`"}}`+"\n")
	total, err := latestTokenTotal(path)
	if err != nil || total != 4321 {
		t.Fatalf("latestTokenTotal() = %d, %v; want 4321", total, err)
	}
}

func TestLiveUsageReaderHonorsCodexHomeAndCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	resolved, err := DefaultCodexHome()
	if err != nil || resolved != filepath.Clean(home) {
		t.Fatalf("DefaultCodexHome() = %q, %v", resolved, err)
	}
	reader, err := NewLiveUsageReader("")
	if err != nil || reader.SessionsRoot != filepath.Join(home, "sessions") {
		t.Fatalf("reader root = %q, %v", reader.SessionsRoot, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.FetchTokenUsage(ctx); err == nil {
		t.Fatal("cancelled discovery returned no error")
	}
}

func tokenCountLine(at time.Time, total int64) string {
	return fmt.Sprintf(
		`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d},"last_token_usage":{"total_tokens":%d}}}}`,
		at.UTC().Format(time.RFC3339Nano), total, total,
	)
}

func testRolloutPath(t *testing.T, home string, created time.Time, suffix string) string {
	t.Helper()
	directory := filepath.Join(home, "sessions", created.Format("2006"), created.Format("01"), created.Format("02"))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rollout-" + created.In(time.Local).Format("2006-01-02T15-04-05") + "-" + suffix + ".jsonl"
	return filepath.Join(directory, name)
}

func writeRollout(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendRollout(t *testing.T, path, contents string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(contents); err != nil {
		t.Fatal(err)
	}
}

func TestRolloutHelpers(t *testing.T) {
	now := time.Now()
	name := "rollout-" + now.Add(time.Second).Format("2006-01-02T15-04-05") + "-id.jsonl"
	if !isRolloutFile(name) || !rolloutCreatedAfter(name, now) {
		t.Fatalf("valid rollout was not recognized: %s", name)
	}
	if isRolloutFile("notes.jsonl") || rolloutCreatedAfter("rollout-bad.jsonl", now) {
		t.Fatal("invalid rollout was recognized")
	}
	if _, _, ok := tokenTotal([]byte(strings.ReplaceAll(tokenCountLine(now, 7), "event_msg", "other"))); ok {
		t.Fatal("non-event token payload was accepted")
	}
}

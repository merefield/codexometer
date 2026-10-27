package codex

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRolloutActivityBootstrapUsesEventTimeWithStaleModTime(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	old := now.Add(-72 * time.Hour)
	home := t.TempDir()
	path := testRolloutPath(t, home, old, "growing")
	writeRollout(t, path, sessionMetaLine("growing", `"cli"`, "/work", nil)+"\n"+
		attentionEventLine(now.Add(-time.Minute), "task_started", nil)+"\n"+
		responseItemLine(now.Add(-time.Second), "reasoning")+"\n")
	freezeRolloutTime(t, path, old)
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	r.statusProvider = stubSessionStatusProvider{unavailable: true}
	usage, err := r.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 0 {
		t.Fatalf("bootstrap counted tokens or failed: %+v, %v", usage, err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionNone, true)
	if !r.files[path].activityTime().Equal(now.Add(-time.Second)) {
		t.Fatalf("bootstrap activity = %v", r.files[path].activityTime())
	}
}

func TestRolloutActivityBootstrapOversizedLatestRecord(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	for _, kind := range []string{"function_call_output", "reasoning"} {
		t.Run(kind, func(t *testing.T) {
			old := now.Add(-72 * time.Hour)
			home := t.TempDir()
			path := testRolloutPath(t, home, old, "growing")
			payload := map[string]any{"type": kind, "output": strings.Repeat("x", rolloutActivityTailBytes+1)}
			line, err := json.Marshal(map[string]any{"timestamp": now, "type": "response_item", "payload": payload})
			if err != nil {
				t.Fatal(err)
			}
			writeRollout(t, path, sessionMetaLine("growing", `"cli"`, "/work", nil)+"\n"+
				attentionEventLine(old, "task_started", nil)+"\n"+string(line)+"\n")
			freezeRolloutTime(t, path, old)
			r, err := NewLiveUsageReader(home)
			if err != nil {
				t.Fatal(err)
			}
			r.now = func() time.Time { return now }
			r.statusProvider = stubSessionStatusProvider{unavailable: true}
			usage, err := r.FetchTokenUsage(context.Background())
			if err != nil || usage.TotalTokens != 0 {
				t.Fatalf("bootstrap = %+v, %v", usage, err)
			}
			if !r.files[path].lastEventAt.Equal(now) {
				t.Fatalf("latest owned event lost: %v", r.files[path].lastEventAt)
			}
			assertRolloutAttention(t, r, now, SessionAttentionNone, true)
		})
	}
}

func TestRolloutActivityBootstrapKeepsRecordAtExactTailBoundary(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	path := testRolloutPath(t, t.TempDir(), now, "boundary")
	recent := responseItemLine(now, "reasoning") + "\n"
	// The first recent record begins exactly at size-tailTarget. The rest of
	// the tail is an unfinished record and must not hide the complete one.
	data := "{}\n" + recent + strings.Repeat(" ", rolloutActivityTailBytes-len(recent))
	writeRollout(t, path, data)
	c := &rolloutCursor{}
	if err := c.bootstrapActivity(context.Background(), path, int64(len(data)), now); err != nil {
		t.Fatal(err)
	}
	if !c.lastEventAt.Equal(now) {
		t.Fatalf("boundary event dropped: %v", c.lastEventAt)
	}
}

func TestRolloutActivityBootstrapOversizedPartialRecord(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	path := testRolloutPath(t, t.TempDir(), now, "partial")
	line, err := json.Marshal(map[string]any{"timestamp": now, "type": "response_item", "payload": map[string]any{
		"type": "reasoning", "text": strings.Repeat("x", rolloutActivityTailBytes+1),
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeRollout(t, path, string(line))
	c := &rolloutCursor{}
	if err := c.bootstrapActivity(context.Background(), path, int64(len(line)), now); err != nil {
		t.Fatal(err)
	}
	if !c.lastEventAt.IsZero() {
		t.Fatalf("unterminated oversized event accepted: %v", c.lastEventAt)
	}
}

func TestRolloutActivityBootstrapRespectsOrdinalBoundary(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	path := testRolloutPath(t, t.TempDir(), now, "ordinal")
	line := `{"timestamp":"2026-10-23T09:00:00Z","ordinal":9,"type":"response_item","payload":{"type":"reasoning"}}` + "\n"
	writeRollout(t, path, line)
	boundary := uint64(10)
	c := &rolloutCursor{nonRoot: true, subagentHistoryStartOrdinal: &boundary}
	if err := c.bootstrapActivity(context.Background(), path, int64(len(line)), now); err != nil {
		t.Fatal(err)
	}
	if !c.lastEventAt.IsZero() {
		t.Fatalf("inherited ordinal accepted: %v", c.lastEventAt)
	}
}

func TestRolloutActivityGrowthClearsCheckAndUnchangedPollsExpire(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	old := now.Add(-72 * time.Hour)
	home := t.TempDir()
	path := testRolloutPath(t, home, old, "growing")
	writeRollout(t, path, sessionMetaLine("growing", `"cli"`, "/work", nil)+"\n"+
		attentionEventLine(old, "task_started", nil)+"\n")
	freezeRolloutTime(t, path, old)
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	r.statusProvider = stubSessionStatusProvider{unavailable: true}
	if _, err := r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A real open writer would produce CHECK while its old log is quiet.
	r.files[path].attention = sessionAttentionWorking
	assertRolloutAttention(t, r, now, SessionAttentionCheck, true)
	// A complete event arrives with no token-count update or mtime change.
	appendRollout(t, path, responseItemLine(now, "reasoning")+"\n")
	freezeRolloutTime(t, path, old)
	usage, err := r.FetchTokenUsageFresh(context.Background())
	if err != nil || usage.TotalTokens != 0 {
		t.Fatalf("growth counted tokens or failed: %+v, %v", usage, err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionNone, true)
	// Re-reading/discovery of unchanged bytes must not keep the log fresh.
	now = now.Add(fallbackAttentionAfter + time.Second)
	if _, err := r.FetchTokenUsageFresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionCheck, true)
}

func TestRolloutActivityPartialRecordRefreshesOnlyWhenSizeChanges(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	home := t.TempDir()
	path := testRolloutPath(t, home, old, "growing")
	writeRollout(t, path, sessionMetaLine("growing", `"cli"`, "/work", nil)+"\n")
	freezeRolloutTime(t, path, old)
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	r.statusProvider = stubSessionStatusProvider{unavailable: true}
	if _, err := r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.files[path].attention = sessionAttentionWorking
	appendRollout(t, path, `{"timestamp":`)
	freezeRolloutTime(t, path, old)
	if _, err := r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionNone, true)
	now = now.Add(fallbackAttentionAfter + time.Second)
	if _, err := r.FetchTokenUsageFresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionCheck, true)
}

func TestRolloutActivityPreservesDefiniteRequests(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		kind string
		want SessionAttention
	}{
		{"request_user_input", SessionAttentionInput},
		{"exec_approval_request", SessionAttentionApproval},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			home := t.TempDir()
			old := now.Add(-time.Hour)
			path := testRolloutPath(t, home, old, "growing")
			writeRollout(t, path, sessionMetaLine("growing", `"cli"`, "/work", nil)+"\n"+
				attentionEventLine(old, tc.kind, nil)+"\n")
			freezeRolloutTime(t, path, old)
			r, err := NewLiveUsageReader(home)
			if err != nil {
				t.Fatal(err)
			}
			r.now = func() time.Time { return now }
			r.statusProvider = stubSessionStatusProvider{unavailable: true}
			if _, err := r.FetchTokenUsage(context.Background()); err != nil {
				t.Fatal(err)
			}
			appendRollout(t, path, responseItemLine(now, "reasoning")+"\n")
			freezeRolloutTime(t, path, old)
			if _, err := r.FetchTokenUsage(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertRolloutAttention(t, r, now, tc.want, false)
		})
	}
}

func TestRolloutActivityIgnoresFutureInheritedAndPartialEvents(t *testing.T) {
	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, data string
		child      bool
	}{
		{"future", responseItemLine(now.Add(time.Hour), "reasoning") + "\n", false},
		{"inherited", responseItemLine(now.Add(-time.Hour), "reasoning") + "\n", true},
		{"partial", responseItemLine(now, "reasoning"), false},
		{"malformed", "not JSON\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := testRolloutPath(t, t.TempDir(), now, tc.name)
			writeRollout(t, path, tc.data)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			c := &rolloutCursor{nonRoot: tc.child, startedAt: now}
			if err := c.bootstrapActivity(context.Background(), path, info.Size(), now); err != nil {
				t.Fatal(err)
			}
			if !c.lastEventAt.IsZero() {
				t.Fatalf("unreliable activity accepted: %v", c.lastEventAt)
			}
		})
	}
}

func freezeRolloutTime(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func assertRolloutAttention(t *testing.T, r *LiveUsageReader, now time.Time, want SessionAttention, working bool) {
	t.Helper()
	sessions, _, _ := r.sessionSnapshots(now, map[string]struct{}{"growing": {}}, true, nil)
	if len(sessions) != 1 || sessions[0].Attention != want || sessions[0].Working != working {
		t.Fatalf("sessions = %+v; want attention %v, working %v", sessions, want, working)
	}
}

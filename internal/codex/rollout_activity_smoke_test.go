package codex

import (
	"context"
	"os"
	"testing"
)

// Explicit opt-in, read-only check against an already running local session.
// It never starts an app-server, a model turn, or a tool in that session.
func TestLiveRolloutActivitySmoke(t *testing.T) {
	home, thread := os.Getenv("CODEXOMETER_ACTIVITY_SMOKE_HOME"), os.Getenv("CODEXOMETER_ACTIVITY_SMOKE_THREAD")
	if home == "" || thread == "" {
		t.Skip("set both CODEXOMETER_ACTIVITY_SMOKE_HOME and CODEXOMETER_ACTIVITY_SMOKE_THREAD")
	}
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.statusProvider = stubSessionStatusProvider{unavailable: true}
	if _, err := r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.files {
		if c.threadID != thread {
			continue
		}
		now := r.activityNow()
		t.Logf("filesystem=%s event=%s detected=%s age=%s", c.lastModified, c.lastEventAt, c.activityTime(), now.Sub(c.activityTime()))
		if now.Sub(c.lastModified) < fallbackAttentionAfter {
			t.Skip("filesystem timestamp is fresh; stale-timestamp case not present")
		}
		if now.Sub(c.lastEventAt) >= fallbackAttentionAfter {
			t.Skip("no fresh persisted event available in this session")
		}
		sessions, _, _ := r.sessionSnapshots(now, map[string]struct{}{thread: {}}, true, nil)
		for _, s := range sessions {
			if s.ID == thread {
				t.Logf("session=%s active=%v working=%v attention=%v", thread, s.Active, s.Working, s.Attention)
				if s.Attention == SessionAttentionCheck {
					t.Fatal("fresh actual events still produced CHECK SESSION")
				}
				return
			}
		}
		t.Fatal("session was hidden despite fresh events")
	}
	t.Fatal("requested session log not found")
}

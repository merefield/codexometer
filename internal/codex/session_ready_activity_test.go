package codex

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Ready activity is independent of how recently the rollout was written.
func TestLiveUsageReaderQuietReadyActivityFallsBackAfterDisconnect(t *testing.T) {
	home := t.TempDir()
	old := time.Now().Add(-time.Hour)
	path := testRolloutPath(t, home, old, "ready")
	writeRollout(t, path, sessionMetaLine("ready", `"cli"`, "/work", nil)+"\n")
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.statusProvider = stubSessionStatusProvider{statuses: map[string]sessionRuntimeStatus{"ready": sessionRuntimeIdle}}
	usage, err := r.FetchTokenUsage(context.Background())
	if err != nil || len(usage.Sessions) != 1 || !usage.Sessions[0].Active || usage.SessionCount != 1 || usage.Sessions[0].Working {
		t.Fatalf("confirmed ready session absent or misclassified: %+v, %v", usage, err)
	}
	r.statusProvider = stubSessionStatusProvider{unavailable: true}
	usage, err = r.FetchTokenUsage(context.Background())
	if err != nil || usage.SessionCount != 0 {
		t.Fatalf("disconnected daemon retained ready activity: %+v, %v", usage, err)
	}
}

func TestSessionSnapshotsQuietActivityUsesCurrentDaemonStatus(t *testing.T) {
	now := time.Now()
	for _, tokens := range []int64{0, 100} {
		for _, tc := range []struct {
			name      string
			statuses  map[string]sessionRuntimeStatus
			active    bool
			working   bool
			attention SessionAttention
		}{
			{"ready", map[string]sessionRuntimeStatus{"root": sessionRuntimeIdle}, true, false, SessionAttentionNone},
			{"working", map[string]sessionRuntimeStatus{"root": sessionRuntimeWorking}, true, true, SessionAttentionNone},
			{"complete", map[string]sessionRuntimeStatus{"root": sessionRuntimeComplete}, true, false, SessionAttentionComplete},
			{"input", map[string]sessionRuntimeStatus{"root": sessionRuntimeInput}, true, false, SessionAttentionInput},
			{"approval", map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval}, true, false, SessionAttentionApproval},
			{"unknown", map[string]sessionRuntimeStatus{"root": sessionRuntimeUnknown}, false, false, SessionAttentionNone},
			{"unloaded", nil, false, false, SessionAttentionNone},
		} {
			t.Run(fmt.Sprintf("%s/tokens=%d", tc.name, tokens), func(t *testing.T) {
				r := &LiveUsageReader{files: map[string]*rolloutCursor{
					"root": {threadID: "root", lastModified: now.Add(-8 * time.Hour), observedTokens: tokens},
				}}
				sessions, active, working := r.sessionSnapshots(now, nil, true, tc.statuses)
				if !tc.active && tokens == 0 {
					if len(sessions) != 0 || active != 0 || working {
						t.Fatal("unobserved historical session was promoted to active", sessions)
					}
					return
				}
				wantCount := 0
				if tc.active {
					wantCount = 1
				}
				if len(sessions) != 1 || active != wantCount || working != tc.working {
					t.Fatalf("sessions=%+v active=%d working=%v", sessions, active, working)
				}
				if sessions[0].Active != tc.active || sessions[0].Working != tc.working || sessions[0].Attention != tc.attention {
					t.Fatalf("unexpected activity/attention: %+v", sessions[0])
				}
			})
		}
	}
}

func TestSessionSnapshotsReadyActivityClearsWithoutCurrentDaemonStatus(t *testing.T) {
	now := time.Now()
	r := &LiveUsageReader{files: map[string]*rolloutCursor{
		"root": {threadID: "root", lastModified: now.Add(-time.Hour), observedTokens: 100},
	}}
	sessions, active, _ := r.sessionSnapshots(now, nil, true, map[string]sessionRuntimeStatus{"root": sessionRuntimeIdle})
	if len(sessions) != 1 || !sessions[0].Active || active != 1 {
		t.Fatal("confirmed ready session was not active", sessions)
	}
	// Disconnection or an unloaded thread removes its exact status. Retaining
	// historical token data must not retain the earlier proof of activity.
	sessions, active, working := r.sessionSnapshots(now.Add(time.Minute), nil, true, nil)
	if len(sessions) != 1 || sessions[0].Active || active != 0 || working {
		t.Fatal("ready activity survived without current confirmation", sessions)
	}
}

func TestSessionSnapshotsOneReadySessionAndThreeChecks(t *testing.T) {
	now := time.Now()
	r := &LiveUsageReader{files: make(map[string]*rolloutCursor)}
	writers := make(map[string]struct{})
	for _, id := range []string{"ready", "check-1", "check-2", "check-3"} {
		r.files[id] = &rolloutCursor{threadID: id, lastModified: now.Add(-time.Hour), observedTokens: 100, attention: sessionAttentionWorking}
		writers[id] = struct{}{}
	}
	sessions, _, _ := r.sessionSnapshots(now, writers, true, map[string]sessionRuntimeStatus{"ready": sessionRuntimeIdle})
	active, checks := 0, 0
	for _, s := range sessions {
		if s.Attention == SessionAttentionCheck {
			checks++
		} else if s.Active {
			active++
		}
		if s.ID == "ready" && (s.Working || s.Attention != SessionAttentionNone) {
			t.Fatal("confirmed idle was relabelled working or needing attention", s)
		}
	}
	if len(sessions) != 4 || checks != 3 || active != 1 {
		t.Fatalf("sessions=%d check=%d active=%d; want 4/3/1", len(sessions), checks, active)
	}
}

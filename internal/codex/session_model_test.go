package codex

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSessionModelSettingsRestoreAndLiveChanges(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now, "settings")
	settings := func(model, effort, tier string) string {
		return fmt.Sprintf(`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":%q,"reasoning_effort":%q,"service_tier":%q}}}`, model, effort, tier)
	}
	turn := `{"type":"turn_context","payload":{"model":"gpt-6-astra","effort":"medium"}}`
	writeRollout(t, path, sessionMetaLine("root", `"cli"`, "/work/root", nil)+"\n"+settings("gpt-6-astra", "high", "fast")+"\n"+turn+"\n"+tokenCountLine(now, 100)+"\n")
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want SessionModelSettings) {
		t.Helper()
		snap, err := r.FetchTokenUsage(context.Background())
		if err != nil || len(snap.Sessions) != 1 || snap.Sessions[0].ModelSettings != want {
			t.Fatalf("settings snapshot: %+v, %v; want %+v", snap.Sessions, err, want)
		}
	}
	check(SessionModelSettings{"gpt-6-astra", "medium", "fast"})
	appendRollout(t, path, settings("gpt-5.6-sol", "low", "")+"\n")
	check(SessionModelSettings{"gpt-5.6-sol", "low", "default"})
	// A fresh reader must restore the newer settings, not the preceding turn.
	r, err = NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	check(SessionModelSettings{"gpt-5.6-sol", "low", "default"})
	appendRollout(t, path, `{"type":"turn_context","payload":{"model":"gpt-5.6-sol"}}`+"\n")
	check(SessionModelSettings{"gpt-5.6-sol", "", "default"})
}

func TestSessionModelSettingsBelongToRoot(t *testing.T) {
	now := time.Now()
	want := SessionModelSettings{"root-model", "medium", "fast"}
	r := &LiveUsageReader{files: map[string]*rolloutCursor{
		"root":  {threadID: "root", lastModified: now, modelSettings: want},
		"child": {threadID: "child", parentThreadID: "root", lastModified: now, modelSettings: SessionModelSettings{"child-model", "high", "default"}},
	}}
	sessions, _, _ := r.sessionSnapshots(now, nil, false, nil)
	if len(sessions) != 1 || sessions[0].ModelSettings != want {
		t.Fatalf("child replaced root settings: %+v", sessions)
	}
	r.files["root"].modelSettings = SessionModelSettings{}
	sessions, _, _ = r.sessionSnapshots(now, nil, false, nil)
	if len(sessions) != 1 || sessions[0].ModelSettings.Model != "" {
		t.Fatal("guessed unknown root model from child")
	}
}

//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"testing"
)

func unreadReceipt(revision, turn string) map[string]any {
	return map[string]any{"revision": revision, "firstUnread": map[string]any{"type": "turn", "turnId": turn}}
}

func readStateDaemon(t *testing.T) (*daemonStatusProvider, *quotaDaemonFixture) {
	t.Helper()
	p, f := newQuotaDaemon(t)
	f.readReceipts = map[string]map[string]any{"one": unreadReceipt("v1", "t1")}
	f.catalogues = map[string]map[string]any{"thread/turns/list": {"data": []any{map[string]any{"id": "t1", "status": "completed", "items": []any{map[string]any{"type": "agentMessage", "phase": "final_answer", "text": "Finished"}}}}}}
	snapshot, ok := p.Fetch(context.Background(), []string{"one"})
	if !ok || snapshot.ReadStates["one"] != (SessionReadState{Revision: "v1", Unread: true}) {
		t.Fatal("receipt not observed", snapshot.ReadStates)
	}
	return p, f
}

func TestReadStateAcknowledgementUsesSavedRevisionAndSurvivesNewClient(t *testing.T) {
	p, f := readStateDaemon(t)
	next, err := p.AcknowledgeSessionResult(context.Background(), "one", "t1", "v1")
	if err != nil || !next.Known() || next.Unread {
		t.Fatal("read acknowledgement failed", next, err)
	}
	if _, err := p.AcknowledgeSessionResult(context.Background(), "one", "t1", "v1"); err == nil {
		t.Fatal("old receipt reused")
	}
	f.mu.Lock()
	if len(f.readUpdates) != 1 || f.readUpdates[0]["expectedRevision"] != "v1" || f.readUpdates[0]["operation"] != "read" || len(f.writes) != 0 {
		t.Fatal("unexpected mutation", f.readUpdates, f.writes)
	}
	f.mu.Unlock()
	fresh := &daemonStatusProvider{socketPath: p.socketPath}
	t.Cleanup(func() { fresh.disconnect(nil) })
	snapshot, ok := fresh.Fetch(context.Background(), []string{"one"})
	if !ok || snapshot.ReadStates["one"] != next {
		t.Fatal("new client did not observe saved receipt", snapshot.ReadStates, next)
	}
	p.mu.Lock()
	p.readStateNotificationLocked("thread/readState/changed", rawJSON(map[string]any{"threadId": "one", "readState": unreadReceipt("v2", "t2")}))
	p.mu.Unlock()
	cached, _ := p.cachedStatusSnapshot([]string{"one"}, p.lastStatusAt)
	if !cached.ReadStates["one"].Unread || cached.ReadStates["one"].Revision != "v2" {
		t.Fatal("shared notification lost", cached.ReadStates)
	}
}

func TestReadStateNeverAcknowledgesUnseenOrUnavailableResult(t *testing.T) {
	for _, mode := range []string{"old-revision", "old-turn", "wrong-thread", "missing-receipt", "conflict", "arrived-during-read", "disconnected"} {
		t.Run(mode, func(t *testing.T) {
			p, f := readStateDaemon(t)
			id, turn, revision := "one", "t1", "v1"
			switch mode {
			case "old-revision":
				revision = "older"
			case "old-turn":
				turn = "older"
			case "wrong-thread":
				id = "two"
			case "missing-receipt":
				p.mu.Lock()
				p.observeReadStateLocked("one", SessionReadState{})
				p.mu.Unlock()
			case "conflict":
				f.mu.Lock()
				f.readConflict = true
				f.mu.Unlock()
			case "arrived-during-read":
				f.mu.Lock()
				f.historyReadHook = func() { f.readReceipts["one"] = unreadReceipt("v2", "t2") }
				f.mu.Unlock()
			case "disconnected":
				p.disconnect(nil)
			}
			if _, err := p.AcknowledgeSessionResult(context.Background(), id, turn, revision); err == nil {
				t.Fatal("unseen result acknowledged")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			expected := 0
			if mode == "conflict" || mode == "arrived-during-read" {
				expected = 1
			}
			if len(f.readUpdates) != expected {
				t.Fatal("unexpected write or automatic retry", f.readUpdates)
			}
		})
	}
}

func TestReadStateUnknownReceiptRetainsCompatibility(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"revision":"r"}`, `{"revision":"r","firstUnread":{"type":"future"}}`} {
		var response struct{ ReadState *wireReadState }
		if err := json.Unmarshal([]byte(`{"readState":`+raw+`}`), &response); err != nil {
			t.Fatal(err)
		}
		if response.ReadState.snapshot().Known() {
			t.Fatal("unknown state treated as read", raw)
		}
	}
}

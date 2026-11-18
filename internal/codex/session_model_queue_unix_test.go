//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func queuedModel(t *testing.T) (*daemonStatusProvider, *quotaDaemonFixture) {
	t.Helper()
	p, f := newQuotaDaemon(t)
	f.mu.Lock()
	f.commandStatus = "active"
	f.mu.Unlock()
	m, err := p.SessionCommands(context.Background(), "one", "model/small")
	if err != nil || len(m.Choices) != 1 {
		t.Fatal(m, err)
	}
	if err := p.ExecuteSessionCommand(context.Background(), "one", m.Path, m.Revision, m.Choices[0].ID); err != nil {
		t.Fatal(err)
	}
	waitModelQueue(t, p, func(item *pendingModelChange) bool { return item != nil && !item.applying })
	return p, f
}

func waitModelQueue(t *testing.T, p *daemonStatusProvider, ready func(*pendingModelChange) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		ok := ready(p.pendingModels["one"])
		p.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("model queue did not settle")
}

func TestQueuedModelAppliesOnceAtIdleStop(t *testing.T) {
	p, f := queuedModel(t)
	f.mu.Lock()
	unchanged := len(f.writes) == 0 && f.sessions["one"].Model == "large"
	f.mu.Unlock()
	if !unchanged {
		t.Fatal("queued model changed the working turn")
	}
	f.mu.Lock()
	f.commandStatus = "idle"
	f.mu.Unlock()
	p.handleNotification("turn/completed", json.RawMessage(`{"threadId":"one","turn":{"id":"turn-one"}}`))
	waitModelQueue(t, p, func(item *pendingModelChange) bool { return item == nil })
	p.handleNotification("turn/completed", json.RawMessage(`{"threadId":"one","turn":{"id":"turn-one"}}`))
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 1 || f.sessions["one"].Model != "small" || f.sessions["one"].Effort != "medium" || f.sessions["two"].Model != "large" {
		t.Fatalf("wrong or repeated mutation: %+v %+v", f.writes, f.sessions)
	}
}

func TestIdleModelReselectionSupersedesOlderIntent(t *testing.T) {
	for _, state := range []string{"queued", "failed; reselect /model"} {
		t.Run(state, func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			f.mu.Lock()
			f.providerModels = map[string]map[string]any{"": {"data": []any{
				map[string]any{"model": "large", "defaultReasoningEffort": "high"},
				map[string]any{"model": "small", "defaultReasoningEffort": "medium"},
			}}}
			f.mu.Unlock()
			m, err := p.SessionCommands(t.Context(), "one", "model/large")
			if err != nil || len(m.Choices) != 1 || !m.Choices[0].Action {
				t.Fatal(m, err)
			}
			p.mu.Lock()
			old := &pendingModelChange{connection: p.connection, before: *m.settings,
				target:  QuotaSession{ID: "one", Model: "small", Effort: "medium"},
				params:  map[string]any{"threadId": "one", "model": "small", "effort": "medium", "serviceTier": nil},
				display: PendingModelChange{State: state}}
			p.pendingModels = map[string]*pendingModelChange{"one": old}
			p.mu.Unlock()

			// Re-select the original settings: a settings-change check alone
			// cannot distinguish this new intent from the older queued one.
			if err := p.ExecuteSessionCommand(t.Context(), "one", m.Path, m.Revision, m.Choices[0].ID); err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			remaining := p.pendingModels["one"]
			p.mu.Unlock()
			if remaining != nil {
				t.Fatal("older model receipt survived explicit reselection")
			}
			p.applyModelChange("one", old) // Even a worker already waiting for settingsMu loses.
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.writes) != 1 || f.sessions["one"].Model != "large" {
				t.Fatalf("obsolete selection overwrote newer intent: %+v", f.writes)
			}
		})
	}
}

func TestQueuedModelRejectsConflictAndNeverRetriesUnconfirmedWrite(t *testing.T) {
	for _, scenario := range []string{"conflict", "unconfirmed", "closed", "disconnected", "still-working"} {
		t.Run(scenario, func(t *testing.T) {
			p, f := queuedModel(t)
			f.mu.Lock()
			f.commandStatus = "idle"
			switch scenario {
			case "conflict":
				changed := f.sessions["one"]
				changed.Effort = "low"
				f.sessions["one"] = changed
			case "unconfirmed":
				f.queued = true
			case "still-working":
				f.commandStatus = "active"
			}
			f.mu.Unlock()
			if scenario == "closed" {
				p.CloseQuotaProfiles()
			}
			if scenario == "disconnected" {
				p.disconnect(nil)
			}
			p.flushModelChanges()
			waitModelQueue(t, p, func(item *pendingModelChange) bool { return item == nil || !item.applying })
			p.mu.Lock()
			item := p.pendingModels["one"]
			badState := scenario == "conflict" && (item == nil || !strings.HasPrefix(item.display.State, "failed")) || scenario == "unconfirmed" && (item == nil || !strings.HasPrefix(item.display.State, "unconfirmed"))
			p.mu.Unlock()
			if badState {
				t.Fatal("missing explicit failure state", item)
			}
			p.flushModelChanges()
			waitModelQueue(t, p, func(item *pendingModelChange) bool { return item == nil || !item.applying })
			f.mu.Lock()
			defer f.mu.Unlock()
			want := 0
			if scenario == "unconfirmed" {
				want = 1
			}
			if len(f.writes) != want {
				t.Fatalf("writes = %d, want %d", len(f.writes), want)
			}
		})
	}
}

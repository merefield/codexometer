package codex

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSteerMessageIdentity(t *testing.T) {
	o := SessionPromptOffer{Token: "single-use-secret", ThreadID: "root", TurnID: "turn", ClientUserMessageID: uuid.NewString()}
	id := o.SteerMessageID()
	if id == "" || id != o.SteerMessageID() || strings.Contains(id, o.Token) {
		t.Fatal("correlation identity is unstable or exposes the capability", id)
	}
	next := o
	next.ClientUserMessageID = uuid.NewString()
	if next.SteerMessageID() == id {
		t.Fatal("different submissions shared an identity")
	}
	for _, incomplete := range []SessionPromptOffer{{}, {Token: "token"}, {Token: "token", ThreadID: "root"}} {
		if incomplete.SteerMessageID() != "" {
			t.Fatal("incomplete offer manufactured a steer identity")
		}
	}
}

func TestCompletedSteerIdentityIsIndependentOfTaskAndGuidance(t *testing.T) {
	for _, initialTask := range []bool{false, true} {
		t.Run(fmt.Sprint(initialTask), func(t *testing.T) {
			states := map[string]*daemonContextState{}
			event := func(method, raw string) { daemonContextEvent(states, method, nil, json.RawMessage(raw), time.Now()) }
			event("turn/started", `{"threadId":"root","turn":{"id":"turn"}}`)
			if !initialTask {
				event("item/completed", `{"threadId":"root","turnId":"turn","item":{"type":"userMessage","id":"initial","content":[{"type":"text","text":"Initial task"}]}}`)
			}
			id := uuid.NewString()
			raw := `{"threadId":"root","turnId":"turn","item":{"type":"userMessage","id":"steer","clientId":"` + id + `","content":[{"type":"text","text":"Use Go"}]}}`
			event("item/started", raw)
			if c := daemonContextSnapshot(states, []string{"root"}, nil)["root"]; c.HasCompletedSteer(id) {
				t.Fatal("started event confirmed incorporation")
			}
			event("item/completed", strings.Replace(raw, `"turnId":"turn"`, `"turnId":"old"`, 1))
			if c := daemonContextSnapshot(states, []string{"root"}, nil)["root"]; c.HasCompletedSteer(id) {
				t.Fatal("old turn confirmed incorporation")
			}
			event("item/completed", raw)
			event("item/completed", raw) // duplicate delivery is idempotent
			c := daemonContextSnapshot(states, []string{"root"}, nil)["root"]
			if !c.HasCompletedSteer(id) || c.CompletedSteerIDs != id || c.ThreadID != "root" || c.TurnID != "turn" {
				t.Fatal("completion did not expose an exact, scoped identity", c)
			}
			if initialTask && c.CurrentTask != "Use Go" || !initialTask && c.LatestGuidance != "Use Go" {
				t.Fatal("test did not exercise both text presentations", c)
			}
			event("turn/completed", `{"threadId":"root","turn":{"id":"turn","status":"completed"}}`)
			if c := daemonContextSnapshot(states, []string{"root"}, nil)["root"]; !c.HasCompletedSteer(id) {
				t.Fatal("turn completion lost confirmation before polling")
			}
			event("turn/started", `{"threadId":"root","turn":{"id":"next"}}`)
			if c := daemonContextSnapshot(states, []string{"root"}, nil)["root"]; c.HasCompletedSteer(id) {
				t.Fatal("completion identity leaked into a different turn")
			}
		})
	}
}

func TestCompletedSteerIdentityBudgetAndMalformedEvents(t *testing.T) {
	states := map[string]*daemonContextState{}
	daemonContextEvent(states, "turn/started", nil, json.RawMessage(`{"threadId":"root","turn":{"id":"turn"}}`), time.Now())
	complete := func(id, item string) {
		raw, _ := json.Marshal(map[string]any{"threadId": "root", "turnId": "turn", "item": map[string]any{"type": "userMessage", "id": item, "clientId": id}})
		daemonContextEvent(states, "item/completed", nil, raw, time.Now())
	}
	for _, id := range []string{"", "unrelated-client", strings.Repeat("z", 36), "550e8400-e29b-41d4-a716-446655440000\n", "550e8400-e29b-71d4-a716-446655440000"} {
		complete(id, "invalid")
	}
	id := uuid.NewString()
	complete(id, "")
	if len(states["root"].completedSteers) != 0 {
		t.Fatal("malformed or unidentifiable message created confirmation")
	}
	for i := range 100 {
		id := uuid.NewString()
		complete(id, fmt.Sprint(i))
	}
	if len(states["root"].completedSteers) != 64 {
		t.Fatal("completion retention is not bounded")
	}
}

func TestSteerConfirmationSurvivesNewerLocalActivity(t *testing.T) {
	for _, kind := range []SessionContextKind{SessionContextActivity, SessionContextReply, SessionContextFailure} {
		now := time.Now()
		r := &LiveUsageReader{files: map[string]*rolloutCursor{"root": {
			threadID: "root", currentTurnID: "turn", lastModified: now,
			preview: SessionContext{Kind: kind, Text: "Newer local text", ThreadID: "root", At: now},
		}}, daemonContexts: map[string]SessionContext{"root": {
			Kind: SessionContextActivity, Text: "Older live text", ThreadID: "root", TurnID: "turn", At: now.Add(-time.Second), CompletedSteerIDs: "completed-id",
		}}}
		rows, _, _ := r.sessionSnapshots(now, nil, false, nil)
		if len(rows) != 1 || !rows[0].Context.HasCompletedSteer("completed-id") || rows[0].Context.Text != "Newer local text" {
			t.Fatal("display selection hid a valid steer completion", kind, rows)
		}
	}
}

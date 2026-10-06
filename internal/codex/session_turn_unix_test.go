//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSessionTurnWireAndIsolation(t *testing.T) {
	for _, action := range []string{"steer", "queue", "interrupt"} {
		t.Run(action, func(t *testing.T) {
			p, received := promptProvider(t, "active")
			p.mu.Lock()
			p.promptLifecycleLocked("turn/started", json.RawMessage(`{"threadId":"root","turn":{"id":"turn-one"}}`))
			p.mu.Unlock()
			o := p.SessionTurn("root")
			if o.Token == "" || o.TurnID != "turn-one" || p.SessionPrompt("root").Token != "" {
				t.Fatal("active offer missing or leaked into idle API")
			}
			if err := p.SendSessionTurn(context.Background(), o, action, "next task"); err != nil {
				t.Fatal(err)
			}
			e := <-received
			var params map[string]json.RawMessage
			if err := json.Unmarshal(e.Params, &params); err != nil {
				t.Fatal(err)
			}
			method := "turn/" + action
			if action == "queue" {
				method = "thread/queue/add"
			}
			if e.Method != method || string(params["threadId"]) != `"root"` {
				t.Fatal(e.Method, string(e.Params))
			}
			if action == "steer" && string(params["expectedTurnId"]) != `"turn-one"` {
				t.Fatal(string(e.Params))
			}
			if action == "interrupt" && string(params["turnId"]) != `"turn-one"` {
				t.Fatal(string(e.Params))
			}
			if action == "queue" && len(params["clientUserMessageId"]) < 10 {
				t.Fatal("missing client identity")
			}
			if action != "interrupt" && string(params["input"]) != `[{"text":"next task","type":"text"}]` {
				t.Fatal(string(e.Params))
			}
			if err := p.SendSessionTurn(context.Background(), o, action, "duplicate"); err == nil {
				t.Fatal("reused capability")
			}
		})
	}
}

func TestSessionTurnRejectsChangedTurnAndApproval(t *testing.T) {
	p, received := promptProvider(t, "active")
	p.mu.Lock()
	p.promptLifecycleLocked("turn/started", json.RawMessage(`{"threadId":"root","turn":{"id":"one"}}`))
	p.mu.Unlock()
	o := p.SessionTurn("root")
	p.mu.Lock()
	p.promptLifecycleLocked("turn/started", json.RawMessage(`{"threadId":"root","turn":{"id":"two"}}`))
	p.mu.Unlock()
	if err := p.SendSessionTurn(context.Background(), o, "interrupt", ""); err == nil {
		t.Fatal("interrupted replacement turn")
	}
	o = p.SessionTurn("root")
	p.mu.Lock()
	p.contexts["root"].requests["approval"] = SessionContext{Kind: SessionContextApproval}
	p.mu.Unlock()
	if p.SessionTurn("root").Token != "" {
		t.Fatal("approval exposed composer")
	}
	if err := p.SendSessionTurn(context.Background(), o, "queue", "message"); err == nil {
		t.Fatal("approval bypassed")
	}
	select {
	case e := <-received:
		t.Fatal("unexpected write", e.Method)
	default:
	}
}

package codex

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCodex167NetworkExpiryResolvesOnlyItsRequest(t *testing.T) {
	states := map[string]*daemonContextState{}
	for _, id := range []string{"expired", "remaining"} {
		daemonContextEvent(states, "item/commandExecution/requestApproval", rawJSON(id), rawJSON(map[string]any{"threadId": "root", "turnId": "turn", "itemId": "command", "command": "curl https://example.com", "cwd": "/work", "networkApprovalContext": map[string]any{"host": "example.com", "protocol": "https"}}), time.Now())
	}
	daemonContextEvent(states, "serverRequest/resolved", nil, rawJSON(map[string]any{"threadId": "other", "requestId": "expired"}), time.Now())
	if len(states["root"].requests) != 2 {
		t.Fatal("resolution crossed threads")
	}
	daemonContextEvent(states, "serverRequest/resolved", nil, rawJSON(map[string]any{"threadId": "root", "requestId": "expired"}), time.Now())
	if _, ok := states["root"].requests[string(rawJSON("expired"))]; ok || len(states["root"].requests) != 1 {
		t.Fatal("expired network prompt remained or removed its sibling")
	}
	c := daemonContextSnapshot(states, []string{"root"}, map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval})["root"]
	if c.PendingApprovals != 1 || c.ApprovalBlocked != "network" {
		t.Fatal("network expiry changed remaining request semantics", c)
	}
	daemonContextEvent(states, "serverRequest/resolved", nil, json.RawMessage(`{"threadId":"root","requestId":"remaining"}`), time.Now())
	if len(states["root"].requests) != 0 {
		t.Fatal("last expired prompt remained")
	}
}

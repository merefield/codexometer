package codex

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHumanApprovalRequiresFreshOneTimeDecision(t *testing.T) {
	states := map[string]*daemonContextState{}
	raw := rawJSON(map[string]any{"threadId": "root", "turnId": "turn", "itemId": "checkpoint", "approvalId": "unique", "kind": "humanCommand", "command": "git push", "cwd": "/work", "availableDecisions": []any{"acceptForSession", map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []string{"git"}}}, "accept", "decline", "cancel"}})
	emit := func() {
		daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(`"human"`), raw, time.Now())
	}
	emit()
	c := states["root"].requests[`"human"`]
	if !c.RequiredHumanApproval || c.ApprovalToken == "" || c.CommandDetails.Command != "git push" {
		t.Fatal("checkpoint without a preceding execution item was not actionable", c)
	}
	if len(states["root"].commands) != 0 || states["root"].activity.snapshot().RunningCommands != 0 {
		t.Fatal("checkpoint invented a running command")
	}
	for i, kind := range []string{"accept", "decline", "cancel"} {
		if c.ApprovalOptions[i].Kind != kind {
			t.Fatal("one-time decisions were lost", c.ApprovalOptions)
		}
	}
	if c.ApprovalOptions[3].Kind != "" {
		t.Fatal("checkpoint offered a reusable grant")
	}
	emit()
	replay := states["root"].requests[`"human"`]
	if replay.ApprovalToken == c.ApprovalToken {
		t.Fatal("replayed checkpoint retained its old capability")
	}
	daemonContextEvent(states, "serverRequest/resolved", nil, rawJSON(map[string]any{"threadId": "root", "requestId": "human"}), time.Now())
	if len(states["root"].requests) != 0 {
		t.Fatal("resolved checkpoint still pending")
	}
	_, invalid := approvalFixture(t, map[string]any{"kind": "humanCommand", "command": "git push", "cwd": "/work", "availableDecisions": []string{"acceptForSession"}})
	if invalid.ApprovalToken != "" {
		t.Fatal("checkpoint with only reusable choices became actionable")
	}
}

func TestHumanCheckpointRequiresOwnCommandAndCurrentTurn(t *testing.T) {
	for _, fields := range []map[string]any{{"kind": "humanCommand"}, {"kind": "humanCommand", "command": "git push"}, {"kind": "humanCommand", "cwd": "/work"}} {
		_, c := approvalFixture(t, fields)
		if c.ApprovalToken != "" {
			t.Fatal("checkpoint inherited another execution's details", c)
		}
	}
	for _, ended := range []bool{false, true} {
		states := map[string]*daemonContextState{"root": {activeTurn: "current", ended: ended, requests: map[string]SessionContext{}}}
		turn := "old"
		if ended {
			turn = "current"
		}
		daemonContextEvent(states, "item/commandExecution/requestApproval", rawJSON("human"), rawJSON(map[string]any{"threadId": "root", "turnId": turn, "itemId": "item", "kind": "humanCommand", "command": "git push", "cwd": "/work"}), time.Now())
		if c := states["root"].requests[`"human"`]; c.ApprovalToken != "" {
			t.Fatal("checkpoint from inactive turn became actionable", c)
		}
	}
}

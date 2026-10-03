package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestApprovalCommentaryScopeAndReplay(t *testing.T) {
	states := map[string]*daemonContextState{}
	emit := func(method, id, body string) {
		daemonContextEvent(states, method, json.RawMessage(id), json.RawMessage(body), time.Now())
	}
	prose := func(thread, turn, text string) {
		body, _ := json.Marshal(map[string]any{"threadId": thread, "turnId": turn, "item": map[string]string{"type": "agentMessage", "phase": "commentary", "text": text}})
		emit("item/completed", "", string(body))
	}
	request := func(thread, turn, id, reason string) SessionContext {
		body, _ := json.Marshal(map[string]any{"threadId": thread, "turnId": turn, "itemId": id, "command": "pwd", "cwd": "/work", "reason": reason, "availableDecisions": []string{"accept", "decline"}})
		emit("item/commandExecution/requestApproval", id, string(body))
		return states[thread].requests[id]
	}
	prose("root", "t", "Parent explanation")
	if c := request("child", "t", "1", "Allow?"); c.ApprovalContext != "" {
		t.Fatal("borrowed parent prose", c)
	}
	prose("child", "t", "Child explanation")
	c := request("child", "t", "2", "Allow?")
	if c.ApprovalContext != "Child explanation" || c.ApprovalToken == "" || c.CommandDetails.Command != "pwd" {
		t.Fatal(c)
	}
	prose("child", "t", "Later explanation")
	if replay := request("child", "t", "2", "Allow?"); replay.ApprovalContext != c.ApprovalContext || replay.At != c.At {
		t.Fatal("replay changed historical context", replay)
	}
	if c := request("child", "other", "3", "Allow?"); c.ApprovalContext != "" {
		t.Fatal("cross-turn context", c)
	}
	if c := request("child", "", "4", "Allow?"); c.ApprovalContext != "" {
		t.Fatal("unidentified turn context", c)
	}
	if c := request("child", "t", "5", "  LATER\n explanation "); c.ApprovalContext != "" {
		t.Fatal("duplicate context", c)
	}
	prose("child", "t", "\x1b[31mBounded\x1b[0m"+strings.Repeat("x", 5000))
	if c := request("child", "t", "6", "Allow?"); strings.Contains(c.ApprovalContext, "\x1b") || len([]rune(c.ApprovalContext)) > sessionContextLimit {
		t.Fatal("unbounded/unsafe context")
	}
	emit("turn/started", "", `{"threadId":"child"}`)
	if c := request("child", "t", "7", "Allow?"); c.ApprovalContext != "" {
		t.Fatal("new turn retained commentary", c)
	}
	prose("child", "t", "Before completion")
	emit("turn/completed", "", `{"threadId":"child","turn":{"status":"completed"}}`)
	if c := request("child", "t", "8", "Allow?"); c.ApprovalContext != "" {
		t.Fatal("completed turn retained commentary", c)
	}
}

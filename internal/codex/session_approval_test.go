package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func approvalFixture(t *testing.T, overrides map[string]any) (map[string]*daemonContextState, SessionContext) {
	t.Helper()
	states := map[string]*daemonContextState{}
	daemonContextEvent(states, "item/started", nil, json.RawMessage(`{"threadId":"root","turnId":"turn","item":{"id":"cmd","type":"commandExecution","command":"git push","cwd":"/work"}}`), time.Now())
	p := map[string]any{"threadId": "root", "turnId": "turn", "itemId": "cmd", "reason": "Publish branch?"}
	for k, v := range overrides {
		p[k] = v
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(`"req"`), raw, time.Now())
	return states, states["root"].requests[`"req"`]
}

func TestApprovalCompleteCommandAndFailClosed(t *testing.T) {
	_, c := approvalFixture(t, nil)
	if c.ApprovalToken == "" || !strings.Contains(c.Text, "Command: git push\nDirectory: /work") {
		t.Fatalf("missing correlated detail: %+v", c)
	}
	for name, fields := range map[string]map[string]any{
		"wrong turn":              {"turnId": "other"},
		"wrong item":              {"itemId": "other"},
		"truncated":               {"command": strings.Repeat("x", 5000)},
		"controls":                {"command": "echo \x1b[31mhidden"},
		"hidden directory suffix": {"cwd": "/work\t"},
		"bidi":                    {"command": "echo \u202Ehidden"},
		"network":                 {"networkApprovalContext": map[string]string{"host": "example.com", "protocol": "https"}},
		"permissions":             {"additionalPermissions": map[string]any{"network": true}},
		"session only":            {"availableDecisions": []string{"acceptForSession", "decline"}},
		"no decisions":            {"availableDecisions": []string{}},
		"ambiguous argv":          {"command": []string{"echo", "a b"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, c := approvalFixture(t, fields)
			if c.ApprovalToken != "" {
				t.Fatalf("unsafe request actionable: %+v", c)
			}
		})
	}
}

func TestApprovalLifecycleAndReplay(t *testing.T) {
	for _, method := range []string{"serverRequest/resolved", "turn/started", "turn/completed", "turn/interrupted", "thread/closed", "item/completed"} {
		t.Run(method, func(t *testing.T) {
			states, _ := approvalFixture(t, nil)
			daemonContextEvent(states, method, nil, json.RawMessage(`{"threadId":"root","turnId":"turn","requestId":"req","item":{"id":"cmd","type":"commandExecution"}}`), time.Now())
			if s := states["root"]; s != nil && len(s.requests) != 0 {
				t.Fatal("pending approval survived lifecycle completion")
			}
		})
	}
	states, old := approvalFixture(t, nil)
	daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(`"req"`), json.RawMessage(`{"threadId":"root","turnId":"turn","itemId":"cmd","command":"git status","cwd":"/work"}`), time.Now())
	if next := states["root"].requests[`"req"`]; next.ApprovalToken == old.ApprovalToken || next.ApprovalToken == "" {
		t.Fatal("replay reused old capability")
	}
}

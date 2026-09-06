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
	p := map[string]any{"threadId": "root", "turnId": "turn", "itemId": "cmd", "reason": "Publish branch?", "availableDecisions": []string{"accept", "decline"}}
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
	if c.CommandDetails != (ApprovalCommandDetails{Justification: "Publish branch?", Command: "git push", Directory: "/work"}) {
		t.Fatal("structured fields did not preserve source values", c.CommandDetails)
	}
	for name, fields := range map[string]map[string]any{
		"stdin action":            {"kind": "writeStdin"},
		"future action":           {"kind": "unknownAction"},
		"null action":             {"kind": nil},
		"malformed action":        {"kind": 42},
		"wrong turn":              {"turnId": "other"},
		"wrong item":              {"itemId": "other"},
		"truncated":               {"command": strings.Repeat("x", 5000)},
		"controls":                {"command": "echo \x1b[31mhidden"},
		"hidden directory suffix": {"cwd": "/work\t"},
		"bidi":                    {"command": "echo \u202Ehidden"},
		"network":                 {"networkApprovalContext": map[string]string{"host": "example.com", "protocol": "https"}},
		"permissions":             {"additionalPermissions": map[string]any{"network": true}},
		"no decisions":            {"availableDecisions": []string{}},
		"ambiguous argv":          {"command": []string{"echo", "a b"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, c := approvalFixture(t, fields)
			if c.ApprovalToken != "" {
				t.Fatalf("unsafe request actionable: %+v", c)
			}
			if c.CommandDetails != (ApprovalCommandDetails{}) {
				t.Fatal("unvalidated request exposed structured command")
			}
		})
	}
}

func TestApprovalKindExplicitAndLegacyCommand(t *testing.T) {
	for _, fields := range []map[string]any{nil, {"kind": "command"}} {
		_, c := approvalFixture(t, fields)
		if c.ApprovalToken == "" || c.CommandDetails.Command != "git push" {
			t.Fatal("valid command blocked", c)
		}
	}
	_, c := approvalFixture(t, map[string]any{"kind": "writeStdin", "command": "git push", "cwd": "/work"})
	if c.ApprovalToken != "" || c.ApprovalBlocked != "approval-kind" {
		t.Fatal("stdin treated as command", c)
	}
}

func TestApprovalStructuredJustificationIsBounded(t *testing.T) {
	_, c := approvalFixture(t, map[string]any{"reason": strings.Repeat(" ", 10000) + "Explanation\nCommand: not the real command"})
	if c.ApprovalToken == "" || c.CommandDetails.Command != "git push" || c.CommandDetails.Justification != "Explanation\nCommand: not the real command" {
		t.Fatal("justification confused command fields", c.CommandDetails)
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

func TestApprovalRejectionDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		fields map[string]any
		reason string
	}{
		{map[string]any{"networkApprovalContext": map[string]string{"host": "example.com"}}, "network"},
		{map[string]any{"additionalPermissions": map[string]any{}}, "permissions"},
		{map[string]any{"command": []string{"pwd"}}, "command-format"},
		{map[string]any{"itemId": "unknown"}, "missing-command"},
		{map[string]any{"itemId": "unknown", "command": "pwd"}, "missing-directory"},
		{map[string]any{"turnId": "", "command": "pwd", "cwd": "/work"}, "missing-identity"},
		{map[string]any{"availableDecisions": []string{}}, "decisions"},
		{map[string]any{"command": strings.Repeat("x", 5000)}, "truncated"},
		{map[string]any{"command": "pwd\t"}, "sanitised"},
	} {
		_, c := approvalFixture(t, tc.fields)
		if c.ApprovalBlocked != tc.reason || c.ApprovalToken != "" {
			t.Errorf("want %s, got reason=%s actionable=%v", tc.reason, c.ApprovalBlocked, c.ApprovalToken != "")
		}
	}
	_, c := approvalFixture(t, nil)
	if c.ApprovalBlocked != "" || c.ApprovalToken == "" {
		t.Fatal("eligible request gained a rejection")
	}
}

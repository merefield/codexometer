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
		"stdin action":                {"kind": "writeStdin"},
		"future action":               {"kind": "unknownAction"},
		"null action":                 {"kind": nil},
		"malformed action":            {"kind": 42},
		"wrong turn":                  {"turnId": "other"},
		"wrong item":                  {"itemId": "other"},
		"truncated":                   {"command": strings.Repeat("x", approvalTextLimit+1)},
		"expanded tabs exceed budget": {"command": strings.Repeat("\t", approvalTextLimit/4+1)},
		"controls":                    {"command": "echo \x1b[31mhidden"},
		"hidden directory suffix":     {"cwd": "/work\u202e"},
		"bidi":                        {"command": "echo \u202Ehidden"},
		"bare carriage return":        {"command": "echo visible\rhidden"},
		"network":                     {"networkApprovalContext": map[string]string{"host": "example.com", "protocol": "https"}},
		"permissions":                 {"additionalPermissions": map[string]any{"network": true}},
		"no decisions":                {"availableDecisions": []string{}},
		"ambiguous argv":              {"command": []string{"echo", "a b"}},
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
	if c.ApprovalToken == "" || c.CommandDetails.Command != "git push" || c.CommandDetails.Justification != strings.Repeat(" ", 10000)+"Explanation\nCommand: not the real command" {
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
		{map[string]any{"command": strings.Repeat("x", approvalTextLimit+1)}, "truncated"},
		{map[string]any{"command": strings.Repeat("\t", approvalTextLimit/4+1)}, "truncated"},
		{map[string]any{"command": "pwd\x1b[31m"}, "sanitised"},
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

func TestLongCommandApprovalKeepsCompleteReview(t *testing.T) {
	command := "python3 - <<'PY'\n" + strings.Repeat("print('long review')\n", 400) + "# END OF COMMAND\nPY"
	reason := strings.Repeat("Long explanation. ", 400) + "END OF JUSTIFICATION"
	_, c := approvalFixture(t, map[string]any{"command": command, "reason": reason})
	if c.ApprovalToken == "" || c.ApprovalBlocked != "" || c.CommandDetails.Command != command || c.CommandDetails.Justification != reason || !strings.Contains(c.Text, command) || !strings.Contains(c.Text, reason) {
		t.Fatal("long approval lost its complete review or controls")
	}
	if SanitizeSessionContext(reason) == reason {
		t.Fatal("fixture does not exceed the ordinary excerpt budget")
	}
}

func TestLongApprovalCorrelatesCompleteStartedCommand(t *testing.T) {
	states := map[string]*daemonContextState{}
	command := strings.Repeat("echo long-command\n", 400) + "END OF COMMAND"
	raw, _ := json.Marshal(map[string]any{"threadId": "root", "turnId": "turn", "item": map[string]any{"id": "cmd", "type": "commandExecution", "command": command, "cwd": "/work"}})
	daemonContextEvent(states, "item/started", nil, raw, time.Now())
	raw, _ = json.Marshal(map[string]any{"threadId": "root", "turnId": "turn", "itemId": "cmd", "reason": "Review command", "availableDecisions": []string{"accept", "decline"}})
	daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(`"req"`), raw, time.Now())
	c := states["root"].requests[`"req"`]
	if c.ApprovalToken == "" || c.CommandDetails.Command != command {
		t.Fatal("started-command fallback truncated the approval")
	}
}

func TestApprovalWhitespaceDoesNotDisableDecisions(t *testing.T) {
	for _, command := range []string{
		"git status\n",
		"\n  git status  \n\n",
		"python3 - <<'PY'\nif True:\n\tprint('ok')\nPY\n",
		"printf 'a\tb'\n",
		"git status\r\n",
	} {
		t.Run(command, func(t *testing.T) {
			reason := "  Review this command.\n\tKeep normal formatting.\n"
			_, c := approvalFixture(t, map[string]any{"command": command, "reason": reason})
			if c.ApprovalToken == "" || c.ApprovalBlocked != "" {
				t.Fatalf("ordinary whitespace disabled decisions: %s", c.ApprovalBlocked)
			}
			if c.CommandDetails.Command != command || c.CommandDetails.Directory != "/work" {
				t.Fatal("formatting changed the original command or directory")
			}
		})
	}
}

func TestApprovalDisplayPreservesWhitespace(t *testing.T) {
	input := "\n  first line  \n\tsecond line\r\n\n"
	want := "\n  first line  \n    second line\n\n"
	if got := SanitizeApprovalText(input); got != want {
		t.Fatalf("review whitespace lost: got %q, want %q", got, want)
	}
	if !approvalTextDisplayable(input) {
		t.Fatal("normal display formatting disabled decisions")
	}
	if approvalTextDisplayable("echo\x1b[2Jhidden") || approvalTextDisplayable("echo\u202ehidden") {
		t.Fatal("non-whitespace controls treated as display formatting")
	}
}

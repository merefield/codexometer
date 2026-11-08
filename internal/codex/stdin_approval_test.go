package codex

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func stdinApprovalFixture(t *testing.T, overrides map[string]any) (map[string]*daemonContextState, SessionContext) {
	t.Helper()
	p := map[string]any{"kind": "writeStdin", "approvalId": "input-callback", "command": "write_stdin --session-id 123 'echo done\n'", "cwd": "/terminal/start", "environmentId": "local", "availableDecisions": []string{"accept", "cancel"}}
	for k, v := range overrides {
		p[k] = v
	}
	return approvalFixture(t, p)
}

func TestStdinApprovalDecodesNativeInputAndOnlyOneTimeChoices(t *testing.T) {
	_, c := stdinApprovalFixture(t, nil)
	if c.ApprovalToken == "" || c.ApprovalBlocked != "" || c.ActionDetails.Kind != "stdin" || c.ActionDetails.Target != "123" || c.ActionDetails.Input != "echo done\n" || c.CommandDetails.Command != "" {
		t.Fatalf("bad stdin approval: %+v", c)
	}
	review := c.ActionReview()
	if review == nil || review.Title != "TERMINAL INPUT APPROVAL" || review.Fields[1].Value != strconv.QuoteToASCII("echo done\n") {
		t.Fatal("literal input not preserved in review", review)
	}
	if c.ApprovalOptions[0].Kind != "acceptStdin" || c.ApprovalOptions[0].Value != "accept" || c.ApprovalOptions[1].Kind != "cancelStdin" || c.ApprovalOptions[1].Value != "cancel" {
		t.Fatal("stdin decision payload changed", c.ApprovalOptions)
	}
	for _, input := range []string{"", "yes\n", "\x03", "\t", "secret\u202evalue", "こんにちは", "line\r\n"} {
		command := "write_stdin --session-id 123 '" + input + "'"
		_, c := stdinApprovalFixture(t, map[string]any{"command": command})
		if c.ApprovalToken == "" || c.ActionDetails.Input != input || !approvalTextDisplayable(c.Text) {
			t.Fatalf("escaped input lost: %q %+v", input, c)
		}
	}
	_, c = stdinApprovalFixture(t, map[string]any{"availableDecisions": []string{"acceptForSession", "accept", "cancel"}})
	if c.ApprovalOptions[0].Kind != "acceptStdin" || c.ApprovalOptions[2].Kind != "" {
		t.Fatal("reusable stdin grant offered")
	}
}

func TestStdinApprovalNeverInfersInputOrBroadensAuthority(t *testing.T) {
	for name, fields := range map[string]map[string]any{
		"missing callback": {"approvalId": ""}, "missing turn": {"turnId": ""}, "missing item": {"itemId": ""}, "missing cwd": {"cwd": ""},
		"missing command": {"command": nil}, "command not input": {"command": "git push"},
		"extra argument":   {"command": "write_stdin --session-id 123 'yes' extra"},
		"shell expansion":  {"command": "write_stdin --session-id 123 \"$HOME\""},
		"invalid terminal": {"command": "write_stdin --session-id nope yes"},
		"hidden cwd":       {"cwd": "/work\u202e"}, "hidden environment": {"environmentId": "env\x1b[31m"},
		"NUL":                    {"command": "write_stdin --session-id 123 'x\x00y'"},
		"additional permissions": {"additionalPermissions": map[string]any{"network": map[string]bool{"enabled": true}}},
		"network request":        {"networkApprovalContext": map[string]string{"host": "example.com", "protocol": "https"}},
		"no decisions":           {"availableDecisions": []string{}}, "omitted decisions": {"availableDecisions": nil},
		"only reusable": {"availableDecisions": []string{"acceptForSession"}},
		"large":         {"command": "write_stdin --session-id 123 '" + strings.Repeat("x", approvalTextLimit) + "'"},
	} {
		t.Run(name, func(t *testing.T) {
			_, c := stdinApprovalFixture(t, fields)
			if c.ApprovalToken != "" || c.ActionReview() != nil || c.ApprovalBlocked == "" || c.Text == "" {
				t.Fatalf("invalid input actionable or invisible: %+v", c)
			}
		})
	}
}

func TestStdinApprovalPOSIXPresentation(t *testing.T) {
	for encoded, want := range map[string]string{
		"'don'\"'\"'t'":                 "don't",
		"\"a\\\"b\\\\c\\$d\"":           "a\"b\\c$d",
		"hello\\ world":                 "hello world",
		"'$(touch ignored); `literal`'": "$(touch ignored); `literal`",
	} {
		terminal, input, ok := terminalApprovalInput("write_stdin --session-id 123 " + encoded)
		if !ok || terminal != "123" || input != want {
			t.Fatalf("incorrect literal decoding %q: %q %v", encoded, input, ok)
		}
	}
	for _, encoded := range []string{"", "'unclosed", "\"unclosed", "a b", "a;run", "$(run)", "`run`", "x\\", "a\\\ncontinued"} {
		if _, ok := quotedApprovalArgument(encoded); ok {
			t.Fatal("unsupported presentation accepted", encoded)
		}
	}
}

func TestStdinApprovalSiblingRequestsRemainIndependent(t *testing.T) {
	states, first := stdinApprovalFixture(t, nil)
	raw, _ := json.Marshal(map[string]any{"threadId": "root", "turnId": "turn", "itemId": "cmd", "kind": "writeStdin", "approvalId": "other-callback", "command": "write_stdin --session-id 456 'different'", "cwd": "/terminal/start", "availableDecisions": []string{"accept", "cancel"}})
	daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(`"other"`), raw, time.Now())
	other := states["root"].requests[`"other"`]
	if other.ApprovalToken == "" || other.ApprovalToken == first.ApprovalToken {
		t.Fatal("input requests share authority")
	}
	daemonContextEvent(states, "serverRequest/resolved", nil, json.RawMessage(`{"threadId":"root","requestId":"req"}`), time.Now())
	if len(states["root"].requests) != 1 || states["root"].requests[`"other"`].ActionDetails.Target != "456" {
		t.Fatal("resolving one input removed another")
	}
}

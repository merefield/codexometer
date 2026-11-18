package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestObservedPermissionScopeIsReadableWithoutGrantingAuthority(t *testing.T) {
	scope := json.RawMessage(`{"network":{"enabled":true},"fileSystem":{"read":["/a b"],"entries":[{"access":"deny","path":{"type":"glob_pattern","pattern":"*.key"}}]},"future":{"quota":9007199254740993}}`)
	params := map[string]any{"threadId": "root", "itemId": "item", "turnId": "turn", "reason": "Read worktree", "cwd": "/work", "environmentId": "env-2", "permissions": scope}
	states := map[string]*daemonContextState{}
	daemonContextEvent(states, "item/permissions/requestApproval", rawJSON("req"), rawJSON(params), time.Now())
	c := states["root"].requests[`"req"`]
	r := c.ActionReview()
	if r == nil || c.ApprovalBlocked != "permissions" || c.ApprovalToken != "" || c.ApprovalOptions[0].Kind != "" {
		t.Fatalf("observation must not enable a decision: %+v", c)
	}
	for _, want := range []string{"Network access:\n  Enabled: true", "Read paths:", `[1]: "/a b"`, `Access: "deny"`, `Pattern: "*.key"`, `"quota": 9007199254740993`} {
		if !strings.Contains(r.Fields[0].Value, want) {
			t.Errorf("missing %q in %s", want, r.Fields[0].Value)
		}
	}
	if r.Fields[1].Value != "/work" || r.Fields[2].Value != "env-2" || r.Fields[3].Value != "Read worktree" {
		t.Fatal(r.Fields)
	}
	_, c = approvalFixture(t, map[string]any{"additionalPermissions": scope})
	if c.ApprovalToken != "" || !strings.Contains(c.Text, "Read paths:") {
		t.Fatal(c)
	}
}

func TestReviewFieldsNeverPartiallyFormatsAmbiguousOrOversizedScopes(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":{"x":0,"x":1}}`, `null`, `{"x":`, `{"a":` + strings.Repeat("[", 20) + "0" + strings.Repeat("]", 20) + "}", `{"x":"` + strings.Repeat("x", approvalTextLimit) + `"}`} {
		if got, ok := reviewFields(json.RawMessage(raw), nil); ok || got != "" {
			t.Fatalf("unexpected partial review: %q", got)
		}
	}
	got, ok := reviewFields(json.RawMessage(`{"args":["a\nb",null,{},[]],"fake\nlabel":true}`), nil)
	if !ok || got != "args:\n  [1]: \"a\\nb\"\n  [2]: null\n  [3]: {}\n  [4]: []\n\"fake\\nlabel\": true" {
		t.Fatal(got)
	}
}

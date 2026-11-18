package codex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdvertisedApprovalChoices(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		kinds []string
	}{
		{`["accept","cancel"]`, []string{"accept", "cancel"}},
		{`["acceptForSession","decline","accept","cancel"]`, []string{"accept", "acceptForSession", "decline", "cancel"}},
		{`["cancel"]`, []string{"cancel"}},
		{`["acceptForSession","decline"]`, []string{"acceptForSession", "decline"}},
		{`null`, []string{"accept", "cancel"}},
		{`[]`, nil},
		{`["future",{"other":{}},"cancel","cancel"]`, []string{"cancel"}},
		{`[{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["git","status"]}},"cancel"]`, []string{"acceptWithExecpolicyAmendment", "cancel"}},
		{`[{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":[],"future":true}}]`, nil},
		{`[{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["pwd\u001b[31m"]}}]`, nil},
	} {
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(tc.raw), &raw); err != nil {
			t.Fatal(err)
		}
		options, summary := commandApprovalOptions(raw)
		for i, o := range options {
			want := ""
			if i < len(tc.kinds) {
				want = tc.kinds[i]
			}
			if o.Kind != want {
				t.Fatalf("%s option %d: %+v want %s", tc.raw, i, o, want)
			}
		}
		if len(tc.kinds) > 0 && summary == "" {
			t.Fatal("missing decision metadata")
		}
	}
	_, c := approvalFixture(t, map[string]any{"availableDecisions": []string{"accept", "cancel"}})
	if c.ApprovalToken == "" || c.ApprovalBlocked != "" {
		t.Fatal("standard accept/cancel prompt still blocked")
	}
	_, c = approvalFixture(t, map[string]any{"availableDecisions": []any{map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []string{"git", "status"}}}}})
	if c.ApprovalToken == "" || !strings.Contains(c.Text, `Persistent command-prefix rule: "git" "status"`) {
		t.Fatal("persistent grant lacks displayed rule")
	}
}

func TestPersistentPrefixPreservesArgumentBoundariesAndWire(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"git", "a b"}, `"git" "a b"`}, {[]string{"git", "a", "b"}, `"git" "a" "b"`},
		{[]string{"git", `"quoted"`, "a\nb"}, `"git" "\"quoted\"" "a\nb"`},
	} {
		raw := rawJSON(map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": tc.args}})
		options, _ := commandApprovalOptions([]json.RawMessage{raw})
		if o := options[0]; o.Detail != tc.want || o.Wire != string(raw) || o.Value != o.Wire {
			t.Fatalf("prefix %v: %+v", tc.args, o)
		}
	}
}

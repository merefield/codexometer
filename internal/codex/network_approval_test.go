package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func networkDecisions() []any {
	rule := func(action string) any {
		return map[string]any{"applyNetworkPolicyAmendment": map[string]any{"network_policy_amendment": map[string]string{"host": "example.com", "action": action}}}
	}
	return []any{"accept", "acceptForSession", rule("allow"), rule("deny"), "decline", "cancel"}
}

func networkApprovalFixture(t *testing.T, overrides map[string]any) (map[string]*daemonContextState, SessionContext) {
	t.Helper()
	p := map[string]any{"command": nil, "cwd": "", "networkApprovalContext": map[string]string{"host": "example.com", "protocol": "https"}, "availableDecisions": networkDecisions()}
	for k, v := range overrides {
		p[k] = v
	}
	return approvalFixture(t, p)
}

func TestNetworkApprovalRequiresNoCommandAndRetainsExactRules(t *testing.T) {
	_, c := networkApprovalFixture(t, nil)
	if c.ApprovalToken == "" || c.ApprovalBlocked != "" || c.CommandDetails.Command != "" || c.ActionReview() == nil {
		t.Fatalf("network not actionable: %+v", c)
	}
	if c.ActionDetails.Target != "example.com" || c.ActionDetails.Protocol != "https" {
		t.Fatal(c.ActionDetails)
	}
	for i, kind := range []string{"accept", "acceptForSession", "allowNetworkHost", "denyNetworkHost", "decline", "cancel"} {
		if c.ApprovalOptions[i].Kind != kind {
			t.Fatal(c.ApprovalOptions)
		}
		raw, _ := json.Marshal(networkDecisions()[i])
		if c.ApprovalOptions[i].Wire != string(raw) {
			t.Fatal("advertised decision changed", c.ApprovalOptions[i])
		}
	}
	for _, protocol := range []string{"http", "https", "socks5Tcp", "socks5Udp"} {
		_, c := networkApprovalFixture(t, map[string]any{"networkApprovalContext": map[string]string{"host": "[::1]:8080", "protocol": protocol}})
		if c.ApprovalToken == "" {
			t.Fatal("supported protocol blocked", protocol)
		}
		for _, option := range c.ApprovalOptions {
			if option.Kind == "allowNetworkHost" || option.Kind == "denyNetworkHost" {
				t.Fatal("rule for another host offered")
			}
		}
	}
}

func TestNetworkApprovalRejectsIncompleteAmbiguousOrBroaderRequests(t *testing.T) {
	for name, overrides := range map[string]map[string]any{
		"no protocol":         {"networkApprovalContext": map[string]string{"host": "example.com"}},
		"unknown protocol":    {"networkApprovalContext": map[string]string{"host": "example.com", "protocol": "future"}},
		"extra context":       {"networkApprovalContext": map[string]any{"host": "example.com", "protocol": "https", "extra": true}},
		"host controls":       {"networkApprovalContext": map[string]string{"host": "example.com\u202e", "protocol": "https"}},
		"host whitespace":     {"networkApprovalContext": map[string]string{"host": "example.com\u00a0other", "protocol": "https"}},
		"URL instead of host": {"networkApprovalContext": map[string]string{"host": "https://example.com", "protocol": "https"}},
		"permissions":         {"additionalPermissions": map[string]any{"network": map[string]bool{"enabled": true}}},
		"wrong kind":          {"kind": "humanCommand"}, "no identity": {"turnId": ""},
		"no decisions": {"availableDecisions": []any{}}, "omitted decisions": {"availableDecisions": nil},
		"unsafe reason": {"reason": "allow\x1b[2J"}, "long reason": {"reason": strings.Repeat("x", approvalTextLimit+1)},
		"unoffered wider rule": {"availableDecisions": []any{map[string]any{"applyNetworkPolicyAmendment": map[string]any{"network_policy_amendment": map[string]string{"host": "*.example.com", "action": "allow"}}}}},
		"exec policy":          {"availableDecisions": []any{map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []string{"curl"}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, c := networkApprovalFixture(t, overrides)
			if c.ApprovalToken != "" || c.ActionReview() != nil {
				t.Fatalf("unsupported network request actionable: %+v", c)
			}
		})
	}
}

func TestNetworkApprovalReplayAndExpiry(t *testing.T) {
	states, first := networkApprovalFixture(t, nil)
	raw, _ := json.Marshal(map[string]any{"threadId": "root", "turnId": "turn", "itemId": "cmd", "networkApprovalContext": map[string]string{"host": "changed.example", "protocol": "https"}, "availableDecisions": []string{"accept", "cancel"}})
	daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(`"req"`), raw, time.Now())
	next := states["root"].requests[`"req"`]
	if next.ApprovalToken == "" || next.ApprovalToken == first.ApprovalToken || next.ActionDetails.Target != "changed.example" {
		t.Fatal("replay retained network authority")
	}
	daemonContextEvent(states, "serverRequest/resolved", nil, json.RawMessage(`{"threadId":"root","requestId":"req"}`), time.Now())
	if len(states["root"].requests) != 0 {
		t.Fatal("expired network decision remained")
	}
}

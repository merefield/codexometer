package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func mcpApprovalFixture(t *testing.T, overrides map[string]any) (map[string]*daemonContextState, SessionContext) {
	t.Helper()
	p := map[string]any{
		"threadId": "root", "serverName": "codex_apps", "mode": "form",
		"message":         "Allow Calendar to create an event?",
		"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		"_meta":           map[string]any{"codex_approval_kind": "mcp_tool_call", "tool_title": "Create Event", "connector_name": "Calendar", "persist": []string{"session", "always"}, "tool_params": map[string]any{"title": "Roadmap review"}},
	}
	for key, value := range overrides {
		p[key] = value
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]*daemonContextState{}
	daemonContextEvent(states, "mcpServer/elicitation/request", json.RawMessage(`"mcp-request"`), raw, time.Now())
	if states["root"] == nil {
		t.Fatal("MCP request not observed")
	}
	return states, states["root"].requests[`"mcp-request"`]
}

func TestMCPToolApprovalMatchesNativeFourChoicesWithoutItemTurnOrToolName(t *testing.T) {
	_, c := mcpApprovalFixture(t, nil)
	if c.Kind != SessionContextApproval || c.ApprovalToken == "" || c.ApprovalBlocked != "" || c.TurnID != "" || c.ItemID != "" || c.MCPTool.Name != "" {
		t.Fatalf("native connector request not actionable: %+v", c)
	}
	if c.MCPTool.Server != "codex_apps" || c.MCPTool.Connector != "Calendar" || !strings.Contains(c.MCPTool.Arguments, `"title": "Roadmap review"`) {
		t.Fatal("complete tool review was lost", c.MCPTool)
	}
	for i, want := range []string{"accept", "acceptForSession", "acceptMcpAlways", "cancelMcpTool"} {
		if c.ApprovalOptions[i].Kind != want {
			t.Fatal("wrong offered choices", c.ApprovalOptions)
		}
		var response struct {
			Action  string            `json:"action"`
			Content any               `json:"content"`
			Meta    map[string]string `json:"_meta"`
		}
		if json.Unmarshal([]byte(c.ApprovalOptions[i].Wire), &response) != nil || response.Content != nil {
			t.Fatal("invalid response")
		}
		wantAction := "accept"
		if i == 3 {
			wantAction = "cancel"
		}
		if response.Action != wantAction {
			t.Fatal("incorrect MCP action", response)
		}
		if i == 1 && response.Meta["persist"] != "session" || i == 2 && response.Meta["persist"] != "always" || (i == 0 || i == 3) && response.Meta != nil {
			t.Fatal("incorrect grant scope", response)
		}
	}
}

func TestMCPApprovalOffersOnlyAdvertisedScopesAndModes(t *testing.T) {
	for _, mode := range []string{"form", "openai/form", "openaiForm"} {
		for _, tc := range []struct {
			persist any
			count   int
		}{{nil, 2}, {"session", 3}, {"always", 3}, {[]string{"always", "session", "session"}, 4}, {"future", 2}} {
			_, c := mcpApprovalFixture(t, map[string]any{"mode": mode, "_meta": map[string]any{"codex_approval_kind": "mcp_tool_call", "persist": tc.persist}})
			if c.MCPTool.Arguments != "" {
				t.Fatal("omitted argument metadata was invented")
			}
			count := 0
			for _, option := range c.ApprovalOptions {
				if option.Kind != "" {
					count++
				}
			}
			if c.ApprovalToken == "" || count != tc.count {
				t.Fatalf("mode %s scope %v: %d choices", mode, tc.persist, count)
			}
		}
	}
	_, c := mcpApprovalFixture(t, map[string]any{"mode": "openai/form", "requestedSchema": nil, "_meta": map[string]any{"codex_approval_kind": "mcp_tool_call", "tool_params_display": []any{map[string]any{"name": "title", "value": "Demo", "display_name": "Title"}}}})
	if c.ApprovalToken == "" || !strings.Contains(c.MCPTool.Arguments, `"title": "Demo"`) {
		t.Fatal("display-only argument metadata not supported")
	}
}

func TestUnsupportedMCPRequestsRemainVisibleWithoutApprovalCapability(t *testing.T) {
	for name, override := range map[string]map[string]any{
		"url":              {"mode": "url"},
		"auth":             {"_meta": map[string]any{"codex_approval_kind": "browser_auth"}},
		"verification":     {"mode": "openai/userVerification"},
		"form-fields":      {"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]string{"type": "string"}}}},
		"required":         {"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{"answer"}}},
		"missing-server":   {"serverName": ""},
		"missing-message":  {"message": ""},
		"bad-arguments":    {"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call", "tool_params": []string{"argument"}}},
		"bad-display":      {"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call", "tool_params_display": []any{map[string]any{"name": "title"}}}},
		"long":             {"message": strings.Repeat("x", approvalTextLimit+1)},
		"hidden-text":      {"message": "Allow \x1b[31mthis?"},
		"identity-newline": {"serverName": "server\nOther server"},
	} {
		t.Run(name, func(t *testing.T) {
			_, c := mcpApprovalFixture(t, override)
			if c.Text == "" || !c.pending() || c.ApprovalToken != "" || c.ApprovalBlocked == "" {
				t.Fatalf("unsupported request disappeared or became actionable: %+v", c)
			}
		})
	}
}

func TestMCPApprovalResolutionAndReplayAreRequestBound(t *testing.T) {
	states, c := mcpApprovalFixture(t, nil)
	daemonContextEvent(states, "mcpServer/elicitation/request", json.RawMessage(`"mcp-request"`), json.RawMessage(`{"threadId":"root","serverName":"codex_apps","mode":"form","message":"Allow updated tool request?","requestedSchema":{"type":"object","properties":{}},"_meta":{"codex_approval_kind":"mcp_tool_call"}}`), time.Now())
	next := states["root"].requests[`"mcp-request"`]
	if next.ApprovalToken == "" || c.ApprovalToken == next.ApprovalToken || !next.At.Equal(c.At) {
		t.Fatal("replay failed to replace the capability while preserving queue position")
	}
	daemonContextEvent(states, "serverRequest/resolved", nil, json.RawMessage(`{"threadId":"root","requestId":"other"}`), time.Now())
	if len(states["root"].requests) != 1 {
		t.Fatal("another request resolved this approval")
	}
	daemonContextEvent(states, "serverRequest/resolved", nil, json.RawMessage(`{"threadId":"root","requestId":"mcp-request"}`), time.Now())
	if len(states["root"].requests) != 0 {
		t.Fatal("resolved approval remained actionable")
	}
}

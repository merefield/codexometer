package codex

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"strings"
)

// MCPToolApprovalDetails is a complete bounded review, not a command or a
// reusable capability. Keep value fields so context comparisons remain stable.
type MCPToolApprovalDetails struct {
	Server    string `json:"server"`
	Name      string `json:"name,omitempty"`
	Title     string `json:"title,omitempty"`
	Connector string `json:"connector,omitempty"`
	Message   string `json:"message"`
	Arguments string `json:"arguments"`
}

func configureMCPApproval(c *SessionContext, raw json.RawMessage) {
	c.Kind, c.ApprovalBlocked = SessionContextQuestion, "mcp"
	c.Text = "MCP request requires review in Codex."
	var p struct {
		Mode    string          `json:"mode"`
		Server  string          `json:"serverName"`
		Message string          `json:"message"`
		Schema  json.RawMessage `json:"requestedSchema"`
		Meta    struct {
			Kind      string          `json:"codex_approval_kind"`
			Name      string          `json:"tool_name"`
			Title     string          `json:"tool_title"`
			Connector string          `json:"connector_name"`
			Arguments json.RawMessage `json:"tool_params"`
			Display   json.RawMessage `json:"tool_params_display"`
			Persist   json.RawMessage `json:"persist"`
		} `json:"_meta"`
	}
	if len(raw) > approvalTextLimit || json.Unmarshal(raw, &p) != nil {
		return
	}
	if p.Meta.Kind != "mcp_tool_call" {
		// General forms, browser auth and device verification need different
		// workflows. Observe them without advertising actionable capability.
		c.Text = SanitizeSessionContext(c.Text + "\n" + p.Message)
		return
	}
	c.Kind = SessionContextApproval
	c.Text = "MCP TOOL APPROVAL\n" + p.Message + "\nMCP server: " + p.Server
	if p.Server == "" || strings.TrimSpace(p.Message) == "" || (p.Mode != "form" && p.Mode != "openai/form" && p.Mode != "openaiForm") || !emptyMCPApprovalSchema(p.Schema) {
		return
	}
	arguments, valid := mcpApprovalArguments(p.Meta.Arguments, p.Meta.Display)
	if !valid {
		return
	}
	d := MCPToolApprovalDetails{Server: p.Server, Name: p.Meta.Name, Title: p.Meta.Title, Connector: p.Meta.Connector, Message: p.Message, Arguments: arguments}
	for _, field := range []struct{ label, text string }{{"Tool", d.Name}, {"Title", d.Title}, {"App", d.Connector}} {
		if field.text != "" {
			c.Text += "\n" + field.label + ": " + field.text
		}
	}
	if arguments == "" {
		c.Text += "\nArguments: —"
	} else {
		c.Text += "\nArguments:\n" + arguments
	}
	if strings.ContainsAny(d.Server+d.Name+d.Title+d.Connector, "\n\r\t") {
		c.ApprovalBlocked = "sanitised"
		return
	}
	if !approvalTextFits(c.Text) {
		c.ApprovalBlocked = "truncated"
		return
	}
	if !approvalTextDisplayable(c.Text) {
		c.ApprovalBlocked = "sanitised"
		return
	}
	c.MCPTool = d
	add := func(kind, action, persist, detail string) {
		var meta any
		if persist != "" {
			meta = map[string]string{"persist": persist}
		}
		wire, _ := json.Marshal(struct {
			Action  string `json:"action"`
			Content any    `json:"content"`
			Meta    any    `json:"_meta"`
		}{action, nil, meta})
		for i := range c.ApprovalOptions {
			if c.ApprovalOptions[i].Kind == "" {
				c.ApprovalOptions[i] = ApprovalOption{Kind: kind, Value: kind, Wire: string(wire), Detail: detail}
				return
			}
		}
	}
	add("accept", "accept", "", "")
	for _, scope := range mcpApprovalScopes(p.Meta.Persist) {
		switch scope {
		case "session":
			add("acceptForSession", "accept", scope, "Allow future calls covered by this tool approval for the current Codex session.")
		case "always":
			add("acceptMcpAlways", "accept", scope, "Allow future calls covered by this tool approval, including future Codex sessions.")
		}
	}
	add("cancelMcpTool", "cancel", "", "")
	c.ApprovalDecisions = "accept"
	for _, option := range c.ApprovalOptions[1:] {
		if option.Kind != "" {
			c.ApprovalDecisions += ", " + option.Kind
		}
	}
	c.ApprovalToken, c.ApprovalBlocked = rand.Text(), ""
}

func emptyMCPApprovalSchema(raw json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	return json.Unmarshal(raw, &schema) == nil && schema.Type == "object" && schema.Properties != nil && len(schema.Properties) == 0 && len(schema.Required) == 0
}

func mcpApprovalArguments(raw, display json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		// Upstream omits tool_name for some connector approvals, and may
		// provide display parameters instead of the raw argument object.
		if len(display) > 0 && !bytes.Equal(bytes.TrimSpace(display), []byte("null")) {
			var fields []struct {
				Name        string          `json:"name"`
				Value       json.RawMessage `json:"value"`
				DisplayName string          `json:"display_name"`
			}
			if json.Unmarshal(display, &fields) != nil {
				return "", false
			}
			if len(fields) == 0 {
				return "", true
			}
			params := make(map[string]json.RawMessage)
			for _, field := range fields {
				if field.Name == "" || len(field.Value) == 0 {
					return "", false
				}
				if _, duplicate := params[field.Name]; duplicate {
					return "", false
				}
				params[field.Name] = field.Value
			}
			raw, _ = json.Marshal(params)
		} else {
			return "", true // Omitted metadata is not proof of an empty argument object.
		}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return "", false
	}
	formatted, err := json.MarshalIndent(object, "", "  ")
	return string(formatted), err == nil && approvalTextFits(string(formatted))
}

func mcpApprovalScopes(raw json.RawMessage) []string {
	var one string
	var values []string
	if json.Unmarshal(raw, &one) == nil {
		values = []string{one}
	} else {
		_ = json.Unmarshal(raw, &values)
	}
	var scopes []string
	// Match the native UI order, regardless of metadata ordering/duplicates.
	for _, scope := range []string{"session", "always"} {
		for _, value := range values {
			if value == scope {
				scopes = append(scopes, scope)
				break
			}
		}
	}
	return scopes
}

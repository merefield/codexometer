package codex

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"time"
)

// Pending requests are keyed by JSON-RPC request id, so resolving one request
// cannot clear a different outstanding question/approval for the same thread.
type daemonContextState struct {
	promptToken string
	promptSpent bool
	completed   bool
	latest      SessionContext
	requests    map[string]SessionContext
	commands    map[string]contextCommandItem
}

type contextCommandItem struct{ Command, CWD string }

func daemonContextEvent(states map[string]*daemonContextState, method string, id, raw json.RawMessage, now time.Time) {
	var p struct {
		Turn struct {
			Status string `json:"status"`
		} `json:"turn"`
		TurnID               string            `json:"turnId"`
		ItemID               string            `json:"itemId"`
		CWD                  string            `json:"cwd"`
		Network              json.RawMessage   `json:"networkApprovalContext"`
		Permissions          json.RawMessage   `json:"additionalPermissions"`
		RequestedPermissions json.RawMessage   `json:"permissions"`
		Decisions            []json.RawMessage `json:"availableDecisions"`
		ThreadID             string            `json:"threadId"`
		GrantRoot            string            `json:"grantRoot"`
		RequestID            json.RawMessage   `json:"requestId"`
		Reason               string            `json:"reason"`
		Command              json.RawMessage   `json:"command"`
		Kind                 json.RawMessage   `json:"kind"`
		Questions            []contextQuestion `json:"questions"`
		IsBlocking           *bool             `json:"isBlocking"`
		Item                 struct {
			ID      string `json:"id"`
			CWD     string `json:"cwd"`
			Type    string `json:"type"`
			Text    string `json:"text"`
			Phase   string `json:"phase"`
			Command string `json:"command"`
		} `json:"item"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ThreadID == "" {
		return
	}
	state := states[p.ThreadID]
	if state == nil {
		if len(states) >= 256 {
			return
		}
		state = &daemonContextState{requests: map[string]SessionContext{}, commands: map[string]contextCommandItem{}}
		states[p.ThreadID] = state
	}
	c := SessionContext{At: now, ThreadID: p.ThreadID, TurnID: p.TurnID, ItemID: p.ItemID, Source: "LIVE"}
	switch method {
	case "turn/started", "thread/closed":
		delete(states, p.ThreadID)
		return
	case "turn/completed", "turn/interrupted":
		state.promptToken, state.promptSpent = "", false
		state.completed = method == "turn/completed" && p.Turn.Status == "completed"
		clear(state.requests)
		clear(state.commands)
		return
	case "serverRequest/resolved":
		delete(state.requests, string(p.RequestID))
		return
	case "item/commandExecution/requestApproval":
		kind := "command" // legacy servers omit the discriminator
		kindValid := len(p.Kind) == 0 || json.Unmarshal(p.Kind, &kind) == nil && kind == "command" && string(p.Kind) != "null"
		var command string
		commandValid := len(p.Command) == 0 || string(p.Command) == "null" || json.Unmarshal(p.Command, &command) == nil
		previous := state.commands[p.TurnID+"/"+p.ItemID]
		if command == "" {
			command = previous.Command
		}
		if p.CWD == "" {
			p.CWD = previous.CWD
		}
		c.Kind = SessionContextApproval
		c.Text = strings.TrimSpace(p.Reason + "\nCommand: " + command + "\nDirectory: " + p.CWD)
		special := func(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }
		if special(p.Network) {
			c.Text += "\nNetwork request: " + string(p.Network)
		}
		if special(p.Permissions) {
			c.Text += "\nPermissions: " + string(p.Permissions)
		}
		c.ApprovalOptions, c.ApprovalDecisions = commandApprovalOptions(p.Decisions)
		for _, option := range c.ApprovalOptions {
			if option.Detail != "" {
				c.Text += "\nPersistent command-prefix rule: " + option.Detail
			}
		}
		allowed := c.ApprovalOptions[0].Kind != ""
		// Fail closed if the display loses ANY command/request content, or if
		// this is a broader grant rather than an ordinary command decision.
		switch {
		case !kindValid:
			c.ApprovalBlocked = "approval-kind"
		case special(p.Network):
			c.ApprovalBlocked = "network"
		case special(p.Permissions):
			c.ApprovalBlocked = "permissions"
		case !commandValid:
			c.ApprovalBlocked = "command-format"
		case command == "":
			c.ApprovalBlocked = "missing-command"
		case p.CWD == "":
			c.ApprovalBlocked = "missing-directory"
		case p.TurnID == "" || p.ItemID == "":
			c.ApprovalBlocked = "missing-identity"
		case !allowed:
			c.ApprovalBlocked = "decisions"
		case len([]rune(c.Text)) > sessionContextLimit:
			c.ApprovalBlocked = "truncated"
		case SanitizeSessionContext(c.Text) != c.Text || SanitizeSessionContext(command) != command || SanitizeSessionContext(p.CWD) != p.CWD:
			c.ApprovalBlocked = "sanitised"
		default:
			c.ApprovalToken = rand.Text()
			c.CommandDetails = ApprovalCommandDetails{Justification: SanitizeSessionContext(p.Reason), Command: command, Directory: p.CWD}
		}
	case "item/fileChange/requestApproval":
		c.ApprovalBlocked = "file-change"
		c.Kind, c.Text = SessionContextApproval, p.Reason
		if c.Text == "" {
			c.Text = "File changes requested"
		}
		if p.GrantRoot != "" {
			c.Text += "\nRoot: " + p.GrantRoot
		}
	case "item/permissions/requestApproval":
		c.ApprovalBlocked = "permissions"
		c.Kind, c.Text = SessionContextApproval, p.Reason
		if c.Text == "" {
			c.Text = "Additional permissions requested"
		}
		if len(p.RequestedPermissions) > 0 {
			c.Text += "\nPermissions: " + string(p.RequestedPermissions)
		}
		if p.CWD != "" {
			c.Text += "\nDirectory: " + p.CWD
		}
	case "item/tool/requestUserInput", "tool/requestUserInput":
		if p.IsBlocking != nil && !*p.IsBlocking {
			return
		}
		c.Kind, c.Text = SessionContextQuestion, questionContext(p.Questions)
		if p.TurnID != "" && p.ItemID != "" && validPromptQuestions(p.Questions) && SanitizeSessionContext(c.Text) == c.Text {
			encoded, _ := json.Marshal(p.Questions)
			if len(encoded) <= 8192 {
				c.InputToken, c.InputQuestions = rand.Text(), string(encoded)
			}
		}
	case "item/completed":
		for key, request := range state.requests {
			if p.Item.ID != "" && request.ItemID == p.Item.ID && request.TurnID == p.TurnID {
				delete(state.requests, key)
			}
		}
		delete(state.commands, p.TurnID+"/"+p.Item.ID)
		if p.Item.Type != "agentMessage" {
			return
		}
		c.Kind, c.Text = SessionContextActivity, p.Item.Text
		if p.Item.Phase == "final_answer" {
			c.Kind = SessionContextReply
		}
	case "item/started":
		if p.Item.Type != "commandExecution" {
			return
		}
		c.Kind, c.Text = SessionContextActivity, p.Item.Command
		if state.commands == nil {
			state.commands = map[string]contextCommandItem{}
		}
		if len(state.commands) < 64 {
			// Keep one character beyond the display limit so oversized items
			// remain ineligible for approval, without retaining unbounded text.
			bounded := func(s string) string {
				r := []rune(s)
				if len(r) > sessionContextLimit+1 {
					return string(r[:sessionContextLimit+1])
				}
				return s
			}
			state.commands[p.TurnID+"/"+p.Item.ID] = contextCommandItem{bounded(p.Item.Command), bounded(p.Item.CWD)}
		}
	default:
		return
	}
	c.Text = SanitizeSessionContext(c.Text)
	if c.Text == "" {
		return
	}
	if c.pending() {
		state.promptToken = ""
		if len(id) == 0 || string(id) == "null" {
			return
		}
		if _, exists := state.requests[string(id)]; exists || len(state.requests) < 16 {
			state.requests[string(id)] = c
		}
	} else {
		state.latest = c
	}
}

func daemonContextSnapshot(states map[string]*daemonContextState, ids []string, statuses map[string]sessionRuntimeStatus) map[string]SessionContext {
	out := map[string]SessionContext{}
	for _, id := range ids {
		state := states[id]
		if state == nil {
			continue
		}
		c := state.latest
		for _, request := range state.requests {
			if request.Kind == SessionContextApproval && statuses[id] != sessionRuntimeApproval || request.Kind == SessionContextQuestion && statuses[id] != sessionRuntimeInput {
				continue
			}
			c = preferSessionContext(c, request)
		}
		if c.Text != "" {
			out[id] = c
		}
	}
	return out
}

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
	turnObserved                                    bool
	userItems                                       map[string]bool
	task, guidance                                  string
	guidanceID                                      string
	ended                                           bool
	streamItem, streamTurn, streamText, streamPhase string
	activeTurn                                      string
	turnToken                                       string
	activity                                        sessionActivityState
	approvalsLimited                                bool
	promptToken                                     string
	promptSpent                                     bool
	completed                                       bool
	latest                                          SessionContext
	requests                                        map[string]SessionContext
	commands                                        map[string]contextCommandItem
	patches                                         map[string]string
}

type contextCommandItem struct{ Command, CWD string }

func daemonContextEvent(states map[string]*daemonContextState, method string, id, raw json.RawMessage, now time.Time) {
	var p struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
		TurnID               string            `json:"turnId"`
		ItemID               string            `json:"itemId"`
		Delta                string            `json:"delta"`
		CWD                  string            `json:"cwd"`
		Network              json.RawMessage   `json:"networkApprovalContext"`
		Permissions          json.RawMessage   `json:"additionalPermissions"`
		RequestedPermissions json.RawMessage   `json:"permissions"`
		Decisions            []json.RawMessage `json:"availableDecisions"`
		ThreadID             string            `json:"threadId"`
		GrantRoot            string            `json:"grantRoot"`
		Changes              json.RawMessage   `json:"changes"`
		RequestID            json.RawMessage   `json:"requestId"`
		Reason               string            `json:"reason"`
		Command              json.RawMessage   `json:"command"`
		Kind                 json.RawMessage   `json:"kind"`
		Questions            []contextQuestion `json:"questions"`
		IsBlocking           *bool             `json:"isBlocking"`
		Item                 struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			ID       string          `json:"id"`
			CWD      string          `json:"cwd"`
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			Phase    string          `json:"phase"`
			Command  string          `json:"command"`
			Status   string          `json:"status"`
			ExitCode *int            `json:"exitCode"`
			Changes  json.RawMessage `json:"changes"`
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
	if (method == "item/started" || method == "item/completed" || method == "item/agentMessage/delta") &&
		(state.ended || state.activeTurn != "" && p.TurnID != state.activeTurn) {
		return
	}
	c := SessionContext{At: now, ThreadID: p.ThreadID, TurnID: p.TurnID, ItemID: p.ItemID, Source: "LIVE"}
	commandID := ""
	if p.Item.ID != "" {
		commandID = p.TurnID + "/" + p.Item.ID
	}
	switch method {
	case "turn/started":
		states[p.ThreadID] = &daemonContextState{activeTurn: p.Turn.ID, turnObserved: true, requests: map[string]SessionContext{}, commands: map[string]contextCommandItem{}}
		return
	case "thread/closed":
		delete(states, p.ThreadID)
		return
	case "turn/completed", "turn/interrupted":
		// Keep the observed task alongside its reply until the next turn.
		state.ended = true
		state.userItems = nil
		state.turnObserved = false
		state.streamItem, state.streamText = "", ""
		state.latest.Streaming = false
		state.promptToken, state.promptSpent = "", false
		state.completed = method == "turn/completed" && p.Turn.Status == "completed"
		clear(state.requests)
		state.approvalsLimited = false
		clear(state.commands)
		clear(state.patches)
		if state.latest.Kind == SessionContextActivity && state.latest.Activity.Command != "" {
			state.latest.Text = state.latest.Activity.Prose
		}
		state.activity = sessionActivityState{}
		state.latest.Activity = SessionActivity{}
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
		// Check completeness and supported action semantics independently of
		// normal display formatting. Preserve the original command and directory.
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
		case !approvalTextFits(c.Text) || !approvalTextFits(p.Reason) || !approvalTextFits(command) || !approvalTextFits(p.CWD):
			c.ApprovalBlocked = "truncated"
		case !approvalTextDisplayable(c.Text) || !approvalTextDisplayable(p.Reason) || !approvalTextDisplayable(command) || !approvalTextDisplayable(p.CWD):
			c.ApprovalBlocked = "sanitised"
		default:
			c.ApprovalToken = rand.Text()
			c.CommandDetails = ApprovalCommandDetails{Justification: SanitizeApprovalText(p.Reason), Command: command, Directory: p.CWD}
		}
	case "item/fileChange/requestApproval":
		c.Kind, c.Text = SessionContextApproval, p.Reason
		if c.Text == "" {
			c.Text = "File changes requested"
		}
		if p.GrantRoot != "" {
			c.Text += "\nRoot: " + p.GrantRoot
		}
		c.FileChanges = state.patches[p.TurnID+"/"+p.ItemID]
		configureFileApproval(&c, p.GrantRoot, state)
	case "item/fileChange/patchUpdated":
		state.capturePatch(p.TurnID, p.ItemID, p.Changes)
		return
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
		if p.Item.Type == "userMessage" {
			if state.observeUserMessage(p.Item.ID, p.TurnID, userMessageText(p.Item.Content)) && state.latest.Text == "" {
				c.Kind = SessionContextActivity
				state.latest = c
			}
			return
		}
		for key, request := range state.requests {
			if p.Item.ID != "" && request.ItemID == p.Item.ID && request.TurnID == p.TurnID {
				delete(state.requests, key)
			}
		}
		delete(state.commands, p.TurnID+"/"+p.Item.ID)
		delete(state.patches, p.TurnID+"/"+p.Item.ID)
		if p.Item.Type == "commandExecution" {
			status := p.Item.Status
			if p.Item.ExitCode != nil {
				if *p.Item.ExitCode != 0 {
					status = "failed"
				} else if status != "failed" && status != "declined" {
					status = "completed"
				}
			}
			state.activity.command(commandID, p.Item.Command, status, true)
			if state.latest.Kind != SessionContextReply {
				state.latest = state.activity.context(c)
			}
			return
		}
		if p.Item.Type != "agentMessage" {
			return
		}
		if state.activeTurn != "" && p.TurnID != state.activeTurn {
			return
		}
		if state.streamItem == p.Item.ID {
			state.streamItem, state.streamText = "", ""
		}
		c.Kind, c.Text = SessionContextActivity, p.Item.Text
		if p.Item.Phase == "final_answer" {
			c.Kind = SessionContextReply
			state.activity = sessionActivityState{}
		} else {
			prose := SanitizeSessionContext(p.Item.Text)
			if prose == "" {
				return
			}
			state.activity.prose = prose
			state.activity.proseTurnID = p.TurnID
			c = state.activity.context(c)
		}
	case "item/started":
		if p.Item.Type == "fileChange" {
			state.capturePatch(p.TurnID, p.Item.ID, p.Item.Changes)
			return
		}
		if p.Item.Type == "userMessage" {
			return
		}
		if p.Item.Type == "agentMessage" {
			if state.activeTurn != "" && p.TurnID != state.activeTurn {
				return
			}
			state.streamItem, state.streamTurn, state.streamPhase = p.Item.ID, p.TurnID, p.Item.Phase
			state.streamText = boundedStreamText(p.Item.Text)
			return
		}
		if p.Item.Type != "commandExecution" {
			return
		}
		c.Kind, c.Text = SessionContextActivity, p.Item.Command
		// The lifecycle event establishes this even on older payloads that
		// omit status. Pending approvals still take presentation precedence.
		state.activity.command(commandID, p.Item.Command, "inProgress", false)
		c = state.activity.context(c)
		if state.commands == nil {
			state.commands = map[string]contextCommandItem{}
		}
		if len(state.commands) < 64 {
			// Keep one character beyond the display limit so oversized items
			// remain ineligible for approval, without retaining unbounded text.
			bounded := func(s string) string {
				r := []rune(s)
				if len(r) > approvalTextLimit+1 {
					return string(r[:approvalTextLimit+1])
				}
				return s
			}
			state.commands[p.TurnID+"/"+p.Item.ID] = contextCommandItem{bounded(p.Item.Command), bounded(p.Item.CWD)}
		}
	case "item/agentMessage/delta":
		if p.ItemID == "" || p.ItemID != state.streamItem || p.TurnID != state.streamTurn {
			return
		}
		state.streamText = boundedStreamText(state.streamText + p.Delta)
		c.ItemID, c.Streaming = p.ItemID, true
		c.Kind, c.Text = SessionContextActivity, SanitizeSessionContext(state.streamText)
		if state.streamPhase == "final_answer" {
			c.Kind = SessionContextReply
		} else {
			state.activity.prose, state.activity.proseTurnID = c.Text, p.TurnID
			c = state.activity.context(c)
		}
	default:
		return
	}
	if c.Kind == SessionContextApproval {
		c.Text = SanitizeApprovalText(c.Text)
	} else {
		c.Text = SanitizeSessionContext(c.Text)
	}
	if c.Text == "" {
		return
	}
	if c.pending() {
		state.promptToken = ""
		if len(id) == 0 || string(id) == "null" {
			return
		}
		c.RequestID = string(id)
		old, exists := state.requests[c.RequestID]
		if c.Kind == SessionContextApproval {
			prose := ""
			if p.TurnID != "" && p.TurnID == state.activity.proseTurnID {
				prose = state.activity.prose
			}
			if exists && old.Kind == c.Kind && old.TurnID == c.TurnID && old.ItemID == c.ItemID {
				// Replays refresh capabilities, not historical context.
				prose = old.ApprovalContext
			}
			c.ApprovalContext = distinctApprovalContext(prose, p.Reason)
		}
		if exists {
			// A replay refreshes the capability, not its queue position.
			c.At = old.At
		}
		if exists || len(state.requests) < 16 {
			state.requests[string(id)] = c
		} else if c.Kind == SessionContextApproval {
			state.approvalsLimited = true
		}
	} else {
		state.latest = c
	}
}

func distinctApprovalContext(prose, reason string) string {
	prose = SanitizeSessionContext(prose)
	if strings.EqualFold(strings.Join(strings.Fields(prose), " "), strings.Join(strings.Fields(SanitizeSessionContext(reason)), " ")) {
		return ""
	}
	return prose
}

func daemonContextSnapshot(states map[string]*daemonContextState, ids []string, statuses map[string]sessionRuntimeStatus) map[string]SessionContext {
	out := map[string]SessionContext{}
	for _, id := range ids {
		state := states[id]
		if state == nil {
			continue
		}
		c := state.latest
		count := 0
		for _, request := range state.requests {
			if request.Kind == SessionContextApproval && statuses[id] != sessionRuntimeApproval || request.Kind == SessionContextQuestion && statuses[id] != sessionRuntimeInput {
				continue
			}
			if request.Kind == SessionContextApproval {
				count++
			}
			c = preferSessionContext(c, request)
		}
		c.PendingApprovals = count
		c.CurrentTask, c.LatestGuidance = state.task, state.guidance
		c.LatestGuidanceID = state.guidanceID
		if c.ThreadID == "" && (c.CurrentTask != "" || c.LatestGuidance != "") {
			c.ThreadID, c.TurnID, c.Source = id, state.activeTurn, "LIVE"
		}
		c.PendingApprovalsLimited = state.approvalsLimited && statuses[id] == sessionRuntimeApproval
		if c.Text != "" || c.CurrentTask != "" || c.LatestGuidance != "" || c.PendingApprovalsLimited {
			out[id] = c
		}
	}
	return out
}

// Retain raw whitespace across deltas, sanitizing the assembled display only.
// The byte cap also bounds malicious escape-only streams before sanitization.
func boundedStreamText(text string) string {
	const limit = sessionContextLimit * 8
	if len(text) > limit {
		return text[:limit]
	}
	return text
}

func userMessageText(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var parts []string
	for _, item := range content {
		if item.Type == "text" {
			parts = append(parts, item.Text)
		}
	}
	return SanitizeSessionContext(strings.Join(parts, "\n"))
}

func (s *daemonContextState) observeUserMessage(id, turn, text string) bool {
	if id == "" || s.userItems[id] || s.completed || s.activeTurn != "" && turn != s.activeTurn || len(s.userItems) >= 64 {
		return false
	}
	if s.userItems == nil {
		s.userItems = map[string]bool{}
	}
	first := len(s.userItems) == 0
	s.userItems[id] = true
	if first && s.turnObserved {
		s.task = text
	} else {
		s.guidance = text
		s.guidanceID = id
	}
	return true
}

package codex

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Display-only, comparable projection. The exact native retry event is retained
// separately by the daemon adapter and is never exposed to either front end.
type SessionAutoReview struct {
	thread, turn, target, kind string
	ID, Status, Action         string
	ReviewFinding
}

type ReviewFinding struct {
	Risk          string `json:"risk,omitempty"`
	Authorization string `json:"authorization,omitempty"`
	Rationale     string `json:"rationale,omitempty"`
}

func (r SessionAutoReview) Active() bool { return r.Status == "inProgress" }

// ReviewAssessment describes evidence only, never approval authority. Attached
// means the subject is the whole command already printed by the interface.
type ReviewAssessment struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Decision string `json:"decision"`
	Status   string `json:"status"`
	Subject  string `json:"subject,omitempty"`
	Attached bool   `json:"attached,omitempty"`
	ReviewFinding
}

var reviewTitles = map[string]string{
	"command": "COMMAND REVIEW", "execve": "PROCESS REVIEW", "applyPatch": "FILE CHANGE REVIEW",
	"networkAccess": "NETWORK ACCESS REVIEW", "mcpToolCall": "TOOL REVIEW",
	"writeStdin": "TERMINAL INPUT REVIEW", "requestPermissions": "PERMISSIONS REVIEW",
}

var reviewDecisions = map[string]string{
	"inProgress": "AUTO REVIEWING", "denied": "BLOCKED BY AUTO-REVIEW", "approved": "AUTO-APPROVED",
	"timedOut": "REVIEW TIMED OUT", "aborted": "REVIEW ABORTED",
}

func (c SessionContext) ReviewAssessment() *ReviewAssessment {
	r := c.AutoReview
	if r.ID == "" || c.Kind == SessionContextReply || c.Kind == SessionContextFailure || c.Kind == SessionContextQuestion ||
		r.thread != "" && c.ThreadID != "" && c.ThreadID != r.thread || r.turn != "" && c.TurnID != "" && c.TurnID != r.turn {
		return nil
	}
	var key, command string
	switch c.Kind {
	case SessionContextActivity:
		key, command = c.Activity.commandKey, c.Activity.Command
	case SessionContextApproval:
		key, command = c.TurnID+"/"+c.ItemID, c.CommandDetails.Command
	}
	sameItem := r.target != "" && r.thread == c.ThreadID && r.turn == c.TurnID && key == r.turn+"/"+r.target
	if (command != "" || c.Kind == SessionContextApproval) && !sameItem || r.kind == "command" && command != "" && r.Action != command {
		return nil // Never put an earlier or unattributable verdict beside another action.
	}
	title := reviewTitles[r.kind]
	if title == "" {
		title = "ACTION REVIEW"
	}
	decision := reviewDecisions[r.Status]
	if decision == "" {
		decision = "OUTCOME UNKNOWN"
	}
	return &ReviewAssessment{ID: SanitizeSessionContext(r.ID), Title: title, Decision: decision, Status: r.Status, Subject: SanitizeSessionContext(r.Action),
		ReviewFinding: ReviewFinding{Risk: SanitizeSessionContext(r.Risk), Authorization: SanitizeSessionContext(r.Authorization), Rationale: SanitizeSessionContext(r.Rationale)},
		Attached:      sameItem && r.kind == "command" && command != ""}
}

// A human-readable subject for telemetry only. The exact native event is
// retained separately for the guarded /approve inspection and retry.
func reviewSubject(raw json.RawMessage) (string, string) {
	var a struct {
		Type, Command, Program, Target, Server, ToolName, ProcessID, Stdin, Reason string
		Argv, Files                                                                []string
	}
	if json.Unmarshal(raw, &a) != nil {
		return "", ""
	}
	var text string
	switch a.Type {
	case "command":
		text = a.Command
	case "execve":
		text = a.Program
		if len(a.Argv) > 0 {
			args := make([]string, len(a.Argv))
			for i, arg := range a.Argv {
				args[i] = strconv.Quote(arg)
			}
			text += " // argv: " + strings.Join(args, " ")
		}
	case "networkAccess":
		text = a.Target
	case "mcpToolCall":
		text = a.Server + " / " + a.ToolName
	case "applyPatch":
		text = strings.Join(a.Files, "\n")
	case "writeStdin":
		text = a.ProcessID + " // " + strconv.Quote(a.Stdin)
	case "requestPermissions":
		text = a.Reason
	}
	return a.Type, SanitizeSessionContext(text)
}

var retryFieldNames = map[string]string{
	"type": "Action type", "source": "Source", "command": "Command", "cwd": "Working directory",
	"program": "Program", "argv": "Arguments", "files": "Files", "approvalId": "Approval ID",
	"processId": "Process ID", "stdin": "Terminal input", "target": "Target", "host": "Host",
	"protocol": "Protocol", "port": "Port", "server": "Server", "toolName": "Tool",
	"connectorId": "Connector ID", "connectorName": "Connector", "toolTitle": "Tool title",
	"reason": "Reason", "permissions": "Requested access",
}

func retryInspection(kind string, raw json.RawMessage, finding ReviewFinding) (string, bool) {
	action, ok := reviewFields(raw, retryFieldNames)
	if !ok {
		return "", false
	}
	parts := []string{reviewTitles[kind] + " // " + reviewDecisions["denied"], "", action, ""}
	if finding.Risk != "" {
		parts = append(parts, "Risk: "+finding.Risk)
	}
	if finding.Authorization != "" {
		parts = append(parts, "User authorisation: "+finding.Authorization)
	}
	if finding.Rationale != "" {
		parts = append(parts, "Rationale: "+finding.Rationale)
	}
	parts = append(parts, "", "Scope: one retry of this action only.")
	doc := strings.Join(parts, "\n")
	return doc, approvalTextFits(doc) && SanitizeApprovalText(doc) == doc
}

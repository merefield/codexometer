package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

type SessionContextKind int

const (
	SessionContextNone SessionContextKind = iota
	SessionContextActivity
	SessionContextReply
	SessionContextQuestion
	SessionContextApproval
)

// SessionContext is a bounded, memory-only excerpt, never a generated summary.
// It is deliberately separate from token accounting and persisted preferences.
type SessionContext struct {
	CurrentTask      string
	LatestGuidance   string
	LatestGuidanceID string
	Streaming        bool
	Activity         SessionActivity
	// ApprovalContext is preceding same-thread/turn prose captured when the
	// request arrived. Display-only: never part of the authorised action.
	ApprovalContext string
	// PendingApprovals counts live requests, not inferred waiting agents.
	// On grouped session contexts it includes the root and its descendants.
	PendingApprovals int
	// PendingApprovalsLimited means the retained request cap was exceeded.
	// The count is a lower bound until the turn ends or the connection resets.
	PendingApprovalsLimited bool
	RequestID               string
	CommandDetails          ApprovalCommandDetails
	FileChanges             string // validated, bounded JSON; immutable comparable snapshot
	InputToken              string
	InputQuestions          string
	// ApprovalToken is an opaque, connection-local capability, never persisted.
	ApprovalToken string
	// ApprovalBlocked is a canonical diagnostic, with no request content.
	ApprovalBlocked   string
	ApprovalOptions   [8]ApprovalOption
	ApprovalDecisions string
	TurnID            string
	ItemID            string
	Kind              SessionContextKind
	Text              string
	At                time.Time
	ThreadID          string
	Source            string
}

// Structured display fields are populated only alongside a complete validated
// command request. Never infer command boundaries from justification prose.
type ApprovalCommandDetails struct {
	Justification string
	Command       string
	Directory     string
}

const sessionContextLimit = 4096

// Approval reviews need the complete request, rather than a telemetry excerpt.
// Keep a separate bounded budget; ordinary context retains its smaller limit.
const approvalTextLimit = 64 * 1024

// SanitizeSessionContext removes terminal escapes and control/bidi formatting.
// It does not promise to redact secrets embedded in ordinary message text.
func SanitizeSessionContext(text string) string {
	return sanitizeContextText(text, sessionContextLimit)
}

// SanitizeApprovalText preserves long review text within the approval budget.
// Callers must still reject a capability if sanitization changes its request.
func SanitizeApprovalText(text string) string {
	return sanitizeContextText(text, approvalTextLimit)
}

func sanitizeContextText(text string, limit int) string {
	text = ansi.Strip(strings.ToValidUTF8(text, "�"))
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	text = strings.ReplaceAll(text, "\t", "    ")
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return string(runes)
}

func (c SessionContext) pending() bool {
	return c.Kind == SessionContextQuestion || c.Kind == SessionContextApproval
}

func preferSessionContext(a, b SessionContext) SessionContext {
	if b.Text == "" && b.CurrentTask == "" && b.LatestGuidance == "" {
		return a
	}
	priority := func(c SessionContext) int {
		if c.Kind == SessionContextApproval {
			return 3
		}
		if c.Kind == SessionContextQuestion {
			return 2
		}
		return 1
	}
	pa, pb := priority(a), priority(b)
	tie := b.At.Equal(a.At) && (b.ThreadID < a.ThreadID || b.ThreadID == a.ThreadID && b.Text < a.Text)
	newer := b.At.After(a.At)
	if a.Kind == SessionContextApproval && b.Kind == SessionContextApproval {
		newer = b.At.Before(a.At)
		tie = b.At.Equal(a.At) && (b.ThreadID < a.ThreadID ||
			b.ThreadID == a.ThreadID && b.RequestID < a.RequestID)
	}
	if a.Text == "" && a.CurrentTask == "" && a.LatestGuidance == "" || pb > pa || pb == pa && (newer || tie) {
		return b
	}
	return a
}

type contextQuestion struct {
	ID       string `json:"id"`
	IsOther  bool   `json:"isOther"`
	IsSecret bool   `json:"isSecret"`
	Question string `json:"question"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}

func questionContext(questions []contextQuestion) string {
	var lines []string
	for _, q := range questions {
		lines = append(lines, q.Question)
		for _, o := range q.Options {
			line := "• " + o.Label
			if o.Description != "" {
				line += " — " + o.Description
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func contextCommand(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var parts []string
	if json.Unmarshal(raw, &parts) == nil {
		return strings.Join(parts, " ")
	}
	return ""
}

func rolloutContextRecord(line []byte, cursor *rolloutCursor) (SessionContext, bool) {
	var event struct {
		Type      string    `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		Ordinal   *uint64   `json:"ordinal"`
		Payload   struct {
			Type       string            `json:"type"`
			Role       string            `json:"role"`
			Phase      string            `json:"phase"`
			Message    string            `json:"message"`
			Last       string            `json:"last_agent_message"`
			Reason     string            `json:"reason"`
			Command    json.RawMessage   `json:"command"`
			CallID     string            `json:"call_id"`
			ExitCode   *int              `json:"exit_code"`
			Questions  []contextQuestion `json:"questions"`
			IsBlocking *bool             `json:"isBlocking"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &event) != nil || !tokenRecordIsOwned(event.Ordinal, cursor.subagentHistoryStartOrdinal, event.Timestamp, cursor.nonRoot, cursor.startedAt) {
		return SessionContext{}, false
	}
	c := SessionContext{At: event.Timestamp, ThreadID: cursor.threadID, Source: "LOCAL"}
	p := event.Payload
	if event.Type == "response_item" && p.Type == "message" && p.Role == "assistant" {
		if p.Phase != "commentary" && p.Phase != "final_answer" {
			return c, false
		}
		c.Kind = SessionContextActivity
		if p.Phase == "final_answer" {
			c.Kind = SessionContextReply
		}
		for _, part := range p.Content {
			if part.Type == "output_text" {
				c.Text += part.Text + "\n"
			}
		}
	} else if event.Type == "event_msg" {
		switch p.Type {
		case "task_started", "turn_started", "user_message":
			cursor.activity = sessionActivityState{}
			return SessionContext{}, true
		case "task_complete", "turn_complete":
			cursor.activity = sessionActivityState{}
			c.Kind, c.Text = SessionContextReply, p.Last
			if p.Last == "" && cursor.preview.Kind == SessionContextReply {
				return cursor.preview, true
			}
		case "agent_message":
			c.Kind, c.Text = SessionContextActivity, p.Message
			if p.Phase == "final_answer" {
				c.Kind = SessionContextReply
			}
		case "request_user_input":
			if p.IsBlocking != nil && !*p.IsBlocking {
				return c, false
			}
			c.Kind, c.Text = SessionContextQuestion, questionContext(p.Questions)
		case "exec_approval_request":
			c.Kind, c.Text = SessionContextApproval, strings.TrimSpace(p.Reason+"\n"+contextCommand(p.Command))
		case "apply_patch_approval_request", "request_permissions":
			c.Kind, c.Text = SessionContextApproval, p.Reason
		case "exec_command_begin":
			c.Kind, c.Text = SessionContextActivity, contextCommand(p.Command)
			cursor.activity.command(p.CallID, c.Text, "inProgress", false)
			c = cursor.activity.context(c)
		case "exec_command_end":
			if cursor.preview.Kind == SessionContextReply {
				return cursor.preview, true
			}
			status := "unknown"
			if p.ExitCode != nil {
				status = "completed"
				if *p.ExitCode != 0 {
					status = "failed"
				}
			}
			cursor.activity.command(p.CallID, contextCommand(p.Command), status, true)
			c = cursor.activity.context(c)
		default:
			return c, false
		}
	} else {
		return c, false
	}
	if c.Kind == SessionContextActivity && p.Type != "exec_command_begin" && p.Type != "exec_command_end" {
		prose := SanitizeSessionContext(c.Text)
		if prose == "" {
			return SessionContext{}, false
		}
		cursor.activity.prose = prose
		c = cursor.activity.context(c)
	}
	if c.Kind == SessionContextReply {
		cursor.activity = sessionActivityState{}
	}
	if c.Kind == SessionContextActivity && cursor.preview.pending() && p.Type != "exec_command_begin" {
		return cursor.preview, true
	}
	c.Text = SanitizeSessionContext(c.Text)
	return c, true
}

// Bootstrap only a bounded tail, not a complete conversation history. Files
// already being followed are handled by the existing incremental reader.
func latestSessionContext(path string, cursor *rolloutCursor) SessionContext {
	f, err := os.Open(path)
	if err != nil {
		return SessionContext{}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return SessionContext{}
	}
	start := max(info.Size()-256*1024, 0)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return SessionContext{}
	}
	data, err := io.ReadAll(io.LimitReader(f, 256*1024))
	if err != nil {
		return SessionContext{}
	}
	lines := bytes.Split(data, []byte{'\n'})
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	var latest SessionContext
	copyCursor := *cursor
	copyCursor.preview = SessionContext{}
	copyCursor.activity = sessionActivityState{}
	for _, line := range lines[:max(len(lines)-1, 0)] {
		if c, ok := rolloutContextRecord(line, &copyCursor); ok {
			latest = c
			copyCursor.preview = c
		}
	}
	cursor.activity = copyCursor.activity
	return latest
}

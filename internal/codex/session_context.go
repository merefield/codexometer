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
	// ApprovalToken is an opaque, connection-local capability, never persisted.
	ApprovalToken string
	TurnID        string
	ItemID        string
	Kind          SessionContextKind
	Text          string
	At            time.Time
	ThreadID      string
	Source        string
}

const sessionContextLimit = 4096

// SanitizeSessionContext removes terminal escapes and control/bidi formatting.
// It does not promise to redact secrets embedded in ordinary message text.
func SanitizeSessionContext(text string) string {
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
	if len(runes) > sessionContextLimit {
		return string(runes[:sessionContextLimit-1]) + "…"
	}
	return string(runes)
}

func (c SessionContext) pending() bool {
	return c.Kind == SessionContextQuestion || c.Kind == SessionContextApproval
}

func preferSessionContext(a, b SessionContext) SessionContext {
	if b.Text == "" {
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
	if a.Text == "" || pb > pa || pb == pa && (b.At.After(a.At) || tie) {
		return b
	}
	return a
}

type contextQuestion struct {
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
			return SessionContext{}, true
		case "task_complete", "turn_complete":
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
		default:
			return c, false
		}
	} else {
		return c, false
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
	for _, line := range lines[:max(len(lines)-1, 0)] {
		if c, ok := rolloutContextRecord(line, &copyCursor); ok {
			latest = c
			copyCursor.preview = c
		}
	}
	return latest
}

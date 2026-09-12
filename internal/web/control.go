package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

// Control capabilities never enter the shared snapshot. A keyed offer digest
// binds the browser's selection to the exact source request without disclosing
// Codex tokens. One pending confirmation bounds memory (including secret input).
type control struct {
	mu        sync.Mutex
	store     *store
	approvals codex.SessionApprovalClient
	prompts   codex.SessionPromptClient
	key       string
	pending   *preparedAction
	expiry    *time.Timer
}

type actionChoice struct {
	Label      string `json:"label"`
	Detail     string `json:"detail"`
	Persistent bool   `json:"persistent"`
}

type actionQuestion struct {
	Text     string   `json:"text"`
	Secret   bool     `json:"secret"`
	FreeText bool     `json:"freeText"`
	Options  []string `json:"options"`
}

type actionOffer struct {
	ID        string           `json:"id"`
	Session   string           `json:"session"`
	Thread    string           `json:"thread"`
	Directory string           `json:"directory"`
	Kind      string           `json:"kind"`
	Command   string           `json:"command"`
	Choices   []actionChoice   `json:"choices"`
	Questions []actionQuestion `json:"questions"`
}

type offeredAction struct {
	actionOffer
	token     string
	decisions []string
	prompt    codex.SessionPromptOffer
}

type actionRequest struct {
	Session      string   `json:"session"`
	Offer        string   `json:"offer"`
	Choice       *int     `json:"choice,omitempty"`
	Answers      []string `json:"answers,omitempty"`
	Confirmation string   `json:"confirmation,omitempty"`
}

type preparedAction struct {
	request actionRequest
	id      string
	until   time.Time
}

func newControl(source Source, store *store) *control {
	c := &control{store: store, key: rand.Text()}
	c.approvals, _ = source.(codex.SessionApprovalClient)
	c.prompts, _ = source.(codex.SessionPromptClient)
	return c
}

var errUnavailable = errors.New("Session action unavailable or changed; refresh and check Codex")

func (c *control) offer(id string) (offeredAction, error) {
	c.store.mu.Lock()
	ctx, exists := c.store.contexts[id]
	directory := c.store.directories[id]
	age := time.Since(c.store.state.SessionsAt)
	fresh := !c.store.state.SessionsError && age >= 0 && age < 10*time.Second
	c.store.mu.Unlock()
	if !exists || !fresh {
		return offeredAction{}, errUnavailable
	}
	o := offeredAction{actionOffer: actionOffer{Session: id, Thread: id, Directory: directory}}
	if ctx.Kind == codex.SessionContextApproval {
		if c.approvals == nil || ctx.ApprovalToken == "" || ctx.ApprovalBlocked != "" || ctx.CommandDetails.Command == "" || ctx.CommandDetails.Directory == "" || ctx.ThreadID == "" || !c.approvals.SessionApprovalPending(ctx.ApprovalToken) {
			return o, nil
		}
		o.Kind, o.token = "approval", ctx.ApprovalToken
		o.Command, o.Directory, o.Thread = ctx.CommandDetails.Command, ctx.CommandDetails.Directory, ctx.ThreadID
		for _, option := range ctx.ApprovalOptions {
			label := map[string]string{"accept": "APPROVE ONCE", "acceptForSession": "ALLOW FOR SESSION", "acceptWithExecpolicyAmendment": "ALWAYS ALLOW PREFIX", "decline": "DECLINE", "cancel": "REJECT & STOP TURN"}[option.Kind]
			if label == "" {
				continue
			}
			o.Choices = append(o.Choices, actionChoice{label, option.Detail, option.Kind == "acceptForSession" || option.Kind == "acceptWithExecpolicyAmendment"})
			o.decisions = append(o.decisions, option.Value)
		}
		if len(o.Choices) == 0 {
			return offeredAction{}, errUnavailable
		}
	} else if c.prompts != nil {
		thread := id
		if ctx.Kind == codex.SessionContextQuestion && ctx.ThreadID != "" {
			thread = ctx.ThreadID
		}
		o.prompt = c.prompts.SessionPrompt(thread)
		if o.prompt.Token == "" || o.prompt.ThreadID != thread {
			return o, nil
		}
		if len(o.prompt.Questions) > 0 {
			if ctx.Kind != codex.SessionContextQuestion || ctx.InputToken != o.prompt.Token {
				return o, nil
			}
		} else if ctx.Kind == codex.SessionContextQuestion {
			return o, nil
		}
		o.Kind, o.token, o.Thread = "prompt", o.prompt.Token, thread
		if thread != id {
			// Grouped telemetry owns the parent's directory, not the child's.
			// Question offers provide no authoritative child working directory.
			o.Directory = ""
		}
		for _, q := range o.prompt.Questions {
			o.Questions = append(o.Questions, actionQuestion{q.Text, q.Secret, q.FreeText, q.Options})
		}
	}
	if o.Kind != "" {
		data, _ := json.Marshal([]any{o.actionOffer, o.token, o.decisions, o.prompt})
		h := hmac.New(sha256.New, []byte(c.key))
		_, _ = h.Write(data)
		o.ID = hex.EncodeToString(h.Sum(nil))
	}
	return o, nil
}

func validAction(o offeredAction, r actionRequest) bool {
	if o.ID == "" || o.ID != r.Offer {
		return false
	}
	if o.Kind == "approval" {
		return len(r.Answers) == 0 && r.Choice != nil && *r.Choice >= 0 && *r.Choice < len(o.decisions)
	}
	if r.Choice != nil || len(r.Answers) != max(1, len(o.Questions)) {
		return false
	}
	for i, answer := range r.Answers {
		if strings.TrimSpace(answer) == "" || len([]rune(answer)) > 4096 || codex.SanitizeSessionContext(answer) != strings.TrimSpace(answer) {
			return false
		}
		if len(o.Questions) > 0 && !o.Questions[i].FreeText {
			found := false
			for _, option := range o.Questions[i].Options {
				found = found || answer == option
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func (c *control) handle(action, origin string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Header.Get("Origin") != origin || media != "application/json" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var body actionRequest
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF || body.Session == "" || len(body.Session) > 512 {
			http.Error(w, "Invalid request", 400)
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.pending != nil && !time.Now().Before(c.pending.until) {
			c.pending = nil
		}
		// Consume before any IO, even on failure. Never retry ambiguous writes.
		if action == "commit" {
			p := c.pending
			if p == nil || p.id != body.Confirmation || p.request.Session != body.Session || p.request.Offer != body.Offer {
				http.Error(w, errUnavailable.Error(), 409)
				return
			}
			c.pending = nil
			body = p.request // Only the exact server-prepared payload can be sent.
		}
		o, err := c.offer(body.Session)
		if err != nil {
			http.Error(w, errUnavailable.Error(), 409)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if action == "offer" {
			_ = json.NewEncoder(w).Encode(o.actionOffer)
			return
		}
		if !validAction(o, body) {
			http.Error(w, errUnavailable.Error(), 409)
			return
		}
		if action == "prepare" {
			p := &preparedAction{request: body, id: rand.Text(), until: time.Now().Add(30 * time.Second)}
			c.pending = p
			// Clear drafts, including secret answers, even if the browser disappears.
			if c.expiry == nil {
				c.expiry = time.AfterFunc(30*time.Second, func() {
					c.mu.Lock()
					defer c.mu.Unlock()
					if c.pending != nil && !time.Now().Before(c.pending.until) {
						c.pending = nil
					}
				})
			} else {
				c.expiry.Reset(30 * time.Second)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"confirmation": p.id, "expires": p.until})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if o.Kind == "approval" {
			err = c.approvals.RespondSessionApproval(ctx, o.token, o.decisions[*body.Choice])
		} else {
			err = c.prompts.SendSessionPrompt(ctx, o.token, body.Answers)
		}
		if err != nil {
			http.Error(w, "Outcome uncertain; check Codex before taking another action. Nothing was retried.", 502)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Sent. Waiting for Codex to update…"})
	}
}

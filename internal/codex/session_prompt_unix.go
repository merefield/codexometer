//go:build unix

package codex

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
)

// Runtime events invalidate editors immediately, between telemetry polls.
func (p *daemonStatusProvider) promptLifecycleLocked(method string, raw json.RawMessage) {
	var event struct {
		ThreadID string `json:"threadId"`
		Status   struct {
			Type        string   `json:"type"`
			ActiveFlags []string `json:"activeFlags"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &event) != nil || event.ThreadID == "" {
		return
	}
	status := sessionRuntimeUnknown
	switch method {
	case "turn/started":
		status = sessionRuntimeWorking
	case "turn/completed", "turn/interrupted":
		status = sessionRuntimeIdle
	case "thread/status/changed":
		status = parseSessionRuntimeStatus(event.Status.Type, event.Status.ActiveFlags)
	case "thread/closed":
		delete(p.statuses, event.ThreadID)
		delete(p.subscribed, event.ThreadID)
		return
	default:
		return
	}
	if p.statuses == nil {
		p.statuses = map[string]sessionRuntimeStatus{}
	}
	p.statuses[event.ThreadID] = status
	if status != sessionRuntimeIdle {
		if s := p.contexts[event.ThreadID]; s != nil {
			s.promptToken = ""
		}
	}
}

func (p *daemonStatusProvider) SessionPrompt(thread string) SessionPromptOffer {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.connection == nil {
		return SessionPromptOffer{}
	}
	if _, ok := p.subscribed[thread]; !ok {
		return SessionPromptOffer{}
	}
	s := p.contexts[thread]
	if s != nil && len(s.requests) > 0 {
		// Do not combine a question with an outstanding approval, or guess
		// which of multiple simultaneous input requests the user is answering.
		if len(s.requests) != 1 {
			return SessionPromptOffer{}
		}
		for _, c := range s.requests {
			return inputOffer(c)
		}
	}
	if p.statuses[thread] != sessionRuntimeIdle {
		return SessionPromptOffer{}
	}
	if s == nil {
		if len(p.contexts) >= 256 {
			return SessionPromptOffer{}
		}
		if p.contexts == nil {
			p.contexts = map[string]*daemonContextState{}
		}
		s = &daemonContextState{requests: map[string]SessionContext{}, commands: map[string]contextCommandItem{}}
		p.contexts[thread] = s
	}
	if s.promptToken == "" {
		if s.promptSpent {
			return SessionPromptOffer{}
		}
		s.promptToken = rand.Text()
	}
	return SessionPromptOffer{Token: s.promptToken, ThreadID: thread}
}

func (p *daemonStatusProvider) SendSessionPrompt(ctx context.Context, token string, answers []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	connection := p.connection
	var offer SessionPromptOffer
	var requestID string
	var state *daemonContextState
	if token != "" && connection != nil {
		for thread, s := range p.contexts {
			if _, ok := p.subscribed[thread]; !ok {
				continue
			}
			if s.promptToken == token && len(s.requests) == 0 && p.statuses[thread] == sessionRuntimeIdle {
				offer = SessionPromptOffer{Token: token, ThreadID: thread}
				state = s
				break
			}
			if len(s.requests) == 1 {
				for id, c := range s.requests {
					if c.InputToken == token {
						offer = inputOffer(c)
						requestID = id
						state = s
					}
				}
			}
		}
	}
	if state == nil || !validPromptAnswers(offer, answers) {
		p.mu.Unlock()
		return errors.New("prompt expired or answer invalid; check Codex")
	}
	// Consume the capability before any IO. Never automatically retry an
	// ambiguous write, or send it over a replacement connection.
	if requestID != "" {
		delete(state.requests, requestID)
	} else {
		state.promptToken = ""
		state.promptSpent = true
		p.statuses[offer.ThreadID] = sessionRuntimeWorking
	}
	p.mu.Unlock()
	if requestID != "" {
		mapped := map[string]any{}
		for i, q := range offer.Questions {
			mapped[q.ID] = map[string]any{"answers": []string{answers[i]}}
		}
		return p.write(connection, map[string]any{"id": json.RawMessage(requestID), "result": map[string]any{"answers": mapped}})
	}
	// Recheck live runtime immediately before dispatch: idle telemetry may
	// have aged while the user composed their prompt.
	var read struct {
		Thread struct {
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"thread"`
	}
	if err := p.requestOn(ctx, connection, "thread/read", map[string]any{"threadId": offer.ThreadID, "includeTurns": false}, &read); err != nil {
		return err
	}
	if read.Thread.Status.Type != "idle" {
		return errors.New("session is no longer idle; reply in Codex")
	}
	p.mu.Lock()
	unchanged := p.connection == connection && p.contexts[offer.ThreadID] == state && len(state.requests) == 0
	p.mu.Unlock()
	if !unchanged {
		return errors.New("session changed; reply in Codex")
	}
	return p.requestOn(ctx, connection, "turn/start", map[string]any{
		"threadId": offer.ThreadID,
		"input":    []any{map[string]any{"type": "text", "text": answers[0]}},
	}, nil)
}

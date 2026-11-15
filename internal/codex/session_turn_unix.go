//go:build unix

package codex

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/google/uuid"
)

func (p *daemonStatusProvider) SessionTurn(thread string) SessionPromptOffer {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.contexts[thread]
	_, subscribed := p.subscribed[thread]
	if p.connection == nil || !subscribed || s == nil || s.ended || s.activeTurn == "" || len(s.requests) != 0 || p.statuses[thread] != sessionRuntimeWorking {
		return SessionPromptOffer{}
	}
	if s.turnToken == "" {
		s.turnToken = rand.Text()
		s.steerMessageID = uuid.NewString()
	}
	return SessionPromptOffer{Token: s.turnToken, ThreadID: thread, TurnID: s.activeTurn, ClientUserMessageID: s.steerMessageID}
}

func (p *daemonStatusProvider) SendSessionTurn(ctx context.Context, offer SessionPromptOffer, action, text string) error {
	if action != "interrupt" && IsSessionCommand(text) {
		return errors.New("slash commands cannot be steered or queued; open the command menu")
	}
	text = literalSessionText(text)
	if err := ctx.Err(); err != nil {
		return err
	}
	if action != "interrupt" && action != "steer" && action != "queue" {
		return errors.New("unknown turn action")
	}
	if action != "interrupt" && !validPromptAnswers(SessionPromptOffer{}, []string{text}) {
		return errors.New("invalid message")
	}
	p.mu.Lock()
	s, connection := p.contexts[offer.ThreadID], p.connection
	_, subscribed := p.subscribed[offer.ThreadID]
	if connection == nil || !subscribed || s == nil || s.ended || offer.Token == "" || offer.TurnID == "" || s.turnToken != offer.Token || s.activeTurn != offer.TurnID || len(s.requests) != 0 || p.statuses[offer.ThreadID] != sessionRuntimeWorking {
		p.mu.Unlock()
		return errors.New("turn changed; review Codex before sending")
	}
	if action == "steer" && (offer.SteerMessageID() == "" || offer.SteerMessageID() != s.steerMessageID) {
		p.mu.Unlock()
		return errors.New("steer identity changed; review Codex before sending")
	}
	// Consume before IO; ambiguous mutations are never automatically retried.
	s.turnToken = ""
	p.mu.Unlock()
	params := map[string]any{"threadId": offer.ThreadID}
	method := "turn/" + action
	switch action {
	case "interrupt":
		params["turnId"] = offer.TurnID
	case "steer":
		params["expectedTurnId"] = offer.TurnID
		params["clientUserMessageId"] = offer.SteerMessageID()
		params["input"] = []any{map[string]any{"type": "text", "text": text}}
	case "queue":
		method = "thread/queue/add"
		params["clientUserMessageId"] = rand.Text()
		params["input"] = []any{map[string]any{"type": "text", "text": text}}
	}
	return p.requestOn(ctx, connection, method, params, nil)
}

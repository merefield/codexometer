package codex

import (
	"context"
	"errors"
)

// Queue entries belong to Codex, not to Codexometer's in-memory scheduler.
type SessionQueuedMessage struct {
	ID, Text, Token string
	Editable        bool
}

type SessionQueueClient interface {
	SessionQueue(context.Context, string) ([]SessionQueuedMessage, error)
	ChangeSessionQueue(context.Context, string, SessionQueuedMessage, string, bool) error
	SessionQueueRevision() uint64
}

func liveSessionCapability[T any](c Client) (capability T) {
	if c.LiveUsage != nil {
		capability, _ = c.LiveUsage.statusProvider.(T)
	}
	return capability
}
func (c Client) SessionQueue(ctx context.Context, thread string) ([]SessionQueuedMessage, error) {
	if p := liveSessionCapability[SessionQueueClient](c); p != nil {
		return p.SessionQueue(ctx, thread)
	}
	return nil, errors.New("native queue unavailable")
}
func (c Client) ChangeSessionQueue(ctx context.Context, thread string, item SessionQueuedMessage, text string, remove bool) error {
	if p := liveSessionCapability[SessionQueueClient](c); p != nil {
		return p.ChangeSessionQueue(ctx, thread, item, text, remove)
	}
	return errors.New("native queue unavailable")
}
func (c Client) SessionQueueRevision() uint64 {
	if p := liveSessionCapability[SessionQueueClient](c); p != nil {
		return p.SessionQueueRevision()
	}
	return 0
}

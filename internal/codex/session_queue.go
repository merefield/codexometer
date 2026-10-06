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

func (c Client) queueClient() SessionQueueClient {
	if c.LiveUsage != nil {
		if p, ok := c.LiveUsage.statusProvider.(SessionQueueClient); ok {
			return p
		}
	}
	return nil
}
func (c Client) SessionQueue(ctx context.Context, thread string) ([]SessionQueuedMessage, error) {
	if p := c.queueClient(); p != nil {
		return p.SessionQueue(ctx, thread)
	}
	return nil, errors.New("native queue unavailable")
}
func (c Client) ChangeSessionQueue(ctx context.Context, thread string, item SessionQueuedMessage, text string, remove bool) error {
	if p := c.queueClient(); p != nil {
		return p.ChangeSessionQueue(ctx, thread, item, text, remove)
	}
	return errors.New("native queue unavailable")
}
func (c Client) SessionQueueRevision() uint64 {
	if p := c.queueClient(); p != nil {
		return p.SessionQueueRevision()
	}
	return 0
}

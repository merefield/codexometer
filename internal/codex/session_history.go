package codex

import (
	"context"
	"errors"
)

const SessionHistoryLimit = 10

// Display-only completed turns. No approval/input capabilities are retained.
type SessionHistoryClient interface {
	SessionHistory(context.Context, string) ([]SessionContext, error)
}

var ErrSessionHistory = errors.New("session history unavailable")

func (c Client) SessionHistory(ctx context.Context, id string) ([]SessionContext, error) {
	if c.LiveUsage != nil {
		if p, ok := c.LiveUsage.statusProvider.(SessionHistoryClient); ok {
			return p.SessionHistory(ctx, id)
		}
	}
	return nil, ErrSessionHistory
}

// Restrict excerpts to display fields even when supplied by another reader.
func HistoryExcerpt(c SessionContext) SessionContext {
	return SessionContext{
		ThreadID: c.ThreadID, TurnID: c.TurnID, ItemID: c.ItemID,
		Kind: SessionContextReply, Source: c.Source, At: c.At,
		Text: SanitizeSessionContext(c.Text), CurrentTask: SanitizeSessionContext(c.CurrentTask),
		LatestGuidance: SanitizeSessionContext(c.LatestGuidance),
	}
}

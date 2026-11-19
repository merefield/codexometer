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
			turns, err := p.SessionHistory(ctx, id)
			if err == nil {
				c.LiveUsage.mu.Lock()
				for _, cursor := range c.LiveUsage.files {
					if cursor.threadID == id {
						for i := range turns {
							if turns[i].ReplyRestricted {
								cursor.answers.note(turns[i].TurnID, turns[i].ItemID)
							}
							cursor.answers.protect(&turns[i])
						}
						cursor.answers.protect(&cursor.preview)
					}
				}
				c.LiveUsage.mu.Unlock()
			}
			return turns, err
		}
	}
	return nil, ErrSessionHistory
}

// Restrict excerpts to display fields even when supplied by another reader.
func HistoryExcerpt(c SessionContext) SessionContext {
	if c.ReplyRestricted {
		c.withholdAnswer()
	}
	return SessionContext{
		ThreadID: c.ThreadID, TurnID: c.TurnID, ItemID: c.ItemID,
		Kind: SessionContextReply, Source: c.Source, At: c.At, ReplyRestricted: c.ReplyRestricted,
		Text: SanitizeSessionContext(c.Text), CurrentTask: SanitizeSessionContext(c.CurrentTask),
		LatestGuidance: SanitizeSessionContext(c.LatestGuidance),
	}
}

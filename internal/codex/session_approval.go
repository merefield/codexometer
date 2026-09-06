package codex

import (
	"context"
	"errors"
)

// SessionApprovalClient is deliberately separate from telemetry. Only live,
// fully displayed, ordinary command requests expose a one-use capability.
type SessionApprovalClient interface {
	SessionApprovalPending(string) bool
	RespondSessionApproval(context.Context, string, string) error
}

func (c Client) SessionApprovalPending(token string) bool {
	if c.LiveUsage == nil {
		return false
	}
	p, ok := c.LiveUsage.statusProvider.(SessionApprovalClient)
	return ok && p.SessionApprovalPending(token)
}

func (c Client) RespondSessionApproval(ctx context.Context, token, decision string) error {
	if c.LiveUsage != nil {
		if p, ok := c.LiveUsage.statusProvider.(SessionApprovalClient); ok {
			return p.RespondSessionApproval(ctx, token, decision)
		}
	}
	return errors.New("approval unavailable; respond in Codex")
}

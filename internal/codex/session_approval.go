package codex

import (
	"context"
	"errors"
)

// SessionApprovalClient is deliberately separate from telemetry. Only live,
// fully displayed ordinary command or file-change requests expose a one-use
// capability. Responses must select a supported choice for that exact request.
type SessionApprovalClient interface {
	SessionApprovalPending(string) bool
	RespondSessionApproval(context.Context, string, string) error
}

func (c Client) SessionApprovalPending(token string) bool {
	p, ok := sessionCapability[SessionApprovalClient](c)
	return ok && p.SessionApprovalPending(token)
}

func (c Client) RespondSessionApproval(ctx context.Context, token, decision string) error {
	if p, ok := sessionCapability[SessionApprovalClient](c); ok {
		return p.RespondSessionApproval(ctx, token, decision)
	}
	return errors.New("approval unavailable; respond in Codex")
}

// Passive clients must not claim they can present a human decision.
func (c Client) SetSessionHumanApprovalSupport(enabled bool) {
	if p, ok := sessionCapability[interface{ setHumanApprovalSupport(bool) }](c); ok {
		p.setHumanApprovalSupport(enabled)
	}
}

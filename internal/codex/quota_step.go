package codex

import (
	"context"
	"errors"
)

type QuotaStep struct {
	Threshold int
	Model     string
	Effort    string
	// An advertised tier name or ID; empty preserves speed.
	ServiceTier string
}
type QuotaSession struct {
	ID     string
	Model  string
	Effort string
	Tier   *string
}
type QuotaStepPolicyProvider interface{ QuotaStepPolicy() []QuotaStep }
type SessionSettingsClient interface {
	QuotaSessions(context.Context) ([]QuotaSession, error)
	ApplyQuotaProfile(context.Context, []QuotaSession, QuotaStep) (int, error)
	ResolveQuotaStep(context.Context, QuotaStep) (QuotaStep, error)
	CloseQuotaProfiles()
}

func (c Client) QuotaStepPolicy() []QuotaStep { return append([]QuotaStep(nil), c.QuotaSteps...) }
func (c Client) settingsClient() SessionSettingsClient {
	if c.LiveUsage != nil {
		p, _ := c.LiveUsage.statusProvider.(SessionSettingsClient)
		return p
	}
	return nil
}
func (c Client) QuotaSessions(ctx context.Context) ([]QuotaSession, error) {
	if p := c.settingsClient(); p != nil {
		return p.QuotaSessions(ctx)
	}
	return nil, errors.New("shared session control unavailable")
}
func (c Client) ApplyQuotaProfile(ctx context.Context, targets []QuotaSession, step QuotaStep) (int, error) {
	if p := c.settingsClient(); p != nil {
		return p.ApplyQuotaProfile(ctx, targets, step)
	}
	return 0, errors.New("shared session control unavailable")
}
func (c Client) ResolveQuotaStep(ctx context.Context, step QuotaStep) (QuotaStep, error) {
	if p := c.settingsClient(); p != nil {
		return p.ResolveQuotaStep(ctx, step)
	}
	return step, errors.New("shared session control unavailable")
}
func (c Client) CloseQuotaProfiles() {
	if p := c.settingsClient(); p != nil {
		p.CloseQuotaProfiles()
	}
}

// MatchesQuotaStep compares current state, not approval history. Resolve an
// advertised speed name to its ID first. Omitted speed imposes no constraint.
func (s QuotaSession) MatchesQuotaStep(step QuotaStep) bool {
	if s.Model != step.Model || s.Effort != step.Effort {
		return false
	}
	switch step.ServiceTier {
	case "":
		return true
	case "default":
		return s.Tier == nil
	default:
		return s.Tier != nil && *s.Tier == step.ServiceTier
	}
}

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
	RestoreSessionSettings(context.Context) (int, error)
	CloseQuotaProfiles(context.Context) (int, error)
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
func (c Client) RestoreSessionSettings(ctx context.Context) (int, error) {
	if p := c.settingsClient(); p != nil {
		return p.RestoreSessionSettings(ctx)
	}
	return 0, nil
}
func (c Client) CloseQuotaProfiles(ctx context.Context) (int, error) {
	if p := c.settingsClient(); p != nil {
		return p.CloseQuotaProfiles(ctx)
	}
	return 0, nil
}

//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func tierEqual(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func sameQuotaSettings(a, b QuotaSession) bool {
	return a.Model == b.Model && a.Effort == b.Effort && tierEqual(a.Tier, b.Tier)
}

// Only call for a thread positively observed as loaded. No settings overrides
// are supplied to resume; its response exposes configured speed as well.
func (p *daemonStatusProvider) readQuotaSession(ctx context.Context, id string) (QuotaSession, error) {
	var response struct {
		Model           string
		ReasoningEffort string
		ServiceTier     json.RawMessage
		Thread          struct {
			Model           string
			ReasoningEffort string
		}
	}
	err := p.request(ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &response)
	s := QuotaSession{ID: id, Model: response.Model, Effort: response.ReasoningEffort}
	if s.Model == "" {
		s.Model = response.Thread.Model
	}
	if s.Effort == "" {
		s.Effort = response.Thread.ReasoningEffort
	}
	if err != nil {
		return s, err
	}
	if s.Model == "" || s.Effort == "" || len(response.ServiceTier) == 0 {
		return s, errors.New("restorable model, effort or service tier unavailable")
	}
	if err = json.Unmarshal(response.ServiceTier, &s.Tier); err != nil {
		return s, err
	}
	return s, nil
}

func (p *daemonStatusProvider) QuotaSessions(ctx context.Context) ([]QuotaSession, error) {
	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	if p.settingsClosed {
		return nil, errors.New("quota controller is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := p.ensureConnected(ctx); err != nil {
		return nil, err
	}
	loaded, err := p.loadedThreads(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(loaded))
	for id := range loaded {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var sessions []QuotaSession
	var failures []error
	for _, id := range ids {
		s, err := p.readQuotaSession(ctx, id)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", id, err))
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, errors.Join(failures...)
}

// Resolve a requested speed against the model catalogue. "slow" is a name,
// never an assumed alias for flex; explicit standard routing is a sentinel.
func (p *daemonStatusProvider) validateQuotaStep(ctx context.Context, step QuotaStep) (QuotaStep, error) {
	var cursor any
	seen := map[string]bool{}
	for {
		var response struct {
			Data []struct {
				Model                     string
				SupportedReasoningEfforts []struct{ ReasoningEffort string }
				ServiceTiers              []struct {
					ID   string
					Name string
				}
			}
			NextCursor *string
		}
		params := map[string]any{}
		if cursor != nil {
			params["cursor"] = cursor
		}
		if err := p.request(ctx, "model/list", params, &response); err != nil {
			return step, err
		}
		for _, model := range response.Data {
			if model.Model != step.Model {
				continue
			}
			effortOK := false
			for _, effort := range model.SupportedReasoningEfforts {
				if effort.ReasoningEffort == step.Effort {
					effortOK = true
				}
			}
			if !effortOK {
				return step, errors.New("model does not advertise requested reasoning effort")
			}
			if step.ServiceTier == "" || step.ServiceTier == "default" {
				return step, nil
			}
			for _, tier := range model.ServiceTiers {
				if tier.ID == step.ServiceTier || strings.EqualFold(tier.Name, step.ServiceTier) {
					step.ServiceTier = tier.ID
					return step, nil
				}
			}
			return step, errors.New("model does not advertise requested speed tier")
		}
		if response.NextCursor == nil || seen[*response.NextCursor] {
			break
		}
		seen[*response.NextCursor] = true
		cursor = *response.NextCursor
	}
	return step, errors.New("requested model not advertised")
}

// The RPC acknowledgement means queued, not applied. Observe the actual
// configured settings before calling an update successful.
func (p *daemonStatusProvider) writeQuotaSettings(ctx context.Context, expected QuotaSession, params map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.request(ctx, "thread/settings/update", params, nil); err != nil {
		return err
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		loaded, err := p.loadedThreads(ctx)
		if err != nil {
			return err
		}
		if _, ok := loaded[expected.ID]; !ok {
			return errors.New("thread unloaded before settings could be verified")
		}
		current, err := p.readQuotaSession(ctx, expected.ID)
		if err != nil {
			return err
		}
		if sameQuotaSettings(current, expected) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("settings queued but application not verified")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (p *daemonStatusProvider) ApplyQuotaProfile(ctx context.Context, targets []QuotaSession, step QuotaStep) (int, error) {
	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	if p.settingsClosed {
		return 0, errors.New("quota controller is closed")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(targets) == 0 {
		return 0, nil
	}
	if err := p.ensureConnected(ctx); err != nil {
		return 0, err
	}
	step, err := p.validateQuotaStep(ctx, step)
	if err != nil {
		return 0, err
	}
	if p.originalSettings == nil {
		p.originalSettings = map[string]quotaOwnership{}
	}
	var failures []error
	updated := 0
	for _, target := range targets {
		if owner, ok := p.originalSettings[target.ID]; ok && owner.pending {
			failures = append(failures, fmt.Errorf("%s: prior update uncertain; restore before another profile", target.ID))
			continue
		}
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		loaded, err := p.loadedThreads(ctx)
		if err != nil {
			failures = append(failures, err)
			break
		}
		if _, ok := loaded[target.ID]; !ok {
			failures = append(failures, fmt.Errorf("%s: no longer loaded", target.ID))
			continue
		}
		current, err := p.readQuotaSession(ctx, target.ID)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", target.ID, err))
			continue
		}
		if !sameQuotaSettings(current, target) {
			failures = append(failures, fmt.Errorf("%s: settings changed since review; skipped", target.ID))
			continue
		}
		desired := current
		desired.Model = step.Model
		desired.Effort = step.Effort
		params := map[string]any{"threadId": target.ID, "model": step.Model, "effort": step.Effort}
		if step.ServiceTier != "" {
			if step.ServiceTier == "default" {
				desired.Tier = nil
				params["serviceTier"] = nil
			} else {
				tier := step.ServiceTier
				desired.Tier = &tier
				params["serviceTier"] = tier
			}
		}
		if sameQuotaSettings(current, desired) {
			updated++
			continue
		}
		owner, exists := p.originalSettings[target.ID]
		if !exists {
			owner.original = current
		} else {
			// Rebase fields changed manually since our last write.
			if current.Model != owner.applied.Model {
				owner.original.Model = current.Model
			}
			if current.Effort != owner.applied.Effort {
				owner.original.Effort = current.Effort
			}
			if !tierEqual(current.Tier, owner.applied.Tier) {
				owner.original.Tier = current.Tier
				owner.tierChanged = false
			}
		}
		owner.applied = desired
		owner.tierChanged = owner.tierChanged || step.ServiceTier != ""
		owner.pending = true
		// Keep recovery information even when the write outcome is uncertain.
		p.originalSettings[target.ID] = owner
		if err := p.writeQuotaSettings(ctx, desired, params); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", target.ID, err))
			continue
		}
		owner.pending = false
		p.originalSettings[target.ID] = owner
		updated++
	}
	return updated, errors.Join(failures...)
}

func (p *daemonStatusProvider) RestoreSessionSettings(ctx context.Context) (int, error) {
	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	return p.restoreQuotaLocked(ctx)
}
func (p *daemonStatusProvider) CloseQuotaProfiles(ctx context.Context) (int, error) {
	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	// Waiting for the lock drains running writes; queued writes must reject.
	p.settingsClosed = true
	return p.restoreQuotaLocked(ctx)
}
func (p *daemonStatusProvider) restoreQuotaLocked(ctx context.Context) (int, error) {
	if len(p.originalSettings) == 0 {
		return 0, nil
	}
	if err := p.ensureConnected(ctx); err != nil {
		return 0, err
	}
	loaded, err := p.loadedThreads(ctx)
	if err != nil {
		return 0, err
	}
	restored := 0
	var failures []error
	for id, owner := range p.originalSettings {
		if _, ok := loaded[id]; !ok {
			failures = append(failures, fmt.Errorf("%s: unloaded; restore manually", id))
			continue
		}
		current, err := p.readQuotaSession(ctx, id)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", id, err))
			continue
		}
		desired, params := quotaRestoration(owner, current)
		// Compensate an uncertain queued write even if the old settings still read
		// back: the acknowledged restore is ordered after our original update.
		if !sameQuotaSettings(current, desired) || owner.pending && len(params) > 1 {
			if err := p.writeQuotaSettings(ctx, desired, params); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", id, err))
				continue
			}
		}
		delete(p.originalSettings, id)
		restored++
	}
	return restored, errors.Join(failures...)
}

func quotaRestoration(owner quotaOwnership, current QuotaSession) (QuotaSession, map[string]any) {
	desired := current
	params := map[string]any{"threadId": current.ID}
	if owner.original.Model != owner.applied.Model && (current.Model == owner.applied.Model || owner.pending && current.Model == owner.original.Model) {
		desired.Model = owner.original.Model
		params["model"] = desired.Model
	}
	if owner.original.Effort != owner.applied.Effort && (current.Effort == owner.applied.Effort || owner.pending && current.Effort == owner.original.Effort) {
		desired.Effort = owner.original.Effort
		params["effort"] = desired.Effort
	}
	if owner.tierChanged && (tierEqual(current.Tier, owner.applied.Tier) || owner.pending && tierEqual(current.Tier, owner.original.Tier)) {
		desired.Tier = owner.original.Tier
		params["serviceTier"] = desired.Tier
	}
	return desired, params
}

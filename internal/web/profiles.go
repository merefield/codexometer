package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/merefield/codexometer/internal/codex"
	"time"
)

type profileReview struct {
	Session   string `json:"session"`
	Threshold int    `json:"threshold"`
	Current   string `json:"current"`
	Proposed  string `json:"proposed"`
	Pending   bool   `json:"pending"`
	Notice    string `json:"notice,omitempty"`
}

// Presentation adapter; the shared controller owns the policy lifecycle.
type profileControl struct {
	engine  *codex.QuotaProfiles
	client  codex.SessionSettingsClient
	steps   []codex.QuotaStep
	refresh time.Duration
}

func newProfileControl(source Source) *profileControl {
	policy, ok := source.(codex.QuotaStepPolicyProvider)
	client, supported := source.(codex.SessionSettingsClient)
	if !ok || !supported || len(policy.QuotaStepPolicy()) == 0 {
		return nil
	}
	return &profileControl{engine: codex.NewQuotaProfiles(client), client: client, steps: policy.QuotaStepPolicy(), refresh: time.Minute}
}

func (c *control) profileSnapshot() codex.Snapshot {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	if c.store.state.QuotaError {
		return codex.Snapshot{}
	}
	return c.store.quotaSnapshot
}

func profileText(model, effort string, tier *string) string {
	speed := "standard"
	if tier != nil && *tier != "default" {
		speed = *tier
	}
	return codex.SanitizeSessionContext(model + " / " + effort + " / " + speed)
}

func profileNotice(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, codex.ErrQuotaProfileUnverified) {
		return "Profile update accepted; awaiting verification. Nothing was retried."
	}
	return "Profile update failed: " + codex.SanitizeSessionContext(err.Error())
}

func (c *control) profileInventory(ctx context.Context) ([]offeredAction, error) {
	if c.profiles == nil {
		return nil, errUnavailable
	}
	p := c.profiles
	inventory, err := p.engine.Inventory(ctx, c.profileSnapshot(), p.steps, p.refresh)
	if err != nil {
		return nil, err
	}
	step, window, ok := codex.SelectQuotaProfile(c.profileSnapshot(), p.steps)
	if inventory.Step.Model == "" && !ok {
		return nil, nil
	}
	if !ok || step != inventory.Step || window != inventory.Window {
		return nil, errUnavailable
	}
	var offers []offeredAction
	for _, s := range inventory.Sessions {
		handled, seen := inventory.Handled[s.ID]
		pending := !seen || handled < step.Threshold
		notice := profileNotice(inventory.Outcomes[s.ID])
		if !pending && notice == "" {
			continue
		}
		proposed := profileText(step.Model, step.Effort, &inventory.Resolved.ServiceTier)
		if step.ServiceTier == "" {
			proposed = codex.SanitizeSessionContext(step.Model + " / " + step.Effort + " / speed unchanged")
		}
		review := &profileReview{Session: s.ID, Threshold: step.Threshold, Current: profileText(s.Model, s.Effort, s.Tier), Proposed: proposed, Pending: pending && step.Mode != "auto", Notice: notice}
		o := offeredAction{actionOffer: actionOffer{Session: s.ID, Thread: s.ID, Kind: "profile", Profile: review, Choices: []actionChoice{{Label: "APPLY PROFILE"}, {Label: "SKIP"}}}, profileTarget: s, profileStep: step, profileWindow: window}
		if pending {
			data, _ := json.Marshal([]any{s, step, inventory.Resolved, window})
			h := hmac.New(sha256.New, []byte(c.key))
			h.Write(data)
			o.ID = hex.EncodeToString(h.Sum(nil))
		}
		offers = append(offers, o)
	}
	return offers, nil
}

func (c *control) profileOffer(ctx context.Context, id string) (offeredAction, error) {
	offers, err := c.profileInventory(ctx)
	if err != nil {
		return offeredAction{}, err
	}
	for _, o := range offers {
		if o.Session == id && o.Profile.Pending {
			return o, nil
		}
	}
	return offeredAction{actionOffer: actionOffer{Session: id}}, nil
}

func (c *control) commitProfile(ctx context.Context, o offeredAction, choice int) error {
	p := c.profiles
	_, err := p.engine.Apply(ctx, c.profileSnapshot(), p.steps, p.refresh, o.profileTarget, o.profileStep, o.profileWindow, choice == 1)
	return err
}

func (c *control) refreshProfiles(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	offers, err := c.profileInventory(ctx)
	if err == nil {
		for i, o := range offers {
			if o.profileStep.Mode == "auto" && o.ID != "" {
				offers[i].Profile.Notice = profileNotice(c.commitProfile(ctx, o, 0))
				break
			}
		}
	}
	reviews := []profileReview{}
	for _, o := range offers {
		if o.Profile.Pending || o.Profile.Notice != "" {
			reviews = append(reviews, *o.Profile)
		}
	}
	c.store.mu.Lock()
	c.store.state.Profiles = reviews
	c.store.state.ProfileError = err != nil
	c.store.publish()
	c.store.mu.Unlock()
}

func (c *control) collectProfiles(ctx context.Context, refresh time.Duration) func() {
	if c.profiles == nil {
		return func() {}
	}
	c.profiles.refresh = max(10*time.Second, refresh)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			request, cancel := context.WithTimeout(ctx, 8*time.Second)
			c.refreshProfiles(request)
			cancel()
		}
	}()
	return func() { <-done; c.profiles.client.CloseQuotaProfiles() }
}

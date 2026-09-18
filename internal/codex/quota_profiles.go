package codex

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// QuotaProfiles owns the policy lifecycle independently of either presentation.
// Its lock also serializes inventory and writes from browser tabs or UI commands.
type QuotaProfiles struct {
	mu       sync.Mutex
	client   SessionSettingsClient
	window   string
	handled  map[string]int
	outcomes map[string]error
}

type QuotaProfileInventory struct {
	Step     QuotaStep
	Resolved QuotaStep
	Window   string
	Sessions []QuotaSession
	Matched  []string
	Handled  map[string]int
	Outcomes map[string]error
}

func NewQuotaProfiles(client SessionSettingsClient) *QuotaProfiles {
	return &QuotaProfiles{client: client}
}

// CaptureQuotaProfile freezes only the quota data used by a queued UI command.
func CaptureQuotaProfile(s Snapshot) Snapshot {
	meter, _, ok := QuotaPolicyWindow(s)
	if !ok {
		return Snapshot{}
	}
	w := meter.Window
	if w.ResetsAt != nil {
		value := *w.ResetsAt
		w.ResetsAt = &value
	}
	if w.WindowDurationMins != nil {
		value := *w.WindowDurationMins
		w.WindowDurationMins = &value
	}
	return Snapshot{FetchedAt: s.FetchedAt, AccountFingerprint: s.AccountFingerprint, RateLimitsByLimitID: map[string]RateLimitSnapshot{"codex": {Secondary: &w}}}
}

// QuotaPolicyWindow identifies the longest ordinary Codex window and account.
func QuotaPolicyWindow(s Snapshot) (Meter, string, bool) {
	var selected Meter
	found := false
	duration := func(m Meter) int64 {
		if m.Window.WindowDurationMins == nil {
			return 0
		}
		return *m.Window.WindowDurationMins
	}
	for _, m := range s.Meters() {
		if m.Kind == MeterQuotaWindow && m.LimitID == "codex" && (!found || duration(m) > duration(selected)) {
			selected = m
			found = true
		}
	}
	if !found || selected.Window.ResetsAt == nil || s.AccountFingerprint == "" {
		return selected, "", false
	}
	return selected, fmt.Sprintf("%s:%d:%d", s.AccountFingerprint, duration(selected), *selected.Window.ResetsAt), true
}

func SelectQuotaProfile(s Snapshot, steps []QuotaStep) (QuotaStep, string, bool) {
	meter, window, ok := QuotaPolicyWindow(s)
	if !ok {
		return QuotaStep{}, "", false
	}
	var selected QuotaStep
	found := false
	for _, step := range steps {
		if step.Threshold <= meter.Window.UsedPercent && (!found || step.Threshold > selected.Threshold) {
			selected = step
			found = true
		}
	}
	return selected, window, found
}

func quotaProfileFresh(s Snapshot, refresh time.Duration) bool {
	meter, _, ok := QuotaPolicyWindow(s)
	age := time.Since(s.FetchedAt)
	return ok && *meter.Window.ResetsAt > time.Now().Unix() && !s.FetchedAt.IsZero() && age >= 0 && age <= 2*refresh
}

func (p *QuotaProfiles) setWindow(window string) {
	if p.window != window {
		p.window = window
		p.handled = map[string]int{}
		p.outcomes = map[string]error{}
	}
}

func (p *QuotaProfiles) Inventory(ctx context.Context, s Snapshot, steps []QuotaStep, refresh time.Duration) (QuotaProfileInventory, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	step, window, ok := SelectQuotaProfile(s, steps)
	out := QuotaProfileInventory{Step: step, Window: window, Handled: map[string]int{}, Outcomes: map[string]error{}}
	if !quotaProfileFresh(s, refresh) {
		return out, errors.New("quota is unavailable or stale")
	}
	p.setWindow(window)
	if !ok {
		return out, nil
	}
	resolved, err := p.client.ResolveQuotaStep(ctx, step)
	if err != nil {
		return out, err
	}
	out.Resolved = resolved
	sessions, err := p.client.QuotaSessions(ctx)
	for _, session := range sessions {
		if session.MatchesQuotaStep(resolved) {
			out.Matched = append(out.Matched, session.ID)
			delete(p.outcomes, session.ID)
			if step.Mode == "auto" {
				p.handled[session.ID] = max(p.handled[session.ID], step.Threshold)
			}
		} else {
			out.Sessions = append(out.Sessions, session)
		}
	}
	for id, threshold := range p.handled {
		out.Handled[id] = threshold
	}
	for id, err := range p.outcomes {
		out.Outcomes[id] = err
	}
	return out, err
}

// Apply binds to the observed quota window, active step and exact target. An
// attempt is consumed before IO, including on errors; no automatic retries.
func (p *QuotaProfiles) Apply(ctx context.Context, s Snapshot, steps []QuotaStep, refresh time.Duration, target QuotaSession, step QuotaStep, window string, skip bool) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	current, currentWindow, ok := SelectQuotaProfile(s, steps)
	if !ok || current != step || currentWindow != window || !quotaProfileFresh(s, refresh) {
		return 0, errors.New("quota profile changed or is stale")
	}
	p.setWindow(window)
	if threshold, done := p.handled[target.ID]; done && threshold >= step.Threshold {
		return 0, errors.New("session already handled for this threshold")
	}
	p.handled[target.ID] = step.Threshold
	if skip {
		return 0, nil
	}
	n, err := p.client.ApplyQuotaProfile(ctx, []QuotaSession{target}, step)
	if err != nil {
		p.outcomes[target.ID] = err
	} else {
		delete(p.outcomes, target.ID)
	}
	return n, err
}

// Skip is presentation-local consent bookkeeping and never performs IO.
func (p *QuotaProfiles) Skip(id string, threshold int, window string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setWindow(window)
	p.handled[id] = max(p.handled[id], threshold)
}

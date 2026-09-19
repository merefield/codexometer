package codex

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type policyTestClient struct{ writes int }

func (c *policyTestClient) QuotaSessions(context.Context) ([]QuotaSession, error) {
	return []QuotaSession{{ID: "one", Model: "large", Effort: "high"}}, nil
}
func (c *policyTestClient) ResolveQuotaStep(_ context.Context, s QuotaStep) (QuotaStep, error) {
	return s, nil
}
func (c *policyTestClient) ApplyQuotaProfile(context.Context, []QuotaSession, QuotaStep) (int, error) {
	c.writes++
	return 0, ErrQuotaProfileUnverified
}
func (c *policyTestClient) CloseQuotaProfiles() {}

func TestQuotaProfilesConcurrentAttemptsAndWindowReset(t *testing.T) {
	client := &policyTestClient{}
	p := NewQuotaProfiles(client)
	s := DemoSnapshot()
	s.AccountFingerprint = "account"
	s.FetchedAt = time.Now()
	s.RateLimits.Secondary.UsedPercent = 85
	steps := []QuotaStep{{Threshold: 80, Model: "small", Effort: "medium", Mode: "auto"}}
	inventory, err := p.Inventory(context.Background(), s, steps, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			p.Apply(context.Background(), s, steps, time.Minute, inventory.Sessions[0], inventory.Step, inventory.Window, false)
		})
	}
	wg.Wait()
	if client.writes != 1 {
		t.Fatal("concurrent presentations retried uncertain write")
	}
	current, err := p.Inventory(context.Background(), s, steps, time.Minute)
	if err != nil || current.Handled["one"] != 80 || !errors.Is(current.Outcomes["one"], ErrQuotaProfileUnverified) {
		t.Fatal("uncertain outcome lost")
	}
	reset := *s.RateLimits.Secondary.ResetsAt + 3600
	s.RateLimits.Secondary.ResetsAt = &reset
	current, err = p.Inventory(context.Background(), s, steps, time.Minute)
	if err != nil || len(current.Handled) != 0 || len(current.Outcomes) != 0 {
		t.Fatal("new window retained old attempts")
	}
}

func TestQuotaProfileCaptureAndSelection(t *testing.T) {
	s := DemoSnapshot()
	s.AccountFingerprint = "account"
	s.FetchedAt = time.Now()
	s.RateLimits.Secondary.UsedPercent = 85
	frozen := CaptureQuotaProfile(s)
	step, window, ok := SelectQuotaProfile(s, []QuotaStep{{Threshold: 95}, {Threshold: 80, Model: "target"}, {Threshold: 50}})
	if !ok || step.Threshold != 80 {
		t.Fatal("wrong active policy")
	}
	s.RateLimits.Secondary.UsedPercent = 99
	*s.RateLimits.Secondary.ResetsAt += 3600
	old, oldWindow, ok := SelectQuotaProfile(frozen, []QuotaStep{{Threshold: 80, Model: "target"}, {Threshold: 95}})
	if !ok || old != step || oldWindow != window {
		t.Fatal("queued capture changed with source snapshot")
	}
}

func TestQuotaStepStatusesAreSortedAndClassified(t *testing.T) {
	s := DemoSnapshot()
	s.AccountFingerprint = "account"
	s.RateLimits.Secondary.UsedPercent = 65
	statuses, used := QuotaStepStatuses(s, []QuotaStep{{Threshold: 90}, {Threshold: 25}, {Threshold: 80}, {Threshold: 50}})
	want := []QuotaStepStage{QuotaStepPassed, QuotaStepActive, QuotaStepNext, QuotaStepArmed}
	if used == nil || *used != 65 || len(statuses) != len(want) {
		t.Fatalf("status summary = %#v, used=%v", statuses, used)
	}
	for index, stage := range want {
		if statuses[index].Stage != stage {
			t.Fatalf("status %d = %#v, want %s", index, statuses[index], stage)
		}
	}
	if statuses[2].Remaining != 15 {
		t.Fatalf("next remaining = %d", statuses[2].Remaining)
	}

	configured, used := QuotaStepStatuses(Snapshot{}, []QuotaStep{{Threshold: 80}})
	if used != nil || configured[0].Stage != QuotaStepConfigured {
		t.Fatalf("unobserved status = %#v, used=%v", configured, used)
	}
}

func TestQuotaProfilesStaleAttemptConsumed(t *testing.T) {
	c := &policyTestClient{}
	p := NewQuotaProfiles(c)
	s := DemoSnapshot()
	s.AccountFingerprint = "account"
	s.FetchedAt = time.Now()
	s.RateLimits.Secondary.UsedPercent = 85
	steps := []QuotaStep{{Threshold: 80, Model: "small", Mode: "auto"}, {Threshold: 95, Model: "tiny", Mode: "auto"}}
	inv, err := p.Inventory(context.Background(), s, steps, time.Minute)
	if err != nil || len(inv.Sessions) != 1 {
		t.Fatalf("inventory: %+v, %v", inv, err)
	}
	s.FetchedAt = time.Now().Add(-time.Hour)
	if _, err := p.Apply(context.Background(), s, steps, time.Minute, inv.Sessions[0], inv.Step, inv.Window, false); err == nil {
		t.Fatal("stale apply accepted")
	}
	s.FetchedAt = time.Now()
	inv, _ = p.Inventory(context.Background(), s, steps, time.Minute)
	if inv.Handled["one"] != 80 || inv.Outcomes["one"] == nil || c.writes != 0 {
		t.Fatal("stale attempt not retained")
	}
	p.Apply(context.Background(), s, steps, time.Minute, inv.Sessions[0], inv.Step, inv.Window, false)
	if c.writes != 0 {
		t.Fatal("stale attempt retried")
	}
	s.RateLimits.Secondary.UsedPercent = 96
	inv, _ = p.Inventory(context.Background(), s, steps, time.Minute)
	p.Apply(context.Background(), s, steps, time.Minute, inv.Sessions[0], inv.Step, inv.Window, false)
	if c.writes != 1 {
		t.Fatal("later threshold blocked")
	}
	reset := *s.RateLimits.Secondary.ResetsAt + 3600
	s.RateLimits.Secondary.ResetsAt = &reset
	inv, _ = p.Inventory(context.Background(), s, steps, time.Minute)
	if len(inv.Handled) != 0 || len(inv.Outcomes) != 0 {
		t.Fatal("new window retains old attempt")
	}
}

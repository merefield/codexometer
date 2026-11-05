package quotaestimate

import (
	"math"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestIndependentEngineSeparatesSlotsAndPreservesRevisedResetProgress(t *testing.T) {
	now := time.Unix(1800000000, 0)
	duration := int64(300)
	reset := now.Add(time.Hour).Unix()
	s := codex.Snapshot{AccountFingerprint: "synthetic", RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 10, WindowDurationMins: &duration, ResetsAt: &reset}, Secondary: &codex.Window{UsedPercent: 30, WindowDurationMins: &duration, ResetsAt: &reset}}}
	var state State
	state.Observe(s, codex.LiveUsageSnapshot{}, now)
	if len(state.Anchors) != 2 {
		t.Fatal("same duration slots shared an anchor", state.Anchors)
	}
	s.RateLimits.Primary.UsedPercent = 12
	reset++
	state.Observe(s, codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1}, now.Add(time.Minute))
	a := state.Anchors[Key(s, s.Meters()[0])]
	if a.UsedPercent != 10 || a.RestartReason != "" {
		t.Fatal("reset revision discarded clean movement", a)
	}
	s.RateLimits.Primary.UsedPercent = 15
	state.Observe(s, codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1}, now.Add(2*time.Minute))
	if estimate, ok := state.Estimate(s, s.Meters()[0], now.Add(2*time.Minute)); !ok || estimate.Samples != 1 || estimate.FullLow != 100.0/6 {
		t.Fatal("independent estimate", estimate, ok)
	}
	state.ObserveCoverage("missing local configuration")
	if len(state.Anchors) != 0 || len(state.Evidence) != 0 {
		t.Fatal("coverage gap retained evidence")
	}
}

func TestEngineRestartsTaintedIntervals(t *testing.T) {
	now := time.Unix(1800000000, 0)
	duration, reset := int64(300), now.Add(time.Hour).Unix()
	initial := codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1}
	for _, tc := range []struct {
		name, reason string
		change       func(*codex.Snapshot, *codex.LiveUsageSnapshot)
	}{
		{"account", RestartAccountChanged, func(s *codex.Snapshot, _ *codex.LiveUsageSnapshot) { s.AccountFingerprint = "other" }},
		{"plan", RestartWindowChanged, func(s *codex.Snapshot, _ *codex.LiveUsageSnapshot) { p := "pro"; s.RateLimits.PlanType = &p }},
		{"duration", RestartWindowChanged, func(s *codex.Snapshot, _ *codex.LiveUsageSnapshot) {
			d := int64(600)
			s.RateLimits.Primary.WindowDurationMins = &d
		}},
		{"reset", RestartWindowReset, func(s *codex.Snapshot, _ *codex.LiveUsageSnapshot) { s.RateLimits.Primary.UsedPercent = 0 }},
		{"rebased", RestartAccountingRebased, func(_ *codex.Snapshot, u *codex.LiveUsageSnapshot) { u.APIEqUSD = 0 }},
		{"adjustment", RestartAccountingRebased, func(_ *codex.Snapshot, u *codex.LiveUsageSnapshot) { u.APIEqAccountingAdjustments++ }},
		{"unpriced", RestartUnpricedUsage, func(_ *codex.Snapshot, u *codex.LiveUsageSnapshot) { u.APIEqUnpricedCalls++ }},
		{"missing", RestartMissingUsage, func(_ *codex.Snapshot, u *codex.LiveUsageSnapshot) { u.APIEqMissingUsageCalls++ }},
		{"inconsistent", RestartInconsistentUsage, func(_ *codex.Snapshot, u *codex.LiveUsageSnapshot) { u.APIEqInconsistentUsageCalls++ }},
		{"no cost movement", RestartCoverageGap, func(_ *codex.Snapshot, u *codex.LiveUsageSnapshot) { *u = initial }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := codex.Snapshot{AccountFingerprint: "synthetic", RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 10, WindowDurationMins: &duration, ResetsAt: &reset}}}
			var state State
			state.Observe(snapshot, initial, now)
			snapshot.RateLimits.Primary.UsedPercent = 15
			usage := codex.LiveUsageSnapshot{APIEqUSD: 2, APIEqPricedCalls: 2}
			tc.change(&snapshot, &usage)
			state.Observe(snapshot, usage, now.Add(time.Minute))
			if a := state.Anchors[Key(snapshot, snapshot.Meters()[0])]; a.RestartReason != tc.reason || len(state.Evidence) != 0 {
				t.Fatalf("tainted interval: anchor=%+v evidence=%+v", a, state.Evidence)
			}
		})
	}
}

func TestEngineValidatesEvidenceAndBoundsConfidence(t *testing.T) {
	now := time.Unix(1800000000, 0)
	snapshot := codex.Snapshot{AccountFingerprint: "synthetic", RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 25}}}
	meter := snapshot.Meters()[0]
	sample := Sample{Key: Key(snapshot, meter), CapacityUSD: 100, LowUSD: 90, HighUSD: 110, DeltaPercent: 10, ObservedAtUnix: now.Unix(), PricingRetrievedOn: codex.StandardAPIPricingRetrievedOn}
	for _, tc := range []struct {
		name   string
		change func(*Sample)
	}{
		{"old", func(s *Sample) { s.ObservedAtUnix = now.Add(-MaxAge - time.Second).Unix() }},
		{"future", func(s *Sample) { s.ObservedAtUnix = now.Add(time.Hour + time.Second).Unix() }},
		{"pricing", func(s *Sample) { s.PricingRetrievedOn = "old" }},
		{"nan", func(s *Sample) { s.CapacityUSD = math.NaN() }},
		{"rounding", func(s *Sample) { s.DeltaPercent = MinimumDelta - 1 }},
		{"premium", func(s *Sample) { s.PremiumCapacityUSD = s.CapacityUSD }},
		{"bounds", func(s *Sample) { s.LowUSD = s.CapacityUSD + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := sample
			tc.change(&invalid)
			if got := ValidSamples([]Sample{invalid, sample}, now); len(got) != 1 || got[0] != sample {
				t.Fatalf("invalid evidence retained: %+v", got)
			}
		})
	}
	state := State{Evidence: []Sample{sample, sample, sample}}
	if estimate, ok := state.Estimate(snapshot, meter, now); !ok || estimate.Confidence != "MED" || estimate.CurrentLow != 22.5 || estimate.CurrentHigh != 27.5 {
		t.Fatalf("medium confidence estimate: %+v %v", estimate, ok)
	}
	state.Evidence[0].UnknownTier = true
	state.Evidence[0].PremiumCapacityUSD = 20
	if estimate, ok := state.Estimate(snapshot, meter, now); !ok || estimate.Confidence != "LOW" || !estimate.UnknownTier || !estimate.HasPremium {
		t.Fatalf("unknown tier must cap confidence: %+v %v", estimate, ok)
	}
	state.CoverageIssue = "gap"
	if _, ok := state.Estimate(snapshot, meter, now); ok {
		t.Fatal("coverage gap must suppress estimate")
	}
	state.CoverageIssue = ""
	state.Evidence = nil
	if _, ok := state.Estimate(snapshot, meter, now); ok {
		t.Fatal("empty evidence must suppress estimate")
	}
	var bounded []Sample
	for i := range MaxSamples + 3 {
		s := sample
		s.ObservedAtUnix += int64(i)
		bounded = trimSamples(append(bounded, s), sample.Key)
	}
	if len(bounded) != MaxSamples || bounded[0].ObservedAtUnix != sample.ObservedAtUnix+3 {
		t.Fatal("old evidence not trimmed", bounded)
	}
}

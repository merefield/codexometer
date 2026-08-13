package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func TestQuotaAPIEstimatorLearnsRangeAndCurrentSpend(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	snapshot := apiEqSnapshot(10, now.Add(4*time.Hour).Unix())
	model := New(nil, time.Minute)
	model.snapshot = snapshot
	model.observeQuotaAPIEq(snapshot, codex.LiveUsageSnapshot{}, now)

	snapshot = apiEqSnapshot(15, now.Add(4*time.Hour).Unix())
	model.snapshot = snapshot
	model.observeQuotaAPIEq(snapshot, codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1}, now.Add(time.Minute))
	meter := snapshot.Meters()[0]
	estimate, ok := model.quotaAPIEstimate(snapshot, meter)
	if !ok || estimate.samples != 1 || estimate.confidence != "LOW" {
		t.Fatalf("estimate = %#v, %v", estimate, ok)
	}
	if math.Abs(estimate.fullLow-100.0/6) > 1e-9 || math.Abs(estimate.fullHigh-25) > 1e-9 ||
		math.Abs(estimate.currentLow-2.5) > 1e-9 || math.Abs(estimate.currentHigh-3.75) > 1e-9 {
		t.Fatalf("estimate range = %#v", estimate)
	}
	line := model.quotaAPILine(meter, 100)
	if !strings.Contains(line, "SPEND") || !strings.Contains(line, "100%") || !strings.Contains(line, "N=1") {
		t.Fatalf("estimate line = %q", line)
	}
}

func TestQuotaAPIEstimatorRequiresCleanPricedMovement(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	reset := now.Add(4 * time.Hour).Unix()
	model := New(nil, time.Minute)
	model.snapshot = apiEqSnapshot(20, reset)
	model.observeQuotaAPIEq(apiEqSnapshot(20, reset), codex.LiveUsageSnapshot{}, now)
	model.snapshot = apiEqSnapshot(25, reset)
	model.observeQuotaAPIEq(apiEqSnapshot(25, reset), codex.LiveUsageSnapshot{
		APIEqUSD: 1, APIEqPricedCalls: 1, APIEqUnpricedCalls: 1,
	}, now.Add(time.Minute))
	if len(model.quotaAPIEvidence) != 0 {
		t.Fatalf("unpriced interval produced evidence: %#v", model.quotaAPIEvidence)
	}
	line := model.quotaAPILine(apiEqSnapshot(25, reset).Meters()[0], 80)
	if !strings.Contains(line, "UNPRICED MODEL MIX") {
		t.Fatalf("unpriced learning state = %q", line)
	}

	model.snapshot = apiEqSnapshot(30, reset)
	model.observeQuotaAPIEq(apiEqSnapshot(30, reset), codex.LiveUsageSnapshot{
		APIEqUSD: 1, APIEqPricedCalls: 1, APIEqUnpricedCalls: 1,
	}, now.Add(2*time.Minute))
	if len(model.quotaAPIEvidence) != 0 || !strings.Contains(model.quotaAPILine(apiEqSnapshot(30, reset).Meters()[0], 80), "LOCAL COVERAGE GAP") {
		t.Fatalf("quota-only movement was not rejected: evidence=%#v issues=%#v", model.quotaAPIEvidence, model.quotaAPIIssues)
	}
}

func TestQuotaAPIEstimatorResetsAnchorAndCapsConfidence(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	model := New(nil, time.Minute)
	reset := now.Add(4 * time.Hour).Unix()
	cost := 0.0
	calls := int64(0)
	used := 10
	model.observeQuotaAPIEq(apiEqSnapshot(used, reset), codex.LiveUsageSnapshot{}, now)
	for index := 0; index < 3; index++ {
		used += 5
		cost += 1
		calls++
		model.observeQuotaAPIEq(apiEqSnapshot(used, reset), codex.LiveUsageSnapshot{
			APIEqUSD: cost, APIEqPricedCalls: calls,
		}, now.Add(time.Duration(index+1)*time.Minute))
	}
	estimate, ok := model.quotaAPIEstimate(apiEqSnapshot(used, reset), apiEqSnapshot(used, reset).Meters()[0])
	if !ok || estimate.samples != 3 || estimate.confidence != "MED" {
		t.Fatalf("multi-sample estimate = %#v, %v", estimate, ok)
	}

	newReset := reset + int64((5 * time.Hour).Seconds())
	model.observeQuotaAPIEq(apiEqSnapshot(1, newReset), codex.LiveUsageSnapshot{
		APIEqUSD: cost, APIEqPricedCalls: calls,
	}, now.Add(4*time.Minute))
	key := quotaAPIKey(apiEqSnapshot(1, newReset), apiEqSnapshot(1, newReset).Meters()[0])
	if anchor := model.quotaAPIAnchors[key]; anchor.usedPercent != 1 || anchor.resetAt != newReset {
		t.Fatalf("reset anchor = %#v", anchor)
	}
}

func TestQuotaAPIEstimatorKeepsDivergentSamplesAtLowConfidence(t *testing.T) {
	now := time.Now()
	snapshot := apiEqSnapshot(25, now.Add(4*time.Hour).Unix())
	key := quotaAPIKey(snapshot, snapshot.Meters()[0])
	model := New(nil, time.Minute)
	for index, capacity := range []float64{1, 100, 10_000} {
		model.quotaAPIEvidence = append(model.quotaAPIEvidence, quotaAPISample{
			Key: key, CapacityUSD: capacity, LowUSD: capacity * 5 / 6, HighUSD: capacity * 5 / 4,
			DeltaPercent: 5, ObservedAtUnix: now.Add(time.Duration(index) * time.Minute).Unix(),
			PricingRetrievedOn: codex.BenchmarkPricingRetrievedOn,
		})
	}
	estimate, ok := model.quotaAPIEstimate(snapshot, snapshot.Meters()[0])
	if !ok || estimate.confidence != "LOW" {
		t.Fatalf("divergent estimate = %#v, %v; want LOW", estimate, ok)
	}
}

func TestQuotaAPIEstimatorRestrictsAdditionalLimitsAndIsolatesAccounts(t *testing.T) {
	now := time.Now()
	reset := now.Add(4 * time.Hour).Unix()
	primary := apiEqSnapshot(10, reset)
	additionalID, additionalName, plan, duration := "spark", "GPT Spark", "pro", int64(300)
	additional := codex.RateLimitSnapshot{
		LimitID: &additionalID, LimitName: &additionalName, PlanType: &plan,
		Primary: &codex.Window{UsedPercent: 10, WindowDurationMins: &duration, ResetsAt: &reset},
	}
	primary.RateLimitsByLimitID = map[string]codex.RateLimitSnapshot{
		"codex": primary.RateLimits, "spark": additional,
	}
	model := New(nil, time.Minute)
	model.snapshot = primary
	model.observeQuotaAPIEq(primary, codex.LiveUsageSnapshot{}, now)
	advanced := primary
	advanced.RateLimitsByLimitID = map[string]codex.RateLimitSnapshot{
		"codex": primary.RateLimits, "spark": additional,
	}
	codexLimit := advanced.RateLimitsByLimitID["codex"]
	codexWindow := *codexLimit.Primary
	codexWindow.UsedPercent = 15
	codexLimit.Primary = &codexWindow
	advanced.RateLimitsByLimitID["codex"] = codexLimit
	sparkLimit := advanced.RateLimitsByLimitID["spark"]
	sparkWindow := *sparkLimit.Primary
	sparkWindow.UsedPercent = 15
	sparkLimit.Primary = &sparkWindow
	advanced.RateLimitsByLimitID["spark"] = sparkLimit
	model.snapshot = advanced
	model.observeQuotaAPIEq(advanced, codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1}, now.Add(time.Minute))
	if len(model.quotaAPIEvidence) != 1 || model.quotaAPIEvidence[0].Key != quotaAPIKey(advanced, advanced.Meters()[0]) {
		t.Fatalf("additional limit received evidence: %#v", model.quotaAPIEvidence)
	}
	view := strings.Join([]string{model.quotaAPILine(advanced.Meters()[0], 80), model.quotaMetersWithInsights(100)[1].Details}, "\n")
	if !strings.Contains(view, "100%") || !strings.Contains(view, "LIMIT ATTRIBUTION UNKNOWN") {
		t.Fatalf("limit attribution presentation = %q", view)
	}

	otherAccount := advanced
	otherAccount.AccountFingerprint = "account-b"
	if _, ok := model.quotaAPIEstimate(otherAccount, otherAccount.Meters()[0]); ok {
		t.Fatal("evidence crossed account fingerprints")
	}
	model.observeQuotaAPIEq(otherAccount, codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1}, now.Add(2*time.Minute))
	if len(model.quotaAPIEvidence) != 0 || len(model.quotaAPIAnchors) != 1 || model.quotaAPIAccount != "account-b" {
		t.Fatalf("account switch retained prior observations: account=%q anchors=%#v evidence=%#v", model.quotaAPIAccount, model.quotaAPIAnchors, model.quotaAPIEvidence)
	}

	unknownAccount := apiEqSnapshot(20, reset)
	unknownAccount.AccountFingerprint = ""
	unknownModel := New(nil, time.Minute)
	unknownModel.snapshot = unknownAccount
	unknownModel.observeQuotaAPIEq(unknownAccount, codex.LiveUsageSnapshot{}, now)
	if len(unknownModel.quotaAPIAnchors) != 0 ||
		!strings.Contains(unknownModel.quotaMetersWithInsights(100)[0].Details, "ACCOUNT ATTRIBUTION UNKNOWN") {
		t.Fatalf("unknown account was not fail-closed: anchors=%#v meters=%#v", unknownModel.quotaAPIAnchors, unknownModel.quotaMetersWithInsights(100))
	}
}

func TestQuotaAPIAccountingBracketRequiresStableCounters(t *testing.T) {
	left := codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 2, APIEqUnpricedCalls: 3}
	if !quotaAPIAccountingEqual(left, left) {
		t.Fatal("identical accounting was unstable")
	}
	right := left
	right.APIEqPricedCalls++
	if quotaAPIAccountingEqual(left, right) {
		t.Fatal("changed accounting was accepted")
	}
}

func TestQuotaAPITelemetryIssueTakesPrecedenceOverOldEstimate(t *testing.T) {
	now := time.Now()
	snapshot := apiEqSnapshot(25, now.Add(4*time.Hour).Unix())
	key := quotaAPIKey(snapshot, snapshot.Meters()[0])
	model := New(nil, time.Minute)
	model.snapshot = snapshot
	model.quotaAPIEvidence = []quotaAPISample{{
		Key: key, CapacityUSD: 20, LowUSD: 18, HighUSD: 22, DeltaPercent: 5,
		ObservedAtUnix: now.Unix(), PricingRetrievedOn: codex.BenchmarkPricingRetrievedOn,
	}}
	model.quotaAPITelemetryIssue = "LOCAL TELEMETRY UNAVAILABLE"
	line := model.quotaAPILine(snapshot.Meters()[0], 100)
	if !strings.Contains(line, "LOCAL TELEMETRY UNAVAILABLE") || strings.Contains(line, "SPEND") {
		t.Fatalf("telemetry issue was hidden by old estimate: %q", line)
	}
}

func TestQuotaAPIEvidenceRejectsStaleAndDifferentPricing(t *testing.T) {
	now := time.Now()
	valid := quotaAPISample{Key: "pro|codex|300", CapacityUSD: 10, LowUSD: 9, HighUSD: 11, DeltaPercent: 5, ObservedAtUnix: now.Unix(), PricingRetrievedOn: codex.BenchmarkPricingRetrievedOn}
	stale := valid
	stale.ObservedAtUnix = now.Add(-quotaAPIMaxAge - time.Hour).Unix()
	oldPrice := valid
	oldPrice.PricingRetrievedOn = "2020-01-01"
	if got := validQuotaAPISamples([]quotaAPISample{stale, oldPrice, valid}, now); len(got) != 1 || got[0] != valid {
		t.Fatalf("valid samples = %#v", got)
	}
}

func TestEveryQuotaViewDisplaysResponsiveAPIEqReadout(t *testing.T) {
	now := time.Now()
	snapshot := apiEqSnapshot(25, now.Add(4*time.Hour).Unix())
	key := quotaAPIKey(snapshot, snapshot.Meters()[0])
	model := New(stubFetcher{snapshot: snapshot}, time.Minute)
	model.loading = false
	model.snapshot = snapshot
	model.width, model.height = 120, 44
	model.quotaAPIEvidence = []quotaAPISample{{
		Key: key, CapacityUSD: 20, LowUSD: 18, HighUSD: 22, DeltaPercent: 10,
		ObservedAtUnix: now.Unix(), PricingRetrievedOn: codex.BenchmarkPricingRetrievedOn,
	}}
	for _, style := range quotaStyleOrder {
		model.meterStyle, model.quotaMeterStyle = style, style
		view := ansi.Strip(model.View())
		if !strings.Contains(view, "API-EQ") || !strings.Contains(view, "100%") {
			t.Errorf("%s omitted API-EQ readout:\n%s", style.name(), view)
		}
	}
	model.width = 48
	if view := ansi.Strip(model.View()); !strings.Contains(view, "EQ NOW") || !strings.Contains(view, "FULL") {
		t.Fatalf("narrow API-EQ readout was not responsive:\n%s", view)
	}
}

func apiEqSnapshot(used int, reset int64) codex.Snapshot {
	duration := int64(300)
	plan := "pro"
	limitID := "codex"
	return codex.Snapshot{AccountFingerprint: "account-a", RateLimits: codex.RateLimitSnapshot{
		LimitID: &limitID, PlanType: &plan,
		Primary: &codex.Window{UsedPercent: used, WindowDurationMins: &duration, ResetsAt: &reset},
	}}
}

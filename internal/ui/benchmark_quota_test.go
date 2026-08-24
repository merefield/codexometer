package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestSubscriptionBenchmarkAccountingDefersAndIncludesFinalCost(t *testing.T) {
	now := time.Now()
	reset := now.Add(7 * 24 * time.Hour).Unix()
	model := Model{
		snapshot:        apiEqSnapshot(10, reset),
		quotaAPIAnchors: make(map[string]quotaAPIAnchor),
		quotaAPIIssues:  make(map[string]string),
		quotaAPIAccount: "account-a",
		benchmarkQuotaAccounting: benchmarkQuotaAccounting{
			billingSource: codex.BenchmarkBillingSubscription,
		},
	}
	model.observeQuotaAPIEq(model.snapshot, codex.LiveUsageSnapshot{}, now)
	model.benchmarkQuotaAccounting.start()
	if line := model.quotaAPILine(model.snapshot.Meters()[0], 100); !strings.Contains(line, "SUBSCRIPTION BENCHMARK ACTIVE") {
		t.Fatalf("active benchmark API-EQ line = %q", line)
	}

	result := codex.BenchmarkResult{
		TaskID: codex.BenchmarkDigBench, TaskName: "DIGBENCH P-6",
		Model: "gpt-5.6-sol", Effort: "high", CostKnown: true, CostUSD: 120,
	}
	model.benchmarkQuotaAccounting.observe(result)
	model.benchmarkQuotaAccounting.observe(result)
	model.benchmarkQuotaAccounting.finish()

	accounting := model.benchmarkQuotaAccounting.combine(codex.LiveUsageSnapshot{
		APIEqUSD: 20, APIEqPricedCalls: 1,
	})
	if accounting.APIEqPendingCalls != 0 || accounting.APIEqPricedCalls != 2 || math.Abs(accounting.APIEqUSD-140) > 1e-12 {
		t.Fatalf("combined accounting = %#v", accounting)
	}
	advanced := apiEqSnapshot(15, reset)
	model.snapshot = advanced
	model.observeQuotaAPIEq(advanced, accounting, now.Add(time.Minute))
	estimate, ok := model.quotaAPIEstimate(advanced, advanced.Meters()[0])
	if !ok || math.Abs(estimate.fullLow-140.0*100/6) > 1e-9 || math.Abs(estimate.fullHigh-140.0*100/4) > 1e-9 {
		t.Fatalf("benchmark-inclusive estimate = %#v, %v", estimate, ok)
	}
}

func TestSubscriptionBenchmarkAccountingFailsClosedForUnknownCost(t *testing.T) {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	accounting.start()
	accounting.observe(codex.BenchmarkResult{
		TaskID: codex.BenchmarkDigBench, TaskName: "DIGBENCH P-6", Model: "future-model", Effort: "high",
	})
	accounting.finish()
	combined := accounting.combine(codex.LiveUsageSnapshot{})
	if combined.APIEqUSD != 0 || combined.APIEqPricedCalls != 0 || combined.APIEqUnpricedCalls != 1 || combined.APIEqPendingCalls != 0 {
		t.Fatalf("unknown benchmark accounting = %#v", combined)
	}
}

func TestSubscriptionBenchmarkAccountingRejectsInvalidKnownCost(t *testing.T) {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	accounting.start()
	accounting.observe(codex.BenchmarkResult{
		TaskID: codex.BenchmarkDigBench, TaskName: "DIGBENCH P-6", Model: "gpt-5.6-sol", Effort: "high",
		CostKnown: true, CostUSD: math.Inf(1),
	})
	accounting.finish()
	combined := accounting.combine(codex.LiveUsageSnapshot{})
	if combined.APIEqUSD != 0 || combined.APIEqPricedCalls != 0 || combined.APIEqUnpricedCalls != 1 {
		t.Fatalf("invalid known benchmark cost was accepted: %#v", combined)
	}
}

func TestSubscriptionBenchmarkAccountingRejectsUnvalidatedResponseUsage(t *testing.T) {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	accounting.start()
	accounting.observe(codex.BenchmarkResult{
		TaskID: codex.BenchmarkDigBench, TaskName: "DIGBENCH P-6", Model: "gpt-5.6-sol", Effort: "high",
		ResponseUsage: []codex.BenchmarkResponseUsage{{
			ResponseID: "partial-response",
			Usage:      codex.BenchmarkUsage{TotalTokens: 1200, InputTokens: 1000, CachedInputTokens: 800, OutputTokens: 200},
		}},
	})
	accounting.finish()
	combined := accounting.combine(codex.LiveUsageSnapshot{})
	if combined.APIEqUSD != 0 || combined.APIEqPricedCalls != 0 || combined.APIEqUnpricedCalls != 1 {
		t.Fatalf("unvalidated response usage was accepted: %#v", combined)
	}
}

func TestSubscriptionBenchmarkAccountingRejectsAggregateLongContextUsage(t *testing.T) {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	accounting.start()
	accounting.observe(codex.BenchmarkResult{
		TaskID: codex.BenchmarkDigBench, TaskName: "DIGBENCH P-6", Model: "gpt-5.6-sol", Effort: "high",
		UsageKnown: true, UsageSource: codex.BenchmarkUsageCumulative,
		Usage: codex.BenchmarkUsage{TotalTokens: 300_100, InputTokens: 300_000, OutputTokens: 100},
	})
	accounting.finish()
	combined := accounting.combine(codex.LiveUsageSnapshot{})
	if combined.APIEqUSD != 0 || combined.APIEqPricedCalls != 0 || combined.APIEqUnpricedCalls != 1 {
		t.Fatalf("aggregate long-context usage was accepted: %#v", combined)
	}
}

func TestSubscriptionBenchmarkAccountingFailsClosedForAbandonedActiveResult(t *testing.T) {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	accounting.start()
	accounting.abandonActiveResult()
	accounting.finish()
	combined := accounting.combine(codex.LiveUsageSnapshot{})
	if combined.APIEqUnpricedCalls != 1 || combined.APIEqPricedCalls != 0 || combined.APIEqUSD != 0 {
		t.Fatalf("abandoned benchmark result was not marked unpriced: %#v", combined)
	}
}

func TestSubscriptionBenchmarkAccountingCanPricePolicyInvalidatedResult(t *testing.T) {
	usage := codex.BenchmarkUsage{TotalTokens: 1200, InputTokens: 1000, CachedInputTokens: 800, OutputTokens: 200}
	want, known, _ := codex.EstimateStandardAPIEqCost("gpt-5.6-sol", usage)
	if !known {
		t.Fatal("test usage should be priceable")
	}
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	accounting.start()
	accounting.observe(codex.BenchmarkResult{
		TaskID: codex.BenchmarkMergeRanges, TaskName: "MERGE RANGES", Model: "gpt-5.6-sol", Effort: "high",
		Usage: usage, UsageKnown: true, CostIssue: "tool use prohibited",
	})
	accounting.finish()
	combined := accounting.combine(codex.LiveUsageSnapshot{})
	if combined.APIEqPricedCalls != 1 || combined.APIEqUnpricedCalls != 0 || math.Abs(combined.APIEqUSD-want) > 1e-12 {
		t.Fatalf("policy-invalidated benchmark accounting = %#v, want cost %f", combined, want)
	}
}

func TestAPIKeyBenchmarkDoesNotAffectQuotaAccounting(t *testing.T) {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingAPIKey}
	accounting.start()
	accounting.observe(codex.BenchmarkResult{
		TaskID: codex.BenchmarkDigBench, TaskName: "DIGBENCH P-6",
		Model: "gpt-5.6-sol", Effort: "high", CostKnown: true, CostUSD: 120,
	})
	base := codex.LiveUsageSnapshot{APIEqUSD: 3, APIEqPricedCalls: 2}
	combined := accounting.combine(base)
	if accounting.deferred() || combined.APIEqUSD != base.APIEqUSD ||
		combined.APIEqPricedCalls != base.APIEqPricedCalls || combined.APIEqUnpricedCalls != 0 || combined.APIEqPendingCalls != 0 {
		t.Fatalf("API-key benchmark changed quota accounting: %#v", combined)
	}
}

func TestBenchmarkQuotaAccountingUsesExplicitClientBillingSource(t *testing.T) {
	subscription := benchmarkQuotaAccountingFor(codex.Client{})
	apiKey := benchmarkQuotaAccountingFor(codex.Client{BenchmarkAPIKey: "secret"})
	if subscription.billingSource != codex.BenchmarkBillingSubscription || apiKey.billingSource != codex.BenchmarkBillingAPIKey {
		t.Fatalf("billing sources = %q and %q", subscription.billingSource, apiKey.billingSource)
	}
}

func TestBenchmarkQuotaAccountingSettlesOnlyAfterStableRefresh(t *testing.T) {
	now := time.Now()
	snapshot := apiEqSnapshot(10, now.Add(time.Hour).Unix())
	model := Model{
		usageFetcher:    stubLiveFetcher{},
		snapshot:        snapshot,
		quotaAPIAnchors: make(map[string]quotaAPIAnchor),
		quotaAPIIssues:  make(map[string]string),
		benchmarkQuotaAccounting: benchmarkQuotaAccounting{
			billingSource: codex.BenchmarkBillingSubscription,
			settling:      true,
		},
	}
	if line := model.quotaAPILine(snapshot.Meters()[0], 100); !strings.Contains(line, "ACCOUNTING SETTLING") {
		t.Fatalf("settling API-EQ line = %q", line)
	}
	updated, _ := model.Update(fetchedMsg{
		snapshot: snapshot,
		usage:    codex.LiveUsageSnapshot{},
		at:       now,
	})
	settled := updated.(Model)
	if settled.benchmarkQuotaAccounting.deferred() {
		t.Fatal("stable quota/accounting refresh did not settle benchmark accounting")
	}
}

func TestBenchmarkQuotaDeferredLabelsAreResponsive(t *testing.T) {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription, active: true}
	for _, width := range []int{20, 30, 60} {
		if label := accounting.deferredLabel(width); len(label) > width {
			t.Errorf("active label %q exceeds width %d", label, width)
		}
	}
	accounting.active, accounting.settling = false, true
	for _, width := range []int{20, 35, 60} {
		if label := accounting.deferredLabel(width); len(label) > width {
			t.Errorf("settling label %q exceeds width %d", label, width)
		}
	}
}

func TestStaleQuotaFetchCannotSettleNewBenchmarkAccounting(t *testing.T) {
	now := time.Now()
	snapshot := apiEqSnapshot(15, now.Add(time.Hour).Unix())
	model := Model{
		usageFetcher:    stubLiveFetcher{},
		snapshot:        snapshot,
		quotaAPIAnchors: make(map[string]quotaAPIAnchor),
		quotaAPIIssues:  make(map[string]string),
		benchmarkQuotaAccounting: benchmarkQuotaAccounting{
			billingSource: codex.BenchmarkBillingSubscription,
			revision:      2,
			settling:      true,
			costUSD:       120,
			pricedCalls:   1,
		},
	}
	updated, _ := model.Update(fetchedMsg{
		snapshot:               snapshot,
		usage:                  codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1},
		at:                     now,
		benchmarkQuotaRevision: 1,
	})
	deferred := updated.(Model)
	if !deferred.benchmarkQuotaAccounting.deferred() || deferred.quotaAPITelemetryIssue != "OBSERVATION DEFERRED" || len(deferred.quotaAPIEvidence) != 0 {
		t.Fatalf("stale fetch changed benchmark quota learning: accounting=%#v issue=%q evidence=%#v",
			deferred.benchmarkQuotaAccounting, deferred.quotaAPITelemetryIssue, deferred.quotaAPIEvidence)
	}
}

func TestSubscriptionBenchmarkRerunIsAccountedAgain(t *testing.T) {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	result := codex.BenchmarkResult{
		TaskID: codex.BenchmarkDigBench, TaskName: "DIGBENCH P-6",
		Model: "gpt-5.6-sol", Effort: "high", CostKnown: true, CostUSD: 2,
	}
	for range 2 {
		accounting.start()
		accounting.observe(result)
		accounting.finish()
	}
	combined := accounting.combine(codex.LiveUsageSnapshot{})
	if combined.APIEqUSD != 4 || combined.APIEqPricedCalls != 2 {
		t.Fatalf("rerun accounting = %#v", combined)
	}
}

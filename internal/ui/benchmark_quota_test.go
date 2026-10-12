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
		UsageKnown: true, Usage: codex.BenchmarkUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110},
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
	if combined.APIEqUSD != 0 || combined.APIEqPricedCalls != 0 || combined.APIEqInconsistentUsageCalls != 1 || combined.APIEqUnpricedCalls != 0 {
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
	if combined.APIEqUSD != 0 || combined.APIEqPricedCalls != 0 || combined.APIEqMissingUsageCalls != 1 || combined.APIEqUnpricedCalls != 0 {
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
	if combined.APIEqMissingUsageCalls != 1 || combined.APIEqUnpricedCalls != 0 || combined.APIEqPricedCalls != 0 || combined.APIEqUSD != 0 {
		t.Fatalf("abandoned benchmark result was not marked as missing response usage: %#v", combined)
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

func TestBenchmarkDiagnosticDoesNotMaskNewerSessionFailure(t *testing.T) {
	a := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	a.start()
	a.observe(codex.BenchmarkResult{Model: "unknown-benchmark", UsageKnown: true, Usage: codex.BenchmarkUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}})
	old := a.diagnostics[0].ObservedAt
	current := codex.APIEqDiagnostic{Kind: codex.UsageIssueUnpriced, Counter: 1, Model: "unknown-live", SessionID: "live", Source: "exact response", ObservedAt: old.Add(time.Second)}
	combined := a.combine(codex.LiveUsageSnapshot{APIEqUnpricedCalls: 1, APIEqDiagnostics: []codex.APIEqDiagnostic{current}})
	if len(combined.APIEqDiagnostics) != 1 || combined.APIEqDiagnostics[0].Model != "unknown-live" || combined.APIEqDiagnostics[0].Counter != 2 {
		t.Fatal("stale benchmark masked live failure", combined)
	}
}

func TestBenchmarkFailureIdentifiesEarlierResponseModelAndCancellationTrial(t *testing.T) {
	a := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	a.start()
	usage := codex.BenchmarkUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}
	a.observe(codex.BenchmarkResult{Model: "gpt-6-astra", ActualModel: "gpt-6-astra", Effort: "xhigh", UsageKnown: true, UsageSource: codex.BenchmarkUsageRawResponses, ResponseUsage: []codex.BenchmarkResponseUsage{{ResponseID: "failed", Model: "gpt-reserve", Usage: usage}, {ResponseID: "later", Model: "gpt-6-astra", Usage: usage}}})
	issue := a.diagnostics[len(a.diagnostics)-1]
	if issue.Model != "gpt-reserve" || issue.ResponseID != "failed" || a.unpricedCalls != 1 || len(a.diagnostics) != 1 || a.pricedCalls != 1 {
		t.Fatal("failure attributed to final model rather than offending response", issue)
	}
	a.abandonActiveResult(codex.BenchmarkResult{Model: "gpt-6-astra", Effort: "xhigh"})
	issue = a.diagnostics[len(a.diagnostics)-1]
	if issue.Kind != codex.UsageIssueMissing || issue.Model != "gpt-6-astra" || issue.Effort != "xhigh" {
		t.Fatal("cancellation lost known trial metadata", issue)
	}
}

func TestBenchmarkMissingUsageDiagnosticIdentifiesOriginatingResponse(t *testing.T) {
	a := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	a.start()
	usage := codex.BenchmarkUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}
	result := codex.BenchmarkResult{Model: "gpt-6-astra", ActualModel: "gpt-6-astra", UsageSource: codex.BenchmarkUsageRawResponses, UsageIssue: "raw response omitted usage", ResponseUsage: []codex.BenchmarkResponseUsage{
		{ResponseID: "missing", Model: "gpt-reserve", UsageMissing: true, UsageIssue: "raw response omitted usage"},
		{ResponseID: "later", Model: "gpt-6-astra", Usage: usage},
	}}
	a.observe(result)
	a.observe(result)
	if a.missingCalls != 1 || a.unpricedCalls != 0 || a.pricedCalls != 0 || a.costUSD != 0 || len(a.diagnostics) != 1 {
		t.Fatal("missing response inflated counters or priced incomplete telemetry", a)
	}
	issue := a.diagnostics[0]
	if issue.Kind != codex.UsageIssueMissing || issue.Model != "gpt-reserve" || issue.ResponseID != "missing" {
		t.Fatal("missing benchmark attributed to final response", issue)
	}
}

func TestBenchmarkAccountingReportsOnlyFailingResponses(t *testing.T) {
	a := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
	a.start()
	usage := codex.BenchmarkUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}
	a.observe(codex.BenchmarkResult{Model: "gpt-6-astra", UsageKnown: true, UsageSource: codex.BenchmarkUsageRawResponses, ResponseUsage: []codex.BenchmarkResponseUsage{
		{ResponseID: "unknown", Model: "gpt-reserve", Usage: usage},
		{ResponseID: "known", Model: "gpt-6-astra", Usage: usage},
		{ResponseID: "another-unknown", Model: "unknown-provider", Usage: usage},
	}})
	want, _, _ := codex.EstimateStandardAPIEqCost("gpt-6-astra", usage)
	if a.unpricedCalls != 2 || a.pricedCalls != 1 || math.Abs(a.costUSD-want) > 1e-12 || len(a.diagnostics) != 2 || a.diagnostics[0].ResponseID != "unknown" || a.diagnostics[1].ResponseID != "another-unknown" || a.diagnostics[1].Model != "unknown-provider" {
		t.Fatal("mixed responses duplicated failures or discarded known cost", a)
	}
}

func TestBenchmarkAggregateFailureIsRecordedOnce(t *testing.T) {
	for _, invalidMeasuredCost := range []bool{false, true} {
		a := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingSubscription}
		a.start()
		result := codex.BenchmarkResult{Model: "gpt-6-astra", UsageIssue: "invalid raw response total: totalTokens overflowed", ResponseUsage: []codex.BenchmarkResponseUsage{{ResponseID: "one"}, {ResponseID: "two"}}}
		if invalidMeasuredCost {
			result.CostKnown = true
			result.CostUSD = math.Inf(1)
		}
		a.observe(result)
		if a.inconsistentCalls != 1 || len(a.diagnostics) != 1 || a.diagnostics[0].ResponseID != "" || a.pricedCalls != 0 || a.costUSD != 0 {
			t.Fatal("aggregate failure attributed to each response", a)
		}
	}
}

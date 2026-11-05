package codex

import (
	"math"
	"strings"
	"testing"
)

func TestBenchmarkMeasurementCostsAgreeAcrossRunners(t *testing.T) {
	cached := BenchmarkUsage{InputTokens: 1_000, CachedInputTokens: 400, OutputTokens: 100, TotalTokens: 1_100}
	cumulative := func(usage BenchmarkUsage) func(*benchmarkTelemetry) {
		return func(telemetry *benchmarkTelemetry) { telemetry.recordCumulative(usage) }
	}
	for _, tc := range []struct {
		name, model, issue string
		observe            func(*benchmarkTelemetry)
		source             BenchmarkUsageSource
		usageKnown, priced bool
		total              int64
		cost               float64
	}{
		{name: "missing telemetry", model: "gpt-5.2", source: BenchmarkUsageUnavailable, issue: "matching usage event was not observed"},
		{name: "observed zero", model: "gpt-5.2", observe: cumulative(BenchmarkUsage{}), source: BenchmarkUsageCumulative, usageKnown: true, priced: true},
		{name: "cumulative cached input", model: "gpt-5.2", observe: cumulative(cached), source: BenchmarkUsageCumulative, usageKnown: true, priced: true, total: 1_100, cost: 0.00252},
		{name: "raw model attribution and fallback", model: "gpt-5.2", source: BenchmarkUsageRawResponses, usageKnown: true, priced: true, total: 1_650, cost: 0.00912,
			observe: func(telemetry *benchmarkTelemetry) {
				telemetry.recordRawResponse("fallback", &cached)
				other := BenchmarkUsage{InputTokens: 500, CachedInputTokens: 100, OutputTokens: 50, TotalTokens: 550}
				telemetry.recordRawResponseForModel("rerouted", &other, "gpt-6-astra")
				telemetry.recordCumulative(BenchmarkUsage{TotalTokens: 128_000}) // Synthetic context snapshot must not replace exact usage.
			}},
		{name: "long context priced per response", model: "gpt-5.6-terra", source: BenchmarkUsageRawResponses, usageKnown: true, priced: true, total: 402_000, cost: 0.824,
			observe: func(telemetry *benchmarkTelemetry) {
				response := BenchmarkUsage{InputTokens: 200_000, OutputTokens: 1_000, TotalTokens: 201_000}
				telemetry.recordRawResponse("one", &response)
				telemetry.recordRawResponse("two", &response)
			}},
		{name: "ambiguous cumulative long context", model: "gpt-5.6-terra", source: BenchmarkUsageCumulative, usageKnown: true, total: 402_000, issue: "requires per-response usage",
			observe: cumulative(BenchmarkUsage{InputTokens: 400_000, OutputTokens: 2_000, TotalTokens: 402_000})},
		{name: "unknown response discards partial price", model: "gpt-5.2", source: BenchmarkUsageRawResponses, usageKnown: true, total: 2_200, issue: "no maintained standard API price",
			observe: func(telemetry *benchmarkTelemetry) {
				telemetry.recordRawResponse("priced", &cached)
				telemetry.recordRawResponseForModel("unpriced", &cached, "unpriced-test-model")
			}},
		{name: "unknown cache write price", model: "gpt-5.2", source: BenchmarkUsageCumulative, usageKnown: true, total: 1_100, issue: "no published cache-write price",
			observe: cumulative(BenchmarkUsage{InputTokens: 1_000, CacheWriteInputTokens: 100, OutputTokens: 100, TotalTokens: 1_100})},
		{name: "missing raw usage stays unknown", model: "gpt-5.2", source: BenchmarkUsageRawResponses, issue: "raw response omitted usage",
			observe: func(telemetry *benchmarkTelemetry) {
				telemetry.recordRawResponse("priced", &cached)
				telemetry.recordRawResponse("missing", nil)
				telemetry.recordCumulative(cached)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			telemetry := newBenchmarkTelemetry()
			if tc.observe != nil {
				tc.observe(telemetry)
			}
			builtin := BenchmarkResult{ActualModel: tc.model, UsageKnown: true, UsageIssue: "stale", CostUSD: 99, CostKnown: true, CostIssue: "stale"}
			dig := DigBenchResult{ActualModel: tc.model, UsageKnown: true, UsageIssue: "stale", CostUSD: 99, CostKnown: true, CostIssue: "stale"}
			for range 2 { // Repeated progress snapshots must not accumulate cost or retain stale errors.
				applyBenchmarkMeasurements(&builtin, telemetry)
				applyDigBenchMeasurements(&dig, telemetry)
				for _, got := range []struct {
					name               string
					usageKnown, priced bool
					source             BenchmarkUsageSource
					total              int64
					cost               float64
					issue              string
				}{
					{"built-in", builtin.UsageKnown, builtin.CostKnown, builtin.UsageSource, builtin.Usage.TotalTokens, builtin.CostUSD, builtin.CostIssue},
					{"DigBench", dig.UsageKnown, dig.CostKnown, dig.UsageSource, dig.Usage.TotalTokens, dig.CostUSD, dig.CostIssue},
				} {
					if got.usageKnown != tc.usageKnown || got.priced != tc.priced || got.source != tc.source || got.total != tc.total || math.Abs(got.cost-tc.cost) > 1e-12 {
						t.Fatalf("%s measurements = %+v; want source=%s usage=%v total=%d priced=%v cost=%g", got.name, got, tc.source, tc.usageKnown, tc.total, tc.priced, tc.cost)
					}
					if tc.issue == "" && got.issue != "" || tc.issue != "" && !strings.Contains(got.issue, tc.issue) {
						t.Fatalf("%s issue = %q; want %q", got.name, got.issue, tc.issue)
					}
				}
			}
		})
	}
}

func TestSharedMeasurementPricingKeepsBenchmarkToolPoliciesSeparate(t *testing.T) {
	telemetry := newBenchmarkTelemetry()
	telemetry.recordCumulative(BenchmarkUsage{InputTokens: 1_000, CachedInputTokens: 400, OutputTokens: 100, TotalTokens: 1_100})
	builtin := BenchmarkResult{ActualModel: "gpt-5.2", ToolUsed: true, ToolType: "exec", CostUSD: 99, CostKnown: true}
	dig := DigBenchResult{ActualModel: "gpt-5.2"}
	applyBenchmarkMeasurements(&builtin, telemetry)
	applyDigBenchMeasurements(&dig, telemetry)
	if !builtin.UsageKnown || builtin.CostKnown || builtin.CostUSD != 0 || builtin.CostIssue != "tool use prohibited: exec" {
		t.Fatalf("built-in tool restriction changed: %+v", builtin)
	}
	if !dig.CostKnown || math.Abs(dig.CostUSD-0.00252) > 1e-12 || dig.CostIssue != "" {
		t.Fatalf("DigBench's permitted tool use was incorrectly rejected: %+v", dig)
	}
}

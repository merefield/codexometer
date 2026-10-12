package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type benchmarkBillingProvider interface {
	BenchmarkBillingSource() codex.BenchmarkBillingSource
}

// benchmarkQuotaAccounting retains only content-free, process-local totals for
// ephemeral benchmark calls. These calls never enter the persisted rollout
// reader, but subscription-funded calls do move the account quota windows.
type benchmarkQuotaAccounting struct {
	billingSource     codex.BenchmarkBillingSource
	revision          uint64
	runSequence       uint64
	active            bool
	settling          bool
	costUSD           float64
	pricedCalls       int64
	unpricedCalls     int64
	missingCalls      int64
	inconsistentCalls int64
	diagnostics       []codex.APIEqDiagnostic
	accounted         map[string]struct{}
}

func benchmarkQuotaAccountingFor(runner BenchmarkRunner) benchmarkQuotaAccounting {
	accounting := benchmarkQuotaAccounting{billingSource: codex.BenchmarkBillingUnknown}
	if provider, ok := runner.(benchmarkBillingProvider); ok {
		accounting.billingSource = provider.BenchmarkBillingSource()
	}
	return accounting
}

func (a *benchmarkQuotaAccounting) start() {
	a.runSequence++
	a.active = a.billingSource == codex.BenchmarkBillingSubscription
	a.settling = false
	a.accounted = make(map[string]struct{})
	if a.active {
		a.revision++
	}
}

func (a *benchmarkQuotaAccounting) finish() bool {
	wasActive := a.active
	a.active = false
	if wasActive {
		a.settling = true
		a.revision++
	}
	return a.settling
}

func (a *benchmarkQuotaAccounting) settle() {
	a.settling = false
}

func (a *benchmarkQuotaAccounting) abandonActiveResult(results ...codex.BenchmarkResult) {
	if a.billingSource != codex.BenchmarkBillingSubscription || !a.active {
		return
	}
	result := codex.BenchmarkResult{}
	if len(results) > 0 {
		result = results[0]
	}
	a.recordIssue(result, codex.UsageIssueMissing, "benchmark cancelled before measured usage was available")
	a.revision++
}

func (a *benchmarkQuotaAccounting) observe(result codex.BenchmarkResult) {
	if a.billingSource != codex.BenchmarkBillingSubscription {
		return
	}
	key := fmt.Sprintf("%d\x00%s", a.runSequence, benchmarkRunKey(result))
	if _, exists := a.accounted[key]; exists {
		return
	}
	a.accounted[key] = struct{}{}

	cost, pricedCalls, issues := benchmarkQuotaCostDetail(result)
	a.costUSD += cost
	a.pricedCalls += pricedCalls
	for _, issue := range issues {
		if issue.response != nil {
			a.recordIssue(result, issue.kind, issue.reason, *issue.response)
		} else {
			a.recordIssue(result, issue.kind, issue.reason)
		}
	}
	a.revision++
}

type benchmarkQuotaIssue struct {
	kind     codex.APIEqIssueKind
	reason   string
	response *codex.BenchmarkResponseUsage
}

func benchmarkQuotaCostDetail(result codex.BenchmarkResult) (float64, int64, []benchmarkQuotaIssue) {
	aggregateIssue := func(kind codex.APIEqIssueKind, reason string) (float64, int64, []benchmarkQuotaIssue) {
		return 0, 0, []benchmarkQuotaIssue{{kind: kind, reason: reason}}
	}
	if result.CostKnown {
		if validBenchmarkQuotaCost(result.CostUSD) {
			return result.CostUSD, int64(max(len(result.ResponseUsage), 1)), nil
		}
		return aggregateIssue(codex.UsageIssueInconsistent, "benchmark returned an invalid measured cost")
	}
	model := strings.TrimSpace(result.ActualModel)
	if model == "" {
		model = result.Model
	}
	var issues []benchmarkQuotaIssue
	for i := range result.ResponseUsage {
		response := &result.ResponseUsage[i]
		if response.UsageIssue != "" {
			issues = append(issues, benchmarkQuotaIssue{kind: benchmarkUsageIssueKind(response.UsageIssue), reason: response.UsageIssue, response: response})
		} else if response.UsageMissing {
			issues = append(issues, benchmarkQuotaIssue{kind: codex.UsageIssueMissing, reason: "raw response omitted usage", response: response})
		}
	}
	if len(issues) > 0 {
		return 0, 0, issues
	}
	if !result.UsageKnown {
		if result.UsageIssue != "" {
			return aggregateIssue(benchmarkUsageIssueKind(result.UsageIssue), result.UsageIssue)
		}
		return aggregateIssue(codex.UsageIssueMissing, "benchmark omitted validated response usage")
	}
	if result.UsageSource == codex.BenchmarkUsageRawResponses && len(result.ResponseUsage) > 0 {
		var total float64
		var pricedCalls int64
		for i := range result.ResponseUsage {
			response := &result.ResponseUsage[i]
			responseModel := response.Model
			if responseModel == "" {
				responseModel = model
			}
			cost, known, reason := codex.EstimateStandardAPIEqCost(responseModel, response.Usage)
			if !known {
				issues = append(issues, benchmarkQuotaIssue{kind: benchmarkCostIssueKind(reason), reason: reason, response: response})
				continue
			}
			total += cost
			pricedCalls++
		}
		if !validBenchmarkQuotaCost(total) {
			return aggregateIssue(codex.UsageIssueInconsistent, "benchmark cost total was not finite")
		}
		return total, pricedCalls, issues
	}
	cost, known, reason := codex.EstimateStandardAPIEqAggregateCost(model, result.Usage)
	if !known {
		return aggregateIssue(benchmarkCostIssueKind(reason), reason)
	}
	if !validBenchmarkQuotaCost(cost) {
		return aggregateIssue(codex.UsageIssueInconsistent, "benchmark cost total was not finite")
	}
	return cost, 1, nil
}

func benchmarkUsageIssueKind(reason string) codex.APIEqIssueKind {
	if strings.Contains(reason, "omitted") || strings.Contains(reason, "not observed") {
		return codex.UsageIssueMissing
	}
	return codex.UsageIssueInconsistent
}

func benchmarkCostIssueKind(reason string) codex.APIEqIssueKind {
	if strings.HasPrefix(reason, "invalid usage:") {
		return codex.UsageIssueInconsistent
	}
	return codex.UsageIssueUnpriced
}

func (a *benchmarkQuotaAccounting) recordIssue(result codex.BenchmarkResult, kind codex.APIEqIssueKind, reason string, responses ...codex.BenchmarkResponseUsage) {
	var counter int64
	switch kind {
	case codex.UsageIssueMissing:
		a.missingCalls++
		counter = a.missingCalls
	case codex.UsageIssueInconsistent:
		a.inconsistentCalls++
		counter = a.inconsistentCalls
	default:
		kind = codex.UsageIssueUnpriced
		a.unpricedCalls++
		counter = a.unpricedCalls
	}
	model := result.ActualModel
	if model == "" {
		model = result.Model
	}
	responseID := ""
	if len(responses) > 0 {
		responseID = responses[0].ResponseID
		if responses[0].Model != "" {
			model = responses[0].Model
		}
	}
	a.diagnostics = append(a.diagnostics, codex.APIEqDiagnostic{Kind: kind, Counter: counter, ObservedAt: time.Now(), At: time.Now(), SessionID: "benchmark", Model: model, Effort: result.Effort, Source: "subscription benchmark", ResponseID: responseID, Reason: reason})
	if len(a.diagnostics) > 32 {
		a.diagnostics = a.diagnostics[len(a.diagnostics)-32:]
	}
}

func validBenchmarkQuotaCost(cost float64) bool {
	return cost >= 0 && !math.IsNaN(cost) && !math.IsInf(cost, 0)
}

func (a benchmarkQuotaAccounting) combine(usage codex.LiveUsageSnapshot) codex.LiveUsageSnapshot {
	if a.billingSource != codex.BenchmarkBillingSubscription {
		return usage
	}
	usage.APIEqUSD += a.costUSD
	usage.APIEqPricedCalls += a.pricedCalls
	// Benchmark results remain a standard-price comparison; their service
	// tier is not established by the benchmark result contract.
	usage.APIEqUnknownTierCalls += a.pricedCalls
	all := append(append([]codex.APIEqDiagnostic(nil), usage.APIEqDiagnostics...), a.diagnostics...)
	usage.APIEqMissingUsageCalls += a.missingCalls
	usage.APIEqInconsistentUsageCalls += a.inconsistentCalls
	usage.APIEqUnpricedCalls += a.unpricedCalls
	usage.APIEqDiagnostics = nil
	for _, kind := range []codex.APIEqIssueKind{codex.UsageIssueUnpriced, codex.UsageIssueMissing, codex.UsageIssueInconsistent} {
		var latest codex.APIEqDiagnostic
		for _, diagnostic := range all {
			if diagnostic.Kind == kind && (latest.Kind == "" || diagnostic.ObservedAt.After(latest.ObservedAt)) {
				latest = diagnostic
			}
		}
		if latest.Kind == "" {
			continue
		}
		switch kind {
		case codex.UsageIssueMissing:
			latest.Counter = usage.APIEqMissingUsageCalls
		case codex.UsageIssueInconsistent:
			latest.Counter = usage.APIEqInconsistentUsageCalls
		default:
			latest.Counter = usage.APIEqUnpricedCalls
		}
		usage.APIEqDiagnostics = append(usage.APIEqDiagnostics, latest)
	}
	if a.active {
		usage.APIEqPendingCalls++
	}
	return usage
}

func (a benchmarkQuotaAccounting) deferred() bool {
	return a.billingSource == codex.BenchmarkBillingSubscription && (a.active || a.settling)
}

func (a benchmarkQuotaAccounting) deferredLabel(width int) string {
	if a.active {
		full, compact := i18n.Text("API-EQ DEFERRED // SUBSCRIPTION BENCHMARK ACTIVE"), i18n.Text("API-EQ // BENCHMARK ACTIVE")
		if width >= lipgloss.Width(full) {
			return full
		}
		if width >= lipgloss.Width(compact) {
			return compact
		}
		return i18n.Text("EQ // BENCH ACTIVE")
	}
	full, compact := i18n.Text("API-EQ DEFERRED // BENCHMARK ACCOUNTING SETTLING"), i18n.Text("API-EQ // BENCHMARK SETTLING")
	if width >= lipgloss.Width(full) {
		return full
	}
	if width >= lipgloss.Width(compact) {
		return compact
	}
	return "EQ // BENCH SETTLE"
}

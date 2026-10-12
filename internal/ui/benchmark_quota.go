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

	cost, known, kind, reason := benchmarkQuotaCostDetail(result)
	calls := int64(len(result.ResponseUsage))
	if calls == 0 {
		calls = 1
	}
	if known {
		a.costUSD += cost
		a.pricedCalls += calls
		a.revision++
		return
	}
	for i := int64(0); i < calls; i++ {
		a.recordIssue(result, kind, reason)
	}
	a.revision++
}

func benchmarkQuotaCostDetail(result codex.BenchmarkResult) (float64, bool, codex.APIEqIssueKind, string) {
	if result.CostKnown {
		if validBenchmarkQuotaCost(result.CostUSD) {
			return result.CostUSD, true, "", ""
		}
		return 0, false, codex.UsageIssueInconsistent, "benchmark returned an invalid measured cost"
	}
	model := strings.TrimSpace(result.ActualModel)
	if model == "" {
		model = result.Model
	}
	if !result.UsageKnown {
		if result.UsageIssue != "" {
			kind := codex.UsageIssueInconsistent
			if strings.Contains(result.UsageIssue, "omitted") || strings.Contains(result.UsageIssue, "not observed") {
				kind = codex.UsageIssueMissing
			}
			return 0, false, kind, result.UsageIssue
		}
		return 0, false, codex.UsageIssueMissing, "benchmark omitted validated response usage"
	}
	if result.UsageSource == codex.BenchmarkUsageRawResponses && len(result.ResponseUsage) > 0 {
		var total float64
		for _, response := range result.ResponseUsage {
			responseModel := response.Model
			if responseModel == "" {
				responseModel = model
			}
			cost, known, issue := codex.EstimateStandardAPIEqCost(responseModel, response.Usage)
			if !known {
				return 0, false, benchmarkCostIssueKind(issue), issue
			}
			total += cost
		}
		return total, validBenchmarkQuotaCost(total), codex.UsageIssueInconsistent, "benchmark cost total was not finite"
	}
	cost, known, issue := codex.EstimateStandardAPIEqAggregateCost(model, result.Usage)
	return cost, known && validBenchmarkQuotaCost(cost), benchmarkCostIssueKind(issue), issue
}

func benchmarkCostIssueKind(reason string) codex.APIEqIssueKind {
	if strings.HasPrefix(reason, "invalid usage:") {
		return codex.UsageIssueInconsistent
	}
	return codex.UsageIssueUnpriced
}

func (a *benchmarkQuotaAccounting) recordIssue(result codex.BenchmarkResult, kind codex.APIEqIssueKind, reason string) {
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
	if !result.CostKnown && result.UsageKnown && result.UsageSource == codex.BenchmarkUsageRawResponses {
		for _, response := range result.ResponseUsage {
			responseModel := response.Model
			if responseModel == "" {
				responseModel = model
			}
			if _, known, _ := codex.EstimateStandardAPIEqCost(responseModel, response.Usage); !known {
				model = responseModel
				responseID = response.ResponseID
				break
			}
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

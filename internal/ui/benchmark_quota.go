package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/merefield/codexometer/internal/codex"
)

type benchmarkBillingProvider interface {
	BenchmarkBillingSource() codex.BenchmarkBillingSource
}

// benchmarkQuotaAccounting retains only content-free, process-local totals for
// ephemeral benchmark calls. These calls never enter the persisted rollout
// reader, but subscription-funded calls do move the account quota windows.
type benchmarkQuotaAccounting struct {
	billingSource codex.BenchmarkBillingSource
	revision      uint64
	runSequence   uint64
	active        bool
	settling      bool
	costUSD       float64
	pricedCalls   int64
	unpricedCalls int64
	accounted     map[string]struct{}
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

func (a *benchmarkQuotaAccounting) abandonActiveResult() {
	if a.billingSource != codex.BenchmarkBillingSubscription || !a.active {
		return
	}
	a.unpricedCalls++
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

	cost, known := benchmarkQuotaCost(result)
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
	a.unpricedCalls += calls
	a.revision++
}

func benchmarkQuotaCost(result codex.BenchmarkResult) (float64, bool) {
	if result.CostKnown && validBenchmarkQuotaCost(result.CostUSD) {
		return result.CostUSD, true
	}
	model := strings.TrimSpace(result.ActualModel)
	if model == "" {
		model = result.Model
	}
	if len(result.ResponseUsage) > 0 {
		var total float64
		for _, response := range result.ResponseUsage {
			cost, known, _ := codex.EstimateStandardAPIEqCost(model, response.Usage)
			if !known {
				return 0, false
			}
			total += cost
		}
		return total, validBenchmarkQuotaCost(total)
	}
	if !result.UsageKnown {
		return 0, false
	}
	cost, known, _ := codex.EstimateStandardAPIEqCost(model, result.Usage)
	return cost, known && validBenchmarkQuotaCost(cost)
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
	usage.APIEqUnpricedCalls += a.unpricedCalls
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
		full, compact := "API-EQ DEFERRED // SUBSCRIPTION BENCHMARK ACTIVE", "API-EQ // BENCHMARK ACTIVE"
		if width >= len(full) {
			return full
		}
		if width >= len(compact) {
			return compact
		}
		return "EQ // BENCH ACTIVE"
	}
	full, compact := "API-EQ DEFERRED // BENCHMARK ACCOUNTING SETTLING", "API-EQ // BENCHMARK SETTLING"
	if width >= len(full) {
		return full
	}
	if width >= len(compact) {
		return compact
	}
	return "EQ // BENCH SETTLE"
}

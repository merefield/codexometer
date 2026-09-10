package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestQuotaTierLearningKeepsDenominatorAndStandardComparison(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	snapshot := apiEqSnapshot(10, now.Add(time.Hour).Unix())
	m := New(nil, time.Minute)
	m.observeQuotaAPIEq(snapshot, codex.LiveUsageSnapshot{}, now)
	snapshot = apiEqSnapshot(15, now.Add(time.Hour).Unix())
	m.snapshot = snapshot
	m.observeQuotaAPIEq(snapshot, codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqTierPremiumUSD: 1, APIEqPricedCalls: 1}, now)
	e, ok := m.quotaAPIEstimate(snapshot, snapshot.Meters()[0])
	if !ok || !e.hasPremium || e.unknownTier || e.standardFull != 20 || math.Abs(e.fullLow-200.0/6) > 1e-9 || e.fullHigh != 50 {
		t.Fatalf("estimate %+v", e)
	}
	line := m.quotaAPILine(snapshot.Meters()[0], 180)
	if !strings.Contains(line, "TIER*") || !strings.Contains(line, "STD 100%") {
		t.Fatal(line)
	}
	if strings.Contains(m.quotaAPILine(snapshot.Meters()[0], 55), "STD 100%") {
		t.Fatal("comparison crowded narrow layout")
	}
	// Unknown-tier responses remain explicitly qualified; a premium from an
	// earlier response must not hide missing attribution in a mixed sample.
	snapshot = apiEqSnapshot(20, now.Add(time.Hour).Unix())
	m.snapshot = snapshot
	m.observeQuotaAPIEq(snapshot, codex.LiveUsageSnapshot{APIEqUSD: 2, APIEqTierPremiumUSD: 1, APIEqPricedCalls: 2, APIEqUnknownTierCalls: 1}, now)
	line = m.quotaAPILine(snapshot.Meters()[0], 180)
	if !strings.Contains(line, "TIER*?") {
		t.Fatal(line)
	}
}

func TestQuotaUnknownTierFallbackAndAtomicity(t *testing.T) {
	base := codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1}
	changed := base
	changed.APIEqTierPremiumUSD = 1
	if quotaAPIAccountingEqual(base, changed) {
		t.Fatal("premium change ignored")
	}
	changed = base
	changed.APIEqUnknownTierCalls = 1
	if quotaAPIAccountingEqual(base, changed) {
		t.Fatal("tier coverage ignored")
	}
	now := time.Now().Truncate(time.Second)
	snapshot := apiEqSnapshot(10, now.Add(time.Hour).Unix())
	m := New(nil, time.Minute)
	m.observeQuotaAPIEq(snapshot, codex.LiveUsageSnapshot{}, now)
	snapshot = apiEqSnapshot(15, now.Add(time.Hour).Unix())
	m.snapshot = snapshot
	m.observeQuotaAPIEq(snapshot, changed, now)
	if line := m.quotaAPILine(snapshot.Meters()[0], 100); !strings.Contains(line, "STD?") {
		t.Fatal(line)
	}
}

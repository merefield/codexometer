// Package quotaestimate owns process-local API-equivalent learning, independent of presentation.
package quotaestimate

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

const (
	MinimumDelta = 5
	MaxSamples   = 12
	MaxAge       = 45 * 24 * time.Hour
)

// State is in memory only and must never be serialized to a browser or disk.
type State struct {
	Anchors                map[string]Anchor
	Evidence               []Sample
	Account, CoverageIssue string
}

func AccountingEqual(left, right codex.LiveUsageSnapshot) bool {
	return left.APIEqCoverageIssue == right.APIEqCoverageIssue &&
		left.APIEqUSD == right.APIEqUSD &&
		left.APIEqTierPremiumUSD == right.APIEqTierPremiumUSD &&
		left.APIEqUnknownTierCalls == right.APIEqUnknownTierCalls &&
		left.APIEqPricedCalls == right.APIEqPricedCalls &&
		left.APIEqUnpricedCalls == right.APIEqUnpricedCalls &&
		left.APIEqMissingUsageCalls == right.APIEqMissingUsageCalls &&
		left.APIEqInconsistentUsageCalls == right.APIEqInconsistentUsageCalls &&
		left.APIEqAccountingAdjustments == right.APIEqAccountingAdjustments &&
		left.APIEqPendingCalls == right.APIEqPendingCalls
}

// Sample is process-local and content-free. Its key contains only an
// in-memory account fingerprint plus the quota-window identity; it retains no
// email, session, model-call, or token-event identifier and is never persisted.
type Sample struct {
	Key                string  `json:"key"`
	CapacityUSD        float64 `json:"capacityUsd"`
	LowUSD             float64 `json:"lowUsd"`
	HighUSD            float64 `json:"highUsd"`
	DeltaPercent       int     `json:"deltaPercent"`
	ObservedAtUnix     int64   `json:"observedAt"`
	PricingRetrievedOn string  `json:"pricingRetrievedOn"`
	PremiumCapacityUSD float64 `json:"premiumCapacityUsd"`
	UnknownTier        bool    `json:"unknownTier"`
}

type Anchor struct {
	UsedPercent            int
	ResetAt                int64
	CostUSD                float64
	PremiumUSD             float64
	UnknownTierCalls       int64
	PricedCalls            int64
	UnpricedCalls          int64
	MissingUsageCalls      int64
	InconsistentUsageCalls int64
	AccountingAdjustments  int64
	RestartReason          string
	RestartIssue           codex.APIEqDiagnostic
}

const (
	RestartAccountChanged    = "ACCOUNT CHANGED"
	RestartAccountingRebased = "LOCAL ACCOUNTING REBASED"
	RestartCoverageGap       = "LOCAL COVERAGE GAP"
	RestartUnpricedUsage     = "UNPRICED USAGE"
	RestartMissingUsage      = "MISSING RESPONSE USAGE"
	RestartInconsistentUsage = "INCONSISTENT RESPONSE USAGE"
	RestartWindowChanged     = "WINDOW DEFINITION CHANGED"
	RestartWindowReset       = "WINDOW RESET"
)

type Estimate struct {
	CurrentLow   float64
	CurrentHigh  float64
	FullLow      float64
	FullHigh     float64
	Samples      int
	Confidence   string
	StandardFull float64
	HasPremium   bool
	UnknownTier  bool
}

// A known coverage gap invalidates evidence even when another error or an
// active benchmark prevents using the associated quota observation.
func (m *State) ObserveCoverage(issue string) {
	previous := m.CoverageIssue
	m.CoverageIssue = issue
	if previous != "" || issue != "" {
		m.Anchors = make(map[string]Anchor)
		m.Evidence = nil
	}
}

func (m *State) Observe(snapshot codex.Snapshot, usage codex.LiveUsageSnapshot, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	account := strings.TrimSpace(snapshot.AccountFingerprint)
	if account == "" {
		return
	}
	accountChanged := m.Account != "" && m.Account != account
	if accountChanged {
		m.Anchors = make(map[string]Anchor)
		m.Evidence = nil
	}
	m.Account = account
	m.ObserveCoverage(usage.APIEqCoverageIssue)
	if usage.APIEqCoverageIssue != "" {
		return
	}

	if m.Anchors == nil {
		m.Anchors = make(map[string]Anchor)
	}
	priorAnchorKeys := make(map[string]struct{}, len(m.Anchors))
	for key := range m.Anchors {
		priorAnchorKeys[key] = struct{}{}
	}
	m.Evidence = ValidSamples(m.Evidence, at)
	for _, meter := range snapshot.Meters() {
		if !Eligible(snapshot, meter) {
			continue
		}
		key := Key(snapshot, meter)
		current := Anchor{
			UsedPercent: min(max(meter.Window.UsedPercent, 0), 100),
			ResetAt:     optionalUnix(meter.Window.ResetsAt), CostUSD: usage.APIEqUSD + usage.APIEqTierPremiumUSD,
			PremiumUSD: usage.APIEqTierPremiumUSD, UnknownTierCalls: usage.APIEqUnknownTierCalls,
			PricedCalls: usage.APIEqPricedCalls, UnpricedCalls: usage.APIEqUnpricedCalls,
			MissingUsageCalls:      usage.APIEqMissingUsageCalls,
			InconsistentUsageCalls: usage.APIEqInconsistentUsageCalls,
			AccountingAdjustments:  usage.APIEqAccountingAdjustments,
		}
		anchor, exists := m.Anchors[key]
		if !exists {
			switch {
			case accountChanged:
				current.RestartReason = RestartAccountChanged
			case hasRelatedAnchor(priorAnchorKeys, snapshot, meter):
				current.RestartReason = RestartWindowChanged
			}
			m.Anchors[key] = current
			continue
		}
		if windowReset(anchor, current, at) {
			current.RestartReason = RestartWindowReset
			m.Anchors[key] = current
			continue
		}
		if current.CostUSD < anchor.CostUSD || current.PricedCalls < anchor.PricedCalls ||
			current.UnpricedCalls < anchor.UnpricedCalls ||
			current.UnknownTierCalls < anchor.UnknownTierCalls ||
			current.MissingUsageCalls < anchor.MissingUsageCalls ||
			current.InconsistentUsageCalls < anchor.InconsistentUsageCalls ||
			current.AccountingAdjustments < anchor.AccountingAdjustments {
			current.RestartReason = RestartAccountingRebased
			m.Anchors[key] = current
			continue
		}
		// The backend may revise an upcoming rolling-window reset timestamp
		// without starting a new quota window. Preserve the earliest known
		// boundary so a later observation can still detect that it passed,
		// while retaining the clean movement and accounting baselines now.
		if current.ResetAt > 0 && (anchor.ResetAt == 0 || current.ResetAt < anchor.ResetAt) {
			anchor.ResetAt = current.ResetAt
			m.Anchors[key] = anchor
		}
		if current.UnpricedCalls > anchor.UnpricedCalls {
			current.RestartReason = RestartUnpricedUsage
			current.RestartIssue = latestIntervalDiagnostic(usage, codex.UsageIssueUnpriced, anchor.UnpricedCalls)
			m.Anchors[key] = current
			continue
		}
		switch {
		case current.MissingUsageCalls > anchor.MissingUsageCalls:
			current.RestartReason = RestartMissingUsage
			current.RestartIssue = latestIntervalDiagnostic(usage, codex.UsageIssueMissing, anchor.MissingUsageCalls)
		case current.InconsistentUsageCalls > anchor.InconsistentUsageCalls:
			current.RestartReason = RestartInconsistentUsage
			current.RestartIssue = latestIntervalDiagnostic(usage, codex.UsageIssueInconsistent, anchor.InconsistentUsageCalls)
		case current.AccountingAdjustments > anchor.AccountingAdjustments:
			current.RestartReason = RestartAccountingRebased
		}
		if current.RestartReason != "" {
			m.Anchors[key] = current
			continue
		}
		deltaPercent := current.UsedPercent - anchor.UsedPercent
		if deltaPercent < MinimumDelta {
			continue
		}
		deltaCost := current.CostUSD - anchor.CostUSD
		if deltaCost <= 0 || current.PricedCalls == anchor.PricedCalls {
			current.RestartReason = RestartCoverageGap
			m.Anchors[key] = current
			continue
		}
		capacity := deltaCost * 100 / float64(deltaPercent)
		low := deltaCost * 100 / float64(deltaPercent+1)
		high := deltaCost * 100 / float64(deltaPercent-1)
		m.Evidence = append(m.Evidence, Sample{
			Key: key, CapacityUSD: capacity, LowUSD: low, HighUSD: high,
			DeltaPercent: deltaPercent, ObservedAtUnix: at.Unix(),
			PricingRetrievedOn: codex.StandardAPIPricingRetrievedOn,
			PremiumCapacityUSD: (current.PremiumUSD - anchor.PremiumUSD) * 100 / float64(deltaPercent),
			UnknownTier:        current.UnknownTierCalls > anchor.UnknownTierCalls,
		})
		m.Evidence = trimSamples(m.Evidence, key)
		m.Anchors[key] = current
	}
}

func hasRelatedAnchor(priorKeys map[string]struct{}, snapshot codex.Snapshot, meter codex.Meter) bool {
	wantedAccount := strings.TrimSpace(snapshot.AccountFingerprint)
	wantedDefinition := meter.Key().String() + "/"
	for key := range priorKeys {
		parts := strings.SplitN(key, "|", 3)
		if len(parts) == 3 && parts[0] == wantedAccount && strings.HasPrefix(parts[2], wantedDefinition) {
			return true
		}
	}
	return false
}

func windowReset(anchor, current Anchor, observedAt time.Time) bool {
	if current.UsedPercent < anchor.UsedPercent {
		return true
	}
	if anchor.ResetAt <= 0 || current.ResetAt <= anchor.ResetAt {
		return false
	}
	return !observedAt.Before(time.Unix(anchor.ResetAt, 0))
}

func Eligible(snapshot codex.Snapshot, meter codex.Meter) bool {
	return meter.Kind == codex.MeterQuotaWindow && snapshot.AccountFingerprint != "" &&
		strings.EqualFold(strings.TrimSpace(meter.LimitID), "codex")
}

func optionalUnix(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func Key(snapshot codex.Snapshot, meter codex.Meter) string {
	account := strings.TrimSpace(snapshot.AccountFingerprint)
	plan := "unknown"
	if len(snapshot.RateLimitsByLimitID) > 0 {
		if limit, ok := snapshot.RateLimitsByLimitID[meter.LimitID]; ok && limit.PlanType != nil {
			plan = strings.ToLower(strings.TrimSpace(*limit.PlanType))
		}
	} else if snapshot.RateLimits.PlanType != nil {
		plan = strings.ToLower(strings.TrimSpace(*snapshot.RateLimits.PlanType))
	}
	return fmt.Sprintf("%s|%s|%s", account, plan, meter.DefinitionKey())
}

func ValidSamples(samples []Sample, now time.Time) []Sample {
	valid := make([]Sample, 0, len(samples))
	cutoff := now.Add(-MaxAge).Unix()
	for _, sample := range samples {
		if sample.Key == "" || sample.PricingRetrievedOn != codex.StandardAPIPricingRetrievedOn ||
			sample.ObservedAtUnix < cutoff || sample.ObservedAtUnix > now.Add(time.Hour).Unix() ||
			sample.DeltaPercent < MinimumDelta || !positiveFinite(sample.CapacityUSD) ||
			!positiveFinite(sample.LowUSD) || !positiveFinite(sample.HighUSD) ||
			math.IsNaN(sample.PremiumCapacityUSD) || math.IsInf(sample.PremiumCapacityUSD, 0) ||
			sample.PremiumCapacityUSD >= sample.CapacityUSD ||
			sample.LowUSD > sample.CapacityUSD || sample.CapacityUSD > sample.HighUSD {
			continue
		}
		valid = append(valid, sample)
	}
	return valid
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func trimSamples(samples []Sample, key string) []Sample {
	matching := 0
	for _, sample := range samples {
		if sample.Key == key {
			matching++
		}
	}
	remove := matching - MaxSamples
	if remove <= 0 {
		return samples
	}
	trimmed := make([]Sample, 0, len(samples)-remove)
	for _, sample := range samples {
		if sample.Key == key && remove > 0 {
			remove--
			continue
		}
		trimmed = append(trimmed, sample)
	}
	return trimmed
}

func (m State) Estimate(snapshot codex.Snapshot, meter codex.Meter, now time.Time) (Estimate, bool) {
	if m.CoverageIssue != "" {
		return Estimate{}, false
	}
	key := Key(snapshot, meter)
	matching := make([]Sample, 0, MaxSamples)
	for _, sample := range ValidSamples(m.Evidence, now) {
		if sample.Key == key {
			matching = append(matching, sample)
		}
	}
	if len(matching) == 0 {
		return Estimate{}, false
	}
	lows := make([]float64, 0, len(matching))
	highs := make([]float64, 0, len(matching))
	centers := make([]float64, 0, len(matching))
	standardCenters := make([]float64, 0, len(matching))
	hasPremium, unknownTier := false, false
	totalDelta := 0
	for _, sample := range matching {
		lows = append(lows, sample.LowUSD)
		highs = append(highs, sample.HighUSD)
		centers = append(centers, sample.CapacityUSD)
		standardCenters = append(standardCenters, sample.CapacityUSD-sample.PremiumCapacityUSD)
		hasPremium = hasPremium || sample.PremiumCapacityUSD != 0
		unknownTier = unknownTier || sample.UnknownTier
		totalDelta += sample.DeltaPercent
	}
	fullLow, fullHigh := medianFloat(lows), medianFloat(highs)
	center := medianFloat(centers)
	minCenter, maxCenter := slicesMinMax(centers)
	// Keep this identifier canonical: compact presentations use L/M codes.
	confidence := "LOW"
	if !unknownTier && len(matching) >= 3 && totalDelta >= 15 && center > 0 &&
		(fullHigh-fullLow)/center <= 0.45 && (maxCenter-minCenter)/center <= 0.50 {
		// Local rollout coverage cannot prove that another machine or client did
		// not consume quota, so the estimator intentionally caps at MEDIUM.
		confidence = "MED"
	}
	used := float64(min(max(meter.Window.UsedPercent, 0), 100)) / 100
	return Estimate{
		CurrentLow: fullLow * used, CurrentHigh: fullHigh * used,
		FullLow: fullLow, FullHigh: fullHigh, Samples: len(matching), Confidence: confidence,
		StandardFull: medianFloat(standardCenters), HasPremium: hasPremium, UnknownTier: unknownTier,
	}, true
}

func slicesMinMax(values []float64) (float64, float64) {
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		minimum = min(minimum, value)
		maximum = max(maximum, value)
	}
	return minimum, maximum
}

func medianFloat(values []float64) float64 {
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func latestIntervalDiagnostic(usage codex.LiveUsageSnapshot, kind codex.APIEqIssueKind, after int64) codex.APIEqDiagnostic {
	for i := len(usage.APIEqDiagnostics) - 1; i >= 0; i-- {
		issue := usage.APIEqDiagnostics[i]
		if issue.Kind == kind && issue.Counter > after {
			return issue
		}
	}
	return codex.APIEqDiagnostic{}
}

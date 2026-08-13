package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

const (
	quotaAPIMinimumDelta = 5
	quotaAPIMaxSamples   = 12
	quotaAPIMaxAge       = 45 * 24 * time.Hour
)

var errQuotaObservationChanged = fmt.Errorf("local accounting changed during quota read")

func quotaAPIAccountingEqual(left, right codex.LiveUsageSnapshot) bool {
	return left.APIEqUSD == right.APIEqUSD &&
		left.APIEqPricedCalls == right.APIEqPricedCalls &&
		left.APIEqUnpricedCalls == right.APIEqUnpricedCalls
}

// quotaAPISample is process-local and content-free. Its key contains only an
// in-memory account fingerprint plus the quota-window identity; it retains no
// email, session, model-call, or token-event identifier and is never persisted.
type quotaAPISample struct {
	Key                string  `json:"key"`
	CapacityUSD        float64 `json:"capacityUsd"`
	LowUSD             float64 `json:"lowUsd"`
	HighUSD            float64 `json:"highUsd"`
	DeltaPercent       int     `json:"deltaPercent"`
	ObservedAtUnix     int64   `json:"observedAt"`
	PricingRetrievedOn string  `json:"pricingRetrievedOn"`
}

type quotaAPIAnchor struct {
	usedPercent   int
	resetAt       int64
	costUSD       float64
	pricedCalls   int64
	unpricedCalls int64
}

type quotaAPIEstimate struct {
	currentLow  float64
	currentHigh float64
	fullLow     float64
	fullHigh    float64
	samples     int
	confidence  string
}

func (m *Model) observeQuotaAPIEq(snapshot codex.Snapshot, usage codex.LiveUsageSnapshot, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	account := strings.TrimSpace(snapshot.AccountFingerprint)
	if account == "" {
		return
	}
	if m.quotaAPIAccount != "" && m.quotaAPIAccount != account {
		m.quotaAPIAnchors = make(map[string]quotaAPIAnchor)
		m.quotaAPIEvidence = nil
		m.quotaAPIIssues = make(map[string]string)
	}
	m.quotaAPIAccount = account
	if m.quotaAPIAnchors == nil {
		m.quotaAPIAnchors = make(map[string]quotaAPIAnchor)
	}
	if m.quotaAPIIssues == nil {
		m.quotaAPIIssues = make(map[string]string)
	}
	m.quotaAPIEvidence = validQuotaAPISamples(m.quotaAPIEvidence, at)
	for _, meter := range snapshot.Meters() {
		if !quotaAPIMeterEligible(snapshot, meter) {
			continue
		}
		key := quotaAPIKey(snapshot, meter)
		current := quotaAPIAnchor{
			usedPercent: min(max(meter.Window.UsedPercent, 0), 100),
			resetAt:     optionalUnix(meter.Window.ResetsAt), costUSD: usage.APIEqUSD,
			pricedCalls: usage.APIEqPricedCalls, unpricedCalls: usage.APIEqUnpricedCalls,
		}
		anchor, exists := m.quotaAPIAnchors[key]
		if !exists || anchor.resetAt != current.resetAt || current.usedPercent < anchor.usedPercent ||
			current.costUSD < anchor.costUSD || current.pricedCalls < anchor.pricedCalls ||
			current.unpricedCalls < anchor.unpricedCalls {
			m.quotaAPIAnchors[key] = current
			m.quotaAPIIssues[key] = ""
			continue
		}
		if current.unpricedCalls > anchor.unpricedCalls {
			m.quotaAPIAnchors[key] = current
			m.quotaAPIIssues[key] = "UNPRICED MODEL MIX"
			continue
		}
		deltaPercent := current.usedPercent - anchor.usedPercent
		if deltaPercent < quotaAPIMinimumDelta {
			continue
		}
		deltaCost := current.costUSD - anchor.costUSD
		if deltaCost <= 0 || current.pricedCalls == anchor.pricedCalls {
			m.quotaAPIAnchors[key] = current
			m.quotaAPIIssues[key] = "LOCAL COVERAGE GAP"
			continue
		}
		capacity := deltaCost * 100 / float64(deltaPercent)
		low := deltaCost * 100 / float64(deltaPercent+1)
		high := deltaCost * 100 / float64(deltaPercent-1)
		m.quotaAPIEvidence = append(m.quotaAPIEvidence, quotaAPISample{
			Key: key, CapacityUSD: capacity, LowUSD: low, HighUSD: high,
			DeltaPercent: deltaPercent, ObservedAtUnix: at.Unix(),
			PricingRetrievedOn: codex.StandardAPIPricingRetrievedOn,
		})
		m.quotaAPIEvidence = trimQuotaAPISamples(m.quotaAPIEvidence, key)
		m.quotaAPIAnchors[key] = current
		m.quotaAPIIssues[key] = ""
	}
}

func quotaAPIMeterEligible(snapshot codex.Snapshot, meter codex.Meter) bool {
	return meter.Kind == codex.MeterQuotaWindow && snapshot.AccountFingerprint != "" &&
		strings.EqualFold(strings.TrimSpace(meter.LimitID), "codex")
}

func optionalUnix(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func quotaAPIKey(snapshot codex.Snapshot, meter codex.Meter) string {
	account := strings.TrimSpace(snapshot.AccountFingerprint)
	plan := "unknown"
	if len(snapshot.RateLimitsByLimitID) > 0 {
		if limit, ok := snapshot.RateLimitsByLimitID[meter.LimitID]; ok && limit.PlanType != nil {
			plan = strings.ToLower(strings.TrimSpace(*limit.PlanType))
		}
	} else if snapshot.RateLimits.PlanType != nil {
		plan = strings.ToLower(strings.TrimSpace(*snapshot.RateLimits.PlanType))
	}
	duration := int64(0)
	if meter.Window.WindowDurationMins != nil {
		duration = *meter.Window.WindowDurationMins
	}
	limitID := strings.ToLower(strings.TrimSpace(meter.LimitID))
	if limitID == "" {
		limitID = strings.ToLower(strings.TrimSpace(meter.Bucket))
	}
	return fmt.Sprintf("%s|%s|%s|%d", account, plan, limitID, duration)
}

func validQuotaAPISamples(samples []quotaAPISample, now time.Time) []quotaAPISample {
	valid := make([]quotaAPISample, 0, len(samples))
	cutoff := now.Add(-quotaAPIMaxAge).Unix()
	for _, sample := range samples {
		if sample.Key == "" || sample.PricingRetrievedOn != codex.StandardAPIPricingRetrievedOn ||
			sample.ObservedAtUnix < cutoff || sample.ObservedAtUnix > now.Add(time.Hour).Unix() ||
			sample.DeltaPercent < quotaAPIMinimumDelta || !positiveFinite(sample.CapacityUSD) ||
			!positiveFinite(sample.LowUSD) || !positiveFinite(sample.HighUSD) ||
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

func trimQuotaAPISamples(samples []quotaAPISample, key string) []quotaAPISample {
	matching := 0
	for _, sample := range samples {
		if sample.Key == key {
			matching++
		}
	}
	remove := matching - quotaAPIMaxSamples
	if remove <= 0 {
		return samples
	}
	trimmed := make([]quotaAPISample, 0, len(samples)-remove)
	for _, sample := range samples {
		if sample.Key == key && remove > 0 {
			remove--
			continue
		}
		trimmed = append(trimmed, sample)
	}
	return trimmed
}

func (m Model) quotaAPIEstimate(snapshot codex.Snapshot, meter codex.Meter) (quotaAPIEstimate, bool) {
	key := quotaAPIKey(snapshot, meter)
	matching := make([]quotaAPISample, 0, quotaAPIMaxSamples)
	for _, sample := range validQuotaAPISamples(m.quotaAPIEvidence, time.Now()) {
		if sample.Key == key {
			matching = append(matching, sample)
		}
	}
	if len(matching) == 0 {
		return quotaAPIEstimate{}, false
	}
	lows := make([]float64, 0, len(matching))
	highs := make([]float64, 0, len(matching))
	centers := make([]float64, 0, len(matching))
	totalDelta := 0
	for _, sample := range matching {
		lows = append(lows, sample.LowUSD)
		highs = append(highs, sample.HighUSD)
		centers = append(centers, sample.CapacityUSD)
		totalDelta += sample.DeltaPercent
	}
	fullLow, fullHigh := medianFloat(lows), medianFloat(highs)
	center := medianFloat(centers)
	minCenter, maxCenter := slicesMinMax(centers)
	confidence := "LOW"
	if len(matching) >= 3 && totalDelta >= 15 && center > 0 &&
		(fullHigh-fullLow)/center <= 0.45 && (maxCenter-minCenter)/center <= 0.50 {
		// Local rollout coverage cannot prove that another machine or client did
		// not consume quota, so the estimator intentionally caps at MEDIUM.
		confidence = "MED"
	}
	used := float64(min(max(meter.Window.UsedPercent, 0), 100)) / 100
	return quotaAPIEstimate{
		currentLow: fullLow * used, currentHigh: fullHigh * used,
		fullLow: fullLow, fullHigh: fullHigh, samples: len(matching), confidence: confidence,
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

func (m Model) quotaMetersWithInsights(width int) []codex.Meter {
	meters := m.snapshot.Meters()
	lineWidth := max(width-4, 1)
	if len(meters) > 0 {
		columns := meterGridColumns(width, m.dashboardLayout().meterHeight, len(meters), m.meterView)
		lineWidth = max((width-(columns-1))/columns-4, 1)
	}
	for index := range meters {
		if meters[index].Kind != codex.MeterQuotaWindow {
			continue
		}
		line := "API-EQ N/A // LIMIT ATTRIBUTION UNKNOWN"
		if m.snapshot.AccountFingerprint == "" {
			line = "API-EQ N/A // ACCOUNT ATTRIBUTION UNKNOWN"
		} else if quotaAPIMeterEligible(m.snapshot, meters[index]) {
			line = m.quotaAPILine(meters[index], lineWidth)
		}
		if meters[index].Details == "" {
			meters[index].Details = line
		} else {
			meters[index].Details += "\n" + line
		}
	}
	return meters
}

func (m Model) quotaAPILine(meter codex.Meter, width int) string {
	if m.quotaAPITelemetryIssue != "" {
		if width < 44 {
			return "API-EQ // " + m.quotaAPITelemetryIssue
		}
		return "API-EQ LEARNING // " + m.quotaAPITelemetryIssue
	}
	if estimate, ok := m.quotaAPIEstimate(m.snapshot, meter); ok {
		current := formatAPIRange(estimate.currentLow, estimate.currentHigh)
		full := formatAPIRange(estimate.fullLow, estimate.fullHigh)
		switch {
		case width >= 72:
			return fmt.Sprintf("OBSERVED API-EQ // SPEND ~%s // 100%% ~%s // %s · N=%d", current, full, estimate.confidence, estimate.samples)
		case width >= 52:
			return fmt.Sprintf("API-EQ NOW ~%s // 100%% ~%s · %s%d", current, full, estimate.confidence[:1], estimate.samples)
		default:
			currentMid := formatUSD((estimate.currentLow + estimate.currentHigh) / 2)
			fullMid := formatUSD((estimate.fullLow + estimate.fullHigh) / 2)
			return fmt.Sprintf("EQ NOW ~%s // FULL ~%s %s%d", currentMid, fullMid, estimate.confidence[:1], estimate.samples)
		}
	}
	key := quotaAPIKey(m.snapshot, meter)
	if issue := m.quotaAPIIssues[key]; issue != "" {
		return "API-EQ LEARNING // " + issue
	}
	progress := 0
	if anchor, ok := m.quotaAPIAnchors[key]; ok {
		progress = min(max(meter.Window.UsedPercent-anchor.usedPercent, 0), quotaAPIMinimumDelta)
	}
	if width < 44 {
		return fmt.Sprintf("API-EQ LEARNING %d/%dPP", progress, quotaAPIMinimumDelta)
	}
	return fmt.Sprintf("API-EQ LEARNING // %d/%dPP CLEAN MOVEMENT", progress, quotaAPIMinimumDelta)
}

func formatAPIRange(low, high float64) string {
	left, right := formatUSD(low), formatUSD(high)
	if left == right {
		return left
	}
	return left + "–" + right
}

func formatUSD(value float64) string {
	switch {
	case value < 0.01:
		return fmt.Sprintf("$%.4f", value)
	case value < 1:
		return fmt.Sprintf("$%.3f", value)
	case value < 10:
		return fmt.Sprintf("$%.2f", value)
	case value < 100:
		return fmt.Sprintf("$%.1f", value)
	default:
		return fmt.Sprintf("$%.0f", value)
	}
}

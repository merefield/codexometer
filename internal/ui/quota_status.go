package ui

import (
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/merefield/codexometer/internal/codex"
)

type quotaHealth int

const (
	quotaHealthUnknown quotaHealth = iota
	quotaHealthClear
	quotaHealthWatch
	quotaHealthNear
	quotaHealthExhausted
)

func (h quotaHealth) label() string {
	return [...]string{
		"QUOTA UNKNOWN",
		"QUOTA CLEAR",
		"QUOTA WATCH",
		"LIMIT NEAR",
		"QUOTA EXHAUSTED",
	}[h]
}

func (h quotaHealth) compactLabel() string {
	return [...]string{
		"UNKNOWN",
		"CLEAR",
		"WATCH",
		"NEAR",
		"EXHAUSTED",
	}[h]
}

func (h quotaHealth) color(colors palette) lipgloss.Color {
	switch h {
	case quotaHealthClear:
		return lipgloss.Color("#57FF8A")
	case quotaHealthWatch:
		return lipgloss.Color("#67B7FF")
	case quotaHealthNear:
		return lipgloss.Color("#FFB454")
	case quotaHealthExhausted:
		return lipgloss.Color("#FF5F6D")
	default:
		return colors.dim
	}
}

func snapshotQuotaHealth(snapshot codex.Snapshot, now time.Time) quotaHealth {
	if snapshotReportsLimitReached(snapshot) {
		return quotaHealthExhausted
	}
	meters := snapshot.Meters()
	if len(meters) == 0 {
		return quotaHealthUnknown
	}
	health := quotaHealthClear
	for _, meter := range meters {
		health = max(health, windowQuotaHealth(meter.Window, now))
	}
	return health
}

func windowQuotaHealth(window codex.Window, now time.Time) quotaHealth {
	used := min(max(window.UsedPercent, 0), 100)
	if used >= 100 {
		return quotaHealthExhausted
	}
	if window.WindowDurationMins == nil || window.ResetsAt == nil || *window.WindowDurationMins <= 0 {
		return quotaHealthWithoutPace(used)
	}

	duration := time.Duration(*window.WindowDurationMins) * time.Minute
	resetAt := time.Unix(*window.ResetsAt, 0)
	elapsedDuration := now.Sub(resetAt.Add(-duration))
	if elapsedDuration <= 0 || elapsedDuration >= duration || used == 0 {
		return quotaHealthWithoutPace(used)
	}

	elapsed := float64(elapsedDuration) / float64(duration)
	usedFraction := float64(used) / 100
	if usedFraction <= elapsed {
		return quotaHealthClear
	}

	remainingQuota := 1 - usedFraction
	remainingTime := 1 - elapsed
	timeToExhaustion := elapsed * remainingQuota / usedFraction
	if remainingQuota <= 0.05 || timeToExhaustion <= remainingTime*0.25 {
		return quotaHealthNear
	}
	return quotaHealthWatch
}

func quotaHealthWithoutPace(used int) quotaHealth {
	switch {
	case used >= 95:
		return quotaHealthNear
	case used >= 80:
		return quotaHealthWatch
	default:
		return quotaHealthClear
	}
}

func snapshotReportsLimitReached(snapshot codex.Snapshot) bool {
	reached := func(limit codex.RateLimitSnapshot) bool {
		return limit.RateLimitReachedType != nil && *limit.RateLimitReachedType != ""
	}
	if reached(snapshot.RateLimits) {
		return true
	}
	for _, limit := range snapshot.RateLimitsByLimitID {
		if reached(limit) {
			return true
		}
	}
	return false
}

package ui

import (
	imagecolor "image/color"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/merefield/codexometer/internal/codex"
)

type quotaHealth int

const (
	quotaHealthUnknown quotaHealth = iota
	quotaHealthFresh
	quotaHealthClear
	quotaHealthWatch
	quotaHealthNear
	quotaHealthExhausted
)

func (h quotaHealth) label() string {
	return [...]string{
		"QUOTA UNKNOWN",
		"RESET FRESH // GO!",
		"QUOTA CLEAR",
		"QUOTA WATCH",
		"LIMIT NEAR",
		"QUOTA EXHAUSTED",
	}[h]
}

func (h quotaHealth) compactLabel() string {
	return [...]string{
		"UNKNOWN",
		"FRESH // GO!",
		"CLEAR",
		"WATCH",
		"NEAR",
		"EXHAUSTED",
	}[h]
}

func (h quotaHealth) color(colors palette) imagecolor.Color {
	switch h {
	case quotaHealthFresh, quotaHealthClear:
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

type quotaSignal struct {
	health quotaHealth
	cause  string
}

func (s quotaSignal) label() string {
	if s.health == quotaHealthFresh || s.health == quotaHealthUnknown || s.cause == "" {
		return s.health.label()
	}
	return s.cause + " // " + s.health.compactLabel()
}

func (s quotaSignal) compactLabel() string {
	if s.health == quotaHealthFresh || s.health == quotaHealthUnknown || s.cause == "" {
		return s.health.compactLabel()
	}
	return compactQuotaCause(s.cause) + " " + s.health.compactLabel()
}

func snapshotQuotaSignal(snapshot codex.Snapshot, now time.Time) quotaSignal {
	if cause, reached := snapshotLimitReachedCause(snapshot); reached {
		return quotaSignal{health: quotaHealthExhausted, cause: cause}
	}
	meters := snapshot.Meters()
	if len(meters) == 0 {
		return quotaSignal{health: quotaHealthUnknown}
	}
	health := quotaHealthClear
	cause := ""
	allZero := true
	for _, meter := range meters {
		if meter.Window.UsedPercent != 0 {
			allZero = false
		}
		candidate := windowQuotaHealth(meter.Window, now)
		if candidate > health {
			health = candidate
			cause = meterQuotaCause(meter)
		}
	}
	if allZero {
		return quotaSignal{health: quotaHealthFresh}
	}
	return quotaSignal{health: health, cause: cause}
}

func snapshotQuotaHealth(snapshot codex.Snapshot, now time.Time) quotaHealth {
	return snapshotQuotaSignal(snapshot, now).health
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

func snapshotLimitReachedCause(snapshot codex.Snapshot) (string, bool) {
	reached := func(limit codex.RateLimitSnapshot, fallback string) (string, bool) {
		if limit.SpendControlReached != nil && *limit.SpendControlReached {
			return "SPEND", true
		}
		if limit.RateLimitReachedType == nil || *limit.RateLimitReachedType == "" {
			return "", false
		}
		switch *limit.RateLimitReachedType {
		case "workspace_owner_credits_depleted", "workspace_member_credits_depleted":
			return "CREDITS", true
		case "workspace_owner_usage_limit_reached", "workspace_member_usage_limit_reached":
			return "SPEND", true
		default:
			return fallback, true
		}
	}
	if cause, ok := reached(snapshot.RateLimits, "QUOTA"); ok {
		return cause, true
	}
	keys := make([]string, 0, len(snapshot.RateLimitsByLimitID))
	for key := range snapshot.RateLimitsByLimitID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		limit := snapshot.RateLimitsByLimitID[key]
		fallback := codex.DisplayName(key)
		if limit.LimitName != nil && *limit.LimitName != "" {
			fallback = codex.DisplayName(*limit.LimitName)
		}
		if cause, ok := reached(limit, fallback); ok {
			return cause, true
		}
	}
	return "", false
}

func meterQuotaCause(meter codex.Meter) string {
	if meter.Kind == codex.MeterIndividualLimit {
		return "MONTHLY"
	}
	if meter.Bucket == "codex" {
		return meter.Name
	}
	return codex.DisplayName(meter.Bucket) + " " + meter.Name
}

func compactQuotaCause(cause string) string {
	fields := strings.Fields(cause)
	if len(fields) == 2 {
		if value, err := strconv.Atoi(fields[0]); err == nil {
			unit := map[string]string{"MINUTE": "M", "MINUTES": "M", "HOUR": "H", "HOURS": "H", "DAY": "D", "DAYS": "D", "WEEK": "W", "WEEKS": "W"}[fields[1]]
			if unit != "" {
				return strconv.Itoa(value) + unit
			}
		}
	}
	runes := []rune(cause)
	if len(runes) > 12 {
		return string(runes[:12])
	}
	return cause
}

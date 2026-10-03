package codex

import (
	"sort"
	"time"
)

// The identity hash is private to the ledger; it never reaches either UI.
type storedUsageReports struct {
	Identity string            `json:"identity"`
	Daily    *DailyUsageReport `json:"daily,omitempty"`
	Plan     *PlanUsageReport  `json:"plan,omitempty"`
}

func reconcileUsageReports(stored **storedUsageReports, incoming *UsageReports, now time.Time) *UsageReports {
	// Never reveal cached analytics on email-only verification. Older clients
	// and token-export-disabled accounts still get the existing token history.
	if incoming == nil || incoming.Identity == "" {
		return nil
	}
	if *stored == nil || (*stored).Identity != incoming.Identity {
		*stored = &storedUsageReports{Identity: incoming.Identity}
	}
	s := *stored
	cutoff := now.UTC().AddDate(0, 0, -historyRetentionDays).Format(time.DateOnly)
	if incoming.Daily != nil {
		r := *incoming.Daily
		days := map[string]UsageBreakdownDay{}
		if s.Daily != nil && s.Daily.Units == r.Units {
			for _, day := range s.Daily.Days {
				// The refreshed range replaces earlier observations, including
				// removed/corrected buckets. Absence is not a fabricated zero.
				if day.Date < r.From || day.Date > r.Through {
					days[day.Date] = day
				}
			}
		}
		for _, day := range r.Days {
			days[day.Date] = day
		}
		r.Days = []UsageBreakdownDay{}
		for date, day := range days {
			if date >= cutoff {
				r.Days = append(r.Days, day)
			}
		}
		sort.Slice(r.Days, func(i, j int) bool { return r.Days[i].Date < r.Days[j].Date })
		// From/Through describe the latest queried range, not claimed coverage
		// of retained older dates. Only actual buckets are navigable.
		s.Daily = &r
	}
	if incoming.Plan != nil {
		r := *incoming.Plan
		periods := map[string]PlanUsagePeriod{}
		if s.Plan != nil {
			for _, p := range s.Plan.Periods {
				// Only retain periods strictly before the newly queried coverage.
				// Corrected/deleted periods inside it must not survive by old ID.
				if r.CoverageStart != "" && p.EndsAt < r.CoverageStart {
					periods[p.ID] = p
				}
			}
		}
		for _, p := range r.Periods {
			periods[p.ID] = p
		}
		r.Periods = []PlanUsagePeriod{}
		for _, p := range periods {
			if p.EndsAt >= cutoff {
				r.Periods = append(r.Periods, p)
			}
		}
		sort.Slice(r.Periods, func(i, j int) bool { return r.Periods[i].StartsAt > r.Periods[j].StartsAt })
		if len(r.Periods) > 2400 {
			r.Periods = r.Periods[:2400]
		}
		s.Plan = &r
	}
	// Retention also applies during an endpoint outage, not just on success.
	if s.Daily != nil {
		r := *s.Daily
		r.Days = []UsageBreakdownDay{}
		for _, d := range s.Daily.Days {
			if d.Date >= cutoff {
				r.Days = append(r.Days, d)
			}
		}
		s.Daily = &r
	}
	if s.Plan != nil {
		r := *s.Plan
		r.Periods = []PlanUsagePeriod{}
		for _, p := range s.Plan.Periods {
			if p.EndsAt >= cutoff {
				r.Periods = append(r.Periods, p)
			}
		}
		s.Plan = &r
	}
	result := *incoming
	if result.Daily == nil && s.Daily != nil {
		result.DailyStatus = "STALE // " + incoming.DailyStatus
	}
	if result.Plan == nil && s.Plan != nil {
		result.PlanStatus = "STALE // " + incoming.PlanStatus
	}
	result.Daily = s.Daily
	result.Plan = s.Plan
	return &result
}

type UsageCategory struct {
	Name  string
	Value float64
}

// Shared ordering and totals keep the TUI and browser reports consistent.
func UsageCategories(groups map[string]map[string]float64, dimension string) []UsageCategory {
	result := []UsageCategory{}
	for name, value := range groups[dimension] {
		result = append(result, UsageCategory{name, value})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Value == result[j].Value {
			return result[i].Name < result[j].Name
		}
		return result[i].Value > result[j].Value
	})
	return result
}

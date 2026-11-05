package codex

import "sort"

type UsageCategory struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

func UsageDimensions() []string { return []string{"surface", "model", "feature", "task start"} }

// UsageDistribution owns numeric scaling and ordering, never terminal/SVG
// geometry. A fractional total remains fractional; zero alone needs a fallback.
type UsageDistribution struct {
	Rows       []UsageCategory `json:"rows"`
	Total      *float64        `json:"total"`
	Peak       float64         `json:"peak"`
	KnownEmpty bool            `json:"knownEmpty"`
}

func usageDistribution(groups map[string]map[string]float64, dimension string, total *float64) UsageDistribution {
	d := UsageDistribution{Rows: UsageCategories(groups, dimension), Total: total}
	if total != nil {
		d.Peak = *total
		d.KnownEmpty = *total == 0 && groups[dimension] != nil
	}
	for _, row := range d.Rows {
		d.Peak = max(d.Peak, row.Value)
	}
	return d
}

func DailyUsageDistribution(day UsageBreakdownDay, dimension string) UsageDistribution {
	return usageDistribution(day.Groups, dimension, &day.Total)
}

func WindowUsageDistribution(period PlanUsagePeriod, dimension string) UsageDistribution {
	wire := dimension
	switch dimension {
	case "feature":
		wire = "thread_source"
	case "task start":
		wire = "turn_trigger"
	}
	groups := map[string]map[string]float64{}
	for _, breakdown := range period.Breakdowns {
		if breakdown.Dimension != wire {
			continue
		}
		if groups[dimension] == nil {
			groups[dimension] = map[string]float64{}
		}
		for _, row := range breakdown.Rows {
			groups[dimension][row.Key] += row.BasisPoints / 100
		}
	}
	var total *float64
	if period.UsedBasisPoints != nil {
		value := *period.UsedBasisPoints / 100
		total = &value
	}
	return usageDistribution(groups, dimension, total)
}

// ProjectUsageReports copies the report containers and adds display-only
// distributions. The raw backend observation is never mutated or persisted.
func ProjectUsageReports(reports *UsageReports) *UsageReports {
	if reports == nil {
		return nil
	}
	r := *reports
	if r.Daily != nil {
		daily := *r.Daily
		daily.Days = append([]UsageBreakdownDay(nil), daily.Days...)
		for i := range daily.Days {
			daily.Days[i].Views = map[string]UsageDistribution{}
			for _, dimension := range UsageDimensions() {
				daily.Days[i].Views[dimension] = DailyUsageDistribution(daily.Days[i], dimension)
			}
		}
		r.Daily = &daily
	}
	if r.Plan != nil {
		plan := *r.Plan
		plan.Periods = append([]PlanUsagePeriod(nil), plan.Periods...)
		for i := range plan.Periods {
			plan.Periods[i].Views = map[string]UsageDistribution{}
			for _, dimension := range UsageDimensions() {
				plan.Periods[i].Views[dimension] = WindowUsageDistribution(plan.Periods[i], dimension)
			}
		}
		r.Plan = &plan
	}
	return &r
}

// UsageCategories returns a deterministic ordering for a reported dimension.
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

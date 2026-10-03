package codex

import "time"

func DemoUsageReports(now time.Time) *UsageReports {
	now = now.UTC()
	r := &UsageReports{DailyStatus: "DEMO", PlanStatus: "DEMO", Daily: &DailyUsageReport{Units: "RELATIVE USAGE", From: now.AddDate(0, 0, -29).Format(time.DateOnly), Through: now.Format(time.DateOnly), FetchedAt: now, DataAsOf: now.Format(time.RFC3339)}}
	for i := 29; i >= 0; i-- {
		total := float64(7 + i%11)
		r.Daily.Days = append(r.Daily.Days, UsageBreakdownDay{Date: now.AddDate(0, 0, -i).Format(time.DateOnly), Total: total, Groups: map[string]map[string]float64{
			"model": {"gpt-6.1-sol": total * .7, "gpt-6-luna": total * .3}, "surface": {"cli": total * .8, "desktop": total * .2}, "feature": {"agent": total * .65, "subagent": total * .35}, "task start": {"user": total},
		}})
	}
	used := 5500.0
	r.Plan = &PlanUsageReport{FetchedAt: now, DataAsOf: now.Format(time.RFC3339), CoverageStart: now.AddDate(0, 0, -7).Format(time.RFC3339), Approximate: true, Periods: []PlanUsagePeriod{{ID: "demo-period", WindowMinutes: 10080, PlanType: "pro", StartsAt: now.AddDate(0, 0, -4).Format(time.RFC3339), EndsAt: now.AddDate(0, 0, 3).Format(time.RFC3339), UsedBasisPoints: &used, Breakdowns: []PlanUsageBreakdown{
		{Dimension: "model", Rows: []PlanUsageValue{{Key: "gpt-6.1-sol", BasisPoints: 4000}, {Key: "gpt-6-luna", BasisPoints: 1500}}},
		{Dimension: "surface", Rows: []PlanUsageValue{{Key: "cli", BasisPoints: 5500}}},
	}}}}
	return r
}

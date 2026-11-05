package web

import (
	"encoding/json"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/quotagraph"
	"os"
	"testing"
	"time"
)

// These checked-in browser fixtures are produced by the very Go projectors
// used by the TUI/server, not a second JavaScript implementation in the tests.
func TestBrowserProjectionFixtures(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	meters := map[string]meter{}
	add := func(name string, used int, duration int64, reset time.Time, trail []quotagraph.Point) {
		r := reset.Unix()
		w := codex.Window{UsedPercent: used, WindowDurationMins: &duration, ResetsAt: &r}
		meters[name] = meter{Name: name, Used: used, Duration: &duration, Reset: &r, Trail: trail, Plot: quotagraph.ProjectPlot(w, trail, now)}
	}
	point := func(hoursAgo, elapsed float64, used int) quotagraph.Point {
		return quotagraph.Point{At: now.Add(-time.Duration(hoursAgo * float64(time.Hour))), Elapsed: elapsed, Used: used}
	}
	weekly := int64(10080)
	end := now.Add(72 * time.Hour)
	add("weeklyTrace", 40, weekly, end, []quotagraph.Point{point(25, 42, 20), point(24, 42.6, 21), point(1, 56.5, 38), point(.25, 56.95, 39), point(0, 57.1, 40)})
	add("weeklyEmpty", 40, weekly, end, nil)
	add("weeklyZero", 0, weekly, end, nil)
	add("weeklyStart", 0, weekly, now.Add(168*time.Hour), nil)
	elapsed := 400.0 / 7
	delta := 50.0 / 168
	add("overWindow", 80, weekly, end, []quotagraph.Point{point(0, elapsed, 80)})
	add("overRise", 80, weekly, end, []quotagraph.Point{point(.5, elapsed-delta, 79), point(0, elapsed, 80)})
	add("overFlat", 80, weekly, end, []quotagraph.Point{point(.5, elapsed-delta, 80), point(0, elapsed, 80)})
	add("boundary", 50, weekly, now.Add(84*time.Hour), nil)
	add("start", 0, 100, now.Add(125*time.Minute), nil)
	add("fast", 75, 100, now.Add(75*time.Minute), nil)
	add("end", 100, 100, now.Add(-50*time.Minute), nil)
	meters["unknown"] = meter{Name: "unknown", Used: 40, Plot: quotagraph.ProjectPlot(codex.Window{}, nil, now)}
	used := 12500.0
	reports := &codex.UsageReports{DailyStatus: "OPENAI", PlanStatus: "OPENAI", Daily: &codex.DailyUsageReport{Units: "RELATIVE USAGE", From: "2026-09-04", Through: "2026-10-03", FetchedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), Days: []codex.UsageBreakdownDay{
		{Date: "2026-10-01", Total: 2, Groups: map[string]map[string]float64{"model": {"older-model": 2}}},
		{Date: "2026-10-02", Total: 4, Groups: map[string]map[string]float64{"model": {"gpt-6.1-sol": 2.5, "gpt-6-luna": 1.5}, "surface": {"cli": 4}}},
	}}, Plan: &codex.PlanUsageReport{FetchedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), DataAsOf: "2026-10-03T11:00:00Z", CoverageStart: "2026-09-26T00:00:00Z", Approximate: true, BoundaryTolerance: new(uint32(60)), Periods: []codex.PlanUsagePeriod{
		{ID: "p1", WindowMinutes: 10080, PlanType: "pro", StartsAt: "2026-09-28T00:00:00Z", EndsAt: "2026-10-05T00:00:00Z", UsedBasisPoints: &used, Breakdowns: []codex.PlanUsageBreakdown{{Dimension: "model", Rows: []codex.PlanUsageValue{{Key: "gpt-6.1-sol", BasisPoints: 12000}, {Key: "unknown", BasisPoints: 500}}}}},
		{ID: "p2", WindowMinutes: 300, PlanType: "pro", StartsAt: "2026-09-27T00:00:00Z", EndsAt: "2026-09-27T05:00:00Z"},
	}}}
	fractional := &codex.UsageReports{Daily: &codex.DailyUsageReport{Units: "RELATIVE USAGE", Days: []codex.UsageBreakdownDay{{Date: "2026-10-02", Total: .1, Groups: map[string]map[string]float64{"surface": {"cli": .05, "desktop": .05}}}}}}
	data, err := json.MarshalIndent(map[string]any{"meters": meters, "reports": codex.ProjectUsageReports(reports), "fractional": codex.ProjectUsageReports(fractional)}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	const path = "../../web/tests/projections.generated.json"
	if os.Getenv("CODEXOMETER_WEB_GENERATE") == "1" {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(data) {
		t.Fatal("browser numerical fixtures are stale; run make web-contracts", err)
	}
}

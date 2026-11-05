package web

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/quotagraph"
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
	correctionPlan := *reports.Plan
	correctionPeriod := correctionPlan.Periods[0]
	correctionPeriod.UsedBasisPoints = new(-25.0)
	correctionPeriod.Breakdowns = []codex.PlanUsageBreakdown{{Dimension: "surface", Rows: []codex.PlanUsageValue{{Key: "cli", BasisPoints: -25}}}}
	correctionPlan.Periods = []codex.PlanUsagePeriod{correctionPeriod}
	correction := &codex.UsageReports{PlanStatus: "OPENAI", Plan: &correctionPlan}
	data, err := json.MarshalIndent(map[string]any{"meters": meters, "reports": codex.ProjectUsageReports(reports), "fractional": codex.ProjectUsageReports(fractional), "correction": codex.ProjectUsageReports(correction)}, "", "  ")
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
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatal("invalid browser numerical fixtures", err)
	}
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if err := compareProjectionValues(actual, expected, "$"); err != nil {
		t.Fatal("browser numerical fixtures are stale; run make web-contracts:", err)
	}
}

// Fused multiply-add on ARM64 and FMA-capable amd64 can change the final bits
// of graph coordinates. Compare JSON structure exactly, allowing only rounding
// noise in numbers; never change production maths to make fixture bytes agree.
func compareProjectionValues(got, want any, path string) error {
	switch expected := want.(type) {
	case float64:
		if actual, ok := got.(float64); ok {
			tolerance := (16 * 0x1p-52) * max(1, math.Abs(expected))
			if math.Abs(actual-expected) <= tolerance {
				return nil
			}
		}
	case map[string]any:
		if actual, ok := got.(map[string]any); ok && len(actual) == len(expected) {
			keys := make([]string, 0, len(expected))
			for key := range expected {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				value, present := actual[key]
				if !present {
					return fmt.Errorf("%s.%s: missing field", path, key)
				}
				if err := compareProjectionValues(value, expected[key], path+"."+key); err != nil {
					return err
				}
			}
			return nil
		}
	case []any:
		if actual, ok := got.([]any); ok && len(actual) == len(expected) {
			for i := range expected {
				if err := compareProjectionValues(actual[i], expected[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
			return nil
		}
	default:
		if reflect.DeepEqual(got, want) {
			return nil
		}
	}
	return fmt.Errorf("%s: got %v, want %v", path, got, want)
}

func TestProjectionFixtureComparison(t *testing.T) {
	for _, tc := range []struct {
		name, got, want string
		equal           bool
	}{
		{"FMA roundoff", `{"segment":{"y1":79.00000000000001,"y2":99.99999999999999}}`, `{"segment":{"y1":79,"y2":100}}`, true},
		{"regression roundoff", `{"y":38.015873015872984}`, `{"y":38.015873015872955}`, true},
		{"coordinate change", `{"y":79.00000001}`, `{"y":79}`, false},
		{"small value change", `{"total":0.05000001}`, `{"total":0.05}`, false},
		{"zero changed", `{"y":0.00000001}`, `{"y":0}`, false},
		{"window change", `{"duration":10081}`, `{"duration":10080}`, false},
		{"safety changed", `{"safe":false}`, `{"safe":true}`, false},
		{"label changed", `{"name":"new"}`, `{"name":"old"}`, false},
		{"missing field", `{}`, `{"segment":null}`, false},
		{"different field", `{"other":null}`, `{"segment":null}`, false},
		{"extra field", `{"segment":null,"other":null}`, `{"segment":null}`, false},
		{"missing row", `[]`, `[1]`, false},
		{"row order", `[1,2]`, `[2,1]`, false},
		{"missing versus empty", `null`, `[]`, false},
		{"number versus string", `"79"`, `79`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got, want any
			if err := json.Unmarshal([]byte(tc.got), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if err := compareProjectionValues(got, want, "$"); (err == nil) != tc.equal {
				t.Fatalf("equal=%v: %v", tc.equal, err)
			}
		})
	}
}

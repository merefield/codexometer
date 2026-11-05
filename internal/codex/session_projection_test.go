package codex

import (
	"reflect"
	"testing"
	"time"
)

func TestSessionStatusProjection(t *testing.T) {
	for _, test := range []struct {
		active, working bool
		attention       SessionAttention
		code, want      string
	}{
		{true, false, SessionAttentionNone, "", "IDLE"}, {true, true, SessionAttentionNone, "", "WORKING"},
		{false, false, SessionAttentionNone, "", "INACTIVE"},
		{true, true, SessionAttentionInput, "", "INPUT NEEDED"}, {true, true, SessionAttentionApproval, "", "APPROVAL NEEDED"},
		{true, false, SessionAttentionCheck, "", "CHECK SESSION"}, {true, false, SessionAttentionComplete, "", "TURN COMPLETE"},
		{true, false, SessionAttentionError, "serverOverloaded", "MODEL BUSY"},
		{true, false, SessionAttentionError, "modelNotFound", "MODEL UNAVAILABLE"},
		{true, false, SessionAttentionError, "policyCapacityUnavailable", "CAPACITY RESTRICTED"},
		{true, false, SessionAttentionError, "future", "TURN FAILED"},
	} {
		if got := SessionStatus(test.active, test.working, test.attention, SessionContext{FailureCode: test.code}); got != test.want {
			t.Fatalf("%+v: %q", test, got)
		}
	}
	if SessionAttentionLabel(SessionAttentionNone, SessionContext{}) != "" {
		t.Fatal("no attention invented an input request")
	}
}

func TestMeterKeysIgnoreLabelsAndDistinguishEverySlot(t *testing.T) {
	duration := int64(300)
	label := "Shared label"
	w := Window{WindowDurationMins: &duration}
	q := Snapshot{RateLimitsByLimitID: map[string]RateLimitSnapshot{"a": {LimitName: &label, Primary: &w, Secondary: &w}, "b": {LimitName: &label, Primary: &w}}}
	seen := map[MeterKey]bool{}
	for _, meter := range q.Meters() {
		if seen[meter.Key()] {
			t.Fatal("identity collision", meter.Key())
		}
		seen[meter.Key()] = true
	}
	q.RateLimitsByLimitID["a"] = RateLimitSnapshot{Secondary: &w}
	if key := q.Meters()[0].Key(); !seen[key] || key.Slot != 1 {
		t.Fatal("secondary identity changed when primary disappeared", key)
	}
}

func TestUsageDistributionFractionalUnitsAndMissingData(t *testing.T) {
	day := UsageBreakdownDay{Total: .1, Groups: map[string]map[string]float64{"surface": {"cli": .05, "desktop": .05}}}
	d := DailyUsageDistribution(day, "surface")
	if d.Peak != .1 || d.Rows[0].Value/d.Peak != .5 || d.Rows[0].Name != "cli" {
		t.Fatal("fractional scale/order changed", d)
	}
	missing := DailyUsageDistribution(UsageBreakdownDay{}, "surface")
	empty := DailyUsageDistribution(UsageBreakdownDay{Groups: map[string]map[string]float64{"surface": {}}}, "surface")
	if missing.KnownEmpty || !empty.KnownEmpty {
		t.Fatal("missing was treated as zero")
	}
	used := 12500.0
	period := PlanUsagePeriod{UsedBasisPoints: &used, Breakdowns: []PlanUsageBreakdown{
		{Dimension: "thread_source", Rows: []PlanUsageValue{{Key: "agent", BasisPoints: 10000}}},
		{Dimension: "thread_source", Rows: []PlanUsageValue{{Key: "agent", BasisPoints: 2500}}},
	}}
	p := WindowUsageDistribution(period, "feature")
	if *p.Total != 125 || p.Peak != 125 || p.Rows[0].Value != 125 {
		t.Fatal("window units or aggregation changed", p)
	}
	raw := &UsageReports{Daily: &DailyUsageReport{Days: []UsageBreakdownDay{day}}, Plan: &PlanUsageReport{Periods: []PlanUsagePeriod{period}}}
	projected := ProjectUsageReports(raw)
	if raw.Daily.Days[0].Views != nil || raw.Plan.Periods[0].Views != nil || !reflect.DeepEqual(projected.Daily.Days[0].Views["surface"], d) {
		t.Fatal("projection mutated source or disagreed with renderer")
	}
}

func TestUsageSamplesShareBoundsAndElapsedIntervals(t *testing.T) {
	now := time.Unix(1800000000, 0)
	var samples []UsageSample
	for i := 0; i < 4; i++ {
		samples = CaptureUsageSample(samples, int64((i+1)*10), int64(i*10), now, now.Add(time.Duration(i+1)*30*time.Second), 2)
	}
	if len(samples) != 2 || samples[0].At != now.Add(90*time.Second) || samples[1].Duration != 30*time.Second || samples[1].Tokens != 10 {
		t.Fatal(samples)
	}
	samples = CaptureUsageSample(samples, 1, 100, now, now.Add(time.Minute), 2)
	if samples[1].Tokens != 0 || samples[1].Duration != 0 {
		t.Fatal("counter/time regressions became activity", samples)
	}
	if AppendUsageSample(samples, UsageSample{}, 0) != nil {
		t.Fatal("unbounded zero limit")
	}
}

func TestUsageDistributionPreservesSignedCorrectionsWithNonNegativeScale(t *testing.T) {
	for _, tc := range []struct {
		name  string
		total *float64
		rows  []PlanUsageValue
		peak  float64
	}{
		{"negative only", new(-25.0), []PlanUsageValue{{Key: "cli", BasisPoints: -25}}, 0},
		{"negative without total", nil, []PlanUsageValue{{Key: "cli", BasisPoints: -25}}, 0},
		{"mixed corrections", new(-25.0), []PlanUsageValue{{Key: "cli", BasisPoints: 100}, {Key: "unknown", BasisPoints: -125}}, 1},
		{"positive total", new(100.0), []PlanUsageValue{{Key: "cli", BasisPoints: -25}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			period := PlanUsagePeriod{UsedBasisPoints: tc.total, Breakdowns: []PlanUsageBreakdown{{Dimension: "surface", Rows: tc.rows}}}
			d := WindowUsageDistribution(period, "surface")
			if d.Peak != tc.peak || d.KnownEmpty || len(d.Rows) != len(tc.rows) {
				t.Fatalf("invalid correction scale: %+v", d)
			}
			if tc.total == nil && d.Total != nil || tc.total != nil && (d.Total == nil || *d.Total != *tc.total/100) {
				t.Fatalf("signed or unknown total changed: %+v", d)
			}
			for _, raw := range tc.rows {
				found := false
				for _, row := range d.Rows {
					if row.Name == raw.Key && row.Value == raw.BasisPoints/100 {
						found = true
					}
				}
				if !found {
					t.Fatalf("signed row changed: %+v in %+v", raw, d.Rows)
				}
			}
		})
	}
}

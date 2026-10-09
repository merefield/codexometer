package quotagraph

import (
	"math"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func testWindow(now time.Time, used int) codex.Window {
	duration, reset := int64(100), now.Add(50*time.Minute).Unix()
	return codex.Window{UsedPercent: used, WindowDurationMins: &duration, ResetsAt: &reset}
}

func TestWindowStartUsesSnapshotWithoutHistory(t *testing.T) {
	now := time.Unix(1800000000, 0)
	w := testWindow(now, 25)
	trend, ok := Project(WindowStart, w, nil, now)
	if !ok || trend.Slope != .5 || trend.Projected != 50 || trend.Start != 0 || trend.Intercept != 0 {
		t.Fatalf("origin projection: %+v %v", trend, ok)
	}
	zone, ok := trend.Segment(false)
	if !ok || zone != (Segment{0, 0, 100, 50}) {
		t.Fatalf("zone segment: %+v %v", zone, ok)
	}
	pace, ok := trend.Segment(true)
	if !ok || pace != (Segment{0, 0, 100, -50}) {
		t.Fatalf("pace segment: %+v %v", pace, ok)
	}
	w.UsedPercent = 0
	if trend, ok = Project(WindowStart, w, nil, now); !ok || trend.Projected != 0 {
		t.Fatal("zero consumption should have a safe projection")
	}
	if _, ok := Project(WindowStart, w, nil, now.Add(-50*time.Minute)); ok {
		t.Fatal("zero elapsed time cannot establish velocity")
	}
	if _, ok := Project(Off, w, nil, now); ok {
		t.Fatal("Off should not draw a trend")
	}
}

func TestWindowStartProjectionAtQuotaBoundary(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for used, want := range map[int]float64{25: 50, 50: 100, 75: 150, 100: 200} {
		trend, ok := Project(WindowStart, testWindow(now, used), nil, now)
		if !ok || trend.Projected != want {
			t.Fatalf("used=%d projection=%v valid=%v, want %v", used, trend.Projected, ok, want)
		}
	}
	if _, ok := Project(WindowStart, codex.Window{UsedPercent: 25}, nil, now); ok {
		t.Fatal("unknown cycle must not produce a projection")
	}
}

func TestRecentRegressionRequiresContinuousCoverage(t *testing.T) {
	now := time.Unix(1800000000, 0)
	w := testWindow(now, 40)
	points := []Point{{At: now.Add(-30 * time.Minute), Elapsed: 20, Used: 10}, {At: now.Add(-15 * time.Minute), Elapsed: 35, Used: 25}, {At: now, Elapsed: 50, Used: 40}}
	trend, ok := Project(HalfHour, w, points, now)
	if !ok || math.Abs(trend.Slope-1) > 1e-10 || trend.Projected != 90 || trend.Start != 20 || trend.Intercept != -10 {
		t.Fatalf("recent regression: %+v %v", trend, ok)
	}
	if Available(Hour, w, points, now) || Available(Day, w, points, now) {
		t.Fatal("unobserved windows should not be available")
	}
	points[1].Break = true
	if Available(HalfHour, w, points, now) {
		t.Fatal("a refresh failure cannot be bridged by a trend")
	}
	for i := 1; i <= 2; i++ {
		points = append(points, Point{At: now.Add(time.Duration(i) * 15 * time.Minute), Elapsed: float64(50 + i*15), Used: 40 + i*10})
	}
	if !Available(HalfHour, w, points, now.Add(30*time.Minute)) {
		t.Fatal("coverage should recover after the gap")
	}
}

func TestTrendClipping(t *testing.T) {
	for _, test := range []struct {
		trend Trend
		pace  bool
		want  Segment
	}{
		{Trend{Slope: 2}, false, Segment{0, 0, 50, 100}},
		{Trend{Slope: 3}, true, Segment{0, 0, 50, 100}},
		{Trend{Slope: 1, Intercept: -20, Start: 10}, false, Segment{20, 0, 100, 80}},
	} {
		got, ok := test.trend.Segment(test.pace)
		if !ok || got != test.want {
			t.Fatalf("clip %+v: %+v %v", test, got, ok)
		}
	}
	if _, ok := (Trend{Intercept: 200}).Segment(false); ok {
		t.Fatal("horizontal line outside plot should be omitted")
	}
}

func TestHistoryIdentityGapsAndResets(t *testing.T) {
	now := time.Unix(1800000000, 0)
	primary, secondary := testWindow(now, 10), testWindow(now, 20)
	q := codex.Snapshot{AccountFingerprint: "A", RateLimitsByLimitID: map[string]codex.RateLimitSnapshot{"a": {Primary: &primary, Secondary: &secondary}, "b": {Primary: &primary}}}
	var h History
	h.Observe(q, false, now)
	h.Observe(q, true, now.Add(time.Minute))
	h.Observe(q, false, now.Add(2*time.Minute))
	for _, series := range h.Series {
		if len(series.Points) != 2 || !series.Points[1].Break {
			t.Fatal("failed refresh must preserve history and mark a gap")
		}
	}
	bucket := q.RateLimitsByLimitID["a"]
	bucket.Primary = nil
	q.RateLimitsByLimitID["a"] = bucket
	h.Observe(q, false, now.Add(3*time.Minute))
	if len(h.Series) != 2 || h.Series[Keys(q)[0]].Points[0].Used != 20 {
		t.Fatal("secondary must not inherit primary history")
	}
	delete(q.RateLimitsByLimitID, "a")
	h.Observe(q, false, now.Add(4*time.Minute))
	if len(h.Series) != 1 {
		t.Fatal("removed limits retained history")
	}
	q.AccountFingerprint = "B"
	h.Observe(q, false, now.Add(5*time.Minute))
	if len(h.Series[Keys(q)[0]].Points) != 1 {
		t.Fatal("account changed without clearing history")
	}
	primary.UsedPercent = 5
	h.Observe(q, false, now.Add(6*time.Minute))
	if len(h.Series[Keys(q)[0]].Points) != 1 {
		t.Fatal("decreasing consumption must start a new trace")
	}
	*primary.ResetsAt += 3600
	h.Observe(q, false, now.Add(7*time.Minute))
	if len(h.Series[Keys(q)[0]].Points) != 1 {
		t.Fatal("reset identity changed without clearing history")
	}
	h.Observe(q, false, now.Add(6*time.Minute))
	if len(h.Series[Keys(q)[0]].Points) != 1 {
		t.Fatal("clock rewind must start a new trace")
	}
}

func TestHistoryBoundPreservesOriginAndBreak(t *testing.T) {
	now := time.Unix(1800000000, 0)
	w := testWindow(now, 10)
	var points []Point
	for i := 0; i < MaxPoints+20; i++ {
		points = Record(w, w, points, now.Add(time.Duration(i)*time.Second), false)
	}
	if len(points) != MaxPoints || points[0].At != now || !points[1].Break || points[len(points)-1].At != now.Add((MaxPoints+19)*time.Second) {
		t.Fatal("history is not bounded with a preserved origin and discontinuity")
	}
	if _, ok := Elapsed(codex.Window{}, now); ok {
		t.Fatal("unknown cycle cannot position a dot")
	}
}

func TestRecordDoesNotMutatePreviousSnapshot(t *testing.T) {
	now := time.Unix(1800000000, 0)
	w := testWindow(now, 10)
	previous := make([]Point, MaxPoints, MaxPoints+1)
	for i := range previous {
		previous[i] = Point{At: now.Add(time.Duration(i) * time.Second), Elapsed: 50, Used: 10}
	}
	second := previous[1]
	Record(w, w, previous, now.Add(MaxPoints*time.Second), false)
	if previous[1] != second {
		t.Fatal("bounding mutated a previous snapshot's trace")
	}
}

// Package quotagraph owns bounded, process-local quota observations and trend
// geometry independently of either presentation.
package quotagraph

import (
	"math"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

const MaxPoints = 1500

type Point struct {
	At      time.Time `json:"at"`
	Elapsed float64   `json:"elapsed"`
	Used    int       `json:"used"`
	Break   bool      `json:"break"`
}

func Elapsed(window codex.Window, now time.Time) (float64, bool) {
	if window.WindowDurationMins == nil || *window.WindowDurationMins <= 0 || window.ResetsAt == nil || *window.ResetsAt <= 0 {
		return 0, false
	}
	return max(0, min(100, 100*(1-(float64(*window.ResetsAt)-float64(now.UnixMilli())/1000)/(float64(*window.WindowDurationMins)*60)))), true
}

func Record(window, previous codex.Window, points []Point, now time.Time, gap bool) []Point {
	elapsed, ok := Elapsed(window, now)
	if !ok {
		return nil
	}
	if len(points) > 0 {
		last := points[len(points)-1]
		if previous.WindowDurationMins == nil || previous.ResetsAt == nil || *previous.WindowDurationMins != *window.WindowDurationMins || *previous.ResetsAt != *window.ResetsAt || !now.After(last.At) || window.UsedPercent < last.Used || elapsed < last.Elapsed {
			points = nil
		}
	}
	// Observers may still hold the previous snapshot; never mutate its trail.
	recorded := make([]Point, len(points)+1)
	copy(recorded, points)
	recorded[len(points)] = Point{At: now, Elapsed: elapsed, Used: max(0, min(100, window.UsedPercent)), Break: gap}
	points = recorded
	if len(points) > MaxPoints {
		points = append(points[:1], points[len(points)-(MaxPoints-1):]...)
		points[1].Break = true
	}
	return points
}

type Key struct {
	Limit string
	Slot  int
	Kind  codex.MeterKind
}

// Keys follows Meters' ordering without identifying windows by mutable labels.
func Keys(snapshot codex.Snapshot) []Key {
	meters := snapshot.Meters()
	keys := make([]Key, len(meters))
	slots := map[string]int{}
	for i, meter := range meters {
		key := Key{Limit: meter.LimitID, Kind: meter.Kind}
		if meter.Kind == codex.MeterQuotaWindow {
			bucket := snapshot.RateLimits
			if len(snapshot.RateLimitsByLimitID) > 0 {
				bucket = snapshot.RateLimitsByLimitID[meter.LimitID]
			}
			key.Slot = slots[meter.LimitID]
			if bucket.Primary == nil {
				key.Slot++
			}
			slots[meter.LimitID]++
		}
		keys[i] = key
	}
	return keys
}

type Series struct {
	Window codex.Window
	Points []Point
}
type History struct {
	Account string
	Series  map[Key]Series
	gap     bool
}

func (h *History) Observe(snapshot codex.Snapshot, failed bool, at time.Time) {
	if failed {
		h.gap = true
		return
	}
	if snapshot.AccountFingerprint != h.Account {
		h.Series = nil
	}
	h.Account = snapshot.AccountFingerprint
	keys := Keys(snapshot)
	next := make(map[Key]Series, len(keys))
	for i, meter := range snapshot.Meters() {
		old := h.Series[keys[i]]
		window := meter.Window
		// Retain values, not pointers into a caller's mutable snapshot.
		if window.WindowDurationMins != nil {
			duration := *window.WindowDurationMins
			window.WindowDurationMins = &duration
		}
		if window.ResetsAt != nil {
			reset := *window.ResetsAt
			window.ResetsAt = &reset
		}
		next[keys[i]] = Series{Window: window, Points: Record(window, old.Window, old.Points, at, h.gap)}
	}
	h.Series, h.gap = next, false
}

type Mode int

const (
	WindowStart Mode = iota
	HalfHour
	Hour
	Day
	Off
	ModeCount
)

func (m Mode) Minutes() float64 {
	switch m {
	case HalfHour:
		return 30
	case Hour:
		return 60
	case Day:
		return 1440
	}
	return 0
}
func (m Mode) Label() string {
	switch m {
	case HalfHour:
		return "LAST 30 MINUTES"
	case Hour:
		return "LAST HOUR"
	case Day:
		return "LAST 24 HOURS"
	case Off:
		return "OFF"
	}
	return "WINDOW START"
}

func Recent(points []Point, mode Mode) []Point {
	if len(points) < 2 || mode.Minutes() == 0 {
		return nil
	}
	start := 0
	for i := len(points) - 1; i > 0; i-- {
		if points[i].Break {
			start = i
			break
		}
	}
	points = points[start:]
	span := time.Duration(mode.Minutes()) * time.Minute
	last := points[len(points)-1].At
	if last.Sub(points[0].At) < span {
		return nil
	}
	cutoff := last.Add(-span)
	for len(points) > 0 && points[0].At.Before(cutoff) {
		points = points[1:]
	}
	if len(points) < 2 {
		return nil
	}
	return points
}

func Available(mode Mode, window codex.Window, points []Point, now time.Time) bool {
	if mode == Off {
		return true
	}
	elapsed, ok := Elapsed(window, now)
	if !ok {
		return false
	}
	if mode == WindowStart {
		return elapsed > 0
	}
	return len(Recent(points, mode)) >= 2
}

type Trend struct{ Slope, Intercept, Projected, Start float64 }

func Project(mode Mode, window codex.Window, points []Point, now time.Time) (Trend, bool) {
	if mode == Off || !Available(mode, window, points, now) {
		return Trend{}, false
	}
	elapsed, _ := Elapsed(window, now)
	used := float64(max(0, min(100, window.UsedPercent)))
	slope := used / elapsed
	if mode != WindowStart {
		recent := Recent(points, mode)
		var xMean, yMean float64
		for _, p := range recent {
			xMean += p.Elapsed
			yMean += float64(p.Used)
		}
		xMean /= float64(len(recent))
		yMean /= float64(len(recent))
		var covariance, variance float64
		for _, p := range recent {
			covariance += (p.Elapsed - xMean) * (float64(p.Used) - yMean)
			variance += math.Pow(p.Elapsed-xMean, 2)
		}
		if variance <= 0 {
			return Trend{}, false
		}
		slope = max(0, covariance/variance)
	}
	intercept := used - slope*elapsed
	start := max(0, elapsed-mode.Minutes()/float64(*window.WindowDurationMins)*100)
	if mode == WindowStart {
		intercept, start = 0, 0
	}
	return Trend{Slope: slope, Intercept: intercept, Projected: used + slope*(100-elapsed), Start: start}, true
}

type Segment struct{ X1, Y1, X2, Y2 float64 }

func (t Trend) Segment() (Segment, bool) {
	slope, lower, upper := t.Slope, 0.0, 100.0
	x1, x2 := t.Start, 100.0
	if slope != 0 {
		a, b := (lower-t.Intercept)/slope, (upper-t.Intercept)/slope
		x1 = max(x1, min(a, b))
		x2 = min(x2, max(a, b))
	}
	y1, y2 := t.Intercept+slope*x1, t.Intercept+slope*x2
	valid := x1 <= x2 && y1 >= lower-1e-9 && y1 <= upper+1e-9 && y2 >= lower-1e-9 && y2 <= upper+1e-9
	return Segment{X1: x1, Y1: max(lower, min(upper, y1)), X2: x2, Y2: max(lower, min(upper, y2))}, valid
}

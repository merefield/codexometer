package ui

import (
	"math"
	"time"
)

// Cache numerator and denominator together: neither redraws nor intermediate
// token polls should make one row's displayed average move independently.
func (m *Model) refreshMonitorRates(now time.Time, force bool) {
	if !force && !m.monitorRateAt.IsZero() && now.Sub(m.monitorRateAt) < 5*time.Second {
		return
	}
	m.monitorRateAt = now
	m.monitorAverageRate = averageTokenRate(m.monitorRecordedTokens(), m.monitorElapsed(now))
	for i := range m.monitorSessionData {
		s := &m.monitorSessionData[i]
		s.averageRate = averageTokenRate(max(s.latest-s.baseline, 0), m.monitorSessionElapsed(*s, now))
	}
}

func averageTokenRate(tokens int64, elapsed time.Duration) int64 {
	if elapsed <= 0 {
		return 0
	}
	rate := math.Round(float64(tokens) / elapsed.Minutes())
	if rate >= math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(rate)
}

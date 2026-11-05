package codex

import "time"

// UsageSample is a process-local counter interval, never a billing record.
type UsageSample struct {
	At       time.Time     `json:"at"`
	Duration time.Duration `json:"-"`
	Tokens   int64         `json:"tokens"`
}

// Callers own their measurement baseline; both interfaces use the same delta,
// elapsed-interval and bounded-history rules without mixing different zeroes.
func CaptureUsageSample(samples []UsageSample, current, previous int64, startedAt, at time.Time, limit int) []UsageSample {
	if len(samples) > 0 {
		startedAt = samples[len(samples)-1].At
	}
	return AppendUsageSample(samples, UsageSample{At: at, Duration: max(at.Sub(startedAt), 0), Tokens: max(current-previous, 0)}, limit)
}

func AppendUsageSample(samples []UsageSample, sample UsageSample, limit int) []UsageSample {
	if limit <= 0 {
		return nil
	}
	if len(samples) >= limit {
		copy(samples, samples[len(samples)-limit+1:])
		samples = samples[:limit-1]
	}
	return append(samples, sample)
}

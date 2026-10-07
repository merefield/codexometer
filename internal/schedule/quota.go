package schedule

import (
	"fmt"
	"github.com/merefield/codexometer/internal/codex"
	"slices"
	"time"
)

func WindowKeys(s codex.Snapshot) []string {
	var keys []string
	for _, m := range s.Meters() {
		keys = append(keys, fmt.Sprintf("%s/%d/%s", m.LimitID, m.Kind, m.Name))
	}
	return keys
}

// Missing data is not evidence that an exhausted window recovered.
func Covers(j Job, s codex.Snapshot) bool {
	keys := WindowKeys(s)
	for _, k := range j.Windows {
		if !slices.Contains(keys, k) {
			return false
		}
	}
	return true
}

// Until Codex exposes authoritative model-to-bucket applicability, waiting for
// every reported limit is conservative: never assume an exhausted bucket is
// irrelevant. No inference, reset redemption, or timestamp-only release.
func QuotaReady(s codex.Snapshot, now time.Time) bool {
	if !Fresh(s, now) {
		return false
	}
	meters := s.Meters()
	if len(meters) == 0 {
		return false
	}
	for _, m := range meters {
		if m.Window.UsedPercent < 0 || m.Window.UsedPercent >= 100 {
			return false
		}
	}
	buckets := s.RateLimitsByLimitID
	if len(buckets) == 0 {
		buckets = map[string]codex.RateLimitSnapshot{"codex": s.RateLimits}
	}
	for _, b := range buckets {
		if b.SpendControlReached != nil && *b.SpendControlReached {
			return false
		}
		if b.RateLimitReachedType != nil && *b.RateLimitReachedType != "" {
			return false
		}
	}
	return true
}

func Fresh(s codex.Snapshot, now time.Time) bool {
	return s.AccountFingerprint != "" && !s.FetchedAt.IsZero() && !now.Before(s.FetchedAt) && now.Sub(s.FetchedAt) <= 90*time.Second
}

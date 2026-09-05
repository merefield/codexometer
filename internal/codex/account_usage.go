package codex

import (
	"context"
	"time"
)

// AccountUsage is account-wide history reported by account/usage/read, not
// local session telemetry. Nil buckets mean unavailable; an empty list means
// the server returned no activity. Summary fields are independently optional.
type AccountUsage struct {
	Summary            AccountUsageSummary `json:"summary"`
	DailyUsageBuckets  []AccountUsageDay   `json:"dailyUsageBuckets"`
	AccountFingerprint string              `json:"-"`
	FetchedAt          time.Time           `json:"-"`
}

type AccountUsageSummary struct {
	LifetimeTokens        *int64 `json:"lifetimeTokens"`
	PeakDailyTokens       *int64 `json:"peakDailyTokens"`
	LongestRunningTurnSec *int64 `json:"longestRunningTurnSec"`
	CurrentStreakDays     *int64 `json:"currentStreakDays"`
	LongestStreakDays     *int64 `json:"longestStreakDays"`
}

type AccountUsageDay struct {
	StartDate string `json:"startDate"`
	Tokens    int64  `json:"tokens"`
}

func (c Client) FetchAccountUsage(ctx context.Context) (AccountUsage, error) {
	var history AccountUsage
	_, err := c.fetch(ctx, nil, &history)
	return history, err
}

package codex

import (
	"context"
	"time"
)

// AccountUsage is account-wide history reported by account/usage/read, not
// local session telemetry. Nil buckets mean unavailable; an empty list means
// the server returned no activity. Summary fields are independently optional.
type AccountUsage struct {
	Summary            AccountUsageSummary  `json:"summary"`
	DailyUsageBuckets  []AccountUsageDay    `json:"dailyUsageBuckets"`
	Coverage           AccountUsageCoverage `json:"coverage"`
	AccountFingerprint string               `json:"-"`
	FetchedAt          time.Time            `json:"-"`
	// Persisted means the result has been reconciled with Codexometer's local
	// history. Stale means the live refresh failed and this is the most recent
	// verified account cache rather than a fresh OpenAI response.
	Persisted bool `json:"persisted"`
	Stale     bool `json:"stale"`
}

type AccountUsageSummary struct {
	LifetimeTokens        *int64 `json:"lifetimeTokens"`
	PeakDailyTokens       *int64 `json:"peakDailyTokens"`
	LongestRunningTurnSec *int64 `json:"longestRunningTurnSec"`
	CurrentStreakDays     *int64 `json:"currentStreakDays"`
	LongestStreakDays     *int64 `json:"longestStreakDays"`
}

type AccountUsageDay struct {
	StartDate         string `json:"startDate"`
	Tokens            int64  `json:"tokens"`
	LocalTokens       int64  `json:"localTokens,omitempty"`
	InputTokens       int64  `json:"inputTokens,omitempty"`
	CachedInputTokens int64  `json:"cachedInputTokens,omitempty"`
	OutputTokens      int64  `json:"outputTokens,omitempty"`
	ReasoningTokens   int64  `json:"reasoningTokens,omitempty"`
	Provenance        string `json:"provenance,omitempty"`
}

// AccountUsageCoverage explains how much of the account-wide OpenAI total can
// also be attributed to retained local Codex rollouts. LocalTokens is never
// added to OpenAITokens: it is a subset/comparison, not another usage source.
type AccountUsageCoverage struct {
	Status        string `json:"status,omitempty"`
	OpenAITokens  int64  `json:"openaiTokens,omitempty"`
	LocalTokens   int64  `json:"localTokens,omitempty"`
	AttributedPct int    `json:"attributedPercent,omitempty"`
	OpenAIDays    int    `json:"openaiDays,omitempty"`
	RecoveredDays int    `json:"recoveredDays,omitempty"`
}

// RecoveredUsageDay is reconstructed from content-free token_count events in
// retained local Codex rollouts. Cached input is part of input, not additional
// usage, and TotalTokens remains the comparison value used against OpenAI.
type RecoveredUsageDay struct {
	StartDate         string `json:"startDate"`
	TotalTokens       int64  `json:"totalTokens"`
	InputTokens       int64  `json:"inputTokens,omitempty"`
	CachedInputTokens int64  `json:"cachedInputTokens,omitempty"`
	OutputTokens      int64  `json:"outputTokens,omitempty"`
	ReasoningTokens   int64  `json:"reasoningTokens,omitempty"`
}

func (c Client) FetchAccountUsage(ctx context.Context) (AccountUsage, error) {
	var history AccountUsage
	_, err := c.fetch(ctx, nil, &history)
	if err != nil {
		if c.History != nil {
			if cached, cacheErr := c.History.Latest(); cacheErr == nil && cached.AccountFingerprint != "" {
				cached.Stale = true
				return cached, nil
			}
		}
		return history, err
	}
	if c.History == nil {
		return history, nil
	}
	var recovered []RecoveredUsageDay
	if c.LiveUsage != nil {
		recovered, _ = c.LiveUsage.RecoverDailyUsage(ctx, time.Now().UTC().AddDate(0, 0, -historyRetentionDays))
	}
	reconciled, reconcileErr := c.History.Reconcile(history, recovered)
	if reconcileErr != nil {
		// A local cache failure must not turn a valid OpenAI response into an
		// unavailable Usage screen.
		return history, nil
	}
	return reconciled, nil
}

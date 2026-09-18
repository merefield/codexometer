package codex

import "testing"

func TestQuotaTargetMatching(t *testing.T) {
	priority, flex, standard := "priority", "flex", "default"
	for _, tc := range []struct {
		name    string
		session QuotaSession
		step    QuotaStep
		want    bool
	}{
		{"exact", QuotaSession{Model: "small", Effort: "medium", Tier: &priority}, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "priority"}, true},
		{"different model", QuotaSession{Model: "large", Effort: "medium", Tier: &priority}, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "priority"}, false},
		{"different reasoning", QuotaSession{Model: "small", Effort: "high", Tier: &priority}, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "priority"}, false},
		{"different speed", QuotaSession{Model: "small", Effort: "medium", Tier: &flex}, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "priority"}, false},
		{"omitted speed", QuotaSession{Model: "small", Effort: "medium", Tier: &flex}, QuotaStep{Model: "small", Effort: "medium"}, true},
		{"standard", QuotaSession{Model: "small", Effort: "medium"}, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "default"}, true},
		{"explicit standard", QuotaSession{Model: "small", Effort: "medium", Tier: &standard}, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "default"}, true},
		{"standard differs", QuotaSession{Model: "small", Effort: "medium", Tier: &priority}, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "default"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.session.MatchesQuotaStep(tc.step); got != tc.want {
				t.Fatalf("match=%v want=%v", got, tc.want)
			}
		})
	}
}

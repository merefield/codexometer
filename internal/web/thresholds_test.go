package web

import (
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestThresholdPolicyProjectionIsSortedAndTracksQuota(t *testing.T) {
	s := newStore()
	s.configureThresholds([]codex.QuotaStep{
		{Threshold: 80, Model: "small", Effort: "low", Mode: "auto"},
		{Threshold: 50, Model: "medium", Effort: "medium", ServiceTier: "fast"},
	})
	if len(s.state.Thresholds) != 2 || s.state.Thresholds[0].Threshold != 50 || s.state.Thresholds[0].State != "CONFIGURED" {
		t.Fatalf("configured thresholds = %#v", s.state.Thresholds)
	}

	snapshot := codex.DemoSnapshot()
	snapshot.AccountFingerprint = "account"
	snapshot.RateLimits.Secondary.UsedPercent = 65
	s.quota(snapshot, nil, time.Now())
	got := s.state.Thresholds
	if got[0].State != "ACTIVE" || got[0].Speed != "fast" || got[0].Mode != "ASK" {
		t.Fatalf("active threshold = %#v", got[0])
	}
	if got[1].State != "NEXT" || got[1].Remaining != 15 || got[1].Mode != "AUTO" || got[1].Speed != "UNCHANGED" {
		t.Fatalf("next threshold = %#v", got[1])
	}
}

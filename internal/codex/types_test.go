package codex

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMetersIncludesEveryBucketAndWindow(t *testing.T) {
	payload := []byte(`{
		"rateLimits": {"primary": {"usedPercent": 1}},
		"rateLimitsByLimitId": {
			"codex_other": {
				"limitName": "codex_other",
				"primary": {"usedPercent": 88, "windowDurationMins": 30, "resetsAt": 123456}
			},
			"codex": {
				"primary": {"usedPercent": 42, "windowDurationMins": 300},
				"secondary": {"usedPercent": 5, "windowDurationMins": 10080},
				"individualLimit": {"limit": "25000", "used": "8000", "remainingPercent": 68, "resetsAt": 987654},
				"credits": {"hasCredits": true, "unlimited": false, "balance": "17000"}
			}
		}
	}`)

	var snapshot Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	meters := snapshot.Meters()

	if len(meters) != 4 {
		t.Fatalf("got %d meters, want 4", len(meters))
	}
	if meters[0].Bucket != "codex" || meters[0].Name != "5 HOURS" || meters[0].Window.UsedPercent != 42 {
		t.Fatalf("unexpected primary meter: %#v", meters[0])
	}
	if meters[1].Name != "1 WEEK" {
		t.Fatalf("unexpected secondary name: %q", meters[1].Name)
	}
	if meters[2].Kind != MeterIndividualLimit || meters[2].Name != "MONTHLY CREDIT LIMIT" ||
		meters[2].Window.UsedPercent != 32 || meters[2].Window.ResetsAt == nil || *meters[2].Window.ResetsAt != 987654 ||
		meters[2].Details != "8000 OF 25000 CREDITS USED" {
		t.Fatalf("unexpected individual-limit meter: %#v", meters[2])
	}
	if meters[3].Bucket != "codex_other" || meters[3].Name != "30 MINUTES" ||
		meters[3].Window.WindowDurationMins == nil || *meters[3].Window.WindowDurationMins != 30 ||
		meters[3].Window.ResetsAt == nil || *meters[3].Window.ResetsAt != 123456 {
		t.Fatalf("unexpected additional meter: %#v", meters[3])
	}
	credits, ok := snapshot.CreditStatus()
	if !ok || credits.Balance == nil || *credits.Balance != "17000" {
		t.Fatalf("unexpected credit status: %#v, %v", credits, ok)
	}
}

func TestCreditStatusFallsBackDeterministicallyAndIgnoresEmptyData(t *testing.T) {
	balance := "42"
	snapshot := Snapshot{RateLimitsByLimitID: map[string]RateLimitSnapshot{
		"zeta":  {Credits: &Credits{HasCredits: true, Balance: &balance}},
		"alpha": {},
	}}
	credits, ok := snapshot.CreditStatus()
	if !ok || credits.Balance == nil || *credits.Balance != balance {
		t.Fatalf("credit fallback = %#v, %v", credits, ok)
	}
	if _, ok := (Snapshot{}).CreditStatus(); ok {
		t.Fatal("empty snapshot reported credits")
	}
}

func TestWindowNamesCoverSupportedDurations(t *testing.T) {
	tests := []struct {
		minutes *int64
		want    string
	}{
		{nil, "QUOTA WINDOW"},
		{minutes(1), "1 MINUTE"},
		{minutes(30), "30 MINUTES"},
		{minutes(60), "1 HOUR"},
		{minutes(300), "5 HOURS"},
		{minutes(1_440), "1 DAY"},
		{minutes(2_880), "2 DAYS"},
		{minutes(10_080), "1 WEEK"},
		{minutes(20_160), "2 WEEKS"},
	}
	for _, test := range tests {
		if got := windowName(Window{WindowDurationMins: test.minutes}); got != test.want {
			t.Errorf("windowName(%v) = %q, want %q", test.minutes, got, test.want)
		}
	}
}

func TestSnapshotHelpers(t *testing.T) {
	snapshot := DemoSnapshot()
	if snapshot.FetchedAt.IsZero() || len(snapshot.Meters()) != 2 {
		t.Fatalf("unexpected demo snapshot: %#v", snapshot)
	}
	if snapshot.RateLimitResetCredits == nil || snapshot.RateLimitResetCredits.AvailableCount != 1 {
		t.Fatalf("demo reset credits missing: %#v", snapshot.RateLimitResetCredits)
	}
	if got := DisplayName("workspace_member"); got != "WORKSPACE MEMBER" {
		t.Fatalf("DisplayName returned %q", got)
	}
	if snapshot.FetchedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("demo snapshot fetch time is stale")
	}
}

func minutes(value int64) *int64 {
	return &value
}

func TestMetersFallsBackToLegacyBucket(t *testing.T) {
	minutes := int64(60)
	snapshot := Snapshot{RateLimits: RateLimitSnapshot{
		Primary: &Window{UsedPercent: 23, WindowDurationMins: &minutes},
	}}

	meters := snapshot.Meters()
	if len(meters) != 1 || meters[0].Name != "1 HOUR" {
		t.Fatalf("unexpected meters: %#v", meters)
	}
}

package codex

import (
	"encoding/json"
	"testing"
)

func TestDecodeResetCreditDetails(t *testing.T) {
	var snapshot Snapshot
	err := json.Unmarshal([]byte(`{"rateLimitResetCredits":{"availableCount":3,"credits":[{"id":"expiring","resetType":"codexRateLimits","status":"available","grantedAt":1700000000,"expiresAt":1800000000,"title":"Weekly reset","description":"Refresh eligible limits"},{"id":"forever","resetType":"codexRateLimits","status":"available","expiresAt":null}]}}`), &snapshot)
	if err != nil {
		t.Fatal(err)
	}
	summary := snapshot.RateLimitResetCredits
	if summary == nil || summary.AvailableCount != 3 || len(summary.Credits) != 2 {
		t.Fatalf("partial inventory: %#v", summary)
	}
	first := summary.Credits[0]
	if first.ID != "expiring" || first.ExpiresAt == nil || *first.ExpiresAt != 1800000000 || first.GrantedAt != 1700000000 || first.Title != "Weekly reset" || first.Description == "" {
		t.Fatalf("lost detail: %#v", first)
	}
	if summary.Credits[1].ExpiresAt != nil {
		t.Fatal("invented non-expiring date")
	}
	var countOnly ResetCredits
	err = json.Unmarshal([]byte(`{"availableCount":2}`), &countOnly)
	if err != nil || countOnly.AvailableCount != 2 || countOnly.Credits != nil {
		t.Fatal("count-only compatibility lost")
	}
}

package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestNativeReviewReadOnlyProjection(t *testing.T) {
	s := newStore()
	s.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "thread", Active: true, Working: true, Context: codex.SessionContext{
		Text: "Existing conversation", Reviewer: "auto_review", ApprovalToken: "private-token",
		AutoReview: codex.SessionAutoReview{ID: "review", Status: "inProgress", Action: "Action\x1b[31m", ReviewFinding: codex.ReviewFinding{Risk: "low", Authorization: "high", Rationale: "Reason\x1b[31m"}},
	}}}}, nil, time.Now())
	data, _ := s.snapshot()
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	row := wire["sessions"].([]any)[0].(map[string]any)
	if row["text"] != "Existing conversation" || row["reviewer"] != "auto_review" || row["autoReview"] == nil {
		t.Fatalf("missing read-only projection: %s", data)
	}
	for _, private := range []string{"private-token", "approveGuardianDeniedAction", "\\u001b", "started_at_ms"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("unsafe review projection: %s", data)
		}
	}
	if s.state.Control {
		t.Fatal("telemetry enabled writes")
	}
}

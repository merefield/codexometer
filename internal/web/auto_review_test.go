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

func TestMarkdownWebProjectionKeepsOriginalAndExcludesRequests(t *testing.T) {
	const raw = "**result**"
	s := newStore()
	for _, tc := range []struct {
		context   codex.SessionContext
		wantProse bool
	}{
		{codex.SessionContext{Kind: codex.SessionContextReply, Text: raw}, true},
		{codex.SessionContext{Kind: codex.SessionContextActivity, Activity: codex.SessionActivity{Prose: raw, Command: "echo **stars**"}}, true},
		{codex.SessionContext{Kind: codex.SessionContextApproval, Text: raw}, false},
		{codex.SessionContext{Kind: codex.SessionContextActivity, Text: raw}, false},
	} {
		s.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "test", Active: true, Context: tc.context}}}, nil, time.Now())
		row := s.state.Sessions[0]
		if row.Text != raw || (len(row.Prose) != 0) != tc.wantProse || row.WorkingCommand != tc.context.Activity.Command {
			t.Fatalf("incorrect classification or source changed: %#v", row)
		}
		if tc.wantProse && (row.Prose[0].Runs[0].Text != "result" || !row.Prose[0].Runs[0].Bold) {
			t.Fatal(row.Prose)
		}
	}
}

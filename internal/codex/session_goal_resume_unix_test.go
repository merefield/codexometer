//go:build unix

package codex

import (
	"errors"
	"testing"
)

func TestScheduledGoalStopEvidenceAfterReconnect(t *testing.T) {
	for _, tc := range []struct {
		name, status, code string
		resume             bool
	}{
		{"API spend cap", "failed", "projectSpendLimitExceeded", false},
		{"unknown error", "failed", "", false},
		{"latest turn succeeded", "completed", "", false},
		{"no history method", "", "", false},
		{"subscription quota", "failed", "usageLimitExceeded", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, f := goalFixture(t)
			ctx := t.Context()
			accountReply := map[string]any{
				"account":          map[string]string{"type": "chatgpt", "email": "example@example.test"},
				"workspaceRouting": map[string]string{"chatgptAccountId": "original"},
			}
			account := quotaAccount(fingerprintAccount(rawJSON(accountReply)), "original")
			f.mu.Lock()
			f.catalogues = map[string]map[string]any{"account/read": accountReply}
			target := NewGoalResumeTarget(f.goals["one"])
			f.goals["one"].Status = "usageLimited"
			if tc.status != "" {
				f.catalogues["thread/turns/list"] = map[string]any{"data": []any{
					map[string]any{"id": "latest", "status": tc.status, "error": map[string]string{"codexErrorInfo": tc.code}},
				}}
			}
			f.mu.Unlock()
			p.disconnect(nil)
			if _, ok := p.Fetch(ctx, []string{"one", "two"}); !ok {
				t.Fatal("could not reconnect")
			}
			menu, err := p.SessionCommands(ctx, "one", ScheduleGoalResumePath)
			if err != nil || len(menu.Choices) != 1 {
				t.Fatal(menu, err)
			}
			if _, err := p.PrepareGoalResume(ctx, "one", account, menu.Revision, menu.Choices[0].ID); (err == nil) != tc.resume {
				t.Fatal("arming disagreed with verified stop reason", err)
			}
			err = p.ResumeScheduledGoal(ctx, "one", account, target)
			f.mu.Lock()
			defer f.mu.Unlock()
			if tc.resume {
				if err != nil || len(f.goalWrites) != 1 {
					t.Fatal("verified quota stop did not resume", err, f.goalWrites)
				}
			} else if !errors.Is(err, ErrScheduledGoalNotReady) || len(f.goalWrites) != 0 {
				t.Fatal("unverified stop did not stay pending", err, f.goalWrites)
			}
			if len(f.historyReads) != 2 || f.historyReads[0]["limit"] != float64(1) || f.historyReads[0]["itemsView"] != "notLoaded" {
				t.Fatal("expected one metadata-only turn per preflight", f.historyReads)
			}
		})
	}
}

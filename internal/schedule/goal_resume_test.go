package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

type goalSchedulerFake struct {
	goal             *codex.SessionGoal
	readErr, sendErr error
	writes           int
}

func (f *goalSchedulerFake) PrepareGoalResume(context.Context, string, string, string, string) (codex.GoalResumeTarget, error) {
	return codex.NewGoalResumeTarget(f.goal), nil
}

func (f *goalSchedulerFake) ObserveScheduledGoal(context.Context, string) (*codex.SessionGoal, error) {
	return f.goal, f.readErr
}

func (f *goalSchedulerFake) ResumeScheduledGoal(_ context.Context, _, _ string, target codex.GoalResumeTarget) error {
	f.writes++
	if !target.Matches(f.goal) || f.goal.Status != "usageLimited" {
		return codex.ErrScheduledGoalChanged
	}
	return f.sendErr
}

func goalQuota(now time.Time) codex.Snapshot {
	return codex.Snapshot{
		AccountFingerprint: "account", FetchedAt: now,
		RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 45}},
	}
}

func TestGoalResumeWaitsForNativeStopAndNewQuota(t *testing.T) {
	q, now := New(), time.Now()
	s := goalQuota(now)
	f := &goalSchedulerFake{goal: &codex.SessionGoal{ThreadID: "session", CreatedAt: 1, Objective: "Keep working", Status: "active"}}
	ctx := context.Background()
	if err := q.ArmGoalResume(ctx, f, s, now, "session", "rev", "choice"); err != nil {
		t.Fatal(err)
	}
	blocked := false
	poll := func() {
		now = now.Add(5 * time.Second)
		q.PollGoalResumes(ctx, f, func() codex.Snapshot { return s }, func() time.Time { return now }, func(string) bool { return blocked })
	}
	poll()
	if f.writes != 0 || !q.List("session")[0].WaitingForGoalStop {
		t.Fatal("armed trigger fired before the goal stopped")
	}
	f.goal.Status = "usageLimited"
	f.goal.TokensUsed++
	poll()
	poll()
	if f.writes != 0 || q.List("session")[0].WaitingForGoalStop {
		t.Fatal("observing a stop must not reuse the previous quota sample")
	}
	now = now.Add(time.Second)
	s.FetchedAt = now
	s.RateLimits.Primary.UsedPercent = 100
	poll()
	s.RateLimits.Primary.UsedPercent = 1
	original := s.RateLimits.Primary
	s.RateLimits.Primary = nil
	poll()
	s.RateLimits.Primary = original
	blocked = true
	poll()
	if f.writes != 0 {
		t.Fatal("sent with exhausted/missing quota or blocked session")
	}
	blocked = false
	poll()
	poll()
	if f.writes != 1 || q.List("session")[0].Status != "sent" {
		t.Fatal("didn't resume exactly once after fresh observation")
	}
}

func TestGoalResumeDisarmsOrDefersWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		change       func(*goalSchedulerFake, *codex.Snapshot)
		remain       bool
	}{
		{name: "manual resume", status: "active"},
		{name: "pause", status: "paused"},
		{name: "complete", status: "complete"},
		{name: "stall", status: "blocked"},
		{name: "unknown status", status: "futureStatus", remain: true},
		{name: "budget limited", status: "budgetLimited"},
		{name: "clear", change: func(f *goalSchedulerFake, _ *codex.Snapshot) { f.goal = nil }},
		{name: "replace", change: func(f *goalSchedulerFake, _ *codex.Snapshot) { f.goal.CreatedAt++ }},
		{name: "edit objective", change: func(f *goalSchedulerFake, _ *codex.Snapshot) { f.goal.Objective += " more" }},
		{name: "edit budget", change: func(f *goalSchedulerFake, _ *codex.Snapshot) { n := int64(11); f.goal.TokenBudget = &n }},
		{name: "different account", change: func(_ *goalSchedulerFake, s *codex.Snapshot) { s.AccountFingerprint = "another" }},
		{name: "stale quota", change: func(_ *goalSchedulerFake, s *codex.Snapshot) { s.FetchedAt = time.Time{} }, remain: true},
		{name: "stale goal", change: func(f *goalSchedulerFake, _ *codex.Snapshot) { f.goal.Stale = true }, remain: true},
		{name: "transient failure", change: func(f *goalSchedulerFake, _ *codex.Snapshot) { f.readErr = errors.New("offline") }, remain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, now := New(), time.Now()
			s := goalQuota(now)
			f := &goalSchedulerFake{goal: &codex.SessionGoal{ThreadID: "session", Objective: "Keep working", Status: "usageLimited"}}
			ctx := context.Background()
			if err := q.ArmGoalResume(ctx, f, s, now, "session", "rev", "choice"); err != nil {
				t.Fatal(err)
			}
			if tc.status != "" {
				f.goal.Status = tc.status
			}
			if tc.change != nil {
				tc.change(f, &s)
			}
			q.PollGoalResumes(ctx, f, func() codex.Snapshot { return s }, func() time.Time { return now }, func(string) bool { return false })
			if f.writes != 0 || q.HasPendingGoalResume() != tc.remain {
				t.Fatalf("writes = %d, remaining = %v", f.writes, q.List(""))
			}
		})
	}
}

func TestGoalResumeSlotAndUncertainty(t *testing.T) {
	q, now := New(), time.Now()
	s := goalQuota(now)
	f := &goalSchedulerFake{goal: &codex.SessionGoal{ThreadID: "session", Objective: "Keep working", Status: "usageLimited"}}
	ctx := context.Background()
	arm := func() error { return q.ArmGoalResume(ctx, f, s, now, "session", "rev", "choice") }
	text := Job{Session: "session", Text: "ordinary scheduled message", Trigger: "quota"}
	if err := q.Save(text, s.AccountFingerprint, now); err != nil {
		t.Fatal(err)
	}
	if err := arm(); !errors.Is(err, ErrGoalSlotOccupied) || q.List("session")[0].Text != text.Text {
		t.Fatal("arming overwrote a scheduled message", err)
	}
	if err := q.Cancel(text.Session, q.List("session")[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := arm(); err != nil {
		t.Fatal(err)
	}
	j := q.List("session")[0]
	if err := q.SaveIfCurrent(text, s.AccountFingerprint, now, j.ID); !errors.Is(err, ErrGoalSlotOccupied) {
		t.Fatal("ordinary edit overwrote a goal resume", err)
	}
	unexpected := func(context.Context, Job) error {
		t.Fatal("ordinary prompt dispatch claimed a goal trigger")
		return nil
	}
	if err := q.Dispatch(ctx, j.ID, s.AccountFingerprint, true, now, unexpected); err != nil || !q.HasPendingGoalResume() {
		t.Fatal("ordinary automatic dispatch did not leave the goal trigger pending", err)
	}
	if err := q.DispatchNow(ctx, j.ID, s.AccountFingerprint, unexpected); err == nil {
		t.Fatal("manual send-now accepted a goal trigger")
	}
	poll := func() {
		now = now.Add(5 * time.Second)
		q.PollGoalResumes(ctx, f, func() codex.Snapshot { return s }, func() time.Time { return now }, func(string) bool { return false })
	}
	f.sendErr = codex.ErrScheduledGoalNotReady
	poll()
	if f.writes != 0 {
		t.Fatal("a stopped goal used quota sampled before arming")
	}
	s.FetchedAt = now
	poll()
	if !q.HasPendingGoalResume() {
		t.Fatal("read-only preflight failure did not defer")
	}
	f.sendErr = context.DeadlineExceeded
	poll()
	poll()
	if f.writes != 2 || q.List("session")[0].Status != "uncertain" {
		t.Fatal("ambiguous dispatch was retried or dropped")
	}
}

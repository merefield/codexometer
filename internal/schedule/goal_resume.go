package schedule

import (
	"context"
	"errors"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

var ErrGoalSlotOccupied = errors.New("cancel the existing scheduled follow-up before setting another trigger")
var ErrGoalQuotaUnavailable = errors.New("fresh account quota is needed to schedule a goal resume")

func (j Job) AwaitingQuotaSample(s codex.Snapshot) bool {
	return j.ResumeGoal && !s.FetchedAt.After(j.stopObserved)
}

func (q *Queue) HasPendingGoalResume() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.jobs {
		if j.ResumeGoal && j.Status == "pending" {
			return true
		}
	}
	return false
}

func (q *Queue) ArmGoalResume(ctx context.Context, c codex.ScheduledGoalClient, quota codex.Snapshot, now time.Time, id, revision, choice string) error {
	windows := WindowKeys(quota)
	if !Fresh(quota, now) || len(windows) == 0 {
		return ErrGoalQuotaUnavailable
	}
	target, err := c.PrepareGoalResume(ctx, id, quota.QuotaAccount(), revision, choice)
	if err != nil {
		return err
	}
	job := Job{Session: id, Trigger: "quota", Windows: windows,
		ResumeGoal: true, WaitingForGoalStop: !target.Stopped, goal: target, baseAccount: quota.AccountFingerprint}
	if target.Stopped {
		// A sample taken before consent may predate the quota stop as well.
		job.stopObserved = now
	}
	return q.save(job, quota.QuotaAccount(), now, nil)
}

func (q *Queue) PendingGoalResumeID(session string) string {
	for _, job := range q.List(session) {
		if job.ResumeGoal && job.Status == "pending" {
			return job.ID
		}
	}
	return ""
}

// DisarmGoalResume cancels only the still-pending job seen before a manual
// mutation. It cannot recall an in-flight write or erase its uncertain result.
func (q *Queue) DisarmGoalResume(id string) {
	if id != "" {
		q.disarmGoal(id)
	}
}

func (q *Queue) disarmGoal(id string) { q.updateGoal(id, time.Time{}) }

func (q *Queue) updateGoal(id string, stopped time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, j := range q.jobs {
		if j.ID != id || !j.ResumeGoal || j.Status != "pending" {
			continue
		}
		if stopped.IsZero() {
			q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
		} else if j.WaitingForGoalStop {
			q.jobs[i].WaitingForGoalStop = false
			q.jobs[i].stopObserved = stopped
		}
		break
	}
}

func (q *Queue) claimGoalRead(id string, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, j := range q.jobs {
		if j.ID == id && j.Status == "pending" && !now.Before(j.nextGoalCheck) {
			q.jobs[i].nextGoalCheck = now.Add(5 * time.Second)
			return true
		}
	}
	return false
}

// PollGoalResumes is shared by terminal and browser schedulers. A paused
// adapter may still observe the stop but must not send while blocked.
func (q *Queue) PollGoalResumes(ctx context.Context, c codex.ScheduledGoalClient, quota func() codex.Snapshot, clock func() time.Time, blocked func(string) bool) {
	// A stalled read must not monopolise the scheduler (or delay ordinary
	// scheduled messages). It backs off, giving the other jobs a turn.
	reads, stopReads := context.WithTimeout(ctx, 2*time.Second)
	defer stopReads()
	for _, j := range q.List("") {
		if !j.ResumeGoal || j.Status != "pending" || ctx.Err() != nil {
			continue
		}
		s := quota()
		if !Fresh(s, clock()) {
			continue
		}
		if !j.MatchesAccount(s.QuotaAccount()) {
			if s.AccountFingerprint == j.baseAccount && codex.IsWorkspaceQuotaAccount(j.account) && !codex.IsWorkspaceQuotaAccount(s.QuotaAccount()) {
				continue // Missing workspace evidence is not an account switch.
			}
			q.disarmGoal(j.ID)
			continue
		}
		if reads.Err() != nil || !q.claimGoalRead(j.ID, clock()) {
			continue
		}
		goal, err := c.ObserveScheduledGoal(reads, j.Session)
		if err != nil || goal != nil && goal.Stale {
			continue
		}
		if !j.goal.Matches(goal) {
			q.disarmGoal(j.ID)
			continue
		}
		switch goal.Status {
		case "active":
			if !j.WaitingForGoalStop {
				q.disarmGoal(j.ID) // The user already resumed it.
			}
		case "paused", "blocked", "complete", "budgetLimited":
			q.disarmGoal(j.ID)
		case "usageLimited":
			if j.WaitingForGoalStop {
				q.updateGoal(j.ID, clock())
			} else {
				q.resumeGoal(ctx, c, j, quota, clock, blocked)
			}
		}
	}
}

func (q *Queue) resumeGoal(ctx context.Context, c codex.ScheduledGoalClient, j Job, quota func() codex.Snapshot, clock func() time.Time, blocked func(string) bool) {
	ready := func(s codex.Snapshot) bool {
		return j.MatchesAccount(s.QuotaAccount()) && !j.AwaitingQuotaSample(s) &&
			Covers(j, s) && QuotaReady(s, clock()) && QuotaRecoveryConfirmed(s) && !blocked(j.Session)
	}
	s := quota()
	if !ready(s) {
		return
	}
	call, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	_, _ = q.dispatch(call, j.ID, s.QuotaAccount(), func(j Job) bool {
		return j.ResumeGoal && !j.WaitingForGoalStop
	}, func(ctx context.Context, j Job) error {
		if !ready(quota()) {
			return ErrDeferred
		}
		err := c.ResumeScheduledGoal(ctx, j.Session, s.QuotaAccount(), j.goal)
		if errors.Is(err, codex.ErrScheduledGoalNotReady) {
			return ErrDeferred
		}
		return err
	})
}

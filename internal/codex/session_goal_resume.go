package codex

import (
	"context"
	"errors"
	"strings"
)

const ScheduleGoalResumePath = "goal/resume-after-quota"

func IsGoalMutation(path string) bool {
	return strings.HasPrefix(path, "goal/") && path != ScheduleGoalResumePath
}

// GoalResumeTarget binds consent to the observed goal, ignoring usage counters
// and the expected active-to-usageLimited transition.
type GoalResumeTarget struct {
	Fingerprint string
	Stopped     bool
}

type ScheduledGoalClient interface {
	PrepareGoalResume(ctx context.Context, session, account, revision, choice string) (GoalResumeTarget, error)
	ObserveScheduledGoal(context.Context, string) (*SessionGoal, error)
	ResumeScheduledGoal(ctx context.Context, session, account string, target GoalResumeTarget) error
}

// These errors guarantee no mutation was sent. Other write failures must not
// be retried: the server may have accepted the resume.
var (
	ErrScheduledGoalChanged  = errors.New("scheduled goal changed")
	ErrScheduledGoalNotReady = errors.New("scheduled goal is not ready")
)

func (t GoalResumeTarget) Matches(g *SessionGoal) bool {
	if g == nil || t.Fingerprint == "" {
		return false
	}
	return t.Fingerprint == NewGoalResumeTarget(g).Fingerprint
}

func NewGoalResumeTarget(g *SessionGoal) GoalResumeTarget {
	return GoalResumeTarget{commandDigest([]any{g.ThreadID, g.CreatedAt, g.Objective, g.TokenBudget}), g.Status == "usageLimited"}
}

func (c Client) PrepareGoalResume(ctx context.Context, session, account, revision, choice string) (GoalResumeTarget, error) {
	if p := liveSessionCapability[ScheduledGoalClient](c); p != nil {
		return p.PrepareGoalResume(ctx, session, account, revision, choice)
	}
	return GoalResumeTarget{}, ErrSessionCommand
}

func (c Client) ObserveScheduledGoal(ctx context.Context, session string) (*SessionGoal, error) {
	if p := liveSessionCapability[ScheduledGoalClient](c); p != nil {
		return p.ObserveScheduledGoal(ctx, session)
	}
	return nil, ErrSessionCommand
}

func (c Client) ResumeScheduledGoal(ctx context.Context, session, account string, t GoalResumeTarget) error {
	if p := liveSessionCapability[ScheduledGoalClient](c); p != nil {
		if cache := c.sessionCommandMenus(); cache != nil {
			defer cache.invalidate(session)
		}
		return p.ResumeScheduledGoal(ctx, session, account, t)
	}
	return ErrScheduledGoalNotReady
}

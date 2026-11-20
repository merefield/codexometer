//go:build unix

package codex

import (
	"context"
	"errors"

	"github.com/gorilla/websocket"
)

func (p *daemonStatusProvider) checkScheduledGoalAccount(ctx context.Context, conn *websocket.Conn, account string) error {
	err := p.verifyQuotaAccount(ctx, conn, account)
	if errors.Is(err, errQuotaAccountChanged) {
		return ErrScheduledGoalChanged
	}
	if err != nil {
		return ErrScheduledGoalNotReady
	}
	return nil
}

func (p *daemonStatusProvider) PrepareGoalResume(ctx context.Context, id, account, revision, choice string) (GoalResumeTarget, error) {
	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	if p.settingsClosed {
		return GoalResumeTarget{}, ErrSessionCommand
	}
	p.mu.Lock()
	conn, generation := p.connection, p.commandGeneration
	p.mu.Unlock()
	menu, err := p.SessionCommands(ctx, id, ScheduleGoalResumePath)
	if err != nil || menu.Revision != revision || menu.goalResume.Fingerprint == "" {
		return GoalResumeTarget{}, ErrSessionCommand
	}
	if err := p.checkScheduledGoalAccount(ctx, conn, account); err != nil {
		return GoalResumeTarget{}, err
	}
	p.mu.Lock()
	_, subscribed := p.subscribed[id]
	current := p.connection == conn && p.commandGeneration == generation && subscribed
	p.mu.Unlock()
	if !current {
		return GoalResumeTarget{}, ErrScheduledGoalNotReady
	}
	for _, o := range menu.Choices {
		if o.ID == choice && o.Action && o.Unavailable == "" {
			return menu.goalResume, nil
		}
	}
	return GoalResumeTarget{}, ErrSessionCommand
}

func (p *daemonStatusProvider) ObserveScheduledGoal(ctx context.Context, id string) (*SessionGoal, error) {
	conn := p.subscribedConnection(id)
	if conn == nil {
		return nil, ErrScheduledGoalNotReady
	}
	goal, err := p.readSessionGoal(ctx, id)
	if p.subscribedConnection(id) != conn {
		return nil, ErrScheduledGoalNotReady
	}
	return goal, err
}

func (p *daemonStatusProvider) ResumeScheduledGoal(ctx context.Context, id, account string, target GoalResumeTarget) error {
	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	p.mu.Lock()
	conn, generation := p.connection, p.commandGeneration
	p.mu.Unlock()
	if conn == nil || p.settingsClosed {
		return ErrScheduledGoalNotReady
	}
	if err := p.checkScheduledGoalAccount(ctx, conn, account); err != nil {
		return err
	}
	thread, err := p.commandThread(ctx, id)
	if err != nil || !threadAcceptsFollowup(thread.Status.Type) {
		return ErrScheduledGoalNotReady
	}
	goal, err := p.ObserveScheduledGoal(ctx, id)
	if err != nil || goal != nil && goal.Stale {
		return ErrScheduledGoalNotReady
	}
	if !target.Matches(goal) || goal.Status != "usageLimited" {
		return ErrScheduledGoalChanged
	}
	p.mu.Lock()
	_, subscribed := p.subscribed[id]
	ready := p.connection == conn && p.commandGeneration == generation && subscribed &&
		p.statuses[id] != sessionRuntimeWorking && !p.commandHumanWaitLocked(id)
	p.mu.Unlock()
	if !ready || ctx.Err() != nil {
		return ErrScheduledGoalNotReady
	}
	return p.executeGoalCommand(ctx, conn, id, SessionCommandChoice{
		method: "thread/goal/set",
		params: map[string]any{"threadId": id, "origin": "user", "status": "active"},
	}, target)
}

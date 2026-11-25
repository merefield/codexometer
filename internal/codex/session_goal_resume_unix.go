//go:build unix

package codex

import (
	"context"
	"encoding/json"
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

type goalStopObservation struct {
	state           *daemonContextState
	turnID, failure string
}

func (p *daemonStatusProvider) goalStopObservationLocked(id string) goalStopObservation {
	observation := goalStopObservation{state: p.contexts[id]}
	if state := observation.state; state != nil {
		observation.turnID = state.activeTurn
		if observation.turnID == "" {
			observation.turnID = state.finishedTurn
		}
		if state.latest.Kind == SessionContextFailure && state.latest.TurnID != "" &&
			(observation.turnID == "" || observation.turnID == state.latest.TurnID) {
			observation.turnID = state.latest.TurnID
			observation.failure = normalizedFailureCode(state.latest.FailureCode)
		}
	}
	return observation
}

var errGoalAPISpendCap = errors.New("API spend cap observed; resolve the provider limit before resuming manually")

// Codex reports usageLimited for both subscription quota and API billing caps.
// After reconnect, obtain the latest turn without loading its transcript.
func (p *daemonStatusProvider) verifyGoalQuotaStop(ctx context.Context, conn *websocket.Conn, id string) (func() bool, error) {
	p.mu.Lock()
	observed := p.goalStopObservationLocked(id)
	p.mu.Unlock()
	code := observed.failure
	if code != "usagelimitexceeded" && apiSpendCapLabel(code) == "" {
		var result struct {
			Data []struct {
				ID, Status string
				Error      json.RawMessage
			}
		}
		if err := p.requestOn(ctx, conn, "thread/turns/list", map[string]any{
			"threadId": id, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded",
		}, &result); err != nil || len(result.Data) != 1 {
			return nil, ErrScheduledGoalNotReady
		}
		turn := result.Data[0]
		if turn.Status != "failed" || turn.ID == "" || observed.turnID != "" && observed.turnID != turn.ID {
			return nil, ErrScheduledGoalNotReady
		}
		code = normalizedFailureCode(sessionFailureContext(SessionContext{}, turn.Error).FailureCode)
	}
	if apiSpendCapLabel(code) != "" {
		return nil, errGoalAPISpendCap
	}
	if code != "usagelimitexceeded" {
		return nil, ErrScheduledGoalNotReady
	}
	return func() bool {
		_, subscribed := p.subscribed[id]
		return subscribed && p.goalStopObservationLocked(id) == observed
	}, nil
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
	var stopStillCurrent func() bool
	if menu.goalResume.Stopped {
		stopStillCurrent, err = p.verifyGoalQuotaStop(ctx, conn, id)
		if err != nil {
			return GoalResumeTarget{}, err
		}
	}
	p.mu.Lock()
	_, subscribed := p.subscribed[id]
	current := p.connection == conn && p.commandGeneration == generation && subscribed && (stopStillCurrent == nil || stopStillCurrent())
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
	stopStillCurrent, err := p.verifyGoalQuotaStop(ctx, conn, id)
	if err != nil {
		return ErrScheduledGoalNotReady
	}
	readyLocked := func() bool {
		current := p.goals[id]
		return !current.failed && target.Matches(current.goal) && current.goal.Status == "usageLimited" &&
			p.statuses[id] != sessionRuntimeWorking && !p.commandHumanWaitLocked(id) && stopStillCurrent()
	}
	p.mu.Lock()
	ready := p.connection == conn && p.commandGeneration == generation && readyLocked()
	p.mu.Unlock()
	if !ready || ctx.Err() != nil {
		return ErrScheduledGoalNotReady
	}
	err = p.executeGoalCommand(ctx, conn, id, SessionCommandChoice{
		method: "thread/goal/set",
		params: map[string]any{"threadId": id, "origin": "user", "status": "active"},
	}, target, &generation, readyLocked)
	if errors.Is(err, errDaemonWriteGuardRejected) {
		return ErrScheduledGoalNotReady
	}
	return err
}

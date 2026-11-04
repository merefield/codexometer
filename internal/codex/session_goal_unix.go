//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const goalRefreshEvery = 30 * time.Second

type goalObservation struct {
	goal   *SessionGoal
	at     time.Time
	failed bool
}

func goalIdentity(goal *SessionGoal) string {
	if goal == nil {
		return commandDigest(nil)
	}
	return commandDigest([]any{goal.ThreadID, goal.Objective, goal.Status, goal.TokenBudget, goal.CreatedAt, goal.Stale})
}

func validateSessionGoal(goal *SessionGoal, id string) error {
	if goal == nil {
		return nil
	}
	if goal.ThreadID != id || !utf8.ValidString(goal.Objective) || utf8.RuneCountInString(goal.Objective) > SessionGoalInputLimit ||
		goal.Objective == "" || goal.TokensUsed < 0 || goal.TimeUsedSeconds < 0 || goal.TokenBudget != nil && *goal.TokenBudget <= 0 {
		return errors.New("goal observation unavailable or invalid")
	}
	return nil
}

func (p *daemonStatusProvider) observeGoalLocked(id string, goal *SessionGoal, at time.Time) {
	if p.goals == nil {
		p.goals = make(map[string]goalObservation)
		p.goalVersions = make(map[string]uint64)
	}
	if _, known := p.goals[id]; !known && len(p.goals) >= 256 {
		return
	}
	p.goals[id] = goalObservation{goal: goal.Clone(), at: at}
	p.goalVersions[id]++
}

// Poll snapshots to cover startup/missed notifications; normal changes arrive
// on the existing daemon stream. Never persist or infer goal state locally.
func (p *daemonStatusProvider) readSessionGoal(ctx context.Context, id string) (*SessionGoal, error) {
	p.mu.Lock()
	conn, version, generation := p.connection, p.goalVersions[id], p.goalGeneration
	unsupported := p.goalsUnsupported
	p.mu.Unlock()
	if conn == nil || unsupported {
		return nil, errors.New("goals require a connected app-server with goal support")
	}
	var response struct{ Goal json.RawMessage }
	err := p.requestOn(ctx, conn, "thread/goal/get", map[string]any{"threadId": id}, &response)
	if err != nil {
		p.mu.Lock()
		if p.connection == conn {
			var rpc *daemonResponseError
			if errors.As(err, &rpc) {
				var wire struct{ Code int }
				if json.Unmarshal([]byte(rpc.payload), &wire) == nil && wire.Code == -32601 {
					p.goalsUnsupported = true
				}
			}
		}
		p.mu.Unlock()
		return nil, err
	}
	if len(response.Goal) == 0 {
		return nil, errors.New("goal response unavailable")
	}
	var goal *SessionGoal
	if json.Unmarshal(response.Goal, &goal) != nil || validateSessionGoal(goal, id) != nil {
		return nil, errors.New("goal response invalid")
	}
	if goal != nil {
		goal.Stale = false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.connection != conn || p.goalGeneration != generation {
		return nil, errors.New("goal connection changed")
	}
	if p.goalVersions[id] != version {
		current := p.goals[id]
		if current.failed {
			return nil, errors.New("newer goal observation unavailable")
		}
		return current.goal.Clone(), nil // A newer successful observation wins.
	}
	p.observeGoalLocked(id, goal, time.Now())
	return goal.Clone(), nil
}

func (p *daemonStatusProvider) refreshSessionGoals(ctx context.Context, ids []string) {
	for _, id := range ids {
		p.mu.Lock()
		observation, known := p.goals[id]
		conn, version := p.connection, p.goalVersions[id]
		_, subscribed := p.subscribed[id]
		refresh := subscribed && !p.goalsUnsupported && (!known || time.Since(observation.at) >= goalRefreshEvery)
		p.mu.Unlock()
		if !refresh || ctx.Err() != nil {
			continue
		}
		if _, err := p.readSessionGoal(ctx, id); err != nil {
			p.mu.Lock()
			// Back off failed reads too; preserve the last value as stale, never
			// substitute "no goal" or claim pursuit after an unavailable read.
			if current := p.goals[id]; p.connection == conn && p.goalVersions[id] == version && current.at == observation.at {
				goal := current.goal.Clone()
				if goal != nil {
					goal.Stale = true
				}
				p.observeGoalLocked(id, goal, time.Now())
				if failed, stored := p.goals[id]; stored {
					failed.failed = true
					p.goals[id] = failed
				}
			}
			p.mu.Unlock()
		}
	}
}

func (p *daemonStatusProvider) goalNotificationLocked(method string, raw json.RawMessage) {
	if method != "thread/goal/updated" && method != "thread/goal/cleared" {
		return
	}
	var event struct {
		ThreadID string       `json:"threadId"`
		Goal     *SessionGoal `json:"goal"`
	}
	if json.Unmarshal(raw, &event) != nil || event.ThreadID == "" {
		return
	}
	if _, subscribed := p.subscribed[event.ThreadID]; !subscribed {
		return
	}
	if method == "thread/goal/updated" && (event.Goal == nil || validateSessionGoal(event.Goal, event.ThreadID) != nil) {
		return
	}
	if method == "thread/goal/cleared" {
		event.Goal = nil
	} else {
		event.Goal.Stale = false
	}
	p.observeGoalLocked(event.ThreadID, event.Goal, time.Now())
}

func goalCommandSummary(goal *SessionGoal) string {
	if goal == nil {
		return "No goal is set for this session. Set an objective to let Codex pursue it across turns."
	}
	return fmt.Sprintf("%s\nObjective: %s\n%s", goal.Label(), SanitizeSessionContext(goal.Objective), goal.Usage())
}

func goalCommandPath(path string) string {
	input, found := strings.CutPrefix(path, "goal ")
	if !found {
		return path
	}
	input = strings.TrimSpace(input)
	for _, action := range []string{"edit", "budget"} {
		if value, found := strings.CutPrefix(input, action+" "); found {
			return "goal/" + action + "/" + url.PathEscape(strings.TrimSpace(value))
		}
	}
	switch strings.ToLower(input) {
	case "pause", "resume", "clear", "edit", "budget":
		return "goal/" + strings.ToLower(input)
	}
	return "goal/set/" + url.PathEscape(input)
}

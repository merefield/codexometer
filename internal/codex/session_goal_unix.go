//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	goalRefreshEvery   = 30 * time.Second
	goalRefreshBudget  = 500 * time.Millisecond
	goalRefreshWorkers = 4
)

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
	if ctx.Err() != nil {
		return
	}
	type refresh struct {
		id      string
		at      time.Time
		version uint64
	}
	p.mu.Lock()
	conn, generation := p.connection, p.goalGeneration
	if conn == nil || p.goalsUnsupported {
		p.mu.Unlock()
		return
	}
	var due []refresh
	for _, id := range ids {
		observation, known := p.goals[id]
		_, subscribed := p.subscribed[id]
		if subscribed && (!known || time.Since(observation.at) >= goalRefreshEvery) {
			due = append(due, refresh{id, observation.at, p.goalVersions[id]})
		}
	}
	p.mu.Unlock()
	if len(due) == 0 {
		return
	}
	// Goal snapshots are optional telemetry: one shared budget, not a fresh
	// timeout per session. Oldest/unattempted reads go first so a stalled batch
	// cannot repeatedly starve the same sessions on later polls.
	slices.SortStableFunc(due, func(a, b refresh) int { return a.at.Compare(b.at) })
	ctx, cancel := context.WithTimeout(ctx, goalRefreshBudget)
	defer cancel()
	failed := func(job refresh, at time.Time) {
		p.mu.Lock()
		defer p.mu.Unlock()
		current := p.goals[job.id]
		_, subscribed := p.subscribed[job.id]
		if !subscribed || p.connection != conn || p.goalGeneration != generation || p.goalVersions[job.id] != job.version || current.at != job.at {
			return // Never overwrite a newer observation, clear or reconnect.
		}
		goal := current.goal.Clone()
		if goal != nil {
			goal.Stale = true
		}
		p.observeGoalLocked(job.id, goal, at)
		if observation, stored := p.goals[job.id]; stored {
			observation.failed = true
			p.goals[job.id] = observation
		}
	}
	jobs := make(chan refresh, len(due))
	for _, job := range due {
		jobs <- job
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(goalRefreshWorkers, len(due)) {
		workers.Go(func() {
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				if _, err := p.readSessionGoal(ctx, job.id); err != nil {
					failed(job, time.Now()) // Back off attempted failures too.
				}
			}
		})
	}
	workers.Wait()
	for _, job := range due {
		// A deferred read is stale, not "no goal". Preserve its old timestamp
		// so it stays ahead of attempted failures at the next refresh.
		failed(job, job.at)
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
		return "No goal set."
	}
	return fmt.Sprintf("%s // %s\nOBJECTIVE // %s", goal.Label(), goal.Usage(), SanitizeSessionContext(strings.Join(strings.Fields(goal.Objective), " ")))
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

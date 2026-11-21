package codex

import (
	"context"
	"slices"
	"strings"
	"time"
)

type SessionAgent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type AgentInspection struct {
	Agents          []SessionAgent `json:"agents,omitempty"`
	Turns           []SavedTurn    `json:"turns,omitempty"`
	UnverifiedTurns bool           `json:"unverifiedTurns,omitempty"`
}

type SessionAgentClient interface {
	InspectSessionAgents(ctx context.Context, parent, child string) (AgentInspection, error)
}

type agentHistoryProvider interface {
	readSessionHistory(context.Context, string, time.Time) ([]SessionContext, bool, error)
}

func (r *LiveUsageReader) linkedAgents(parent, child string) ([]SessionAgent, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byID := map[string]*rolloutCursor{}
	for _, c := range r.files {
		if prev := byID[c.threadID]; c.threadID != "" && (prev == nil || c.activityTime().After(prev.activityTime())) {
			byID[c.threadID] = c
		}
	}
	root := byID[parent]
	if root == nil || root.nonRoot || root.parentThreadID != "" {
		return nil, time.Time{}
	}
	var children []SessionAgent
	var ownedSince time.Time
	now := r.activityNow()
	for id, c := range byID {
		if c.parentThreadID == "" || c.guardianReviewer || now.Sub(c.activityTime()) > activeRolloutHorizon && c.observedTokens == 0 {
			continue
		}
		if idRoot, unlinked := rolloutRoot(c, byID); unlinked || idRoot != parent {
			continue
		}
		if id == child {
			ownedSince = c.startedAt
		}
		status, exact := r.daemonRuntime[id]
		attention := sessionAttention(c.attention, false, false, now.Sub(c.activityTime()), status, exact)
		active := exact && status != sessionRuntimeUnknown || now.Sub(c.activityTime()) < fallbackAttentionAfter
		name := c.agentName
		if name == "" {
			name = r.sessionNames[id]
		}
		children = append(children, SessionAgent{id, singleLine(name), SessionStatus(active, status == sessionRuntimeWorking, attention, r.daemonContexts[id])})
	}
	slices.SortFunc(children, func(a, b SessionAgent) int {
		if byName := strings.Compare(a.Name, b.Name); byName != 0 {
			return byName
		}
		return strings.Compare(a.ID, b.ID)
	})
	return children[:min(len(children), 64)], ownedSince
}

func (c Client) InspectSessionAgents(ctx context.Context, parent, child string) (AgentInspection, error) {
	if c.LiveUsage == nil || parent == "" || len(parent) > 512 || len(child) > 512 {
		return AgentInspection{}, ErrSessionHistory
	}
	rows, since := c.LiveUsage.linkedAgents(parent, child)
	if child == "" {
		return AgentInspection{Agents: rows}, nil
	}
	p, ok := c.LiveUsage.statusProvider.(agentHistoryProvider)
	if !ok || since.IsZero() {
		return AgentInspection{}, ErrSessionHistory
	}
	turns, unverified, err := p.readSessionHistory(ctx, child, since)
	if err == nil {
		c.LiveUsage.protectSessionHistory(child, turns)
	}
	return AgentInspection{Turns: savedTurns(turns), UnverifiedTurns: unverified}, err
}

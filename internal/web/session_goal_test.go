package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestSessionGoalProjectionIsIsolatedSanitizedAndReadOnly(t *testing.T) {
	s := newStore()
	budget := int64(500)
	observed := &codex.SessionGoal{ThreadID: "private-thread-capability", Status: "active", Objective: "\x1b[31mFinish\x1b[0m\x07 the task", TokenBudget: &budget, TokensUsed: 42, TimeUsedSeconds: 3}
	rows := []codex.LiveUsageSession{{ID: "one", Goal: observed}, {ID: "two"}}
	s.live(codex.LiveUsageSnapshot{Sessions: rows}, nil, time.Now())
	g := s.state.Sessions[0].Goal
	if g == nil || g.Label != "PURSUING GOAL" || g.Objective != "Finish the task" || *g.TokenBudget != 500 || g.TokensUsed != 42 || s.state.Sessions[1].Goal != nil {
		t.Fatal("goal projection missing, unsanitized or attached to another session", g)
	}
	budget = 600
	if *g.TokenBudget != 500 {
		t.Fatal("projected goal aliased source state")
	}
	data, _ := json.Marshal(s.state)
	if strings.Contains(string(data), "private-thread-capability") || s.state.Control {
		t.Fatal("read-only telemetry leaked capabilities or enabled writes")
	}
	rows[0].Goal = nil
	s.live(codex.LiveUsageSnapshot{Sessions: rows}, nil, time.Now())
	if s.state.Sessions[0].Goal != nil {
		t.Fatal("cleared goal persisted in web state")
	}
}

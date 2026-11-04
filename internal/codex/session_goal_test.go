package codex

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSessionGoalLabelsAndCloning(t *testing.T) {
	for status, label := range map[string]string{"active": "PURSUING GOAL", "blocked": "GOAL STALLED", "paused": "GOAL PAUSED", "usageLimited": "GOAL USAGE LIMITED", "budgetLimited": "GOAL BUDGET LIMITED", "complete": "GOAL COMPLETE", "future": "GOAL STATUS UNKNOWN"} {
		g := &SessionGoal{Status: status}
		if g.Label() != label {
			t.Fatal(status, g.Label())
		}
		g.Stale = true
		if g.Label() != "GOAL STALE" {
			t.Fatal("stale observation advertised live goal state")
		}
	}
	budget := int64(100)
	g := &SessionGoal{TokenBudget: &budget, TokensUsed: 4}
	clone := g.Clone()
	*clone.TokenBudget = 999
	if *g.TokenBudget != 100 || g.Usage() != "4 / 100 TOKENS" || (*SessionGoal)(nil).Clone() != nil {
		t.Fatal("goal clone aliased its budget or lost usage")
	}
}

func TestSessionGoalDoesNotMergeAcrossLinkedAgents(t *testing.T) {
	now := time.Now()
	r := &LiveUsageReader{files: map[string]*rolloutCursor{
		"root":  {threadID: "root", lastModified: now},
		"child": {threadID: "child", parentThreadID: "root", nonRoot: true, lastModified: now},
	}, daemonGoals: map[string]*SessionGoal{
		"child": {ThreadID: "child", Status: "active", Objective: "Child objective"},
	}}
	sessions, _, _ := r.sessionSnapshots(now, nil, false, nil)
	if len(sessions) != 1 || sessions[0].Goal != nil {
		t.Fatal("child goal leaked into the grouped root")
	}
	r.daemonGoals["root"] = &SessionGoal{ThreadID: "root", Status: "paused", Objective: "Root objective"}
	sessions, _, _ = r.sessionSnapshots(now, nil, false, nil)
	if sessions[0].Goal == nil || sessions[0].Goal.Objective != "Root objective" {
		t.Fatal("root lost its own goal")
	}
	sessions[0].Goal.Objective = "Changed projection"
	if r.daemonGoals["root"].Objective != "Root objective" {
		t.Fatal("session projection aliased the goal cache")
	}
}

func TestGoalCommandInputBounds(t *testing.T) {
	for _, parent := range []string{"goal/set", "goal/edit"} {
		value := strings.Repeat("界", SessionGoalInputLimit)
		path := parent + "/" + url.PathEscape(value)
		if !ValidSessionCommandPath(path) {
			t.Fatal("Unicode goal at the input limit rejected")
		}
		p, v, ok := SessionCommandInputValue(path)
		if !ok || p != parent || v != value || !SessionCommandInputPath(parent) {
			t.Fatal("goal input path did not round-trip")
		}
		if ValidSessionCommandPath(parent + "/" + url.PathEscape(value+"x")) {
			t.Fatal("oversized goal accepted")
		}
	}
	if !ValidSessionCommandPath("/goal "+strings.Repeat("😀", SessionGoalInputLimit)) || ValidSessionCommandPath("goal/edit/%zz") {
		t.Fatal("direct/encoded goal input bounds differ")
	}
}

func TestGoalReviewsAreNotCachedAsCatalogues(t *testing.T) {
	for _, path := range []string{"goal/set/Objective", "goal/edit/Objective", "goal/budget/1000", "goal An objective", "goal pause"} {
		if cacheableCommandPath(path) {
			t.Fatal("goal input/review cached as a catalogue branch:", path)
		}
	}
	for _, path := range []string{"goal", "goal/edit", "goal/budget", "goal/pause"} {
		if !cacheableCommandPath(path) {
			t.Fatal("goal menu lost normal in-memory browsing:", path)
		}
	}
}

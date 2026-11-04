//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func goalFixture(t *testing.T) (*daemonStatusProvider, *quotaDaemonFixture) {
	t.Helper()
	p, f := newQuotaDaemon(t)
	budget := int64(10000)
	f.goals = map[string]*SessionGoal{
		"one": {ThreadID: "one", Objective: "Fix the flaky tests", Status: "active", TokenBudget: &budget, TokensUsed: 200, CreatedAt: 1},
		"two": {ThreadID: "two", Objective: "Other session objective", Status: "paused", CreatedAt: 2},
	}
	if _, ok := p.Fetch(context.Background(), []string{"one", "two"}); !ok {
		t.Fatal("goal fixture did not connect")
	}
	return p, f
}

func applyGoalCommand(t *testing.T, p *daemonStatusProvider, path string) {
	t.Helper()
	menu, err := p.SessionCommands(context.Background(), "one", path)
	if err != nil || len(menu.Choices) != 1 || !menu.Choices[0].Action || menu.Choices[0].Reversible {
		t.Fatal("goal action unavailable or incorrectly reversible", path, menu, err)
	}
	if err := p.ExecuteSessionCommand(context.Background(), "one", menu.Path, menu.Revision, menu.Choices[0].ID); err != nil {
		t.Fatal("goal mutation failed", path, err)
	}
}

func TestGoalCommandsLifecycleAndSessionScope(t *testing.T) {
	p, f := goalFixture(t)
	root, err := p.SessionCommands(context.Background(), "one", "goal")
	if err != nil || !strings.Contains(root.Help, "200 / 10000") || len(root.Choices) < 5 {
		t.Fatal("goal summary/options missing", root, err)
	}
	applyGoalCommand(t, p, "goal pause")
	edit, err := p.SessionCommands(context.Background(), "one", "goal edit")
	if err != nil || !edit.Input || edit.Value != "Fix the flaky tests" || edit.ReversibleInput {
		t.Fatal("goal edit did not retain the objective/confirmation")
	}
	applyGoalCommand(t, p, "goal/edit/"+url.PathEscape("Preserve spaces / Unicode 界"))
	if f.goals["one"].Status != "paused" {
		t.Fatal("editing a paused goal resumed it")
	}
	applyGoalCommand(t, p, "goal budget 20000")
	applyGoalCommand(t, p, "goal resume")
	applyGoalCommand(t, p, "goal/budget/unlimited")
	if f.goals["one"].Objective != "Preserve spaces / Unicode 界" || f.goals["one"].Status != "active" || f.goals["one"].TokenBudget != nil {
		t.Fatal("goal state/budget wrong", f.goals["one"])
	}
	applyGoalCommand(t, p, "goal clear")
	if p.statusSnapshotLocked([]string{"one"}).Goals["one"] != nil {
		t.Fatal("cleared goal remained visible")
	}
	f.commandStatus = "active"
	applyGoalCommand(t, p, "goal New objective while a turn is running")
	if f.goals["one"].Objective != "New objective while a turn is running" || f.goals["two"].Objective != "Other session objective" {
		t.Fatal("goal update lost its target")
	}
	for i, write := range f.goalWrites {
		if write["threadId"] != "one" || write["origin"] != "user" || !strings.HasPrefix(f.goalMethods[i], "thread/goal/") {
			t.Fatal("goal changed another session or omitted user provenance", write)
		}
	}
}

func TestGoalMenuIsCompactAndDistinguishesCreateEditReplace(t *testing.T) {
	p, f := goalFixture(t)
	menu, err := p.SessionCommands(context.Background(), "one", "goal")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(menu.Help, "Fix the flaky tests") != 1 || len(strings.Split(menu.Help, "\n")) != 2 {
		t.Fatal("goal overview is repeated or not compact", menu.Help)
	}
	labels := map[string]string{}
	for _, choice := range menu.Choices {
		if choice.Next == "" || strings.Contains(choice.Help, "Fix the flaky tests") {
			t.Fatal("overview repeated as a selectable choice", choice)
		}
		labels[choice.Label] = choice.Next
	}
	if labels["Edit objective"] != "goal/edit" || labels["Replace goal"] != "goal/set" || labels["Create goal"] != "" {
		t.Fatal("existing goal has ambiguous create/edit choices", labels)
	}
	editor, err := p.SessionCommands(context.Background(), "one", "goal/edit")
	if err != nil || editor.Value != "Fix the flaky tests" || strings.Contains(editor.Help, editor.Value) {
		t.Fatal("editor repeated the objective outside the input", editor, err)
	}
	for _, path := range []string{"goal/pause", "goal/resume", "goal/clear", "goal/edit/New", "goal/budget/1000"} {
		menu, err := p.SessionCommands(context.Background(), "one", path)
		if err != nil || len(menu.Choices) != 1 || !menu.Choices[0].Action || menu.Choices[0].Reversible || strings.Contains(menu.Choices[0].Help, "Fix the flaky tests") {
			t.Fatal("shortened review duplicated current context or weakened confirmation", path, menu, err)
		}
		if path == "goal/resume" && !strings.Contains(menu.Choices[0].Help, "consume quota") {
			t.Fatal("resume lost its quota warning")
		}
	}
	f.mu.Lock()
	delete(f.goals, "one")
	f.mu.Unlock()
	menu, err = p.SessionCommands(context.Background(), "one", "goal")
	if err != nil || menu.Help != "No goal set." || len(menu.Choices) != 1 || menu.Choices[0].Label != "Create goal" {
		t.Fatal("empty goal offered replace/edit or repeated instructions", menu, err)
	}
}

func TestGoalReviewIgnoresProgressButRejectsSemanticDrift(t *testing.T) {
	for _, drift := range []string{"progress", "objective", "status", "budget"} {
		t.Run(drift, func(t *testing.T) {
			p, f := goalFixture(t)
			menu, err := p.SessionCommands(context.Background(), "one", "goal/pause")
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			g := f.goals["one"]
			switch drift {
			case "progress":
				g.TokensUsed, g.TimeUsedSeconds, g.UpdatedAt = 999, 100, 42
				f.commandStatus = "active"
			case "objective":
				g.Objective = "Different objective"
			case "status":
				g.Status = "blocked"
			case "budget":
				budget := int64(20000)
				g.TokenBudget = &budget
			}
			f.mu.Unlock()
			err = p.ExecuteSessionCommand(context.Background(), "one", menu.Path, menu.Revision, menu.Choices[0].ID)
			wantWrites := 0
			if drift == "progress" {
				wantWrites = 1
			}
			if (err == nil) != (drift == "progress") || len(f.goalWrites) != wantWrites {
				t.Fatal("incorrect goal revision/choice validation", drift, err, f.goalWrites)
			}
		})
	}
}

func TestGoalWritesRequireVerifiedEffectAndNeverRetry(t *testing.T) {
	p, f := goalFixture(t)
	menu, _ := p.SessionCommands(context.Background(), "one", "goal/pause")
	f.ignoreGoal = true
	if err := p.ExecuteSessionCommand(context.Background(), "one", menu.Path, menu.Revision, menu.Choices[0].ID); err == nil || len(f.goalWrites) != 1 {
		t.Fatal("ignored goal update was reported successful or retried")
	}
	f.goalReadError = map[string]any{"code": -1, "message": "unavailable"}
	if err := p.ExecuteSessionCommand(context.Background(), "one", menu.Path, menu.Revision, menu.Choices[0].ID); err == nil || len(f.goalWrites) != 1 {
		t.Fatal("unavailable state allowed another write")
	}
}

func TestGoalObservationNotificationsStaleAndClear(t *testing.T) {
	p, f := goalFixture(t)
	before := p.statusSnapshotLocked([]string{"one", "two"})
	f.mu.Lock()
	f.goalReadHook = func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		raw, _ := json.Marshal(map[string]any{"threadId": "one", "goal": &SessionGoal{ThreadID: "one", Objective: "Newer streamed objective", Status: "blocked"}})
		p.goalNotificationLocked("thread/goal/updated", raw)
	}
	f.mu.Unlock()
	g, err := p.readSessionGoal(context.Background(), "one")
	if err != nil || g.Label() != "GOAL STALLED" || g.Objective != "Newer streamed objective" || before.Goals["one"].Status != "active" {
		t.Fatal("older read displaced stream state or snapshots aliased", g, err)
	}
	f.mu.Lock()
	f.goalReadHook = nil
	f.goalReadError = map[string]any{"code": -1, "message": "temporary read failure"}
	f.mu.Unlock()
	p.mu.Lock()
	obs := p.goals["one"]
	obs.at = time.Now().Add(-time.Minute)
	p.goals["one"] = obs
	p.mu.Unlock()
	p.refreshSessionGoals(context.Background(), []string{"one"})
	if goal := p.statusSnapshotLocked([]string{"one"}).Goals["one"]; goal == nil || !goal.Stale || goal.Label() != "GOAL STALE" {
		t.Fatal("failed goal read did not retain a stale observation")
	}
	p.mu.Lock()
	p.goalNotificationLocked("thread/goal/cleared", json.RawMessage(`{"threadId":"one"}`))
	p.mu.Unlock()
	if snapshot := p.statusSnapshotLocked([]string{"one", "two"}); snapshot.Goals["one"] != nil || snapshot.Goals["two"] == nil {
		t.Fatal("clear affected the wrong session")
	}
}

func TestGoalReadCannotResurrectClearedOrClosedSession(t *testing.T) {
	for _, closed := range []bool{false, true} {
		p, f := goalFixture(t)
		f.mu.Lock()
		f.goalReadHook = func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if closed {
				p.promptLifecycleLocked("thread/closed", json.RawMessage(`{"threadId":"one"}`))
			} else {
				p.goalNotificationLocked("thread/goal/cleared", json.RawMessage(`{"threadId":"one"}`))
			}
		}
		f.mu.Unlock()
		g, err := p.readSessionGoal(context.Background(), "one")
		if g != nil || closed && err == nil || p.statusSnapshotLocked([]string{"one"}).Goals["one"] != nil {
			t.Fatal("late goal read resurrected a cleared/closed goal", closed, g, err)
		}
	}
}

func TestGoalConcurrentFailedObservationCannotVerifyChange(t *testing.T) {
	for _, existing := range []bool{false, true} {
		p, f := goalFixture(t)
		f.mu.Lock()
		f.goalReadHook = func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			var retained *SessionGoal
			if existing {
				retained = p.goals["one"].goal.Clone()
				retained.Stale = true
			}
			p.observeGoalLocked("one", retained, time.Now())
			observation := p.goals["one"]
			observation.failed = true
			p.goals["one"] = observation
		}
		f.mu.Unlock()
		if _, err := p.readSessionGoal(context.Background(), "one"); err == nil {
			t.Fatal("an overlapping failed refresh was treated as verified goal state", existing)
		}
	}
}

func TestGoalUnsupportedServerAndInvalidInputs(t *testing.T) {
	p, f := newQuotaDaemon(t)
	p.Fetch(context.Background(), []string{"one", "two"})
	if f.goalReads != 1 || !p.goalsUnsupported || len(p.statusSnapshotLocked([]string{"one"}).Goals) != 0 {
		t.Fatal("unsupported goal API was not gracefully suppressed", f.goalReads)
	}
	p, f = goalFixture(t)
	for _, path := range []string{"goal/budget/0", "goal/budget/-1", "goal/budget/1.5", "goal/budget/9007199254740992", "goal/edit/%zz", "goal/set/%1b%5b31munsafe"} {
		if _, err := p.SessionCommands(context.Background(), "one", path); err == nil || len(f.goalWrites) != 0 {
			t.Fatal("invalid goal input accepted", path)
		}
	}
}

func TestGoalMaximumObjectiveHasCompleteActionableReview(t *testing.T) {
	p, _ := goalFixture(t)
	objective := strings.Repeat("界", SessionGoalInputLimit)
	menu, err := p.SessionCommands(context.Background(), "one", "goal/set/"+url.PathEscape(objective))
	if err != nil || len(menu.Choices) != 1 || !menu.Choices[0].Action || strings.Contains(menu.Help, objective) || !strings.Contains(menu.Choices[0].Help, objective) {
		t.Fatal("maximum objective was clipped or lost its guarded action", err)
	}
}

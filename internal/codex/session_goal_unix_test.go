//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"fmt"
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
	if f.goalReads < 1 || f.goalReads > 2 || !p.goalsUnsupported || len(p.statusSnapshotLocked([]string{"one"}).Goals) != 0 {
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

func TestGoalRefreshSharesBudgetAcross256StalledSessions(t *testing.T) {
	p, f := goalFixture(t)
	ids := []string{"one", "two"}
	for len(ids) < 256 {
		ids = append(ids, fmt.Sprintf("stalled-%03d", len(ids)))
	}
	past := time.Now().Add(-time.Minute)
	p.mu.Lock()
	for _, id := range ids {
		p.subscribed[id] = struct{}{}
		p.observeGoalLocked(id, &SessionGoal{ThreadID: id, Status: "active", Objective: "Retain this objective"}, past)
	}
	p.mu.Unlock()
	f.mu.Lock()
	before := f.goalReads
	f.goalNoReply = make(map[string]bool, len(ids))
	for _, id := range ids {
		f.goalNoReply[id] = true
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	p.refreshSessionGoals(ctx, ids)
	if elapsed := time.Since(started); ctx.Err() != nil || elapsed > goalRefreshBudget+time.Second {
		t.Fatalf("goal reads used per-session timeouts: elapsed %v, parent %v", elapsed, ctx.Err())
	}
	f.mu.Lock()
	reads := f.goalReads - before
	f.mu.Unlock()
	if reads != goalRefreshWorkers {
		t.Fatalf("stalled requests were not worker-bounded: got %d, want %d", reads, goalRefreshWorkers)
	}
	p.mu.Lock()
	backedOff := 0
	for _, id := range ids {
		observation := p.goals[id]
		if observation.goal == nil || observation.goal.Objective != "Retain this objective" || !observation.goal.Stale || !observation.failed {
			t.Errorf("failed/deferred goal %s was dropped or claimed fresh", id)
		}
		if observation.at.After(past) {
			backedOff++
		}
	}
	pending := len(p.pending)
	p.mu.Unlock()
	if pending != 0 || backedOff != goalRefreshWorkers {
		t.Fatal("worker requests leaked or deferred reads incorrectly backed off", pending, backedOff)
	}
	var status struct{ Thread struct{ ID string } }
	if err := p.request(ctx, "thread/read", map[string]any{"threadId": "one"}, &status); err != nil || status.Thread.ID != "one" {
		t.Fatal("stalled goal endpoint blocked ordinary telemetry", err)
	}
}

func TestGoalRefreshReadsHealthyBatchAndSkipsFresh(t *testing.T) {
	p, f := goalFixture(t)
	var ids []string
	f.mu.Lock()
	before := f.goalReads
	for i := range goalRefreshWorkers * 2 {
		id := fmt.Sprintf("healthy-%d", i)
		ids = append(ids, id)
		f.goals[id] = &SessionGoal{ThreadID: id, Objective: id, Status: "paused"}
	}
	f.mu.Unlock()
	p.mu.Lock()
	for _, id := range ids {
		p.subscribed[id] = struct{}{}
	}
	p.mu.Unlock()
	p.refreshSessionGoals(context.Background(), ids)
	p.mu.Lock()
	for _, id := range ids {
		goal := p.goals[id].goal
		if goal == nil || goal.Stale || goal.Objective != id {
			t.Errorf("worker pool lost healthy goal %s", id)
		}
	}
	p.mu.Unlock()
	p.refreshSessionGoals(context.Background(), ids)
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.goalReads - before; got != len(ids) {
		t.Fatal("healthy batch was duplicated or fresh observations were repolled", got)
	}
}

func TestGoalRefreshCollectsCoreStatusBeforeOptionalGoals(t *testing.T) {
	p, f := goalFixture(t)
	p.mu.Lock()
	p.lastStatusAt = time.Time{}
	for id, observation := range p.goals {
		observation.at = time.Now().Add(-time.Minute)
		p.goals[id] = observation
	}
	p.mu.Unlock()
	f.mu.Lock()
	f.commandStatus = "active"
	f.goalNoReply = map[string]bool{"one": true, "two": true}
	f.threadReads = 0
	coreBeforeGoals := true
	f.goalReadHook = func() { coreBeforeGoals = coreBeforeGoals && f.threadReads == 2 }
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	snapshot, ok := p.Fetch(ctx, []string{"one", "two"})
	if !ok || ctx.Err() != nil || snapshot.Statuses["one"] != sessionRuntimeWorking || snapshot.Statuses["two"] != sessionRuntimeWorking {
		t.Fatal("optional goal failure delayed/lost core status", snapshot.Statuses, ctx.Err())
	}
	f.mu.Lock()
	readFirst := coreBeforeGoals
	f.mu.Unlock()
	if !readFirst {
		t.Fatal("optional goal reads preceded core runtime status")
	}
	for _, id := range []string{"one", "two"} {
		if goal := snapshot.Goals[id]; goal == nil || !goal.Stale {
			t.Fatal("stalled goal was lost or claimed fresh state", id)
		}
	}
}

func TestGoalRefreshKeepsNewerRuntimeSignals(t *testing.T) {
	p, f := goalFixture(t)
	p.mu.Lock()
	p.lastStatusAt = time.Time{}
	observation := p.goals["one"]
	observation.at = time.Now().Add(-time.Minute)
	p.goals["one"] = observation
	p.mu.Unlock()
	f.mu.Lock()
	f.commandStatus = "active"
	f.goalReadHook = func() {
		// Same mutex/state updated by the daemon notification reader. This
		// signal arrives after the core thread/read, during optional polling.
		p.mu.Lock()
		p.statuses["one"] = sessionRuntimeApproval
		p.mu.Unlock()
	}
	f.mu.Unlock()
	snapshot, ok := p.Fetch(context.Background(), []string{"one", "two"})
	if !ok || snapshot.Statuses["one"] != sessionRuntimeApproval {
		t.Fatal("older core read overwrote a runtime signal received during goal refresh")
	}
}

func TestGoalRefreshDeferredReadsKeepTheirPlace(t *testing.T) {
	p, f := goalFixture(t)
	ids := []string{"stall-a", "stall-b", "stall-c", "stall-d", "healthy"}
	past := time.Now().Add(-time.Minute)
	p.mu.Lock()
	for _, id := range ids {
		p.subscribed[id] = struct{}{}
		p.observeGoalLocked(id, &SessionGoal{ThreadID: id, Objective: "Old objective", Status: "active"}, past)
	}
	p.mu.Unlock()
	f.mu.Lock()
	f.goalNoReply = map[string]bool{}
	for _, id := range ids[:goalRefreshWorkers] {
		f.goalNoReply[id] = true
	}
	f.goals["healthy"] = &SessionGoal{ThreadID: "healthy", Objective: "Fresh objective", Status: "complete"}
	f.mu.Unlock()
	p.refreshSessionGoals(context.Background(), ids)
	p.mu.Lock()
	if observation := p.goals["healthy"]; observation.at != past || !observation.goal.Stale {
		p.mu.Unlock()
		t.Fatal("unattempted goal was backed off or claimed current")
	}
	// Simulate the attempted failures becoming due again before the whole
	// backlog has been observed. The oldest/deferred session must go first.
	for _, id := range ids[:goalRefreshWorkers] {
		observation := p.goals[id]
		observation.at = time.Now().Add(-goalRefreshEvery - time.Second)
		p.goals[id] = observation
	}
	p.mu.Unlock()
	p.refreshSessionGoals(context.Background(), ids)
	p.mu.Lock()
	goal := p.goals["healthy"].goal.Clone()
	p.mu.Unlock()
	if goal == nil || goal.Stale || goal.Objective != "Fresh objective" || goal.Status != "complete" {
		t.Fatal("stalled sessions starved the oldest deferred read", goal)
	}
}

func TestGoalRefreshHonoursCancellationAndDoesNotOverwriteStream(t *testing.T) {
	for _, event := range []string{"cancel", "update", "clear", "close", "disconnect"} {
		t.Run(event, func(t *testing.T) {
			p, f := goalFixture(t)
			p.mu.Lock()
			observation := p.goals["one"]
			observation.at = time.Now().Add(-time.Minute)
			p.goals["one"] = observation
			p.mu.Unlock()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.mu.Lock()
			f.goalNoReply = map[string]bool{"one": true}
			f.goalReadHook = func() {
				if event == "disconnect" {
					p.disconnect(nil)
					return
				}
				p.mu.Lock()
				defer p.mu.Unlock()
				switch event {
				case "cancel":
					cancel()
				case "update":
					p.goalNotificationLocked("thread/goal/updated", json.RawMessage(`{"threadId":"one","goal":{"threadId":"one","objective":"Streamed objective","status":"complete"}}`))
				case "clear":
					p.goalNotificationLocked("thread/goal/cleared", json.RawMessage(`{"threadId":"one"}`))
				case "close":
					p.promptLifecycleLocked("thread/closed", json.RawMessage(`{"threadId":"one"}`))
				}
			}
			f.mu.Unlock()
			p.refreshSessionGoals(ctx, []string{"one"})
			p.mu.Lock()
			defer p.mu.Unlock()
			g := p.goals["one"].goal
			if len(p.pending) != 0 {
				t.Fatal("cancelled goal read leaked a pending request")
			}
			if event == "cancel" && (ctx.Err() != context.Canceled || g == nil || !g.Stale) ||
				event == "update" && (g == nil || g.Stale || g.Objective != "Streamed objective") ||
				(event == "clear" || event == "close" || event == "disconnect") && g != nil {
				t.Fatal("deadline fallback overwrote a newer event", event, g)
			}
		})
	}
}

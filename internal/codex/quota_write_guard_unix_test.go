//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type pausedDaemonWrite struct {
	id      int64
	release func()
}

// Called by the fake server before answering the last read. Keep the next
// writer queued until the test has delivered an account change.
func pauseNextDaemonWrite(p *daemonStatusProvider) pausedDaemonWrite {
	p.writeMu.Lock()
	p.mu.Lock()
	defer p.mu.Unlock()
	return pausedDaemonWrite{p.nextRequestID + 1, sync.OnceFunc(p.writeMu.Unlock)}
}

func changeAccountAtQueuedWrite(t *testing.T, p *daemonStatusProvider, paused <-chan pausedDaemonWrite) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	var write pausedDaemonWrite
	select {
	case write = <-paused:
		defer write.release()
	case <-deadline.C:
		t.Fatal("last RPC preflight never reached")
	}
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		p.mu.Lock()
		_, queued := p.pending[write.id]
		p.mu.Unlock()
		if queued {
			p.handleNotification("account/updated", json.RawMessage(`{"reason":null}`))
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("mutation was not waiting at the writer")
		}
	}
}

func TestScheduledPromptAccountChangesAfterLastPreflight(t *testing.T) {
	paused := make(chan pausedDaemonWrite, 1)
	var p *daemonStatusProvider
	p, received := promptProvider(t, "idle", promptTestFault{beforeRead: func() { paused <- pauseNextDaemonWrite(p) }})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token := p.SessionPrompt("root").Token
	account := quotaAccount(fingerprintAccount(json.RawMessage(scheduledPromptAccount)), "original")
	done := make(chan error, 1)
	go func() { done <- p.SendScheduledPrompt(ctx, token, "Continue", account) }()
	changeAccountAtQueuedWrite(t, p, paused)
	if err := <-done; !errors.Is(err, ErrScheduledQuotaUnverified) {
		t.Fatal("missing no-send preflight outcome", err)
	}
	for len(received) != 0 {
		if (<-received).Method == "turn/start" {
			t.Fatal("scheduled prompt escaped the account guard")
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if state := p.contexts["root"]; p.connection == nil || len(p.pending) != 0 || state == nil || state.promptSpent || state.promptDispatched {
		t.Fatal("guard rejection leaked a reservation or disconnected the daemon")
	}
}

func TestScheduledGoalAccountChangesAfterLastPreflight(t *testing.T) {
	p, f := goalFixture(t)
	paused := make(chan pausedDaemonWrite, 1)
	var accountReply map[string]any
	if err := json.Unmarshal([]byte(scheduledPromptAccount), &accountReply); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.goals["one"].Status = "usageLimited"
	target := NewGoalResumeTarget(f.goals["one"])
	f.catalogues = map[string]map[string]any{"account/read": accountReply}
	f.goalReadHook = func() { paused <- pauseNextDaemonWrite(p) }
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	account := quotaAccount(fingerprintAccount(json.RawMessage(scheduledPromptAccount)), "original")
	done := make(chan error, 1)
	go func() { done <- p.ResumeScheduledGoal(ctx, "one", account, target) }()
	changeAccountAtQueuedWrite(t, p, paused)
	if err := <-done; !errors.Is(err, ErrScheduledGoalNotReady) {
		t.Fatal("missing no-send preflight outcome", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.goalWrites) != 0 || f.goals["one"].Status != "usageLimited" {
		t.Fatal("scheduled goal resume escaped the account guard")
	}
}

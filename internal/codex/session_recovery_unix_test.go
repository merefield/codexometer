//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

func TestFailedSessionCanRecoverWithExplicitFollowup(t *testing.T) {
	for _, code := range []string{"serverOverloaded", "modelNotFound", "policyCapacityUnavailable", "httpConnectionFailed", "usageLimitExceeded", "contextWindowExceeded", "unauthorized", "internalServerError"} {
		for _, completed := range []bool{false, true} {
			name := code + "/error-only"
			if completed {
				name = code + "/completed"
			}
			t.Run(name, func(t *testing.T) {
				p, received := promptProvider(t, "systemError")
				emit := func(method string, raw json.RawMessage) {
					p.mu.Lock()
					defer p.mu.Unlock()
					daemonContextEvent(p.contexts, method, nil, raw, time.Now())
					p.promptLifecycleLocked(method, raw)
				}
				emit("turn/started", json.RawMessage(`{"threadId":"root","turn":{"id":"failed-turn"}}`))
				active := p.SessionTurn("root")
				if active.Token == "" {
					t.Fatal("test did not establish an active turn")
				}
				p.contexts["root"].promptSpent = true
				errJSON, _ := json.Marshal(map[string]string{"message": "Recovery required", "codexErrorInfo": code})
				raw, _ := json.Marshal(map[string]any{"threadId": "root", "turnId": "failed-turn", "willRetry": false, "error": json.RawMessage(errJSON)})
				emit("error", raw)
				if p.SessionPrompt("root").Token != "" || p.SessionTurn("root").Token != "" {
					t.Fatal("terminal error alone invented idle readiness or allowed steering an ended turn")
				}
				if err := p.SendSessionTurn(context.Background(), active, "steer", "stale"); err == nil {
					t.Fatal("old active-turn offer survived terminal failure")
				}
				if completed {
					emit("turn/completed", json.RawMessage(`{"threadId":"root","turn":{"id":"failed-turn","status":"failed"}}`))
				}
				emit("thread/status/changed", json.RawMessage(`{"threadId":"root","status":{"type":"systemError"}}`))
				o := p.SessionPrompt("root")
				if o.Token == "" || o.TurnID != "" || len(o.Questions) != 0 {
					t.Fatal("stopped failure did not offer an ordinary recovery composer")
				}
				if p.statusSnapshotLocked([]string{"root"}).Statuses["root"] != sessionRuntimeFailed {
					t.Fatal("making the session recoverable lost the failure status")
				}
				if len(received) != 0 {
					t.Fatal("observing a failure automatically sent a request")
				}
				if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"continue"}); err != nil {
					t.Fatal("explicit recovery failed", err)
				}
				read, start := <-received, <-received
				if read.Method != "thread/read" || start.Method != "turn/start" || string(start.Params) != `{"input":[{"text":"continue","type":"text"}],"threadId":"root"}` {
					t.Fatal("recovery did not use a freshly checked, ordinary turn/start", read.Method, start.Method, string(start.Params))
				}
				if p.SendSessionPrompt(context.Background(), o.Token, []string{"again"}) == nil || p.SessionPrompt("root").Token != "" {
					t.Fatal("recovery reused its one-use capability")
				}
			})
		}
	}
}

func TestSessionRecoveryKeepsNonReadyStatesBlocked(t *testing.T) {
	for _, runtime := range []string{"active", "notLoaded", "unknown"} {
		t.Run(runtime, func(t *testing.T) {
			p, received := promptProvider(t, runtime)
			p.statuses["root"] = sessionRuntimeFailed
			o := p.SessionPrompt("root")
			if o.Token == "" {
				t.Fatal("test has no initial recovery offer")
			}
			if p.SendSessionPrompt(context.Background(), o.Token, []string{"continue"}) == nil {
				t.Fatal("recovery ignored the fresh runtime guard")
			}
			if read := <-received; read.Method != "thread/read" || len(received) != 0 {
				t.Fatal("unready preflight sent a mutation")
			}
			// Nothing was sent. A fresh stopped status may return the composer,
			// but the old capability remains one-use and no retry is automatic.
			p.promptLifecycleLocked("thread/status/changed", json.RawMessage(`{"threadId":"root","status":{"type":"systemError"}}`))
			if next := p.SessionPrompt("root"); next.Token == "" || next.Token == o.Token {
				t.Fatal("failed preflight permanently locked recovery or reused a capability")
			}
		})
	}
	for _, runtime := range []sessionRuntimeStatus{sessionRuntimeUnknown, sessionRuntimeWorking, sessionRuntimeApproval, sessionRuntimeInput} {
		p, _ := promptProvider(t, "idle")
		p.statuses["root"] = runtime
		if p.SessionPrompt("root").Token != "" {
			t.Fatal("non-ready runtime granted an ordinary prompt", runtime)
		}
	}
	p, _ := promptProvider(t, "systemError")
	p.statuses["root"] = sessionRuntimeFailed
	delete(p.subscribed, "root")
	if p.SessionPrompt("root").Token != "" {
		t.Fatal("unsubscribed error granted a prompt")
	}
}

func TestSessionRecoveryDistinguishesPreflightFromDispatchedFailures(t *testing.T) {
	for _, method := range []string{"thread/read", "turn/start"} {
		t.Run(method, func(t *testing.T) {
			p, received := promptProvider(t, "systemError", promptTestFault{method: method})
			p.statuses["root"] = sessionRuntimeFailed
			o := p.SessionPrompt("root")
			if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"continue"}); err == nil {
				t.Fatal("synthetic failure was ignored")
			}
			if read := <-received; read.Method != "thread/read" {
				t.Fatal("preflight missing")
			}
			if method == "turn/start" {
				if start := <-received; start.Method != "turn/start" {
					t.Fatal("test did not dispatch the failed mutation")
				}
			}
			p.promptLifecycleLocked("thread/status/changed", json.RawMessage(`{"threadId":"root","status":{"type":"systemError"}}`))
			next := p.SessionPrompt("root")
			if want := method == "thread/read"; (next.Token != "") != want || next.Token == o.Token {
				t.Fatal("preflight failure spent the composer or uncertain mutation revived it", method)
			}
			if len(received) != 0 {
				t.Fatal("failure was automatically retried")
			}
		})
	}
}

func TestOlderRecoveryPreflightCannotSendOrReleaseNewerAttempt(t *testing.T) {
	var p *daemonStatusProvider
	var received <-chan daemonEnvelope
	p, received = promptProvider(t, "systemError", promptTestFault{beforeRead: func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		// Model a later attempt reserved after a terminal event while this
		// read-only preflight was still in flight on the same connection/state.
		p.contexts["root"].promptAttempt = "newer-attempt"
	}})
	p.statuses["root"] = sessionRuntimeFailed
	o := p.SessionPrompt("root")
	if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"older message"}); err == nil {
		t.Fatal("superseded preflight sent its older prompt")
	}
	p.mu.Lock()
	state := p.contexts["root"]
	spent, attempt := state.promptSpent, state.promptAttempt
	p.mu.Unlock()
	if !spent || attempt != "newer-attempt" {
		t.Fatal("old preflight released a newer attempt")
	}
	if read := <-received; read.Method != "thread/read" || len(received) != 0 {
		t.Fatal("superseded preflight performed a mutation")
	}
}

func TestRecoveryPreflightRejectsNewerNonReadyRuntime(t *testing.T) {
	for _, runtime := range []string{"active", "notLoaded", "unknown"} {
		t.Run(runtime, func(t *testing.T) {
			var p *daemonStatusProvider
			var received <-chan daemonEnvelope
			p, received = promptProvider(t, "systemError", promptTestFault{beforeRead: func() {
				p.mu.Lock()
				defer p.mu.Unlock()
				raw, _ := json.Marshal(map[string]any{"threadId": "root", "status": map[string]string{"type": runtime}})
				p.promptLifecycleLocked("thread/status/changed", raw)
			}})
			p.statuses["root"] = sessionRuntimeFailed
			o := p.SessionPrompt("root")
			if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"continue"}); err == nil {
				t.Fatal("older preflight overrode a newer non-ready runtime")
			}
			if read := <-received; read.Method != "thread/read" || len(received) != 0 || p.SessionPrompt("root").Token != "" {
				t.Fatal("runtime transition failed to invalidate pending recovery")
			}
		})
	}
}

func TestInvalidatedRecoveryPreflightCanRecoverOnLaterReadyRuntime(t *testing.T) {
	for _, runtime := range []string{"active", "notLoaded", "unknown"} {
		for _, ready := range []string{"idle", "systemError"} {
			t.Run(runtime+"/"+ready, func(t *testing.T) {
				var p *daemonStatusProvider
				var received <-chan daemonEnvelope
				invalidated := false
				p, received = promptProvider(t, ready, promptTestFault{beforeRead: func() {
					if invalidated {
						return
					}
					invalidated = true
					p.mu.Lock()
					defer p.mu.Unlock()
					raw, _ := json.Marshal(map[string]any{"threadId": "root", "status": map[string]string{"type": runtime}})
					p.promptLifecycleLocked("thread/status/changed", raw)
				}})
				p.statuses["root"] = sessionRuntimeFailed
				old := p.SessionPrompt("root")
				if err := p.SendSessionPrompt(context.Background(), old.Token, []string{"cancelled by runtime change"}); err == nil {
					t.Fatal("invalidated preflight dispatched a prompt")
				}
				if read := <-received; read.Method != "thread/read" || len(received) != 0 || p.SessionPrompt("root").Token != "" {
					t.Fatal("non-ready runtime did not block recovery")
				}
				raw, _ := json.Marshal(map[string]any{"threadId": "root", "status": map[string]string{"type": ready}})
				p.promptLifecycleLocked("thread/status/changed", raw)
				next := p.SessionPrompt("root")
				if next.Token == "" || next.Token == old.Token {
					t.Fatal("later ready state did not restore a fresh composer after invalidating preflight")
				}
				if err := p.SendSessionPrompt(context.Background(), old.Token, []string{"stale"}); err == nil {
					t.Fatal("invalidated capability was reused")
				}
				if err := p.SendSessionPrompt(context.Background(), next.Token, []string{"explicit recovery"}); err != nil {
					t.Fatal("fresh recovery composer could not send", err)
				}
				read, start := <-received, <-received
				if read.Method != "thread/read" || start.Method != "turn/start" || len(received) != 0 {
					t.Fatal("recovery did not dispatch exactly one explicitly requested turn")
				}
			})
		}
	}
}

func TestDispatchedRecoveryRemainsSpentAcrossRuntimeChanges(t *testing.T) {
	for _, ready := range []string{"idle", "systemError"} {
		t.Run(ready, func(t *testing.T) {
			p, received := promptProvider(t, "systemError", promptTestFault{method: "turn/start"})
			p.statuses["root"] = sessionRuntimeFailed
			o := p.SessionPrompt("root")
			if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"uncertain dispatch"}); err == nil {
				t.Fatal("test did not exercise a dispatched failure")
			}
			read, start := <-received, <-received
			if read.Method != "thread/read" || start.Method != "turn/start" {
				t.Fatal("test did not dispatch a mutation")
			}
			for _, kind := range []string{"active", "unknown", "notLoaded", ready} {
				raw, _ := json.Marshal(map[string]any{"threadId": "root", "status": map[string]string{"type": kind}})
				p.promptLifecycleLocked("thread/status/changed", raw)
				if p.SessionPrompt("root").Token != "" {
					t.Fatal("runtime notification revived an uncertain dispatched write", kind)
				}
			}
			if len(received) != 0 {
				t.Fatal("uncertain write was automatically retried")
			}
		})
	}
}

func TestNonTerminalErrorsDoNotUnlockRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, code, turn string
		retry            bool
	}{
		{"retrying", "serverOverloaded", "turn", true},
		{"steer rejected", "activeTurnNotSteerable", "turn", false},
		{"rollback rejected", "threadRollbackFailed", "turn", false},
		{"older failure", "serverOverloaded", "older", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := promptProvider(t, "active")
			raw := json.RawMessage(`{"threadId":"root","turn":{"id":"turn"}}`)
			daemonContextEvent(p.contexts, "turn/started", nil, raw, time.Now())
			p.promptLifecycleLocked("turn/started", raw)
			p.contexts["root"].promptSpent = true
			active := p.SessionTurn("root")
			raw, _ = json.Marshal(map[string]any{"threadId": "root", "turnId": tc.turn, "willRetry": tc.retry, "error": map[string]string{"message": "not a terminal failure", "codexErrorInfo": tc.code}})
			daemonContextEvent(p.contexts, "error", nil, raw, time.Now())
			p.promptLifecycleLocked("error", raw)
			if p.contexts["root"].ended || !p.contexts["root"].promptSpent || p.SessionPrompt("root").Token != "" || p.SessionTurn("root").Token != active.Token {
				t.Fatal("non-terminal/old error unlocked recovery or invalidated the running editor")
			}
		})
	}
}

func TestModelCommandsRecoverSystemErrorsButNotUnknownOrActiveSessions(t *testing.T) {
	for _, status := range []string{"systemError", "active", "notLoaded", "unknown"} {
		t.Run(status, func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			f.commandStatus = status
			ctx := context.Background()
			menu, err := p.SessionCommands(ctx, "one", "model/small")
			if err != nil || len(menu.Choices) != 1 {
				t.Fatal("model discovery failed", menu, err)
			}
			choice := menu.Choices[0]
			if want := status == "systemError"; choice.Action != want {
				t.Fatal("model action has wrong readiness", status, choice)
			}
			err = p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, choice.ID)
			if (err == nil) != (status == "systemError") {
				t.Fatal("model recovery ignored runtime readiness", status, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if status == "systemError" {
				if len(f.writes) != 1 || f.writes[0]["threadId"] != "one" || f.writes[0]["model"] != "small" || f.sessions["two"].Model != "large" {
					t.Fatal("model recovery changed the wrong session", f.writes)
				}
			} else if len(f.writes) != 0 {
				t.Fatal("non-ready model choice wrote settings")
			}
		})
	}
}

func TestDirectoryCommandCanRecoverSystemError(t *testing.T) {
	p, f, base := directoryFixture(t)
	f.commandStatus = "systemError"
	menu, err := p.SessionCommands(context.Background(), "one", "cd/"+url.PathEscape(base))
	if err != nil || len(menu.Choices) != 1 || !menu.Choices[0].Action {
		t.Fatal("stopped error blocked directory recovery", menu, err)
	}
	if err := p.ExecuteSessionCommand(context.Background(), "one", menu.Path, menu.Revision, menu.Choices[0].ID); err != nil {
		t.Fatal("verified directory recovery failed", err)
	}
}

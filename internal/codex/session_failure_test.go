package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const busyMessage = "Selected model is at capacity. Please try a different model."

func TestSessionFailureBootstrapIgnoresLateOlderTurnFailures(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		format := "plain"
		if compressed {
			format = "compressed"
		}
		for _, kind := range []string{"task_complete", "turn_complete", "error"} {
			t.Run(format+"/"+kind, func(t *testing.T) {
				home := t.TempDir()
				path := testRolloutPath(t, home, time.Now(), "late-failure")
				line := func(payload map[string]any) string {
					return string(contextRecord(t, "event_msg", payload)) + "\n"
				}
				failure := map[string]any{"type": kind, "turn_id": "old"}
				if kind == "error" {
					failure["message"], failure["codex_error_info"] = busyMessage, "server_overloaded"
				} else {
					failure["error"] = map[string]string{"message": busyMessage, "codex_error_info": "server_overloaded"}
				}
				writeRollout(t, path, sessionMetaLine("s", `"cli"`, "/work", nil)+"\n"+
					line(map[string]any{"type": "task_started", "turn_id": "old"})+
					line(map[string]any{"type": "task_started", "turn_id": "new"})+
					line(map[string]any{"type": "agent_message", "turn_id": "new", "message": "Current turn is running"})+
					line(failure))
				if compressed {
					compressTestRollout(t, path, true)
				}
				r, err := NewLiveUsageReader(home)
				if err != nil {
					t.Fatal(err)
				}
				r.statusProvider = nil
				u, err := r.FetchTokenUsage(context.Background())
				if err != nil || len(u.Sessions) != 1 {
					t.Fatal("could not restore session", u, err)
				}
				session := u.Sessions[0]
				if session.Attention == SessionAttentionError || session.Context.Kind != SessionContextActivity || session.Context.Text != "Current turn is running" || session.Context.TurnID != "new" {
					t.Fatal("late old failure replaced current-turn status or context at startup", session)
				}
				for _, cursor := range r.files {
					if cursor.currentTurnID != "new" || cursor.attention != sessionAttentionWorking {
						t.Fatal("bootstrap restored the wrong turn state", cursor.currentTurnID, cursor.attention)
					}
				}
			})
		}
	}
}

func TestSessionFailureReaderBootstrapAndRecovery(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		home := t.TempDir()
		path := testRolloutPath(t, home, time.Now(), "failure")
		start := string(contextRecord(t, "event_msg", map[string]string{"type": "task_started", "turn_id": "one"})) + "\n"
		failed := string(contextRecord(t, "event_msg", map[string]any{"type": "task_complete", "turn_id": "one", "error": map[string]string{"message": busyMessage, "codex_error_info": "server_overloaded"}})) + "\n"
		writeRollout(t, path, sessionMetaLine("s", `"cli"`, "/work", nil)+"\n"+start+failed)
		if compressed {
			compressTestRollout(t, path, true)
		}
		r, err := NewLiveUsageReader(home)
		if err != nil {
			t.Fatal(err)
		}
		r.statusProvider = nil
		u, err := r.FetchTokenUsage(context.Background())
		if err != nil || len(u.Sessions) != 1 || u.Sessions[0].Attention != SessionAttentionError || u.Sessions[0].Context.Text != busyMessage {
			t.Fatal(compressed, u, err)
		}
		if compressed {
			continue
		}
		appendRollout(t, path, string(contextRecord(t, "event_msg", map[string]string{"type": "task_started", "turn_id": "two"}))+"\n"+failed)
		u, err = r.FetchTokenUsage(context.Background())
		if err != nil || u.Sessions[0].Attention == SessionAttentionError || u.Sessions[0].Context.Kind == SessionContextFailure {
			t.Fatal("late old failure replaced new turn", u, err)
		}
	}
}

func TestSessionFailureDaemonLifecycle(t *testing.T) {
	states := map[string]*daemonContextState{}
	event := func(method, raw string) { daemonContextEvent(states, method, nil, json.RawMessage(raw), time.Now()) }
	event("turn/started", `{"threadId":"s","turn":{"id":"one"}}`)
	event("error", `{"threadId":"s","turnId":"one","willRetry":true,"error":{"message":"retrying","codexErrorInfo":"serverOverloaded"}}`)
	if states["s"].latest.Kind == SessionContextFailure || states["s"].ended {
		t.Fatal("retry marked failed")
	}
	event("error", `{"threadId":"s","turnId":"old","willRetry":false,"error":{"message":"old error"}}`)
	if states["s"].ended {
		t.Fatal("old turn ended new turn")
	}
	event("error", `{"threadId":"s","turnId":"one","willRetry":false,"error":{"message":"Selected model is at capacity. Please try a different model.","codexErrorInfo":"serverOverloaded"}}`)
	if c := states["s"].latest; c.Text != busyMessage || SessionFailureLabel(c) != "MODEL BUSY" || c.Streaming {
		t.Fatal(c)
	}
	event("turn/completed", `{"threadId":"s","turn":{"id":"one","status":"failed"}}`)
	if states["s"].latest.Text != busyMessage || states["s"].completed {
		t.Fatal("failure lost on completion")
	}
	event("turn/started", `{"threadId":"s","turn":{"id":"two"}}`)
	event("turn/completed", `{"threadId":"s","turn":{"id":"one","status":"failed","error":{"message":"late"}}}`)
	if states["s"].ended || states["s"].latest.Kind == SessionContextFailure {
		t.Fatal("late completion displaced new turn")
	}
	event("turn/completed", `{"threadId":"s","turn":{"id":"two","status":"failed","error":{"message":"Connection lost","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":503}},"additionalDetails":"Try later"}}}`)
	if c := states["s"].latest; c.Text != "Connection lost\nTry later" || SessionFailureLabel(c) != "TURN FAILED" {
		t.Fatal(c)
	}
	event("turn/started", `{"threadId":"s","turn":{"id":"three"}}`)
	event("turn/completed", `{"threadId":"s","turn":{"id":"three","status":"completed"}}`)
	if !states["s"].completed || states["s"].latest.Kind == SessionContextFailure {
		t.Fatal("recovered turn still failed")
	}
}

func TestSessionFailureLocalHistory(t *testing.T) {
	for _, nested := range []bool{false, true} {
		cursor := &rolloutCursor{threadID: "s"}
		payload := map[string]any{"type": "error", "message": busyMessage, "codex_error_info": "server_overloaded"}
		if nested {
			payload = map[string]any{"type": "task_complete", "error": map[string]string{"message": busyMessage, "codex_error_info": "server_overloaded"}}
		}
		c, ok := rolloutContextRecord(contextRecord(t, "event_msg", payload), cursor)
		if !ok || c.Kind != SessionContextFailure || c.Text != busyMessage || SessionFailureLabel(c) != "MODEL BUSY" {
			t.Fatal(c, ok)
		}
		cursor.preview = c
		c, ok = rolloutContextRecord(contextRecord(t, "event_msg", map[string]string{"type": "task_complete"}), cursor)
		if !ok || c.Kind != SessionContextFailure {
			t.Fatal("empty completion lost failure", c)
		}
		c, ok = rolloutContextRecord(contextRecord(t, "event_msg", map[string]string{"type": "task_started"}), cursor)
		if !ok || c.Kind != SessionContextNone {
			t.Fatal("new turn retained failure", c)
		}
	}
}

func TestSessionFailureNonTerminalErrorsAndSanitization(t *testing.T) {
	for _, code := range []string{"thread_rollback_failed", "active_turn_not_steerable"} {
		c, ok := rolloutContextRecord(contextRecord(t, "event_msg", map[string]any{"type": "error", "message": "not terminal", "codex_error_info": map[string]any{code: map[string]string{}}}), &rolloutCursor{})
		if ok {
			t.Fatal("non-turn error changed context", c)
		}
	}
	c, ok := rolloutContextRecord(contextRecord(t, "event_msg", map[string]any{"type": "error", "message": "retrying", "will_retry": true}), &rolloutCursor{})
	if ok {
		t.Fatal("retry became failure", c)
	}
	raw, _ := json.Marshal(map[string]string{"message": "\x1b[31merror\x1b[0m\x00" + strings.Repeat("界", 5000)})
	c = sessionFailureContext(SessionContext{}, raw)
	if strings.ContainsAny(c.Text, "\x1b\x00") || len([]rune(c.Text)) != sessionContextLimit {
		t.Fatal("unsafe/unbounded error")
	}
}

func TestSessionFailureGroupingAndInputSeparation(t *testing.T) {
	now := time.Now()
	failure := sessionFailureContext(SessionContext{ThreadID: "child"}, json.RawMessage(`{"message":"busy","codexErrorInfo":"serverOverloaded"}`))
	r := &LiveUsageReader{files: map[string]*rolloutCursor{
		"root":  {threadID: "root", lastModified: now},
		"child": {threadID: "child", parentThreadID: "root", nonRoot: true, lastModified: now, preview: failure},
	}}
	rows, _, _ := r.sessionSnapshots(now, map[string]struct{}{"root": {}, "child": {}}, true, nil)
	if len(rows) != 1 || rows[0].Attention != SessionAttentionError || rows[0].Context.Text != "busy" {
		t.Fatal(rows)
	}
	for _, higher := range []SessionAttention{SessionAttentionApproval, SessionAttentionInput} {
		if mergeSessionAttention(SessionAttentionError, higher) != higher {
			t.Fatal("error displaced live request")
		}
	}
	if got := sessionAttention(sessionAttentionIdle, true, true, 0, sessionRuntimeUnknown, false); got != SessionAttentionComplete {
		t.Fatal("completion masqueraded as input", got)
	}
	if got := sessionAttention(sessionAttentionInput, true, true, 0, sessionRuntimeUnknown, false); got != SessionAttentionInput {
		t.Fatal("blocking question lost", got)
	}
	if got := sessionAttention(sessionAttentionWorking, true, true, 0, sessionRuntimeFailed, true); got != SessionAttentionError {
		t.Fatal(got)
	}
	r.files["child"].preview = SessionContext{}
	rows, _, _ = r.sessionSnapshots(now, nil, false, map[string]sessionRuntimeStatus{"root": sessionRuntimeWorking, "child": sessionRuntimeWorking})
	if rows[0].Attention != SessionAttentionNone {
		t.Fatal("recovery kept error", rows)
	}
}

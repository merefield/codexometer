package codex

import (
	"encoding/json"
	"testing"
	"time"
)

func TestInferenceHookDenialLifecycle(t *testing.T) {
	for _, code := range []string{"inferenceHookDenied", "inference_hook_denied"} {
		failure := sessionFailureContext(SessionContext{}, json.RawMessage(`{"message":"Workspace inference hook denied this request","codexErrorInfo":"`+code+`"}`))
		if SessionFailureLabel(failure) != "HOOK DENIED" || failure.FailureCode != "inferenceHookDenied" || !failureAffectsTurn(failure) {
			t.Fatal("hook denial lost terminal error semantics", failure)
		}
	}
	states := map[string]*daemonContextState{}
	event := func(method, raw string) { daemonContextEvent(states, method, nil, json.RawMessage(raw), time.Now()) }
	event("turn/started", `{"threadId":"s","turn":{"id":"one"}}`)
	event("error", `{"threadId":"s","turnId":"one","willRetry":false,"error":{"message":"Workspace inference hook denied this request","codexErrorInfo":"inferenceHookDenied"}}`)
	if !states["s"].ended || SessionFailureLabel(states["s"].latest) != "HOOK DENIED" {
		t.Fatal("hook denial was treated as working or retrying")
	}
	event("turn/completed", `{"threadId":"s","turn":{"id":"one","status":"failed"}}`)
	if SessionFailureLabel(states["s"].latest) != "HOOK DENIED" {
		t.Fatal("completion erased hook denial")
	}
	event("turn/started", `{"threadId":"s","turn":{"id":"two"}}`)
	if states["s"].ended || states["s"].latest.Kind == SessionContextFailure {
		t.Fatal("new turn retained the previous denial")
	}
}

func TestLateInterruptionAndFileApprovalResolutionDoNotAffectNewTurn(t *testing.T) {
	states := map[string]*daemonContextState{}
	event := func(method, id, raw string) {
		daemonContextEvent(states, method, json.RawMessage(id), json.RawMessage(raw), time.Now())
	}
	event("turn/started", "", `{"threadId":"s","turn":{"id":"old"}}`)
	event("item/fileChange/requestApproval", "1", `{"threadId":"s","turnId":"old","itemId":"file-old","changes":{},"availableDecisions":["accept","decline"]}`)
	event("turn/interrupted", "", `{"threadId":"s","turn":{"id":"old","status":"interrupted"}}`)
	event("turn/started", "", `{"threadId":"s","turn":{"id":"new"}}`)
	event("item/started", "", `{"threadId":"s","turnId":"new","item":{"id":"reply","type":"agentMessage","phase":"final_answer"}}`)
	event("item/agentMessage/delta", "", `{"threadId":"s","turnId":"new","itemId":"reply","delta":"Current reply"}`)
	event("item/fileChange/requestApproval", "2", `{"threadId":"s","turnId":"new","itemId":"file-new","changes":{},"availableDecisions":["accept","decline"]}`)
	if len(states["s"].requests) != 1 {
		t.Fatal("current approval missing")
	}
	event("turn/interrupted", "", `{"threadId":"s","turn":{"id":"old","status":"interrupted"}}`)
	event("turn/completed", "", `{"threadId":"s","turn":{"id":"old","status":"interrupted"}}`)
	event("serverRequest/resolved", "", `{"threadId":"s","requestId":1}`)
	s := states["s"]
	if s.ended || s.activeTurn != "new" || len(s.requests) != 1 || s.latest.Text != "Current reply" || !s.latest.Streaming {
		t.Fatal("late old-turn cancellation affected current turn", s)
	}
	event("serverRequest/resolved", "", `{"threadId":"s","requestId":2}`)
	if len(s.requests) != 0 || s.ended || s.latest.Text != "Current reply" {
		t.Fatal("resolving approval should not interrupt current turn")
	}
}

func TestAssistantReplyAcceptsAdditiveContentTokenTelemetry(t *testing.T) {
	states := map[string]*daemonContextState{}
	daemonContextEvent(states, "turn/started", nil, json.RawMessage(`{"threadId":"s","turn":{"id":"one"}}`), time.Now())
	daemonContextEvent(states, "item/completed", nil, json.RawMessage(`{"threadId":"s","turnId":"one","contentTokenCount":42,"item":{"id":"reply","type":"agentMessage","text":"Finished","phase":"final_answer"}}`), time.Now())
	if c := states["s"].latest; c.Text != "Finished" || c.Streaming {
		t.Fatal("additive completion telemetry broke assistant reply", c)
	}
}

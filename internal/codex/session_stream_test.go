package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLiveTaskGuidanceAndStream(t *testing.T) {
	states := map[string]*daemonContextState{}
	now := time.Now()
	event := func(method, raw string) {
		now = now.Add(time.Millisecond)
		daemonContextEvent(states, method, nil, json.RawMessage(raw), now)
	}
	snapshot := func() SessionContext {
		return daemonContextSnapshot(states, []string{"root"}, map[string]sessionRuntimeStatus{"root": sessionRuntimeWorking})["root"]
	}
	event("turn/started", `{"threadId":"root","turn":{"id":"turn"}}`)
	event("item/completed", `{"threadId":"root","turnId":"turn","item":{"type":"userMessage","id":"u1","content":[{"type":"text","text":"Build it"}]}}`)
	if c := snapshot(); c.CurrentTask != "Build it" || c.LatestGuidance != "" {
		t.Fatal(c)
	}
	event("item/completed", `{"threadId":"root","turnId":"turn","item":{"type":"userMessage","id":"u2","content":[{"type":"text","text":"Use Go"}]}}`)
	event("item/completed", `{"threadId":"root","turnId":"turn","item":{"type":"userMessage","id":"u1","content":[{"type":"text","text":"Build it"}]}}`)
	if c := snapshot(); c.CurrentTask != "Build it" || c.LatestGuidance != "Use Go" || c.LatestGuidanceID != "u2" || c.ThreadID != "root" || c.TurnID != "turn" {
		t.Fatal(c)
	}
	event("item/started", `{"threadId":"root","turnId":"turn","item":{"type":"agentMessage","id":"a","phase":"final_answer"}}`)
	event("item/agentMessage/delta", `{"threadId":"root","turnId":"turn","itemId":"a","delta":"Hello "}`)
	event("item/agentMessage/delta", `{"threadId":"root","turnId":"old","itemId":"a","delta":"STALE"}`)
	event("item/agentMessage/delta", `{"threadId":"root","turnId":"turn","itemId":"a","delta":"world\n\u001b[31mred\u001b[0m"}`)
	if c := snapshot(); c.Text != "Hello world\nred" || !c.Streaming || c.Kind != SessionContextReply {
		t.Fatal(c)
	}
	event("item/completed", `{"threadId":"root","turnId":"turn","item":{"type":"agentMessage","id":"a","phase":"final_answer","text":"Authoritative reply"}}`)
	event("item/agentMessage/delta", `{"threadId":"root","turnId":"turn","itemId":"a","delta":"LATE"}`)
	if c := snapshot(); c.Text != "Authoritative reply" || c.Streaming {
		t.Fatal(c)
	}
	event("turn/completed", `{"threadId":"root","turn":{"status":"completed"}}`)
	if c := snapshot(); c.CurrentTask != "Build it" || c.LatestGuidance != "Use Go" || c.Streaming {
		t.Fatal(c)
	}
	event("turn/started", `{"threadId":"root","turn":{"id":"next"}}`)
	if c := snapshot(); c.Text != "" || c.CurrentTask != "" || c.LatestGuidanceID != "" {
		t.Fatal(c)
	}
}

func TestStreamBoundsAndMidTurnGuidance(t *testing.T) {
	s := daemonContextState{}
	s.observeUserMessage("u", "turn", "More guidance")
	if s.task != "" || s.guidance != "More guidance" {
		t.Fatal("invented initial task")
	}
	text := boundedStreamText(strings.Repeat("x", sessionContextLimit*10))
	if len(text) != sessionContextLimit*8 || len([]rune(SanitizeSessionContext(text))) != sessionContextLimit {
		t.Fatal("unbounded stream")
	}
}

func TestLocalCompletionRetainsSameTurnLiveTask(t *testing.T) {
	now := time.Now()
	cursor := &rolloutCursor{threadID: "root", currentTurnID: "turn", lastModified: now,
		preview: SessionContext{Kind: SessionContextReply, Text: "Final reply", ThreadID: "root", Source: "LOCAL", At: now}}
	r := &LiveUsageReader{files: map[string]*rolloutCursor{"root": cursor},
		daemonContexts: map[string]SessionContext{"root": {Kind: SessionContextReply, Text: "Final reply", ThreadID: "root", TurnID: "turn", Source: "LIVE", At: now.Add(-time.Second), CurrentTask: "Build it", LatestGuidance: "Use Go"}}}
	sessions, _, _ := r.sessionSnapshots(now, nil, true, map[string]sessionRuntimeStatus{"root": sessionRuntimeIdle})
	if len(sessions) != 1 || sessions[0].Context.CurrentTask != "Build it" || sessions[0].Context.LatestGuidance != "Use Go" {
		t.Fatalf("newer local completion discarded the live task: %+v", sessions)
	}
	for _, turn := range []string{"next-turn", ""} {
		cursor.currentTurnID = turn
		sessions, _, _ = r.sessionSnapshots(now, nil, true, map[string]sessionRuntimeStatus{"root": sessionRuntimeIdle})
		if sessions[0].Context.CurrentTask != "" || sessions[0].Context.LatestGuidance != "" {
			t.Fatal("attached task without matching turn", sessions)
		}
	}
}

func TestStreamingCommentaryKeepsCommandAndApprovalPriority(t *testing.T) {
	states := map[string]*daemonContextState{}
	now := time.Now()
	event := func(method, raw string) {
		now = now.Add(time.Millisecond)
		daemonContextEvent(states, method, json.RawMessage(`1`), json.RawMessage(raw), now)
	}
	event("turn/started", `{"threadId":"root","turn":{"id":"t"}}`)
	event("item/started", `{"threadId":"root","turnId":"t","item":{"type":"commandExecution","id":"cmd","command":"go test","status":"inProgress"}}`)
	event("item/started", `{"threadId":"root","turnId":"t","item":{"type":"agentMessage","id":"a","phase":"commentary"}}`)
	event("item/agentMessage/delta", `{"threadId":"root","turnId":"t","itemId":"a","delta":"Testing now"}`)
	c := daemonContextSnapshot(states, []string{"root"}, nil)["root"]
	if c.Kind != SessionContextActivity || c.Activity.Prose != "Testing now" || c.Activity.Command != "go test" {
		t.Fatal(c)
	}
	event("item/commandExecution/requestApproval", `{"threadId":"root","turnId":"t","itemId":"cmd","command":"go test","cwd":"/work","reason":"Permission?"}`)
	event("item/agentMessage/delta", `{"threadId":"root","turnId":"t","itemId":"a","delta":" More prose"}`)
	c = daemonContextSnapshot(states, []string{"root"}, map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval})["root"]
	if c.Kind != SessionContextApproval || c.Streaming || c.ApprovalContext != "Testing now" {
		t.Fatal(c)
	}
	event("turn/interrupted", `{"threadId":"root"}`)
	event("item/agentMessage/delta", `{"threadId":"root","turnId":"t","itemId":"a","delta":"LATE"}`)
	if states["root"].latest.Streaming || strings.Contains(states["root"].latest.Text, "LATE") {
		t.Fatal("stream revived after interruption")
	}
}

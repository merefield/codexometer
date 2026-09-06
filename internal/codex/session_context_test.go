package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func contextRecord(t *testing.T, kind string, payload any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"type": kind, "timestamp": time.Now().UTC(), "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCompletedRootAttentionAndWorkingChild(t *testing.T) {
	now := time.Now()
	r := &LiveUsageReader{files: map[string]*rolloutCursor{
		"root":  {threadID: "root", lastModified: now},
		"child": {threadID: "child", parentThreadID: "root", nonRoot: true, lastModified: now},
	}}
	statuses := map[string]sessionRuntimeStatus{"root": sessionRuntimeComplete, "child": sessionRuntimeIdle}
	sessions, _, _ := r.sessionSnapshots(now, nil, false, statuses)
	if len(sessions) != 1 || sessions[0].Attention != SessionAttentionComplete {
		t.Fatalf("completion missing: %+v", sessions)
	}
	for _, child := range []sessionRuntimeStatus{sessionRuntimeWorking, sessionRuntimeInput, sessionRuntimeApproval} {
		statuses["child"] = child
		sessions, _, _ = r.sessionSnapshots(now, nil, false, statuses)
		want := SessionAttentionNone
		if child == sessionRuntimeInput {
			want = SessionAttentionInput
		}
		if child == sessionRuntimeApproval {
			want = SessionAttentionApproval
		}
		if sessions[0].Attention != want {
			t.Fatalf("child %v got %v want %v", child, sessions[0].Attention, want)
		}
	}
	statuses["root"] = sessionRuntimeIdle
	statuses["child"] = sessionRuntimeComplete
	sessions, _, _ = r.sessionSnapshots(now, nil, false, statuses)
	if sessions[0].Attention != SessionAttentionNone {
		t.Fatal("child completion labelled whole root complete")
	}
}

func TestSessionContextExtractsOnlyDisplayText(t *testing.T) {
	cursor := &rolloutCursor{threadID: "root"}
	for _, tc := range []struct {
		payload  any
		kind     SessionContextKind
		contains string
	}{
		{map[string]any{"type": "task_complete", "last_agent_message": "Pushed directly to main"}, SessionContextReply, "Pushed directly"},
		{map[string]any{"type": "request_user_input", "questions": []any{map[string]any{"question": "Which branch?", "options": []any{map[string]string{"label": "main", "description": "Default branch"}}}}}, SessionContextQuestion, "Default branch"},
		{map[string]any{"type": "exec_approval_request", "reason": "Allow push?", "command": []string{"git", "push"}}, SessionContextApproval, "git push"},
		{map[string]any{"type": "exec_command_begin", "command": "go test ./..."}, SessionContextActivity, "go test"},
	} {
		c, ok := rolloutContextRecord(contextRecord(t, "event_msg", tc.payload), cursor)
		if !ok || c.Kind != tc.kind || !strings.Contains(c.Text, tc.contains) || c.ThreadID != "root" || c.Source != "LOCAL" {
			t.Fatalf("context=%+v", c)
		}
	}
	for _, payload := range []map[string]any{
		{"type": "agent_reasoning", "text": "private reasoning"},
		{"type": "request_user_input", "isBlocking": false, "questions": []any{map[string]string{"question": "Not blocking"}}},
	} {
		if c, ok := rolloutContextRecord(contextRecord(t, "event_msg", payload), cursor); ok {
			t.Fatalf("retained excluded content: %+v", c)
		}
	}
	if c, ok := rolloutContextRecord(contextRecord(t, "response_item", map[string]any{"type": "message", "role": "user", "content": []any{map[string]string{"type": "input_text", "text": "user secret"}}}), cursor); ok {
		t.Fatalf("retained user prompt: %+v", c)
	}
}

func TestSessionContextSanitizationAndBound(t *testing.T) {
	raw := "\x1b[31mhello\x1b[0m\x1b]8;;https://bad.example\aLINK\x1b]8;;\a\x00\r\u202E" + strings.Repeat("界", 5000)
	got := SanitizeSessionContext(raw)
	if !utf8.ValidString(got) || strings.ContainsAny(got, "\x1b\x00\r\u202E") || strings.Contains(got, "bad.example") || len([]rune(got)) != sessionContextLimit {
		t.Fatalf("unsafe/unbounded context %q", got[:min(len(got), 100)])
	}
}

func TestSessionContextRequestSurvivesCommentary(t *testing.T) {
	cursor := &rolloutCursor{threadID: "root", preview: SessionContext{Kind: SessionContextQuestion, Text: "Which branch?", ThreadID: "root"}}
	c, ok := rolloutContextRecord(contextRecord(t, "event_msg", map[string]string{"type": "agent_message", "message": "Waiting for your choice"}), cursor)
	if !ok || c != cursor.preview {
		t.Fatal("commentary displaced pending question", c)
	}
	c, ok = rolloutContextRecord(contextRecord(t, "event_msg", map[string]string{"type": "task_started"}), cursor)
	if !ok || c.Text != "" {
		t.Fatal("new turn kept question", c)
	}
	a := SessionContext{Kind: SessionContextReply, Text: "A", At: time.Now(), ThreadID: "a"}
	b := a
	b.ThreadID = "b"
	if preferSessionContext(a, b) != preferSessionContext(b, a) {
		t.Fatal("selection depends on map order")
	}
}

func TestSessionContextBootstrapAndOwnership(t *testing.T) {
	cursor := &rolloutCursor{threadID: "root"}
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	complete := contextRecord(t, "event_msg", map[string]string{"type": "task_complete", "last_agent_message": "Finished"})
	if err := os.WriteFile(path, append(complete, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if c := latestSessionContext(path, cursor); c.Text != "Finished" {
		t.Fatal(c)
	}
	start := contextRecord(t, "event_msg", map[string]string{"type": "task_started"})
	if err := os.WriteFile(path, append(append(append(complete, '\n'), start...), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if c := latestSessionContext(path, cursor); c.Text != "" {
		t.Fatal("stale prior-turn reply", c)
	}
	// An inherited parent event must not be presented as the child's message.
	boundary := uint64(10)
	cursor.nonRoot = true
	cursor.subagentHistoryStartOrdinal = &boundary
	if c, ok := rolloutContextRecord([]byte(`{"type":"event_msg","ordinal":2,"payload":{"type":"task_complete","last_agent_message":"parent"}}`), cursor); ok {
		t.Fatal("inherited context", c)
	}
}

func TestDaemonContextRequestLifecycle(t *testing.T) {
	states := map[string]*daemonContextState{}
	now := time.Now()
	emit := func(method, id, params string) {
		daemonContextEvent(states, method, json.RawMessage(id), json.RawMessage(params), now)
	}
	emit("item/completed", "", `{"threadId":"root","item":{"type":"agentMessage","text":"Last reply","phase":"final_answer"}}`)
	emit("item/commandExecution/requestApproval", "1", `{"threadId":"root","reason":"Allow push?","command":"git push"}`)
	emit("item/commandExecution/requestApproval", "2", `{"threadId":"root","reason":"Second approval","command":"git status"}`)
	emit("serverRequest/resolved", "", `{"threadId":"root","requestId":1}`)
	out := daemonContextSnapshot(states, []string{"root"}, map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval})
	if c := out["root"]; c.Kind != SessionContextApproval || !strings.Contains(c.Text, "Second approval") {
		t.Fatal(c)
	}
	emit("serverRequest/resolved", "", `{"threadId":"root","requestId":2}`)
	out = daemonContextSnapshot(states, []string{"root"}, map[string]sessionRuntimeStatus{"root": sessionRuntimeIdle})
	if c := out["root"]; c.Text != "Last reply" || c.Kind != SessionContextReply {
		t.Fatal(c)
	}
	emit("turn/started", "", `{"threadId":"root"}`)
	if len(states) != 0 {
		t.Fatal("stale context after next turn")
	}
	emit("item/tool/requestUserInput", "3", `{"threadId":"root","isBlocking":false,"questions":[{"question":"Optional feedback?"}]}`)
	if len(states["root"].requests) != 0 {
		t.Fatal("non-blocking question treated as pending input")
	}
}

func TestSessionContextGroupPrefersOutstandingChildRequest(t *testing.T) {
	now := time.Now()
	r := LiveUsageReader{files: map[string]*rolloutCursor{
		"root":  {threadID: "root", lastModified: now, preview: SessionContext{Kind: SessionContextReply, Text: "Root done", At: now, ThreadID: "root"}},
		"child": {threadID: "child", parentThreadID: "root", nonRoot: true, lastModified: now, attention: sessionAttentionApproval, preview: SessionContext{Kind: SessionContextApproval, Text: "Child approval", At: now.Add(-time.Minute), ThreadID: "child"}},
	}}
	sessions, _, _ := r.sessionSnapshots(now, map[string]struct{}{"root": {}, "child": {}}, true, nil)
	if len(sessions) != 1 || sessions[0].Context.ThreadID != "child" {
		t.Fatalf("sessions=%+v", sessions)
	}
	sessions, _, _ = r.sessionSnapshots(now, map[string]struct{}{"root": {}, "child": {}}, true, map[string]sessionRuntimeStatus{"child": sessionRuntimeWorking})
	if sessions[0].Context.Text != "Root done" {
		t.Fatal("stale approval overrode exact working status", sessions)
	}
}

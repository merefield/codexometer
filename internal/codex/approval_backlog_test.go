package codex

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestApprovalBacklogReplayPreservesAgeAndUsesRequestID(t *testing.T) {
	now := time.Unix(100, 0)
	states := map[string]*daemonContextState{}
	raw := json.RawMessage(`{"threadId":"root","turnId":"turn","itemId":"cmd","command":"pwd","cwd":"/work"}`)
	for _, id := range []string{`"b"`, `"a"`} {
		daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(id), raw, now)
	}
	status := map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval}
	old := daemonContextSnapshot(states, []string{"root"}, status)["root"]
	if old.RequestID != `"a"` {
		t.Fatalf("non-deterministic tie: %+v", old)
	}
	daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(`"a"`), raw, now.Add(time.Minute))
	next := daemonContextSnapshot(states, []string{"root"}, status)["root"]
	if next.RequestID != old.RequestID || !next.At.Equal(now) || next.ApprovalToken == old.ApprovalToken || next.PendingApprovals != 2 {
		t.Fatalf("replay changed age/count or reused capability: %+v", next)
	}
}

func TestApprovalBacklogOverflowIsExplicitUntilTurnEnds(t *testing.T) {
	now := time.Unix(100, 0)
	states := map[string]*daemonContextState{}
	raw := json.RawMessage(`{"threadId":"root","turnId":"turn","itemId":"cmd","command":"pwd","cwd":"/work"}`)
	for i := 0; i < 17; i++ {
		daemonContextEvent(states, "item/commandExecution/requestApproval", json.RawMessage(fmt.Sprint(i)), raw, now.Add(time.Duration(i)*time.Second))
	}
	status := map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval}
	got := daemonContextSnapshot(states, []string{"root"}, status)["root"]
	if got.PendingApprovals != 16 || !got.PendingApprovalsLimited || len(states["root"].requests) != 16 {
		t.Fatalf("cap silently truncated count: %+v", got)
	}
	for i := 0; i < 16; i++ {
		daemonContextEvent(states, "serverRequest/resolved", nil, json.RawMessage(fmt.Sprintf(`{"threadId":"root","requestId":%d}`, i)), now)
	}
	got = daemonContextSnapshot(states, []string{"root"}, status)["root"]
	if got.PendingApprovals != 0 || !got.PendingApprovalsLimited {
		t.Fatalf("lost overflow warning after draining retained requests: %+v", got)
	}
	daemonContextEvent(states, "turn/completed", nil, json.RawMessage(`{"threadId":"root","turn":{"status":"completed"}}`), now)
	if states["root"].approvalsLimited {
		t.Fatal("overflow survived turn completion")
	}
}

func TestApprovalBacklogCountsRequestsAndSelectsOldest(t *testing.T) {
	now := time.Now()
	first := SessionContext{Kind: SessionContextApproval, Text: "first", At: now, ThreadID: "child", ApprovalToken: "first-token"}
	second := first
	second.Text, second.At, second.ApprovalToken = "second", now.Add(time.Second), "second-token"
	states := map[string]*daemonContextState{"child": {requests: map[string]SessionContext{"one": first, "two": second}}}
	statuses := map[string]sessionRuntimeStatus{"child": sessionRuntimeApproval}
	got := daemonContextSnapshot(states, []string{"child"}, statuses)["child"]
	if got.PendingApprovals != 2 || got.ApprovalToken != "first-token" {
		t.Fatalf("wrong backlog: %+v", got)
	}
	delete(states["child"].requests, "one")
	got = daemonContextSnapshot(states, []string{"child"}, statuses)["child"]
	if got.PendingApprovals != 1 || got.ApprovalToken != "second-token" {
		t.Fatalf("next request not exposed: %+v", got)
	}
	statuses["child"] = sessionRuntimeWorking
	if got := daemonContextSnapshot(states, []string{"child"}, statuses)["child"]; got.PendingApprovals != 0 {
		t.Fatal("stale requests counted")
	}
}

func TestApprovalBacklogAggregatesRootAndChildren(t *testing.T) {
	now := time.Now()
	r := &LiveUsageReader{
		files: map[string]*rolloutCursor{
			"root":  {threadID: "root", lastModified: now},
			"child": {threadID: "child", parentThreadID: "root", nonRoot: true, lastModified: now},
		},
		daemonContexts: map[string]SessionContext{
			"root":  {Kind: SessionContextApproval, Text: "parent", ThreadID: "root", At: now, PendingApprovals: 1},
			"child": {Kind: SessionContextApproval, Text: "child", ThreadID: "child", At: now.Add(-time.Minute), PendingApprovals: 2, ApprovalToken: "child-token", PendingApprovalsLimited: true},
		},
	}
	sessions, _, _ := r.sessionSnapshots(now, nil, true, map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval, "child": sessionRuntimeApproval})
	if len(sessions) != 1 || sessions[0].Context.PendingApprovals != 3 || sessions[0].Context.ApprovalToken != "child-token" {
		t.Fatalf("wrong grouped backlog: %+v", sessions)
	}
	if !sessions[0].Context.PendingApprovalsLimited {
		t.Fatal("lost child overflow warning")
	}
	r.daemonContexts["child"] = SessionContext{PendingApprovalsLimited: true}
	sessions, _, _ = r.sessionSnapshots(now, nil, true, map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval, "child": sessionRuntimeApproval})
	if len(sessions) != 1 || !sessions[0].Context.PendingApprovalsLimited || sessions[0].Context.PendingApprovals != 1 {
		t.Fatal("empty overflow-only child context lost during aggregation", sessions)
	}
}

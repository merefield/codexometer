package codex

import (
	"testing"
	"time"
)

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
			"child": {Kind: SessionContextApproval, Text: "child", ThreadID: "child", At: now.Add(-time.Minute), PendingApprovals: 2, ApprovalToken: "child-token"},
		},
	}
	sessions, _, _ := r.sessionSnapshots(now, nil, true, map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval, "child": sessionRuntimeApproval})
	if len(sessions) != 1 || sessions[0].Context.PendingApprovals != 3 || sessions[0].Context.ApprovalToken != "child-token" {
		t.Fatalf("wrong grouped backlog: %+v", sessions)
	}
}

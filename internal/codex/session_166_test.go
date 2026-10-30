package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestCodex166FailureClassificationAndRecovery(t *testing.T) {
	for _, tc := range []struct{ code, local, label string }{
		{"serverOverloaded", "server_overloaded", "MODEL BUSY"},
		{"modelNotFound", "model_not_found", "MODEL UNAVAILABLE"},
		{"policyCapacityUnavailable", "policy_capacity_unavailable", "CAPACITY RESTRICTED"},
		{"futureError", "future_error", "TURN FAILED"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			for _, code := range []string{tc.code, tc.local} {
				for _, tagged := range []bool{false, true} {
					var wire any = code
					if tagged {
						wire = map[string]any{code: map[string]any{}}
					}
					c := sessionFailureContext(SessionContext{}, rawJSON(map[string]any{"message": "Upstream explanation", "codexErrorInfo": wire}))
					if SessionFailureLabel(c) != tc.label || c.Text != "Upstream explanation" {
						t.Fatal("lost error classification or message", c)
					}
				}
			}
			states := map[string]*daemonContextState{}
			emit := func(method string, params any) { daemonContextEvent(states, method, nil, rawJSON(params), time.Now()) }
			emit("turn/started", map[string]any{"threadId": "s", "turn": map[string]string{"id": "one"}})
			failure := map[string]any{"threadId": "s", "turnId": "one", "willRetry": true, "error": map[string]string{"message": "Upstream explanation", "codexErrorInfo": tc.code}}
			emit("error", failure)
			if states["s"].ended {
				t.Fatal("automatically retrying response marked failed")
			}
			failure["willRetry"] = false
			emit("error", failure)
			if !states["s"].ended || SessionFailureLabel(states["s"].latest) != tc.label {
				t.Fatal("terminal failure not classified", states["s"])
			}
			emit("turn/started", map[string]any{"threadId": "s", "turn": map[string]string{"id": "two"}})
			emit("error", failure)
			if states["s"].ended || states["s"].latest.Kind == SessionContextFailure {
				t.Fatal("old error failed the new turn")
			}
			cursor := &rolloutCursor{threadID: "s"}
			c, ok := rolloutContextRecord(contextRecord(t, "event_msg", map[string]string{"type": "error", "message": "Upstream explanation", "codex_error_info": tc.local}), cursor)
			if !ok || SessionFailureLabel(c) != tc.label {
				t.Fatal("local fallback differs from daemon", c)
			}
		})
	}
}

func TestCodex166ItemTimingAndLegacyFallback(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint(nested), func(t *testing.T) {
			states := map[string]*daemonContextState{}
			emit := func(method, status string, start, end *int64) {
				item := map[string]any{"id": "cmd", "type": "commandExecution", "command": "go test", "status": status}
				p := map[string]any{"threadId": "s", "turnId": "t", "item": item}
				target := p
				if nested {
					target = item
				}
				if start != nil {
					target["startedAtMs"] = *start
				}
				if end != nil {
					target["completedAtMs"] = *end
				}
				daemonContextEvent(states, method, nil, rawJSON(p), now)
			}
			start, end := now.Add(-42*time.Second).UnixMilli(), now.Add(-35*time.Second).UnixMilli()
			emit("item/started", "inProgress", &start, nil)
			if c := states["s"].latest; c.Activity.CommandTiming(now) != "42s" || c.At != now {
				t.Fatal("running duration or observation time wrong", c)
			}
			// Older envelopes omit the start on completion; keep the matching start.
			emit("item/completed", "completed", nil, &end)
			a := states["s"].latest.Activity
			if a.CommandTiming(now.Add(time.Hour)) != "7s" || a.RunningCommands != 0 {
				t.Fatal("completed timer kept advancing or lost its start", a)
			}
		})
	}
	for _, a := range []SessionActivity{
		{CommandStatus: "running"},
		{CommandStatus: "running", CommandStartedAt: now.Add(time.Second)},
		{CommandStatus: "completed", CommandStartedAt: now.Add(-time.Minute)},
		{CommandStatus: "completed", CommandStartedAt: now, CommandCompletedAt: now.Add(-time.Second)},
		{CommandStatus: "completed", CommandStartedAt: now.Add(-time.Minute), CommandCompletedAt: now.Add(time.Second)},
	} {
		if a.CommandTiming(now) != "" {
			t.Fatal("invented duration", a)
		}
	}
	for _, ms := range []int64{0, -1, now.Add(time.Second).UnixMilli()} {
		if !itemLifecycleTime(&ms, nil, now).IsZero() {
			t.Fatal("trusted invalid lifecycle time", ms)
		}
	}
	bad, good := int64(0), now.Add(-time.Second).UnixMilli()
	if !itemLifecycleTime(&bad, &good, now).IsZero() {
		t.Fatal("contradictory envelope hid invalid item time")
	}
	var activity sessionActivityState
	activity.commandTimed("first", "go test", "inProgress", false, now.Add(-time.Minute), time.Time{})
	activity.commandTimed("second", "go vet", "inProgress", false, now.Add(-5*time.Second), time.Time{})
	activity.commandTimed("first", "", "completed", true, time.Time{}, now)
	if a := activity.snapshot(); a.Command != "go vet" || a.CommandTiming(now) != "5s" {
		t.Fatal("parallel command timing used the wrong item", a)
	}
	activity.command("unknown", "pwd", "completed", true)
	activity.commandTimed("second", "", "completed", true, time.Time{}, now)
	activity.command("legacy", "echo done", "completed", true)
	if a := activity.snapshot(); a.CommandTiming(now) != "" {
		t.Fatal("legacy command inherited another item's timing", a)
	}
}

func TestCodex166ReviewerEventsDoNotCompleteParentBenchmark(t *testing.T) {
	message := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	usage := BenchmarkUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120}
	server, _ := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{"thread": map[string]string{"id": "parent"}, "model": "gpt-6.1-sol"})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn"}})},
		benchmarkEnvelope{Method: "rawResponseItem/completed", Params: rawJSON(map[string]any{"threadId": "reviewer", "turnId": "turn", "item": map[string]string{"type": "message", "text": "private reviewer"}})},
		benchmarkEnvelope{Method: "rawResponse/completed", Params: rawJSON(map[string]any{"threadId": "reviewer", "turnId": "turn", "responseId": "private", "usage": BenchmarkUsage{TotalTokens: 999999}})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{"threadId": "reviewer", "turn": map[string]string{"id": "turn", "status": "interrupted"}})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{"threadId": "parent", "turn": map[string]string{"id": "reviewer-turn", "status": "interrupted"}})},
		benchmarkEnvelope{Method: "thread/tokenUsage/updated", Params: rawJSON(map[string]any{"threadId": "parent", "turnId": "turn", "tokenUsage": map[string]any{"total": usage}})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{"threadId": "parent", "turnId": "turn", "item": map[string]string{"type": "agentMessage", "text": message}})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{"threadId": "parent", "turn": map[string]string{"id": "turn", "status": "completed"}})},
	)
	result, err := server.runBenchmark(context.Background(), benchmarkCombination{model: benchmarkModel{Model: "gpt-6.1-sol"}, effort: "high"}, benchmarkTaskDefinitions[0])
	if err != nil || !result.Correct || !result.UsageKnown || result.Usage.TotalTokens != 120 || result.ToolUsed {
		t.Fatalf("reviewer event contaminated parent result/accounting: %+v, %v", result, err)
	}
}

func TestCodex165AttachedForkRemainsIndependentAndDoesNotRecountHistory(t *testing.T) {
	home, now := t.TempDir(), time.Now()
	root := testRolloutPath(t, home, now, "root")
	fork := testRolloutPath(t, home, now, "attached")
	var metadata map[string]any
	if err := json.Unmarshal([]byte(sessionMetaLine("attached", `"cli"`, "/work", nil)), &metadata); err != nil {
		t.Fatal(err)
	}
	payload := metadata["payload"].(map[string]any)
	payload["attached_thread"], payload["forked_from_id"] = true, "root"
	payload["forked_from_ordinal_exclusive"] = 20
	u := BenchmarkUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120}
	baseline := exactLine(t, now, "root", "old", "historical", u)
	writeRollout(t, root, sessionMetaLine("root", `"cli"`, "/work", nil)+"\n"+baseline+"\n")
	writeRollout(t, fork, string(rawJSON(metadata))+"\n"+baseline+"\n")
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.statusProvider = nil
	first, err := r.FetchTokenUsage(context.Background())
	if err != nil || first.TotalTokens != 0 {
		t.Fatal("history billed on startup", first, err)
	}
	appendRollout(t, root, exactLine(t, now, "root", "t", "root-new", u)+"\n")
	appendRollout(t, fork, baseline+"\n"+exactLine(t, now, "attached", "t", "fork-new", u)+"\n")
	next, err := r.FetchTokenUsage(context.Background())
	if err != nil || next.TotalTokens != 240 || len(next.Sessions) != 2 {
		t.Fatal("attached fork merged, omitted or double billed", next, err)
	}
	for _, s := range next.Sessions {
		if s.TotalTokens != 120 || s.AgentCount != 0 {
			t.Fatal("attachment treated as spawned subagent", s)
		}
	}
}

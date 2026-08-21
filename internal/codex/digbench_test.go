package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/digbench"
)

type fakeDigBenchService struct {
	started   digbench.StartRequest
	session   digbench.Session
	stepCalls int
}

func (f *fakeDigBenchService) StartSession(_ context.Context, request digbench.StartRequest) (digbench.Session, error) {
	f.started = request
	return f.session, nil
}

func (f *fakeDigBenchService) GetSession(context.Context, string) (digbench.Session, error) {
	return f.session, nil
}

func (f *fakeDigBenchService) Step(_ context.Context, _ string, request digbench.StepRequest) (digbench.StepResponse, error) {
	f.stepCalls++
	maxLevel := 3
	f.session = digbench.Session{
		Game: "P-1", SessionID: "session-1", Done: true, StepIndex: request.StepIndex,
		LevelsBeaten: 3, Seed: f.session.Seed, FrameworkVersion: f.session.FrameworkVersion,
		State: digbench.State{Done: true, Status: "completed", Level: 3, MaxLevel: &maxLevel, Observation: "WIN"},
	}
	return digbench.StepResponse{Session: f.session}, nil
}

func TestRunDigBenchBridgesScopedStepAndDetectsWin(t *testing.T) {
	seed := int64(42)
	framework := "engine-1"
	maxLevel := 3
	service := &fakeDigBenchService{session: digbench.Session{
		Game: "P-1", SessionID: "session-1", Seed: &seed, FrameworkVersion: &framework,
		State: digbench.State{Status: "in_progress", Level: 1, MaxLevel: &maxLevel, Observation: "___", Actions: []string{"b"}},
	}}
	usage := BenchmarkUsage{TotalTokens: 120, InputTokens: 100, OutputTokens: 20}
	server, requests := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{"data": []any{map[string]any{
			"model": "gpt-5.6-sol", "displayName": "GPT-5.6 Sol",
			"supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "high"}},
		}}})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-1"}, "model": "gpt-5.6-sol",
		})},
		benchmarkEnvelope{ID: rawJSON(3), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-1"}})},
		benchmarkEnvelope{ID: rawJSON(50), Method: "item/tool/call", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "callId": "call-1", "tool": "step",
			"arguments": map[string]any{"session_id": "session-1", "step_index": 1, "action": "b"},
		})},
		benchmarkEnvelope{Method: "thread/tokenUsage/updated", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "tokenUsage": map[string]any{"total": usage},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turn": map[string]any{
				"id": "turn-1", "status": "failed", "error": map[string]string{"message": "debrief failed after terminal move"},
			},
		})},
	)
	server.experimentalAPI = true

	original := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string) (*appServerSession, error) { return server, nil }
	t.Cleanup(func() { openBenchmarkAppServer = original })

	result, err := (Client{}).RunDigBench(context.Background(), service, DigBenchOptions{
		Game: "P-1", Model: "gpt-5.6-sol", Effort: "high", Timeout: time.Minute, ClientVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Won || result.Failure != "" || result.Status != "completed" || result.LevelsBeaten != 3 || result.MaxLevel != 3 || result.Steps != 1 {
		t.Fatalf("result = %#v", result)
	}
	if service.stepCalls != 1 || service.started.ModelName == nil || !strings.Contains(*service.started.ModelName, "gpt-5.6-sol/high") {
		t.Fatalf("service = %#v", service)
	}
	if !result.UsageKnown || result.Usage != usage || !result.CostKnown {
		t.Fatalf("telemetry = %#v", result)
	}
	requestLog := requests.String()
	for _, expected := range []string{`"dynamicTools"`, `"sandbox":"workspace-write"`, `"method":"turn/start"`, `"id":50`, `"success":true`, `\"status\":\"completed\"`} {
		if !strings.Contains(requestLog, expected) {
			t.Fatalf("request log missing %q: %s", expected, requestLog)
		}
	}
}

func TestDigBenchToolRejectsAnotherSession(t *testing.T) {
	service := &fakeDigBenchService{session: digbench.Session{SessionID: "session-1"}}
	current := service.session
	params, _ := json.Marshal(map[string]any{
		"threadId": "thread-1", "turnId": "turn-1", "tool": "step",
		"arguments": map[string]any{"session_id": "session-2", "step_index": 1, "action": "a"},
	})
	response := handleDigBenchToolCall(context.Background(), service, "thread-1", "turn-1", "session-1", params, &current)
	if response["success"] != false || service.stepCalls != 0 {
		t.Fatalf("response = %#v, calls = %d", response, service.stepCalls)
	}
	content := response["contentItems"].([]map[string]string)
	if !strings.Contains(content[0]["text"], "access denied") {
		t.Fatalf("response = %#v", response)
	}
}

func TestFindDigBenchCombinationRequiresExactEffort(t *testing.T) {
	models := []benchmarkModel{{
		Model: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol",
		SupportedReasoningEfforts: []benchmarkEffortOption{{ReasoningEffort: "medium"}, {ReasoningEffort: "high"}},
	}}
	combination, err := findDigBenchCombination(models, "gpt-5.6-sol", "high")
	if err != nil || combination.effort != "high" {
		t.Fatalf("combination = %#v, error = %v", combination, err)
	}
	if _, err := findDigBenchCombination(models, "gpt-5.6-sol", "max"); err == nil {
		t.Fatal("unsupported effort was accepted")
	}
}

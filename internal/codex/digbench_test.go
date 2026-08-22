package codex

import (
	"context"
	"encoding/json"
	"errors"
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
				"items": []any{map[string]string{"type": "agentMessage", "text": "The winning rule for session-1 was discovered."}},
			},
		})},
	)
	server.experimentalAPI = true

	original := openBenchmarkAppServer
	var receivedAPIKey string
	openBenchmarkAppServer = func(_ context.Context, _, apiKey string) (*appServerSession, error) {
		receivedAPIKey = apiKey
		return server, nil
	}
	t.Cleanup(func() { openBenchmarkAppServer = original })

	var progress []DigBenchProgress
	var snapshots []DigBenchResult
	result, err := (Client{BenchmarkAPIKey: "benchmark-secret"}).RunDigBench(context.Background(), service, DigBenchOptions{
		Game: "P-1", Model: "gpt-5.6-sol", Effort: "high", Timeout: time.Minute, ClientVersion: "test",
		Progress: func(event DigBenchProgress) { progress = append(progress, event) },
		Snapshot: func(result DigBenchResult) { snapshots = append(snapshots, result) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if receivedAPIKey != "benchmark-secret" {
		t.Fatal("DigBench did not route the benchmark API key to app-server")
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
	if len(progress) != 3 || progress[0].Phase != DigBenchProgressSession || progress[1].Phase != DigBenchProgressTurn || progress[2].Phase != DigBenchProgressUpdate {
		t.Fatalf("progress phases = %#v", progress)
	}
	if final := progress[2]; final.Level != 3 || final.LevelsBeaten != 3 || final.MaxLevel != 3 || final.Steps != 1 || final.Status != "completed" || final.Elapsed < 0 {
		t.Fatalf("final progress = %#v", final)
	}
	if len(snapshots) < 3 || len(result.Interactions) != 4 || result.Interactions[1].Kind != BenchmarkInteractionMove || result.Interactions[1].Content != "b" || result.Interactions[2].Kind != BenchmarkInteractionState || result.Interactions[3].Kind != BenchmarkInteractionResponse {
		t.Fatalf("DigBench snapshots/interactions = %d / %#v", len(snapshots), result.Interactions)
	}
	for _, interaction := range result.Interactions {
		if strings.Contains(interaction.Content, "session-1") {
			t.Fatalf("session id leaked into transcript: %#v", interaction)
		}
	}
	requestLog := requests.String()
	for _, expected := range []string{`"dynamicTools"`, `"sandbox":"workspace-write"`, `"method":"turn/start"`, `"id":50`, `"success":true`, `\"status\":\"completed\"`} {
		if !strings.Contains(requestLog, expected) {
			t.Fatalf("request log missing %q: %s", expected, requestLog)
		}
	}
}

func TestClientBenchmarkTasksRequireDigBenchToken(t *testing.T) {
	plain := (Client{}).BenchmarkTasks()
	enabled := (Client{DigBenchToken: "secret"}).BenchmarkTasks()
	if len(enabled) != len(plain)+1 || enabled[len(enabled)-1] != DigBenchTask() {
		t.Fatalf("plain=%#v enabled=%#v", plain, enabled)
	}
}

func TestNextPendingEnvelopeCanReportHeartbeat(t *testing.T) {
	server, _ := newFakeBenchmarkServer()
	heartbeat := make(chan time.Time, 1)
	heartbeat <- time.Now()
	envelope, pulsed, err := server.nextPendingEnvelopeOrHeartbeat(context.Background(), heartbeat)
	if err != nil || !pulsed || len(envelope.ID) != 0 || envelope.Method != "" || len(envelope.Params) != 0 || len(envelope.Result) != 0 || envelope.Error != nil {
		t.Fatalf("envelope=%#v pulsed=%v error=%v", envelope, pulsed, err)
	}
}

func TestRunDigBenchInterruptsTurnWhenStopped(t *testing.T) {
	maxLevel := 3
	service := &fakeDigBenchService{session: digbench.Session{
		Game: "P-1", SessionID: "session-1",
		State: digbench.State{Status: "in_progress", Level: 1, MaxLevel: &maxLevel, Observation: "___", Actions: []string{"b"}},
	}}
	server, requests := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{"data": []any{map[string]any{
			"model": "gpt-5.6-sol", "displayName": "GPT-5.6 Sol",
			"supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "high"}},
		}}})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-1"}, "model": "gpt-5.6-sol",
		})},
		benchmarkEnvelope{ID: rawJSON(3), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-1"}})},
	)
	server.experimentalAPI = true
	original := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string, string) (*appServerSession, error) { return server, nil }
	t.Cleanup(func() { openBenchmarkAppServer = original })

	ctx, cancel := context.WithCancel(context.Background())
	result, err := (Client{}).RunDigBench(ctx, service, DigBenchOptions{
		Game: "P-1", Model: "gpt-5.6-sol", Effort: "high", Timeout: time.Minute,
		Progress: func(progress DigBenchProgress) {
			if progress.Phase == DigBenchProgressTurn {
				cancel()
				go func() {
					time.Sleep(time.Millisecond)
					server.envelopes <- benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
						"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "interrupted"},
					})}
					server.envelopes <- benchmarkEnvelope{ID: rawJSON(4), Result: rawJSON(map[string]any{})}
				}()
			}
		},
	})
	if !errors.Is(err, context.Canceled) || result.Failure != "" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if !strings.Contains(requests.String(), `"method":"turn/interrupt"`) {
		t.Fatalf("turn interrupt was not requested: %s", requests.String())
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

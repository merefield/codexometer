package codex

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
	creativeToggle := "/"
	service := &fakeDigBenchService{session: digbench.Session{
		Game: "P-1", SessionID: "session-1", Seed: &seed, FrameworkVersion: &framework,
		State: digbench.State{
			Status: "in_progress", Level: 1, MaxLevel: &maxLevel, CreativeToggle: &creativeToggle,
			Observation: "___", Actions: []string{"b"},
		},
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
	if len(snapshots) < 3 || len(result.Interactions) != 9 ||
		result.Interactions[0].Kind != BenchmarkInteractionPolicy ||
		result.Interactions[1].Kind != BenchmarkInteractionPrompt ||
		result.Interactions[2].Kind != BenchmarkInteractionTools ||
		result.Interactions[3].Kind != BenchmarkInteractionState ||
		result.Interactions[4].Kind != BenchmarkInteractionTool ||
		result.Interactions[5].Kind != BenchmarkInteractionToolResponse ||
		result.Interactions[6].Kind != BenchmarkInteractionMove || result.Interactions[6].Content != "b" ||
		result.Interactions[7].Kind != BenchmarkInteractionState ||
		result.Interactions[8].Kind != BenchmarkInteractionResponse {
		t.Fatalf("DigBench snapshots/interactions = %d / %#v", len(snapshots), result.Interactions)
	}
	if !strings.Contains(result.Interactions[0].Content, "Play only the assigned DigBench session") ||
		!strings.Contains(result.Interactions[1].Content, `session_id="[REDACTED]"`) ||
		!strings.Contains(result.Interactions[1].Content, "Creative mode") ||
		!strings.Contains(result.Interactions[2].Content, `"get_session"`) ||
		!strings.Contains(result.Interactions[2].Content, `"inputSchema"`) ||
		result.Interactions[4].Content != `STEP // SESSION_ID [REDACTED] // STEP_INDEX 1 // ACTION "b"` ||
		!strings.Contains(result.Interactions[5].Content, "ACCEPTED") ||
		!strings.Contains(result.Interactions[5].Content, `"session_id": "[REDACTED]"`) ||
		!strings.Contains(result.Interactions[5].Content, `"framework_version": "engine-1"`) {
		t.Fatalf("DigBench authored context = %#v", result.Interactions[:6])
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

func TestRunDigBenchTimeoutIncludesAppServerStartup(t *testing.T) {
	original := openBenchmarkAppServer
	openBenchmarkAppServer = func(ctx context.Context, _, _ string) (*appServerSession, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	t.Cleanup(func() { openBenchmarkAppServer = original })

	startedAt := time.Now()
	_, err := (Client{}).RunDigBench(context.Background(), &fakeDigBenchService{}, DigBenchOptions{
		Game: "P-1", Model: "model", Effort: "high", Timeout: 10 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("startup timeout error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("startup exceeded configured timeout by too much: %s", elapsed)
	}
}

func TestDigBenchTranscriptPromptRedactsOnlyRuntimeSessionID(t *testing.T) {
	maxLevel := 3
	maxSteps := 100
	startingLives := 8
	mode := "creative"
	creativeToggle := "/"
	transition := "session-secret advanced"
	session := digbench.Session{
		Game: "P-1", SessionID: "session-secret", StepIndex: 4,
		Description: "Reach the marked state for session-secret using the special action.",
		State: digbench.State{
			Status: "in_progress", Level: 2, MaxLevel: &maxLevel, MaxSteps: &maxSteps,
			StartingLives: &startingLives, Mode: &mode, CreativeToggle: &creativeToggle,
			Transition: &transition, Observation: "session-secret sees ___",
			Actions: []string{"b", "session-secret-action"},
		},
	}
	runtimePrompt := digBenchPrompt(session)
	transcriptPrompt := digBenchTranscriptPrompt(session)
	if !strings.Contains(runtimePrompt, `session_id="session-secret"`) {
		t.Fatalf("runtime prompt lost assigned session: %q", runtimePrompt)
	}
	if strings.Contains(transcriptPrompt, "session-secret") || !strings.Contains(transcriptPrompt, `session_id="[REDACTED]"`) {
		t.Fatalf("transcript prompt did not safely redact session: %q", transcriptPrompt)
	}
	for _, shared := range []string{
		`game="P-1"`, "starting state (step_index=4)", "first move uses step_index=5",
		"TASK DESCRIPTION (objective + any special actions, NOT the rules)",
		"After each successful \"step\" call, wait for and inspect its returned authoritative state",
		"Never queue or precompute multiple future step calls against an unobserved state",
		`"creative_toggle":"/"`, `"max_steps":100`, `"starting_lives":8`,
	} {
		if !strings.Contains(runtimePrompt, shared) || !strings.Contains(transcriptPrompt, shared) {
			t.Fatalf("prompt variants do not share %q", shared)
		}
	}
	if !strings.Contains(runtimePrompt, "Reach the marked state for session-secret") ||
		!strings.Contains(runtimePrompt, `"observation":"session-secret sees ___"`) ||
		!strings.Contains(transcriptPrompt, "Reach the marked state for [REDACTED]") ||
		!strings.Contains(transcriptPrompt, `"observation":"[REDACTED] sees ___"`) ||
		!strings.Contains(transcriptPrompt, `"[REDACTED]-action"`) {
		t.Fatalf("state strings were not safely represented: %q", transcriptPrompt)
	}
	state := formatDigBenchState(session)
	if strings.Contains(state, "session-secret") || !strings.Contains(state, `"observation": "[REDACTED] sees ___"`) {
		t.Fatalf("state transcript leaked session ID: %s", state)
	}
}

func TestDigBenchPromptMarksMissingTaskDescription(t *testing.T) {
	prompt := digBenchPrompt(digbench.Session{Game: "P-1", SessionID: "session-1"})
	if !strings.Contains(prompt, "TASK DESCRIPTION") || !strings.Contains(prompt, "(none provided)") {
		t.Fatalf("missing description was not represented explicitly: %q", prompt)
	}
	if strings.Contains(prompt, "Important: creative mode") {
		t.Fatalf("creative-mode guidance was shown without a toggle: %q", prompt)
	}
}

func TestDigBenchPromptUsesProvidedCreativeToggle(t *testing.T) {
	toggle := "creative-mode"
	prompt := digBenchPrompt(digbench.Session{
		Game: "P-1", SessionID: "session-1",
		State: digbench.State{CreativeToggle: &toggle},
	})
	for _, expected := range []string{
		"Important: creative mode",
		`Call the "step" tool with action "creative-mode" to enter creative mode.`,
		`Only submit "creative-mode" when it appears in the state's actions list.`,
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("creative-mode guidance missing %q: %q", expected, prompt)
		}
	}
	if strings.Contains(prompt, `action "/"`) {
		t.Fatalf("creative-mode guidance used a hard-coded toggle: %q", prompt)
	}
}

func TestDigBenchAgentResponseIsCompactWhileTranscriptRetainsFullPayload(t *testing.T) {
	framework := "engine-1"
	seed := int64(42)
	maxLevel := 5
	invalid := false
	response := digbench.StepResponse{
		Session: digbench.Session{
			Description: "objective", FrameworkVersion: &framework, Game: "P-2",
			MoveSchema: json.RawMessage(`{"type":"object"}`), Seed: &seed,
			SessionID: "session-secret", StepIndex: 7, LevelsBeaten: 1,
			State: digbench.State{Observation: "abc", Actions: []string{"x"}, Level: 2, MaxLevel: &maxLevel, Status: "in_progress"},
		},
		InvalidAction: &invalid,
	}
	handled := digBenchSuccessfulToolCall(response, digBenchCompactStep(response), nil)
	agent := handled.agent["contentItems"].([]map[string]string)[0]["text"]
	transcript := handled.transcript["contentItems"].([]map[string]string)[0]["text"]
	for _, expected := range []string{`"step_index":7`, `"observation":"abc"`, `"actions":["x"]`, `"invalid_action":false`} {
		if !strings.Contains(agent, expected) {
			t.Fatalf("compact agent response missing %q: %s", expected, agent)
		}
	}
	for _, excluded := range []string{"session-secret", "framework_version", "move_schema", "objective", `"seed"`, `"levels_beaten"`} {
		if strings.Contains(agent, excluded) {
			t.Fatalf("compact agent response retained %q: %s", excluded, agent)
		}
	}
	for _, retained := range []string{"session-secret", "framework_version", "move_schema", "objective", `"seed"`, `"levels_beaten":1`} {
		if !strings.Contains(transcript, retained) {
			t.Fatalf("full transcript response lost %q: %s", retained, transcript)
		}
	}
}

func TestDefaultDigBenchTimeoutAllowsLongDiscoveryRuns(t *testing.T) {
	if DefaultDigBenchTimeout != 2*time.Hour {
		t.Fatalf("default timeout = %s", DefaultDigBenchTimeout)
	}
}

func TestDigBenchToolResponseTranscriptRedactsSensitiveIdentifiers(t *testing.T) {
	response := digBenchToolResponse(map[string]any{
		"session_id": "session-secret",
		"threadId":   "thread-secret",
		"token":      "token-secret",
		"state": map[string]any{
			"observation": "session-secret reached the door",
			"actions":     []any{"left", "right"},
		},
	}, nil)
	formatted := formatDigBenchToolResponse(response, true, "session-secret")
	for _, secret := range []string{"session-secret", "thread-secret", "token-secret"} {
		if strings.Contains(formatted, secret) {
			t.Fatalf("tool response leaked %q: %s", secret, formatted)
		}
	}
	for _, visible := range []string{"ACCEPTED", `"session_id": "[REDACTED]"`, `"threadId": "[REDACTED]"`, `"token": "[REDACTED]"`, `"actions"`, `"left"`} {
		if !strings.Contains(formatted, visible) {
			t.Fatalf("sanitized tool response missing %q: %s", visible, formatted)
		}
	}
	rejected := formatDigBenchToolResponse(digBenchToolResponse(nil, errors.New("session-secret was rejected")), false, "session-secret")
	if !strings.Contains(rejected, "REJECTED") || !strings.Contains(rejected, "[REDACTED] was rejected") || strings.Contains(rejected, "session-secret") {
		t.Fatalf("rejected tool response was not safely documented: %s", rejected)
	}
}

func TestDigBenchRuntimeTextSanitizationCoversFailuresAndFinalResponse(t *testing.T) {
	const (
		sessionID = "session-secret"
		threadID  = "thread-secret"
		turnID    = "turn-secret"
		workspace = "/tmp/codexometer-digbench-secret"
	)
	unsafe := sessionID + " " + threadID + " " + turnID + " " + workspace + "\x1b]52;c;payload\a\nkept\tformat"

	redacted := redactDigBenchText(unsafe, sessionID, threadID, turnID, workspace)
	assertSafeDigBenchText(t, redacted, sessionID, threadID, turnID, workspace)
	if !strings.Contains(redacted, "\nkept\tformat") {
		t.Fatalf("intentional formatting whitespace was removed: %q", redacted)
	}

	completed := rawJSON(map[string]any{
		"threadId": threadID,
		"turn": map[string]any{
			"id": turnID, "status": "failed",
			"error": map[string]string{"message": unsafe},
			"items": []any{map[string]string{"type": "agentMessage", "text": unsafe}},
		},
	})
	failure := ""
	if !digBenchTurnCompleted(completed, threadID, turnID, &failure, sessionID, threadID, turnID, workspace) {
		t.Fatal("matching failed turn was not recognized")
	}
	assertSafeDigBenchText(t, failure, sessionID, threadID, turnID, workspace)
	final := digBenchFinalResponse(completed, threadID, turnID, sessionID, threadID, turnID, workspace)
	assertSafeDigBenchText(t, final, sessionID, threadID, turnID, workspace)
}

func assertSafeDigBenchText(t *testing.T, value string, sensitiveValues ...string) {
	t.Helper()
	for _, sensitive := range sensitiveValues {
		if strings.Contains(value, sensitive) {
			t.Fatalf("runtime value %q leaked in %q", sensitive, value)
		}
	}
	for _, character := range value {
		if character != '\n' && character != '\t' && (character < 0x20 || (character >= 0x7f && character <= 0x9f)) {
			t.Fatalf("terminal control U+%04X remained in %q", character, value)
		}
	}
}

func TestClientBenchmarkTasksRequireDigBenchToken(t *testing.T) {
	plain := (Client{}).BenchmarkTasks()
	enabled := (Client{DigBenchToken: "secret", DigBenchGames: []string{"P-1"}}).BenchmarkTasks()
	if len(enabled) != len(plain)+1 || enabled[len(enabled)-1] != DigBenchTask() {
		t.Fatalf("plain=%#v enabled=%#v", plain, enabled)
	}
}

func TestDigBenchBenchmarkSuiteRunsSelectedDiscoveredGames(t *testing.T) {
	server, _ := newFakeBenchmarkServer(benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{"data": []any{map[string]any{
		"model": "model", "displayName": "Model",
		"supportedReasoningEfforts": []any{
			map[string]string{"reasoningEffort": "high"},
			map[string]string{"reasoningEffort": "medium"},
		},
	}}})})
	originalOpen := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string, string) (*appServerSession, error) { return server, nil }
	t.Cleanup(func() { openBenchmarkAppServer = originalOpen })

	originalRun := runDigBenchTrial
	var trials []string
	runDigBenchTrial = func(_ context.Context, _ Client, _ digBenchService, options DigBenchOptions) (DigBenchResult, error) {
		trials = append(trials, options.Game+"/"+options.Effort)
		// DigBench step payloads do not need to repeat session-static identity.
		// The suite must still publish a stable active row for this snapshot.
		options.Snapshot(DigBenchResult{Status: "in_progress", CurrentLevel: 2})
		result := DigBenchResult{Game: options.Game, Model: options.Model, DisplayName: "Model", ActualModel: options.Model, Effort: options.Effort, Won: true, Status: "completed"}
		options.Snapshot(result)
		return result, nil
	}
	t.Cleanup(func() { runDigBenchTrial = originalRun })

	var events []BenchmarkEvent
	client := Client{DigBenchToken: "secret", DigBenchGames: []string{"P-1", "P-2", "P-3"}}
	client.RunBenchmarkSuiteScoped(context.Background(), []BenchmarkTaskID{BenchmarkDigBench}, BenchmarkScope{
		Models: []string{"model"}, Efforts: []string{"high", "medium"}, Games: []string{"P-2", "P-3"},
	}, func(event BenchmarkEvent) { events = append(events, event) })
	wantTrials := []string{"P-2/high", "P-2/medium", "P-3/high", "P-3/medium"}
	if !slices.Equal(trials, wantTrials) {
		t.Fatalf("selected trials run = %#v, want %#v", trials, wantTrials)
	}
	results := 0
	for _, event := range events {
		if event.Active != nil {
			if event.Active.Provider != "digbench" || !strings.HasPrefix(event.Active.TaskName, "DIGBENCH P-") || event.Active.Model == "" || event.Active.DisplayName == "" || event.Active.Effort == "" {
				t.Fatalf("active DigBench identity was not preserved: %#v", *event.Active)
			}
		}
		if event.Result != nil {
			results++
		}
	}
	final := events[len(events)-1]
	if results != 4 || !final.Done || final.Total != 4 || final.Completed != 4 || final.Combinations != 2 || final.Err != nil {
		t.Fatalf("events = %#v", events)
	}
}

func TestDigBenchBenchmarkSuitePersistsSanitizedRunnerErrorsAsIncomplete(t *testing.T) {
	server, _ := newFakeBenchmarkServer(benchmarkEnvelope{
		ID: rawJSON(1),
		Result: rawJSON(map[string]any{"data": []any{map[string]any{
			"model": "model", "displayName": "Model",
			"supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "high"}},
		}}}),
	})
	originalOpen := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string, string) (*appServerSession, error) { return server, nil }
	t.Cleanup(func() { openBenchmarkAppServer = originalOpen })

	originalRun := runDigBenchTrial
	runDigBenchTrial = func(_ context.Context, _ Client, _ digBenchService, options DigBenchOptions) (DigBenchResult, error) {
		return DigBenchResult{Game: options.Game, Model: options.Model, Effort: options.Effort}, errors.New("transport \x1b]52;c;payload\a failed")
	}
	t.Cleanup(func() { runDigBenchTrial = originalRun })

	var events []BenchmarkEvent
	client := Client{DigBenchToken: "secret", DigBenchGames: []string{"P-1"}}
	client.RunBenchmarkSuiteScoped(context.Background(), []BenchmarkTaskID{BenchmarkDigBench}, BenchmarkScope{
		Models: []string{"model"}, Efforts: []string{"high"}, Games: []string{"P-1"},
	}, func(event BenchmarkEvent) { events = append(events, event) })

	var result *BenchmarkResult
	for _, event := range events {
		if event.Result != nil {
			copy := *event.Result
			result = &copy
		}
	}
	if result == nil || result.Correct || result.Failure == "" {
		t.Fatalf("failed trial was not persisted as incomplete: %#v", result)
	}
	assertSafeDigBenchText(t, result.Failure)
}

func TestApplyDigBenchSessionPreservesStaticMetadataFromInitialSession(t *testing.T) {
	seed := int64(42)
	framework := "engine-1"
	maxLevel := 14
	result := DigBenchResult{Game: "P-1", Seed: &seed, FrameworkVersion: framework, MaxLevel: maxLevel}

	applyDigBenchSession(&result, digbench.Session{
		SessionID: "session-1", StepIndex: 211, LevelsBeaten: 6,
		State: digbench.State{Status: "in_progress", Level: 7},
	})

	if result.Game != "P-1" || result.Seed == nil || *result.Seed != seed || result.FrameworkVersion != framework || result.MaxLevel != maxLevel {
		t.Fatalf("partial session erased static metadata: %#v", result)
	}
	if result.Status != "in_progress" || result.CurrentLevel != 7 || result.LevelsBeaten != 6 || result.Steps != 211 {
		t.Fatalf("partial session did not update progress: %#v", result)
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
	if response.agent["success"] != false || service.stepCalls != 0 {
		t.Fatalf("response = %#v, calls = %d", response, service.stepCalls)
	}
	content := response.agent["contentItems"].([]map[string]string)
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

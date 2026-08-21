package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/merefield/codexometer/internal/digbench"
)

const (
	defaultDigBenchTimeout          = 30 * time.Minute
	defaultDigBenchProgressInterval = 15 * time.Second
)

// DigBenchProgressPhase identifies a safe, content-free runner milestone.
type DigBenchProgressPhase string

const (
	DigBenchProgressSession   DigBenchProgressPhase = "session"
	DigBenchProgressTurn      DigBenchProgressPhase = "turn"
	DigBenchProgressUpdate    DigBenchProgressPhase = "update"
	DigBenchProgressHeartbeat DigBenchProgressPhase = "heartbeat"
)

// DigBenchProgress reports authoritative game counters without exposing the
// session ID, observations, actions, reasoning, or credentials.
type DigBenchProgress struct {
	Phase        DigBenchProgressPhase
	Level        int
	LevelsBeaten int
	MaxLevel     int
	Steps        int
	Status       string
	Elapsed      time.Duration
	ActualModel  string
}

// DigBenchOptions selects one deliberately bounded external benchmark run.
type DigBenchOptions struct {
	Game          string
	Model         string
	Effort        string
	Timeout       time.Duration
	ClientVersion string
	Progress      func(DigBenchProgress)
}

// DigBenchResult is the terminal state and Codex telemetry for one game.
type DigBenchResult struct {
	Game             string
	Model            string
	DisplayName      string
	Effort           string
	ActualModel      string
	Won              bool
	Status           string
	CurrentLevel     int
	LevelsBeaten     int
	MaxLevel         int
	Steps            int
	Seed             *int64
	FrameworkVersion string
	Duration         time.Duration
	Usage            BenchmarkUsage
	UsageObserved    bool
	UsageKnown       bool
	UsageIssue       string
	UsageSource      BenchmarkUsageSource
	ResponseUsage    []BenchmarkResponseUsage
	CostUSD          float64
	CostKnown        bool
	CostIssue        string
	Failure          string
}

type digBenchService interface {
	StartSession(context.Context, digbench.StartRequest) (digbench.Session, error)
	GetSession(context.Context, string) (digbench.Session, error)
	Step(context.Context, string, digbench.StepRequest) (digbench.StepResponse, error)
}

// RunDigBench runs one game through Codex's agentic tool loop. The remote game
// session is created only after the requested model and effort are validated.
func (c Client) RunDigBench(ctx context.Context, service digBenchService, options DigBenchOptions) (DigBenchResult, error) {
	if service == nil {
		return DigBenchResult{}, errors.New("DigBench client is required")
	}
	options.Game = strings.TrimSpace(options.Game)
	options.Model = strings.TrimSpace(options.Model)
	options.Effort = strings.TrimSpace(options.Effort)
	if options.Game == "" || options.Model == "" || options.Effort == "" {
		return DigBenchResult{}, errors.New("DigBench game, model, and effort are required")
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultDigBenchTimeout
	}

	server, err := openBenchmarkAppServer(ctx, c.Binary, c.BenchmarkAPIKey)
	if err != nil {
		return DigBenchResult{}, err
	}
	defer server.close()
	if !server.experimentalAPI {
		return DigBenchResult{}, errors.New("DigBench requires a Codex CLI with experimental dynamic-tool support")
	}
	models, err := server.models(ctx)
	if err != nil {
		return DigBenchResult{}, err
	}
	combination, err := findDigBenchCombination(models, options.Model, options.Effort)
	if err != nil {
		return DigBenchResult{}, err
	}
	return server.runDigBench(ctx, service, combination, options)
}

func findDigBenchCombination(models []benchmarkModel, modelID, effort string) (benchmarkCombination, error) {
	for _, combination := range benchmarkCombinations(models) {
		if combination.model.Model == modelID && combination.effort == effort {
			return combination, nil
		}
	}
	return benchmarkCombination{}, fmt.Errorf("Codex model/effort combination %s/%s is unavailable", modelID, effort)
}

func (s *appServerSession) runDigBench(
	ctx context.Context,
	service digBenchService,
	combination benchmarkCombination,
	options DigBenchOptions,
) (DigBenchResult, error) {
	result := DigBenchResult{
		Game: options.Game, Model: combination.model.Model,
		DisplayName: combination.model.DisplayName, Effort: combination.effort,
		ActualModel: combination.model.Model,
	}
	runCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()

	temporary, err := os.MkdirTemp("", "codexometer-digbench-")
	if err != nil {
		return result, fmt.Errorf("create isolated DigBench workspace: %w", err)
	}
	defer os.RemoveAll(temporary)

	threadParams := map[string]any{
		"model": combination.model.Model, "cwd": temporary,
		"approvalPolicy": "never", "sandbox": "workspace-write", "ephemeral": true,
		"serviceName":           "codexometer-digbench",
		"developerInstructions": "Play only the assigned DigBench session. Use its scoped tools for game access, keep useful notes in the isolated workspace if needed, and stop calling tools as soon as the game is done.",
		"dynamicTools":          digBenchDynamicTools(),
	}
	if s.experimentalRawEvents {
		threadParams["experimentalRawEvents"] = true
	}
	threadResult, err := s.call(runCtx, "thread/start", threadParams, nil)
	if err != nil && s.experimentalRawEvents && experimentalAPIUnsupported(err) {
		s.experimentalRawEvents = false
		delete(threadParams, "experimentalRawEvents")
		threadResult, err = s.call(runCtx, "thread/start", threadParams, nil)
	}
	if err != nil {
		return result, fmt.Errorf("start DigBench Codex thread: %w", err)
	}
	var startedThread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(threadResult, &startedThread); err != nil || startedThread.Thread.ID == "" {
		return result, errors.New("Codex returned an invalid DigBench thread")
	}
	if startedThread.Model != "" {
		result.ActualModel = startedThread.Model
	}

	modelName := combination.model.Model + "/" + combination.effort + " via Codexometer"
	modelVersion := strings.TrimSpace(options.ClientVersion)
	startRequest := digbench.StartRequest{Game: options.Game, ModelName: &modelName}
	if modelVersion != "" {
		startRequest.ModelVersion = &modelVersion
	}
	session, err := service.StartSession(runCtx, startRequest)
	if err != nil {
		return result, fmt.Errorf("start DigBench game: %w", err)
	}
	applyDigBenchSession(&result, session)

	startedAt := time.Now()
	emitDigBenchProgress(options.Progress, DigBenchProgressSession, result, startedAt)
	turnParams := map[string]any{
		"threadId": startedThread.Thread.ID,
		"input":    []map[string]any{{"type": "text", "text": digBenchPrompt(session)}},
	}
	if combination.effort != "default" && combination.effort != "" {
		turnParams["effort"] = combination.effort
	}
	turnResponse, err := s.call(runCtx, "turn/start", turnParams, nil)
	if err != nil {
		return result, fmt.Errorf("start DigBench turn: %w", err)
	}
	var startedTurn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(turnResponse, &startedTurn); err != nil || startedTurn.Turn.ID == "" {
		return result, errors.New("Codex returned an invalid DigBench turn")
	}
	emitDigBenchProgress(options.Progress, DigBenchProgressTurn, result, startedAt)

	telemetry := newBenchmarkTelemetry()
	completed := false
	turnFailure := ""
	progressTicker := time.NewTicker(defaultDigBenchProgressInterval)
	defer progressTicker.Stop()
	for !completed {
		envelope, heartbeat, envelopeErr := s.nextPendingEnvelopeOrHeartbeat(runCtx, progressTicker.C)
		if heartbeat {
			emitDigBenchProgress(options.Progress, DigBenchProgressHeartbeat, result, startedAt)
			continue
		}
		if envelopeErr != nil {
			result.Duration = time.Since(startedAt)
			if recoverableBenchmarkTimeout(ctx, runCtx, envelopeErr) {
				result.Failure = fmt.Sprintf("DigBench run timed out after %s", options.Timeout)
				cleanupCompleted := false
				handle := func(method string, params json.RawMessage) bool {
					if method == "turn/completed" && digBenchTurnCompleted(params, startedThread.Thread.ID, startedTurn.Turn.ID, &turnFailure) {
						cleanupCompleted = true
						return true
					}
					return false
				}
				if interruptErr := s.interruptBenchmarkTurn(ctx, startedThread.Thread.ID, startedTurn.Turn.ID, &cleanupCompleted, handle); interruptErr != nil {
					return result, fmt.Errorf("interrupt timed-out DigBench turn: %w", interruptErr)
				}
				applyDigBenchMeasurements(&result, telemetry)
				if session.Done {
					result.Failure = ""
				}
				return result, nil
			}
			return result, envelopeErr
		}
		if len(envelope.ID) > 0 && envelope.Method != "" {
			if envelope.Method != "item/tool/call" {
				return result, fmt.Errorf("unsupported Codex app-server request %q", envelope.Method)
			}
			response := handleDigBenchToolCall(
				runCtx, service, startedThread.Thread.ID, startedTurn.Turn.ID,
				session.SessionID, envelope.Params, &session,
			)
			applyDigBenchSession(&result, session)
			if response["success"] == true {
				emitDigBenchProgress(options.Progress, DigBenchProgressUpdate, result, startedAt)
			}
			if err := s.respond(envelope.ID, response); err != nil {
				return result, fmt.Errorf("respond to DigBench tool call: %w", err)
			}
			continue
		}
		switch envelope.Method {
		case "thread/tokenUsage/updated":
			var event struct {
				ThreadID   string `json:"threadId"`
				TurnID     string `json:"turnId"`
				TokenUsage struct {
					Total BenchmarkUsage `json:"total"`
				} `json:"tokenUsage"`
			}
			if json.Unmarshal(envelope.Params, &event) == nil && event.ThreadID == startedThread.Thread.ID && event.TurnID == startedTurn.Turn.ID {
				telemetry.recordCumulative(event.TokenUsage.Total)
			}
		case "rawResponse/completed":
			var event struct {
				ThreadID   string          `json:"threadId"`
				TurnID     string          `json:"turnId"`
				ResponseID string          `json:"responseId"`
				Usage      *BenchmarkUsage `json:"usage"`
			}
			if json.Unmarshal(envelope.Params, &event) == nil && event.ThreadID == startedThread.Thread.ID && event.TurnID == startedTurn.Turn.ID {
				telemetry.recordRawResponse(event.ResponseID, event.Usage)
			}
		case "model/rerouted":
			var event struct {
				ThreadID string `json:"threadId"`
				TurnID   string `json:"turnId"`
				ToModel  string `json:"toModel"`
			}
			if json.Unmarshal(envelope.Params, &event) == nil && event.ThreadID == startedThread.Thread.ID && event.TurnID == startedTurn.Turn.ID && event.ToModel != "" {
				result.ActualModel = event.ToModel
			}
		case "turn/completed":
			completed = digBenchTurnCompleted(envelope.Params, startedThread.Thread.ID, startedTurn.Turn.ID, &turnFailure)
		}
	}

	result.Duration = time.Since(startedAt)
	applyDigBenchMeasurements(&result, telemetry)
	if session.Done {
		result.Won = session.State.Status == "completed"
		return result, nil
	}
	if turnFailure != "" {
		result.Failure = turnFailure
		return result, nil
	}
	result.Failure = "Codex stopped before the DigBench game ended"
	return result, nil
}

func emitDigBenchProgress(callback func(DigBenchProgress), phase DigBenchProgressPhase, result DigBenchResult, startedAt time.Time) {
	if callback == nil {
		return
	}
	callback(DigBenchProgress{
		Phase: phase, Level: result.CurrentLevel, LevelsBeaten: result.LevelsBeaten,
		MaxLevel: result.MaxLevel, Steps: result.Steps, Status: result.Status, Elapsed: time.Since(startedAt),
		ActualModel: result.ActualModel,
	})
}

func digBenchDynamicTools() []map[string]any {
	sessionProperty := map[string]any{"type": "string", "description": "The assigned DigBench session_id."}
	return []map[string]any{
		{
			"type": "function", "name": "get_session",
			"description": "Re-read the authoritative state of the assigned DigBench game session.",
			"inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{"session_id": sessionProperty},
				"required": []string{"session_id"}, "additionalProperties": false,
			},
		},
		{
			"type": "function", "name": "step",
			"description": "Apply exactly one legal action to the assigned DigBench game session.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": sessionProperty,
					"step_index": map[string]any{"type": "integer", "minimum": 1, "description": "The server's last returned step_index plus one."},
					"action":     map[string]any{"type": "string", "description": "Exactly one string from the current state's actions list."},
				},
				"required": []string{"session_id", "step_index", "action"}, "additionalProperties": false,
			},
		},
	}
}

func digBenchPrompt(session digbench.Session) string {
	state, _ := json.Marshal(session.State)
	return fmt.Sprintf(`We are not going to tell you the rules of this game—you have to figure them out for yourself.

The aim is to reach as high a level as possible. You advance by reaching unknown states. Each level has a limited number of steps; exhausting them or reaching certain states can cost a life. Losing all lives ends the game.

Creative mode is available when "/" appears in the state's actions list. Use the step tool with action "/" to enter or leave it. Creative mode permits experimentation without consuming ordinary steps or lives, but has its own finite budget.

Your game session is already started: session_id=%q, game=%q. Do not start or choose another game. Use only get_session and step for game access, and pass this session_id to every call.

Starting step_index: %d. Your first move must use step_index %d.
Initial state:
%s

Each move must use exactly one string from the latest state's actions list and the latest returned step_index plus one. Infer the rules from state changes, keep useful notes if helpful, and continue at a deliberate but efficient pace until state.done is true. Then stop making moves and give a concise debrief of the mechanics, objective, strategy, and uncertainties.`,
		session.SessionID, session.Game, session.StepIndex, session.StepIndex+1, state)
}

func handleDigBenchToolCall(
	ctx context.Context,
	service digBenchService,
	threadID string,
	turnID string,
	sessionID string,
	params json.RawMessage,
	current *digbench.Session,
) map[string]any {
	var call struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return digBenchToolResponse(nil, fmt.Errorf("decode tool request: %w", err))
	}
	if call.ThreadID != threadID || call.TurnID != turnID {
		return digBenchToolResponse(nil, errors.New("access denied: tool call belongs to another Codex turn"))
	}
	var common struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(call.Arguments, &common); err != nil {
		return digBenchToolResponse(nil, fmt.Errorf("decode %s arguments: %w", call.Tool, err))
	}
	if common.SessionID != sessionID {
		return digBenchToolResponse(nil, errors.New("access denied: tool is scoped to the assigned session"))
	}
	switch call.Tool {
	case "get_session":
		session, err := service.GetSession(ctx, sessionID)
		if err == nil {
			*current = session
		}
		return digBenchToolResponse(session, err)
	case "step":
		var arguments struct {
			SessionID string `json:"session_id"`
			StepIndex int    `json:"step_index"`
			Action    string `json:"action"`
		}
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
			return digBenchToolResponse(nil, fmt.Errorf("decode step arguments: %w", err))
		}
		response, err := service.Step(ctx, sessionID, digbench.StepRequest{Action: arguments.Action, StepIndex: arguments.StepIndex})
		if err == nil {
			*current = response.Session
		}
		return digBenchToolResponse(response, err)
	default:
		return digBenchToolResponse(nil, fmt.Errorf("unknown DigBench tool %q", call.Tool))
	}
}

func digBenchToolResponse(value any, err error) map[string]any {
	success := err == nil
	var data []byte
	if err != nil {
		data, _ = json.Marshal(map[string]string{"error": err.Error()})
	} else {
		data, err = json.Marshal(value)
		if err != nil {
			success = false
			data, _ = json.Marshal(map[string]string{"error": "encode DigBench tool response: " + err.Error()})
		}
	}
	return map[string]any{
		"contentItems": []map[string]string{{"type": "inputText", "text": string(data)}},
		"success":      success,
	}
}

func applyDigBenchSession(result *DigBenchResult, session digbench.Session) {
	result.Game = session.Game
	result.Status = session.State.Status
	result.CurrentLevel = session.State.Level
	result.LevelsBeaten = session.LevelsBeaten
	result.Steps = session.StepIndex
	result.Seed = session.Seed
	if session.State.MaxLevel != nil {
		result.MaxLevel = *session.State.MaxLevel
	}
	if session.FrameworkVersion != nil {
		result.FrameworkVersion = *session.FrameworkVersion
	}
	result.Won = session.Done && session.State.Done && session.State.Status == "completed"
}

func digBenchTurnCompleted(params json.RawMessage, threadID, turnID string, failure *string) bool {
	var event struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &event) != nil || event.ThreadID != threadID || event.Turn.ID != turnID {
		return false
	}
	if event.Turn.Status != "completed" {
		*failure = "turn " + event.Turn.Status
		if event.Turn.Error != nil && event.Turn.Error.Message != "" {
			*failure += ": " + event.Turn.Error.Message
		}
	}
	return true
}

func applyDigBenchMeasurements(result *DigBenchResult, telemetry *benchmarkTelemetry) {
	var measurements BenchmarkResult
	telemetry.apply(&measurements)
	result.Usage = measurements.Usage
	result.UsageObserved = measurements.UsageObserved
	result.UsageKnown = measurements.UsageKnown
	result.UsageIssue = measurements.UsageIssue
	result.UsageSource = measurements.UsageSource
	result.ResponseUsage = measurements.ResponseUsage
	if !result.UsageKnown {
		result.CostIssue = result.UsageIssue
		return
	}
	if result.UsageSource != BenchmarkUsageRawResponses {
		result.CostUSD, result.CostKnown, result.CostIssue = estimateAPICostWithIssue(result.ActualModel, result.Usage)
		return
	}
	result.CostKnown = true
	for _, response := range result.ResponseUsage {
		cost, known, issue := estimateSingleResponseAPICostWithIssue(result.ActualModel, response.Usage)
		if !known {
			result.CostUSD, result.CostKnown, result.CostIssue = 0, false, issue
			return
		}
		result.CostUSD += cost
	}
}

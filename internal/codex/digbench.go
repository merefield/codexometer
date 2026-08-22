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
	// DefaultDigBenchTimeout gives discovery-heavy games enough room to finish
	// while retaining a finite safety boundary. Headless callers can override it.
	DefaultDigBenchTimeout          = 2 * time.Hour
	defaultDigBenchProgressInterval = 15 * time.Second
	digBenchDeveloperInstructions   = "Play only the assigned DigBench session. Use its scoped tools for game access, keep useful notes and hypotheses in the isolated workspace if needed, inspect every returned state before choosing the next move, and stop calling tools as soon as the game is done."
)

var errDigBenchTimeout = errors.New("DigBench timeout reached")

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
	Snapshot      func(DigBenchResult)
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
	Interactions     []BenchmarkInteraction
}

type digBenchService interface {
	StartSession(context.Context, digbench.StartRequest) (digbench.Session, error)
	GetSession(context.Context, string) (digbench.Session, error)
	Step(context.Context, string, digbench.StepRequest) (digbench.StepResponse, error)
}

var newDigBenchService = func(token string) digBenchService {
	return digbench.Client{Token: token}
}

var runDigBenchTrial = func(ctx context.Context, client Client, service digBenchService, options DigBenchOptions) (DigBenchResult, error) {
	return client.RunDigBench(ctx, service, options)
}

// BenchmarkTasks exposes the external suite only when this process received a
// DigBench token and discovered at least one game.
func (c Client) BenchmarkTasks() []BenchmarkTask {
	tasks := BenchmarkTasks()
	if strings.TrimSpace(c.DigBenchToken) != "" && len(c.DigBenchGames) > 0 {
		tasks = append(tasks, DigBenchTask())
	}
	return tasks
}

func (c Client) runDigBenchBenchmarkSuite(ctx context.Context, taskID BenchmarkTaskID, emit func(BenchmarkEvent), scopes ...BenchmarkScope) {
	if strings.TrimSpace(c.DigBenchToken) == "" {
		emit(benchmarkTerminalEvent(ctx, 0, 0, 0, errors.New("DIGBENCH_API_TOKEN is required for DigBench")))
		return
	}
	plan, err := c.BenchmarkPlan(ctx)
	if err != nil {
		emit(benchmarkTerminalEvent(ctx, 0, 0, 0, err))
		return
	}
	scope := plan.AllScope()
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	models, efforts := stringSet(scope.Models), stringSet(scope.Efforts)
	var selected []struct{ model, name, effort string }
	for _, model := range plan.Models {
		if !models[model.Model] {
			continue
		}
		for _, effort := range model.Efforts {
			if efforts[effort] {
				selected = append(selected, struct{ model, name, effort string }{model.Model, model.DisplayName, effort})
			}
		}
	}
	if len(selected) == 0 {
		emit(benchmarkTerminalEvent(ctx, 0, 0, 0, errors.New("selected DigBench scope has no compatible model/effort pairs")))
		return
	}
	availableGames := stringSet(plan.Games)
	games := make([]string, 0, len(scope.Games))
	for _, game := range scope.Games {
		if availableGames[game] {
			games = append(games, game)
		}
	}
	if len(games) == 0 {
		emit(benchmarkTerminalEvent(ctx, 0, 0, 0, errors.New("select at least one DigBench game")))
		return
	}
	total, completed := len(games)*len(selected), 0
	emit(BenchmarkEvent{Total: total, Combinations: len(selected)})
	for _, game := range games {
		for _, choice := range selected {
			event := BenchmarkEvent{Total: total, Completed: completed, Combinations: len(selected), CurrentTaskID: taskID, CurrentTask: "DIGBENCH " + game, CurrentModel: choice.name, CurrentEffort: choice.effort}
			pending := BenchmarkResult{
				TaskID: taskID, TaskName: "DIGBENCH " + game, Provider: "digbench",
				Model: choice.model, DisplayName: choice.name, ActualModel: choice.model, Effort: choice.effort,
				GameStatus: "connecting",
			}
			event.Active = &pending
			emit(event)
			result, runErr := runDigBenchTrial(ctx, c, newDigBenchService(c.DigBenchToken), DigBenchOptions{
				Game: game, Model: choice.model, Effort: choice.effort,
				Snapshot: func(snapshot DigBenchResult) {
					fillDigBenchIdentity(&snapshot, game, choice.model, choice.name, choice.effort)
					active := benchmarkResultFromDigBench(taskID, snapshot)
					event.Active = &active
					emit(event)
				},
			})
			fillDigBenchIdentity(&result, game, choice.model, choice.name, choice.effort)
			if runErr != nil && result.Failure == "" && !errors.Is(ctx.Err(), context.Canceled) {
				result.Failure = redactDigBenchText(runErr.Error())
			}
			result.Failure = redactDigBenchText(result.Failure)
			converted := benchmarkResultFromDigBench(taskID, result)
			if errors.Is(ctx.Err(), context.Canceled) {
				markBenchmarkStopped(&converted)
				event.Active, event.Result, event.Stopped = nil, &converted, true
				emit(event)
				emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(selected), Stopped: true, Done: true})
				return
			}
			completed++
			event.Completed, event.Active, event.Result = completed, nil, &converted
			emit(event)
			if runErr != nil {
				emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(selected), Done: true, Err: runErr})
				return
			}
		}
	}
	emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(selected), Done: true})
}

func fillDigBenchIdentity(result *DigBenchResult, game, model, displayName, effort string) {
	if strings.TrimSpace(result.Game) == "" {
		result.Game = game
	}
	if strings.TrimSpace(result.Model) == "" {
		result.Model = model
	}
	if strings.TrimSpace(result.DisplayName) == "" {
		result.DisplayName = displayName
	}
	if strings.TrimSpace(result.ActualModel) == "" {
		result.ActualModel = model
	}
	if strings.TrimSpace(result.Effort) == "" {
		result.Effort = effort
	}
}

func benchmarkResultFromDigBench(taskID BenchmarkTaskID, result DigBenchResult) BenchmarkResult {
	return BenchmarkResult{
		TaskID: taskID, TaskName: "DIGBENCH " + result.Game,
		Model: result.Model, DisplayName: result.DisplayName, Effort: result.Effort, ActualModel: result.ActualModel,
		Correct: result.Won, Duration: result.Duration, Usage: result.Usage, UsageObserved: result.UsageObserved,
		UsageKnown: result.UsageKnown, UsageIssue: result.UsageIssue, UsageSource: result.UsageSource,
		ResponseUsage: append([]BenchmarkResponseUsage(nil), result.ResponseUsage...), CostUSD: result.CostUSD,
		CostKnown: result.CostKnown, CostIssue: result.CostIssue, Failure: result.Failure,
		Interactions: append([]BenchmarkInteraction(nil), result.Interactions...), Provider: "digbench",
		GameStatus: result.Status, CurrentLevel: result.CurrentLevel, LevelsBeaten: result.LevelsBeaten,
		MaxLevel: result.MaxLevel, Steps: result.Steps,
	}
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
		options.Timeout = DefaultDigBenchTimeout
	}
	runCtx, cancel := context.WithTimeoutCause(ctx, options.Timeout, errDigBenchTimeout)
	defer cancel()

	server, err := openBenchmarkAppServer(runCtx, c.Binary, c.BenchmarkAPIKey)
	if err != nil {
		return DigBenchResult{}, sanitizeDigBenchError(err)
	}
	defer server.close()
	if !server.experimentalAPI {
		return DigBenchResult{}, errors.New("DigBench requires a Codex CLI with experimental dynamic-tool support")
	}
	models, err := server.models(runCtx)
	if err != nil {
		return DigBenchResult{}, sanitizeDigBenchError(err)
	}
	combination, err := findDigBenchCombination(models, options.Model, options.Effort)
	if err != nil {
		return DigBenchResult{}, err
	}
	return server.runDigBench(runCtx, service, combination, options)
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
) (result DigBenchResult, runErr error) {
	result = DigBenchResult{
		Game: options.Game, Model: combination.model.Model,
		DisplayName: combination.model.DisplayName, Effort: combination.effort,
		ActualModel: combination.model.Model,
	}
	sensitiveValues := make([]string, 0, 5)
	defer func() {
		if runErr != nil {
			runErr = sanitizeDigBenchError(runErr, sensitiveValues...)
			if result.Failure == "" && !errors.Is(runErr, context.Canceled) {
				result.Failure = runErr.Error()
			}
		}
		result.Failure = redactDigBenchText(result.Failure, sensitiveValues...)
	}()

	temporary, err := os.MkdirTemp("", "codexometer-digbench-")
	if err != nil {
		return result, fmt.Errorf("create isolated DigBench workspace: %w", err)
	}
	defer os.RemoveAll(temporary)
	sensitiveValues = append(sensitiveValues, temporary)

	threadParams := map[string]any{
		"model": combination.model.Model, "cwd": temporary,
		"approvalPolicy": "never", "sandbox": "workspace-write", "ephemeral": true,
		"serviceName":           "codexometer-digbench",
		"developerInstructions": digBenchDeveloperInstructions,
		"dynamicTools":          digBenchDynamicTools(),
	}
	if s.experimentalRawEvents {
		threadParams["experimentalRawEvents"] = true
	}
	threadResult, err := s.call(ctx, "thread/start", threadParams, nil)
	if err != nil && s.experimentalRawEvents && experimentalAPIUnsupported(err) {
		s.experimentalRawEvents = false
		delete(threadParams, "experimentalRawEvents")
		threadResult, err = s.call(ctx, "thread/start", threadParams, nil)
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
	sensitiveValues = append(sensitiveValues, startedThread.Thread.ID)
	if startedThread.Model != "" {
		result.ActualModel = startedThread.Model
	}

	modelName := combination.model.Model + "/" + combination.effort + " via Codexometer"
	modelVersion := strings.TrimSpace(options.ClientVersion)
	startRequest := digbench.StartRequest{Game: options.Game, ModelName: &modelName}
	if modelVersion != "" {
		startRequest.ModelVersion = &modelVersion
	}
	session, err := service.StartSession(ctx, startRequest)
	if err != nil {
		return result, fmt.Errorf("start DigBench game: %w", err)
	}
	assignedSessionID := session.SessionID
	sensitiveValues = append(sensitiveValues, assignedSessionID)
	applyDigBenchSession(&result, session)

	startedAt := time.Now()
	appendDigBenchInteraction(&result, time.Time{}, BenchmarkInteractionPolicy, digBenchDeveloperInstructions)
	appendDigBenchInteraction(&result, time.Time{}, BenchmarkInteractionPrompt, digBenchTranscriptPrompt(session))
	appendDigBenchInteraction(&result, time.Time{}, BenchmarkInteractionTools, formatDigBenchTools())
	appendDigBenchInteraction(&result, startedAt, BenchmarkInteractionState, formatDigBenchState(session, assignedSessionID))
	publishDigBenchSnapshot(options.Snapshot, result)
	emitDigBenchProgress(options.Progress, DigBenchProgressSession, result, startedAt)
	turnParams := map[string]any{
		"threadId": startedThread.Thread.ID,
		"input":    []map[string]any{{"type": "text", "text": digBenchPrompt(session)}},
	}
	if combination.effort != "default" && combination.effort != "" {
		turnParams["effort"] = combination.effort
	}
	turnResponse, err := s.call(ctx, "turn/start", turnParams, nil)
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
	sensitiveValues = append(sensitiveValues, startedTurn.Turn.ID)
	emitDigBenchProgress(options.Progress, DigBenchProgressTurn, result, startedAt)
	publishDigBenchSnapshot(options.Snapshot, result)

	telemetry := newBenchmarkTelemetry()
	completed := false
	turnFailure := ""
	progressTicker := time.NewTicker(defaultDigBenchProgressInterval)
	defer progressTicker.Stop()
	for !completed {
		envelope, heartbeat, envelopeErr := s.nextPendingEnvelopeOrHeartbeat(ctx, progressTicker.C)
		if heartbeat {
			emitDigBenchProgress(options.Progress, DigBenchProgressHeartbeat, result, startedAt)
			continue
		}
		if envelopeErr != nil {
			result.Duration = time.Since(startedAt)
			if errors.Is(context.Cause(ctx), errDigBenchTimeout) {
				result.Failure = fmt.Sprintf("DigBench run timed out after %s", options.Timeout)
				cleanupCompleted := false
				handle := func(method string, params json.RawMessage) bool {
					if method == "turn/completed" && digBenchTurnCompleted(params, startedThread.Thread.ID, startedTurn.Turn.ID, &turnFailure, sensitiveValues...) {
						cleanupCompleted = true
						return true
					}
					return false
				}
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), s.benchmarkInterruptTimeout())
				interruptErr := s.interruptBenchmarkTurn(cleanupCtx, startedThread.Thread.ID, startedTurn.Turn.ID, &cleanupCompleted, handle)
				cleanupCancel()
				if interruptErr != nil {
					result.Failure += "; cleanup failed: " + redactDigBenchText(interruptErr.Error(), sensitiveValues...)
					return result, fmt.Errorf("interrupt timed-out DigBench turn: %w", interruptErr)
				}
				applyDigBenchMeasurements(&result, telemetry)
				if session.Done {
					result.Failure = ""
				}
				publishDigBenchSnapshot(options.Snapshot, result)
				return result, nil
			}
			if ctx.Err() != nil {
				cleanupCompleted := false
				handle := func(method string, params json.RawMessage) bool {
					if method == "turn/completed" && digBenchTurnCompleted(params, startedThread.Thread.ID, startedTurn.Turn.ID, &turnFailure, sensitiveValues...) {
						cleanupCompleted = true
						return true
					}
					return false
				}
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), s.benchmarkInterruptTimeout())
				interruptErr := s.interruptBenchmarkTurn(cleanupCtx, startedThread.Thread.ID, startedTurn.Turn.ID, &cleanupCompleted, handle)
				cleanupCancel()
				applyDigBenchMeasurements(&result, telemetry)
				if interruptErr != nil {
					result.Failure = "remote interruption could not be confirmed: " + redactDigBenchText(interruptErr.Error(), sensitiveValues...)
				}
				publishDigBenchSnapshot(options.Snapshot, result)
				return result, ctx.Err()
			}
			return result, envelopeErr
		}
		if len(envelope.ID) > 0 && envelope.Method != "" {
			if envelope.Method != "item/tool/call" {
				return result, fmt.Errorf("unsupported Codex app-server request %q", envelope.Method)
			}
			tool, action, stepIndex := digBenchToolSummary(envelope.Params)
			response := handleDigBenchToolCall(
				ctx, service, startedThread.Thread.ID, startedTurn.Turn.ID,
				assignedSessionID, envelope.Params, &session,
			)
			applyDigBenchSession(&result, session)
			success := response.agent["success"] == true
			appendDigBenchInteraction(&result, startedAt, BenchmarkInteractionTool, formatDigBenchToolInteraction(tool, action, stepIndex, assignedSessionID, session.SessionID))
			appendDigBenchInteraction(&result, startedAt, BenchmarkInteractionToolResponse, formatDigBenchToolResponse(response.transcript, success, assignedSessionID, session.SessionID))
			if success {
				if tool == "step" {
					appendDigBenchInteraction(&result, startedAt, BenchmarkInteractionMove, redactDigBenchText(action, assignedSessionID, session.SessionID))
				}
				appendDigBenchInteraction(&result, startedAt, BenchmarkInteractionState, formatDigBenchState(session, assignedSessionID))
				emitDigBenchProgress(options.Progress, DigBenchProgressUpdate, result, startedAt)
				publishDigBenchSnapshot(options.Snapshot, result)
			}
			if err := s.respond(envelope.ID, response.agent); err != nil {
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
			completed = digBenchTurnCompleted(envelope.Params, startedThread.Thread.ID, startedTurn.Turn.ID, &turnFailure, sensitiveValues...)
			if completed {
				if final := digBenchFinalResponse(envelope.Params, startedThread.Thread.ID, startedTurn.Turn.ID, sensitiveValues...); final != "" {
					appendDigBenchInteraction(&result, startedAt, BenchmarkInteractionResponse, final)
					publishDigBenchSnapshot(options.Snapshot, result)
				}
			}
		}
	}

	result.Duration = time.Since(startedAt)
	applyDigBenchMeasurements(&result, telemetry)
	if session.Done {
		result.Won = session.State.Status == "completed"
		publishDigBenchSnapshot(options.Snapshot, result)
		return result, nil
	}
	if turnFailure != "" {
		result.Failure = turnFailure
		return result, nil
	}
	result.Failure = "Codex stopped before the DigBench game ended"
	publishDigBenchSnapshot(options.Snapshot, result)
	return result, nil
}

func publishDigBenchSnapshot(callback func(DigBenchResult), result DigBenchResult) {
	if callback == nil {
		return
	}
	result.Interactions = append([]BenchmarkInteraction(nil), result.Interactions...)
	result.ResponseUsage = append([]BenchmarkResponseUsage(nil), result.ResponseUsage...)
	callback(result)
}

func appendDigBenchInteraction(result *DigBenchResult, startedAt time.Time, kind BenchmarkInteractionKind, content string) {
	proxy := BenchmarkResult{Interactions: result.Interactions}
	appendBenchmarkInteraction(&proxy, startedAt, kind, content)
	result.Interactions = proxy.Interactions
}

func digBenchToolSummary(params json.RawMessage) (tool, action string, stepIndex int) {
	var call struct {
		Tool      string `json:"tool"`
		Arguments struct {
			Action    string `json:"action"`
			StepIndex int    `json:"step_index"`
		} `json:"arguments"`
	}
	if json.Unmarshal(params, &call) != nil {
		return "", "", 0
	}
	return call.Tool, call.Arguments.Action, call.Arguments.StepIndex
}

func formatDigBenchToolInteraction(tool, action string, stepIndex int, sensitiveValues ...string) string {
	action = redactDigBenchText(action, sensitiveValues...)
	switch tool {
	case "step":
		return fmt.Sprintf("STEP // SESSION_ID [REDACTED] // STEP_INDEX %d // ACTION %q", stepIndex, action)
	case "get_session":
		return "GET_SESSION // SESSION_ID [REDACTED] // AUTHORITATIVE STATE REFRESH"
	default:
		label := strings.ToUpper(tool)
		if label == "" {
			label = "UNKNOWN_TOOL"
		}
		return label
	}
}

func formatDigBenchToolResponse(response map[string]any, success bool, sensitiveValues ...string) string {
	status := "REJECTED"
	if success {
		status = "ACCEPTED"
	}
	items, ok := response["contentItems"].([]map[string]string)
	if !ok || len(items) == 0 {
		return status + "\n(response unavailable)"
	}
	var payload any
	if err := json.Unmarshal([]byte(items[0]["text"]), &payload); err != nil {
		return status + "\n(response could not be safely decoded)"
	}
	payload = redactDigBenchTranscriptValue(payload, sensitiveValues...)
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return status + "\n(response could not be safely encoded)"
	}
	return status + "\n" + string(encoded)
}

func redactDigBenchTranscriptValue(value any, sensitiveValues ...string) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			switch normalized {
			case "sessionid", "threadid", "turnid", "callid", "responseid", "token", "apikey", "authorization":
				redacted[key] = "[REDACTED]"
			default:
				redacted[key] = redactDigBenchTranscriptValue(child, sensitiveValues...)
			}
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for index, child := range typed {
			redacted[index] = redactDigBenchTranscriptValue(child, sensitiveValues...)
		}
		return redacted
	case string:
		return redactDigBenchText(typed, sensitiveValues...)
	default:
		return value
	}
}

func redactDigBenchJSONValue(value any, sensitiveValues ...string) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "(value could not be safely encoded)"
	}
	var generic any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return "(value could not be safely decoded)"
	}
	return redactDigBenchTranscriptValue(generic, sensitiveValues...)
}

func redactDigBenchText(value string, sensitiveValues ...string) string {
	for _, sensitive := range sensitiveValues {
		if sensitive != "" {
			value = strings.ReplaceAll(value, sensitive, "[REDACTED]")
		}
	}
	return strings.Map(func(character rune) rune {
		// Preserve formatting whitespace, but neutralize terminal C0/C1 control
		// bytes so benchmark content cannot inject ANSI or OSC sequences into the
		// detail renderer or clipboard output.
		if character == '\n' || character == '\t' {
			return character
		}
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) {
			return -1
		}
		return character
	}, value)
}

type sanitizedDigBenchError struct {
	cause   error
	message string
}

func (e *sanitizedDigBenchError) Error() string { return e.message }
func (e *sanitizedDigBenchError) Unwrap() error { return e.cause }

func sanitizeDigBenchError(err error, sensitiveValues ...string) error {
	if err == nil {
		return nil
	}
	message := redactDigBenchText(err.Error(), sensitiveValues...)
	if message == err.Error() {
		return err
	}
	return &sanitizedDigBenchError{cause: err, message: message}
}

func formatDigBenchState(session digbench.Session, sensitiveValues ...string) string {
	state := map[string]any{
		"step_index": session.StepIndex, "levels_beaten": session.LevelsBeaten, "state": session.State,
	}
	sensitiveValues = append(sensitiveValues, session.SessionID)
	encoded, err := json.MarshalIndent(redactDigBenchJSONValue(state, sensitiveValues...), "", "  ")
	if err != nil {
		return "DigBench state could not be encoded."
	}
	return string(encoded)
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
			"description": "Apply exactly one legal action to the assigned DigBench game session. Inspect the returned authoritative state before choosing and submitting the next move; never queue moves against states you have not observed.",
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
	return formatDigBenchPrompt(session, session.SessionID, false)
}

func digBenchTranscriptPrompt(session digbench.Session) string {
	return formatDigBenchPrompt(session, "[REDACTED]", true)
}

func formatDigBenchTools() string {
	encoded, err := json.MarshalIndent(digBenchDynamicTools(), "", "  ")
	if err != nil {
		return "DigBench dynamic tool definitions could not be encoded."
	}
	return string(encoded)
}

func formatDigBenchPrompt(session digbench.Session, sessionID string, redactState bool) string {
	description := session.Description
	stateValue := any(digBenchModelState(session.State))
	if redactState {
		description = redactDigBenchText(description, session.SessionID)
		stateValue = redactDigBenchJSONValue(stateValue, session.SessionID)
	}
	state, _ := json.Marshal(stateValue)
	return fmt.Sprintf(`We are not going to tell you the rules of this game—you have to figure them out for yourself.

Levels, lives and steps:
- The aim is to reach as high a level as possible. For each level you reach you will be awarded a bonus.
- You advance levels by reaching certain states within the game. You will have to figure out what these are.
- Within each level, you have a limited number of steps. If you run out of steps, you lose a life. If you lose all your lives, the game is over.
- It is also possible to lose a life by reaching certain states within the game.

TASK DESCRIPTION (objective + any special actions, NOT the rules):
%s

Important: creative mode
At nearly any time, you can use a button to switch into "creative mode", where you can experiment safely without losing ordinary steps or lives. It may be necessary to use creative mode in order to discover the rules without running out of steps. Creative mode has its own finite budget.

Call the "step" tool with action "/" to enter creative mode.
Call the "step" tool with action "/" again to return to survival mode.
Only submit "/" when it appears in the state's actions list.

How you play—use the scoped DigBench tools to drive the game:
1. Your game session is ALREADY started for you:
session_id=%q, game=%q. You do NOT start or choose a game—you only have the "get_session" and "step" game tools, scoped to this one session, and you must pass this session_id to every call. Your starting state (step_index=%d) is:
%s

2. Each turn, read the current state and reason from these fields: observation (the rendered screen), level, max_level, lives_left, steps_remaining, status, done, the actions list (your legal moves), and mode/creative_toggle/transition when present.

3. Make a move with the "step" tool: session_id=%q, step_index=<the server's last returned step_index + 1>, action=<EXACTLY ONE string from the current state's actions list>. Your first move uses step_index=%d. A step_index mismatch is a conflict—always step off the server's last returned step_index. Use the "get_session" tool if you ever need to re-read the current state.

4. Infer what each action does from how the state changes, and build on what you learn across turns. Keep useful notes and hypotheses in the isolated workspace when helpful. After each successful "step" call, wait for and inspect its returned authoritative state before deciding the next action. Never queue or precompute multiple future step calls against an unobserved state. You may deliberately test a sequence, but submit it one observed move at a time.

Keep playing at a deliberate but efficient pace until the state's done is true (status game_over or completed). When the game is done, STOP making moves and write a concise debrief: the mechanics you discovered, the objective, useful strategies, and remaining uncertainties.`,
		digBenchTaskDescription(description), sessionID, session.Game, session.StepIndex, state,
		sessionID, session.StepIndex+1)
}

func digBenchTaskDescription(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return "(none provided)"
	}
	return description
}

// digBenchModelState keeps every game-relevant field while omitting repeated
// protocol/session metadata. The complete API payload remains in the sanitized
// benchmark transcript for auditability.
func digBenchModelState(state digbench.State) map[string]any {
	modelState := map[string]any{
		"observation":     state.Observation,
		"level":           state.Level,
		"max_level":       state.MaxLevel,
		"lives_left":      state.LivesLeft,
		"steps_remaining": state.StepsRemaining,
		"status":          state.Status,
		"done":            state.Done,
		"actions":         state.Actions,
	}
	// These static limits are useful when planning experiments and were already
	// visible in Codexometer's original model-facing response.
	if state.MaxSteps != nil {
		modelState["max_steps"] = state.MaxSteps
	}
	if state.StartingLives != nil {
		modelState["starting_lives"] = state.StartingLives
	}
	if state.Mode != nil {
		modelState["mode"] = state.Mode
	}
	if state.CreativeToggle != nil {
		modelState["creative_toggle"] = state.CreativeToggle
	}
	if state.Transition != nil {
		modelState["transition"] = state.Transition
	}
	return modelState
}

type digBenchHandledToolCall struct {
	agent      map[string]any
	transcript map[string]any
}

func handleDigBenchToolCall(
	ctx context.Context,
	service digBenchService,
	threadID string,
	turnID string,
	sessionID string,
	params json.RawMessage,
	current *digbench.Session,
) digBenchHandledToolCall {
	var call struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return digBenchErrorToolCall(fmt.Errorf("decode tool request: %w", err))
	}
	if call.ThreadID != threadID || call.TurnID != turnID {
		return digBenchErrorToolCall(errors.New("access denied: tool call belongs to another Codex turn"))
	}
	var common struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(call.Arguments, &common); err != nil {
		return digBenchErrorToolCall(fmt.Errorf("decode %s arguments: %w", call.Tool, err))
	}
	if common.SessionID != sessionID {
		return digBenchErrorToolCall(errors.New("access denied: tool is scoped to the assigned session"))
	}
	switch call.Tool {
	case "get_session":
		session, err := service.GetSession(ctx, sessionID)
		if err == nil {
			*current = session
		}
		return digBenchSuccessfulToolCall(session, digBenchCompactSession(session), err)
	case "step":
		var arguments struct {
			SessionID string `json:"session_id"`
			StepIndex int    `json:"step_index"`
			Action    string `json:"action"`
		}
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
			return digBenchErrorToolCall(fmt.Errorf("decode step arguments: %w", err))
		}
		response, err := service.Step(ctx, sessionID, digbench.StepRequest{Action: arguments.Action, StepIndex: arguments.StepIndex})
		if err == nil {
			*current = response.Session
		}
		return digBenchSuccessfulToolCall(response, digBenchCompactStep(response), err)
	default:
		return digBenchErrorToolCall(fmt.Errorf("unknown DigBench tool %q", call.Tool))
	}
}

func digBenchSuccessfulToolCall(full, compact any, err error) digBenchHandledToolCall {
	if err != nil {
		return digBenchErrorToolCall(err)
	}
	return digBenchHandledToolCall{
		agent:      digBenchToolResponse(compact, nil),
		transcript: digBenchToolResponse(full, nil),
	}
}

func digBenchErrorToolCall(err error) digBenchHandledToolCall {
	response := digBenchToolResponse(nil, err)
	return digBenchHandledToolCall{agent: response, transcript: response}
}

func digBenchCompactSession(session digbench.Session) map[string]any {
	return map[string]any{
		"step_index":    session.StepIndex,
		"levels_beaten": session.LevelsBeaten,
		"state":         digBenchModelState(session.State),
	}
}

func digBenchCompactStep(response digbench.StepResponse) map[string]any {
	compact := digBenchCompactSession(response.Session)
	if response.InvalidAction != nil {
		compact["invalid_action"] = *response.InvalidAction
	}
	return compact
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
	// Step responses may omit identity and other session-static metadata. Treat
	// those fields as incremental updates so a valid initial session cannot lose
	// the game name used by live UI snapshots.
	if strings.TrimSpace(session.Game) != "" {
		result.Game = session.Game
	}
	result.Status = session.State.Status
	result.CurrentLevel = session.State.Level
	result.LevelsBeaten = session.LevelsBeaten
	result.Steps = session.StepIndex
	if session.Seed != nil {
		result.Seed = session.Seed
	}
	if session.State.MaxLevel != nil {
		result.MaxLevel = *session.State.MaxLevel
	}
	if session.FrameworkVersion != nil {
		result.FrameworkVersion = *session.FrameworkVersion
	}
	result.Won = session.Done && session.State.Done && session.State.Status == "completed"
}

func digBenchTurnCompleted(params json.RawMessage, threadID, turnID string, failure *string, sensitiveValues ...string) bool {
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
		*failure = "turn " + redactDigBenchText(event.Turn.Status, sensitiveValues...)
		if event.Turn.Error != nil && event.Turn.Error.Message != "" {
			*failure += ": " + redactDigBenchText(event.Turn.Error.Message, sensitiveValues...)
		}
	}
	return true
}

func digBenchFinalResponse(params json.RawMessage, threadID, turnID string, sensitiveValues ...string) string {
	var event struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID    string `json:"id"`
			Items []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"items"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &event) != nil || event.ThreadID != threadID || event.Turn.ID != turnID {
		return ""
	}
	for _, item := range event.Turn.Items {
		if item.Type == "agentMessage" && strings.TrimSpace(item.Text) != "" {
			return redactDigBenchText(item.Text, sensitiveValues...)
		}
	}
	return ""
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

package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.starlark.net/starlark"

	"github.com/merefield/codexometer/internal/version"
)

const (
	benchmarkTurnTimeout      = 5 * time.Minute
	benchmarkInterruptTimeout = 15 * time.Second
	benchmarkCodeLimit        = 64 * 1024
	benchmarkStepLimit        = 250_000
	benchmarkHardStepLimit    = 2_000_000
)

// BenchmarkUsage is the app-server token breakdown for one isolated turn.
type BenchmarkUsage struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	CacheWriteInputTokens int64 `json:"cacheWriteInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
}

// BenchmarkUsageSource identifies the telemetry used for a benchmark result.
type BenchmarkUsageSource string

const (
	BenchmarkUsageUnavailable  BenchmarkUsageSource = "unavailable"
	BenchmarkUsageCumulative   BenchmarkUsageSource = "cumulative"
	BenchmarkUsageRawResponses BenchmarkUsageSource = "raw-responses"
)

// BenchmarkResponseUsage is the exact upstream usage for one Responses API
// completion when the current Codex app-server exposes experimental raw events.
type BenchmarkResponseUsage struct {
	ResponseID string
	Usage      BenchmarkUsage
}

// BenchmarkResult contains one model/reasoning-effort run.
type BenchmarkResult struct {
	TaskID        BenchmarkTaskID
	TaskName      string
	Model         string
	DisplayName   string
	Effort        string
	ActualModel   string
	Correct       bool
	Duration      time.Duration
	Usage         BenchmarkUsage
	UsageObserved bool
	UsageKnown    bool
	UsageIssue    string
	UsageSource   BenchmarkUsageSource
	ResponseUsage []BenchmarkResponseUsage
	CostUSD       float64
	CostKnown     bool
	CostIssue     string
	ToolUsed      bool
	ToolType      string
	Failure       string
}

// BenchmarkEvent incrementally reports discovery, execution, and results.
type BenchmarkEvent struct {
	Total         int
	Completed     int
	Combinations  int
	CurrentTaskID BenchmarkTaskID
	CurrentTask   string
	CurrentModel  string
	CurrentEffort string
	Result        *BenchmarkResult
	Done          bool
	Err           error
}

type benchmarkModel struct {
	ID                        string                  `json:"id"`
	Model                     string                  `json:"model"`
	DisplayName               string                  `json:"displayName"`
	Hidden                    bool                    `json:"hidden"`
	SupportedReasoningEfforts []benchmarkEffortOption `json:"supportedReasoningEfforts"`
	DefaultReasoningEffort    string                  `json:"defaultReasoningEffort"`
}

type benchmarkEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
}

type benchmarkCombination struct {
	model  benchmarkModel
	effort string
}

type benchmarkRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type benchmarkMethodError struct {
	method  string
	code    int
	message string
}

func (e *benchmarkMethodError) Error() string {
	return fmt.Sprintf("%s (RPC %d)", e.message, e.code)
}

type benchmarkEnvelope struct {
	ID     json.RawMessage    `json:"id"`
	Method string             `json:"method"`
	Params json.RawMessage    `json:"params"`
	Result json.RawMessage    `json:"result"`
	Error  *benchmarkRPCError `json:"error"`
}

type appServerSession struct {
	cmd                   *exec.Cmd
	stdin                 io.WriteCloser
	encoder               *json.Encoder
	stderr                *lockedBuffer
	nextID                int
	wait                  sync.Once
	pending               []benchmarkEnvelope
	envelopes             chan benchmarkEnvelope
	readErrors            chan error
	done                  chan struct{}
	stop                  sync.Once
	experimentalRawEvents bool
	turnTimeout           time.Duration
	interruptTimeout      time.Duration
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// BenchmarkCombinationCount returns the number of visible model/effort pairs
// without starting a model turn.
func (c Client) BenchmarkCombinationCount(ctx context.Context) (int, error) {
	server, err := openBenchmarkAppServer(ctx, c.Binary)
	if err != nil {
		return 0, err
	}
	defer server.close()
	models, err := server.models(ctx)
	if err != nil {
		return 0, err
	}
	return len(benchmarkCombinations(models)), nil
}

// RunBenchmarkSuite runs each selected deterministic task once for every
// visible model/reasoning-effort combination advertised by the current login.
var openBenchmarkAppServer = startAppServer

func (c Client) RunBenchmarkSuite(ctx context.Context, taskIDs []BenchmarkTaskID, emit func(BenchmarkEvent)) {
	if emit == nil {
		emit = func(BenchmarkEvent) {}
	}
	definitions, err := resolveBenchmarkTasks(taskIDs)
	if err != nil {
		emit(BenchmarkEvent{Done: true, Err: err})
		return
	}
	server, err := openBenchmarkAppServer(ctx, c.Binary)
	if err != nil {
		emit(BenchmarkEvent{Done: true, Err: err})
		return
	}
	defer server.close()

	models, err := server.models(ctx)
	if err != nil {
		emit(BenchmarkEvent{Done: true, Err: err})
		return
	}
	combinations := benchmarkCombinations(models)
	if len(combinations) == 0 {
		emit(BenchmarkEvent{Done: true, Err: errors.New("Codex returned no benchmarkable models")})
		return
	}
	total := len(combinations) * len(definitions)
	emit(BenchmarkEvent{Total: total, Combinations: len(combinations)})

	completed := 0
	for _, definition := range definitions {
		for _, combination := range combinations {
			if err := ctx.Err(); err != nil {
				emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(combinations), Done: true, Err: err})
				return
			}
			event := BenchmarkEvent{
				Total: total, Completed: completed, Combinations: len(combinations),
				CurrentTaskID: definition.task.ID, CurrentTask: definition.task.Name,
				CurrentModel: combination.model.DisplayName, CurrentEffort: combination.effort,
			}
			emit(event)
			result, fatalErr := server.runBenchmark(ctx, combination, definition)
			completed++
			event.Completed, event.Result = completed, &result
			emit(event)
			if fatalErr != nil {
				emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(combinations), Done: true, Err: fatalErr})
				return
			}
		}
	}
	emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(combinations), Done: true})
}

func startAppServer(ctx context.Context, binary string) (*appServerSession, error) {
	return startAppServerWithExperimentalUsage(ctx, binary, true)
}

func startAppServerWithExperimentalUsage(ctx context.Context, binary string, experimental bool) (*appServerSession, error) {
	if strings.TrimSpace(binary) == "" {
		binary = "codex"
	}
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex input: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex output: %w", err)
	}
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("Codex CLI not found; install it or pass --codex PATH")
		}
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}
	server := &appServerSession{
		cmd: cmd, stdin: stdin, encoder: json.NewEncoder(stdin),
		stderr: stderr, envelopes: make(chan benchmarkEnvelope, 64), readErrors: make(chan error, 1), done: make(chan struct{}),
		experimentalRawEvents: experimental,
	}
	go server.readLoop(json.NewDecoder(bufio.NewReader(stdout)))
	initialize := map[string]any{
		"clientInfo": map[string]string{
			"name": "codexometer", "title": "Codexometer", "version": version.Current(),
		},
	}
	if experimental {
		initialize["capabilities"] = map[string]bool{"experimentalApi": true}
	}
	if _, err := server.call(ctx, "initialize", initialize, nil); err != nil {
		server.close()
		if experimental && experimentalAPIUnsupported(err) {
			return startAppServerWithExperimentalUsage(ctx, binary, false)
		}
		return nil, fmt.Errorf("initialize Codex app-server: %w", err)
	}
	if err := server.encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
		server.close()
		return nil, fmt.Errorf("acknowledge Codex app-server: %w", err)
	}
	return server, nil
}

func experimentalAPIUnsupported(err error) bool {
	var methodError *benchmarkMethodError
	if !errors.As(err, &methodError) {
		return false
	}
	return methodError.code == -32601 || methodError.code == -32602 ||
		strings.Contains(strings.ToLower(methodError.message), "experimentalapi") ||
		strings.Contains(strings.ToLower(methodError.message), "experimentalrawevents")
}

func (s *appServerSession) readLoop(decoder *json.Decoder) {
	for {
		var envelope benchmarkEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			select {
			case s.readErrors <- err:
			default:
			}
			return
		}
		select {
		case s.envelopes <- envelope:
		case <-s.done:
			return
		}
	}
}

func (s *appServerSession) nextEnvelope(ctx context.Context) (benchmarkEnvelope, error) {
	// Drain decoded protocol messages before observing a terminal decoder error.
	// A short-lived server may write its final response and close stdout so close
	// together that both channels are ready at once.
	select {
	case envelope := <-s.envelopes:
		return envelope, nil
	default:
	}
	select {
	case <-ctx.Done():
		return benchmarkEnvelope{}, ctx.Err()
	case <-s.done:
		return benchmarkEnvelope{}, errors.New("Codex app-server session closed")
	case err := <-s.readErrors:
		select {
		case envelope := <-s.envelopes:
			return envelope, nil
		default:
		}
		if errors.Is(err, io.EOF) {
			return benchmarkEnvelope{}, fmt.Errorf("Codex app-server closed unexpectedly: %s", strings.TrimSpace(s.stderr.String()))
		}
		return benchmarkEnvelope{}, err
	case envelope := <-s.envelopes:
		return envelope, nil
	}
}

func (s *appServerSession) close() {
	if s == nil {
		return
	}
	s.stop.Do(func() { close(s.done) })
	_ = s.stdin.Close()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.wait.Do(func() {
		if s.cmd != nil {
			_ = s.cmd.Wait()
		}
	})
}

func (s *appServerSession) call(
	ctx context.Context,
	method string,
	params any,
	onNotification func(string, json.RawMessage),
) (json.RawMessage, error) {
	s.nextID++
	id := s.nextID
	request := map[string]any{"method": method, "id": id}
	if params != nil {
		request["params"] = params
	}
	if err := s.encoder.Encode(request); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		envelope, err := s.nextEnvelope(ctx)
		if err != nil {
			return nil, err
		}
		if envelope.Method != "" {
			if onNotification != nil {
				onNotification(envelope.Method, envelope.Params)
			} else {
				s.pending = append(s.pending, envelope)
			}
			continue
		}
		var responseID int
		if len(envelope.ID) == 0 || json.Unmarshal(envelope.ID, &responseID) != nil || responseID != id {
			continue
		}
		if envelope.Error != nil {
			return nil, &benchmarkMethodError{method: method, code: envelope.Error.Code, message: envelope.Error.Message}
		}
		return envelope.Result, nil
	}
}

func (s *appServerSession) models(ctx context.Context) ([]benchmarkModel, error) {
	var models []benchmarkModel
	var cursor string
	for {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, err := s.call(ctx, "model/list", params, nil)
		if err != nil {
			return nil, fmt.Errorf("list Codex models: %w", err)
		}
		var page struct {
			Data       []benchmarkModel `json:"data"`
			NextCursor *string          `json:"nextCursor"`
		}
		if err := json.Unmarshal(result, &page); err != nil {
			return nil, fmt.Errorf("decode Codex models: %w", err)
		}
		for _, model := range page.Data {
			if !model.Hidden {
				models = append(models, model)
			}
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
	}
	return models, nil
}

func benchmarkCombinations(models []benchmarkModel) []benchmarkCombination {
	var combinations []benchmarkCombination
	for _, model := range models {
		if strings.TrimSpace(model.Model) == "" {
			continue
		}
		if strings.TrimSpace(model.DisplayName) == "" {
			model.DisplayName = model.Model
		}
		if len(model.SupportedReasoningEfforts) == 0 {
			effort := strings.TrimSpace(model.DefaultReasoningEffort)
			if effort == "" {
				effort = "default"
			}
			combinations = append(combinations, benchmarkCombination{model: model, effort: effort})
			continue
		}
		for _, option := range model.SupportedReasoningEfforts {
			if effort := strings.TrimSpace(option.ReasoningEffort); effort != "" {
				combinations = append(combinations, benchmarkCombination{model: model, effort: effort})
			}
		}
	}
	return combinations
}

func (s *appServerSession) runBenchmark(ctx context.Context, combination benchmarkCombination, definition benchmarkDefinition) (BenchmarkResult, error) {
	result := BenchmarkResult{
		TaskID: definition.task.ID, TaskName: definition.task.Name,
		Model: combination.model.Model, DisplayName: combination.model.DisplayName,
		Effort: combination.effort, ActualModel: combination.model.Model,
	}
	turnTimeout := s.benchmarkTurnTimeout()
	turnCtx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()

	temporary, err := os.MkdirTemp("", "codexometer-benchmark-")
	if err != nil {
		result.Failure = fmt.Sprintf("create isolated workspace: %v", err)
		return result, nil
	}
	defer os.RemoveAll(temporary)

	threadParams := map[string]any{
		"model":                 combination.model.Model,
		"cwd":                   temporary,
		"approvalPolicy":        "never",
		"sandbox":               "read-only",
		"ephemeral":             true,
		"serviceName":           "codexometer-benchmark",
		"developerInstructions": "Complete only the supplied deterministic benchmark. Do not use tools or inspect the environment. Return exactly the requested structured output.",
	}
	if s.experimentalRawEvents {
		threadParams["experimentalRawEvents"] = true
	}
	threadResult, err := s.call(turnCtx, "thread/start", threadParams, nil)
	if err != nil && s.experimentalRawEvents && experimentalAPIUnsupported(err) {
		// Older Codex versions may support the stable benchmark API but not raw
		// response telemetry. Retry this thread without the experimental field and
		// keep cumulative usage as the compatibility path for the rest of the suite.
		s.experimentalRawEvents = false
		delete(threadParams, "experimentalRawEvents")
		threadResult, err = s.call(turnCtx, "thread/start", threadParams, nil)
	}
	if err != nil {
		result.Failure = fmt.Sprintf("start thread: %v", err)
		return result, fatalBenchmarkError(turnCtx, err)
	}
	var startedThread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(threadResult, &startedThread); err != nil || startedThread.Thread.ID == "" {
		result.Failure = "Codex returned an invalid thread"
		return result, nil
	}
	if startedThread.Model != "" {
		result.ActualModel = startedThread.Model
	}
	startedAt := time.Now()
	turnParams := map[string]any{
		"threadId":     startedThread.Thread.ID,
		"input":        []map[string]any{{"type": "text", "text": benchmarkPrompt(definition)}},
		"outputSchema": benchmarkOutputSchema,
	}
	if combination.effort != "default" && combination.effort != "" {
		turnParams["effort"] = combination.effort
	}
	turnResponse, err := s.call(turnCtx, "turn/start", turnParams, nil)
	if err != nil {
		result.Duration = time.Since(startedAt)
		result.Failure = fmt.Sprintf("start turn: %v", err)
		return result, fatalBenchmarkError(turnCtx, err)
	}
	var startedTurn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(turnResponse, &startedTurn); err != nil || startedTurn.Turn.ID == "" {
		result.Duration = time.Since(startedAt)
		result.Failure = "Codex returned an invalid turn"
		return result, nil
	}

	var finalMessage, turnFailure string
	telemetry := newBenchmarkTelemetry()
	completed := false
	handleNotification := func(method string, params json.RawMessage) bool {
		switch method {
		case "thread/tokenUsage/updated":
			var event struct {
				ThreadID   string `json:"threadId"`
				TurnID     string `json:"turnId"`
				TokenUsage struct {
					Total BenchmarkUsage `json:"total"`
				} `json:"tokenUsage"`
			}
			if json.Unmarshal(params, &event) == nil && event.ThreadID == startedThread.Thread.ID && event.TurnID == startedTurn.Turn.ID {
				telemetry.recordCumulative(event.TokenUsage.Total)
			}
		case "rawResponse/completed":
			var event struct {
				ThreadID   string          `json:"threadId"`
				TurnID     string          `json:"turnId"`
				ResponseID string          `json:"responseId"`
				Usage      *BenchmarkUsage `json:"usage"`
			}
			if json.Unmarshal(params, &event) == nil && event.ThreadID == startedThread.Thread.ID && event.TurnID == startedTurn.Turn.ID {
				telemetry.recordRawResponse(event.ResponseID, event.Usage)
			}
		case "model/rerouted":
			var event struct {
				ThreadID string `json:"threadId"`
				TurnID   string `json:"turnId"`
				ToModel  string `json:"toModel"`
			}
			if json.Unmarshal(params, &event) == nil && event.ThreadID == startedThread.Thread.ID && event.TurnID == startedTurn.Turn.ID && event.ToModel != "" {
				result.ActualModel = event.ToModel
			}
		case "item/started", "item/completed":
			var event struct {
				ThreadID string `json:"threadId"`
				TurnID   string `json:"turnId"`
				Item     struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(params, &event) == nil && event.ThreadID == startedThread.Thread.ID && event.TurnID == startedTurn.Turn.ID {
				if benchmarkToolItem(event.Item.Type) && !result.ToolUsed {
					result.ToolUsed, result.ToolType = true, event.Item.Type
				}
				if method == "item/completed" && event.Item.Type == "agentMessage" {
					finalMessage = event.Item.Text
				}
			}
		case "turn/completed":
			var event struct {
				ThreadID string `json:"threadId"`
				Turn     struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Error  *struct {
						Message string `json:"message"`
					} `json:"error"`
					Items []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"items"`
				} `json:"turn"`
			}
			if json.Unmarshal(params, &event) == nil && event.ThreadID == startedThread.Thread.ID && event.Turn.ID == startedTurn.Turn.ID {
				completed = true
				if event.Turn.Status != "completed" {
					turnFailure = "turn " + event.Turn.Status
					if event.Turn.Error != nil && event.Turn.Error.Message != "" {
						turnFailure += ": " + event.Turn.Error.Message
					}
				}
				if finalMessage == "" {
					for _, item := range event.Turn.Items {
						if item.Type == "agentMessage" {
							finalMessage = item.Text
						}
					}
				}
				return true
			}
		}
		return false
	}
	for !completed {
		_, err = s.readUntilNotification(turnCtx, handleNotification)
		if err != nil {
			result.Duration = time.Since(startedAt)
			if recoverableBenchmarkTimeout(ctx, turnCtx, err) {
				result.Failure = fmt.Sprintf("turn timed out after %s", turnTimeout)
				if interruptErr := s.interruptBenchmarkTurn(ctx, startedThread.Thread.ID, startedTurn.Turn.ID, &completed, handleNotification); interruptErr != nil {
					result.Failure += "; cleanup failed: " + interruptErr.Error()
					return result, fmt.Errorf("recover timed-out benchmark turn: %w", interruptErr)
				}
				applyBenchmarkMeasurements(&result, telemetry)
				return result, nil
			}
			result.Failure = fmt.Sprintf("wait for turn: %v", err)
			return result, err
		}
	}
	result.Duration = time.Since(startedAt)
	applyBenchmarkMeasurements(&result, telemetry)
	if turnFailure != "" {
		result.Failure = turnFailure
		return result, nil
	}
	if result.ToolUsed {
		result.Failure = "tool use prohibited: " + result.ToolType
		return result, nil
	}
	code, err := benchmarkCode(finalMessage)
	if err != nil {
		result.Failure = err.Error()
		return result, nil
	}
	if err := definition.verify(code); err != nil {
		result.Failure = err.Error()
		return result, nil
	}
	result.Correct = true
	return result, nil
}

func (s *appServerSession) benchmarkTurnTimeout() time.Duration {
	if s.turnTimeout > 0 {
		return s.turnTimeout
	}
	return benchmarkTurnTimeout
}

func (s *appServerSession) benchmarkInterruptTimeout() time.Duration {
	if s.interruptTimeout > 0 {
		return s.interruptTimeout
	}
	return benchmarkInterruptTimeout
}

func recoverableBenchmarkTimeout(parent, turn context.Context, err error) bool {
	return parent.Err() == nil && errors.Is(turn.Err(), context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded)
}

func (s *appServerSession) interruptBenchmarkTurn(
	ctx context.Context,
	threadID, turnID string,
	completed *bool,
	handleNotification func(string, json.RawMessage) bool,
) error {
	cleanupCtx, cancel := context.WithTimeout(ctx, s.benchmarkInterruptTimeout())
	defer cancel()

	_, err := s.call(cleanupCtx, "turn/interrupt", map[string]string{
		"threadId": threadID,
		"turnId":   turnID,
	}, func(method string, params json.RawMessage) {
		handleNotification(method, params)
	})
	if err != nil {
		var methodError *benchmarkMethodError
		// The turn can finish naturally between the deadline and the interrupt
		// request. A method rejection is safe only when its matching completion
		// notification was also observed.
		if !*completed || !errors.As(err, &methodError) {
			return fmt.Errorf("interrupt turn: %w", err)
		}
	}
	if *completed {
		return nil
	}
	if _, err := s.readUntilNotification(cleanupCtx, handleNotification); err != nil {
		return fmt.Errorf("confirm interrupted turn: %w", err)
	}
	if !*completed {
		return errors.New("confirm interrupted turn: completion was not observed")
	}
	return nil
}

func applyBenchmarkMeasurements(result *BenchmarkResult, telemetry *benchmarkTelemetry) {
	telemetry.apply(result)
	if result.ToolUsed {
		result.CostIssue = "tool use prohibited: " + result.ToolType
	} else if result.UsageKnown {
		result.CostUSD, result.CostKnown, result.CostIssue = estimateAPICostWithIssue(result.ActualModel, result.Usage)
	} else {
		result.CostIssue = result.UsageIssue
	}
}

type benchmarkTelemetry struct {
	cumulative         BenchmarkUsage
	cumulativeObserved bool
	lastCumulative     BenchmarkUsage
	hasLastCumulative  bool
	rawResponses       []BenchmarkResponseUsage
	rawIDs             map[string]struct{}
	rawObserved        bool
	rawMissingUsage    bool
	issue              string
}

func newBenchmarkTelemetry() *benchmarkTelemetry {
	return &benchmarkTelemetry{rawIDs: make(map[string]struct{})}
}

func (t *benchmarkTelemetry) recordCumulative(usage BenchmarkUsage) {
	t.cumulativeObserved = true
	if issue := validateBenchmarkUsage(usage); issue != "" {
		t.setIssue("invalid cumulative usage: " + issue)
	}
	if t.hasLastCumulative && !benchmarkUsageAtLeast(usage, t.lastCumulative) {
		t.setIssue("cumulative usage regressed")
	}
	t.cumulative = usage
	t.lastCumulative = usage
	t.hasLastCumulative = true
}

func (t *benchmarkTelemetry) recordRawResponse(responseID string, usage *BenchmarkUsage) {
	t.rawObserved = true
	if strings.TrimSpace(responseID) == "" {
		t.setIssue("raw response usage omitted response id")
		return
	}
	if _, duplicate := t.rawIDs[responseID]; duplicate {
		t.setIssue("duplicate raw response usage: " + responseID)
		return
	}
	t.rawIDs[responseID] = struct{}{}
	if usage == nil {
		t.rawMissingUsage = true
		return
	}
	if issue := validateBenchmarkUsage(*usage); issue != "" {
		t.setIssue("invalid raw response usage: " + issue)
	}
	t.rawResponses = append(t.rawResponses, BenchmarkResponseUsage{ResponseID: responseID, Usage: *usage})
}

func (t *benchmarkTelemetry) apply(result *BenchmarkResult) {
	result.UsageObserved = t.cumulativeObserved || t.rawObserved
	result.UsageSource = BenchmarkUsageUnavailable
	result.ResponseUsage = append([]BenchmarkResponseUsage(nil), t.rawResponses...)
	if t.issue != "" {
		result.UsageIssue = t.issue
		return
	}

	rawComplete := len(t.rawResponses) > 0 && !t.rawMissingUsage
	if rawComplete {
		rawTotal := BenchmarkUsage{}
		for _, response := range t.rawResponses {
			if issue := rawTotal.add(response.Usage); issue != "" {
				result.UsageIssue = "invalid raw response total: " + issue
				return
			}
		}
		if issue := validateBenchmarkUsage(rawTotal); issue != "" {
			result.UsageIssue = "invalid raw response total: " + issue
			return
		}
		if t.cumulativeObserved && rawTotal != t.cumulative {
			result.UsageIssue = "raw and cumulative usage disagree"
			return
		}
		result.Usage, result.UsageKnown, result.UsageSource = rawTotal, true, BenchmarkUsageRawResponses
		return
	}
	if t.cumulativeObserved {
		result.Usage, result.UsageKnown, result.UsageSource = t.cumulative, true, BenchmarkUsageCumulative
		return
	}
	if t.rawObserved {
		result.UsageIssue = "raw response omitted usage and no cumulative usage was observed"
	} else {
		result.UsageIssue = "matching usage event was not observed"
	}
}

func (t *benchmarkTelemetry) setIssue(issue string) {
	if t.issue == "" {
		t.issue = issue
	}
}

func (u *BenchmarkUsage) add(other BenchmarkUsage) string {
	fields := []struct {
		name   string
		target *int64
		value  int64
	}{
		{"totalTokens", &u.TotalTokens, other.TotalTokens},
		{"inputTokens", &u.InputTokens, other.InputTokens},
		{"cachedInputTokens", &u.CachedInputTokens, other.CachedInputTokens},
		{"cacheWriteInputTokens", &u.CacheWriteInputTokens, other.CacheWriteInputTokens},
		{"outputTokens", &u.OutputTokens, other.OutputTokens},
		{"reasoningOutputTokens", &u.ReasoningOutputTokens, other.ReasoningOutputTokens},
	}
	for _, field := range fields {
		if field.value < 0 || *field.target > math.MaxInt64-field.value {
			return field.name + " overflowed"
		}
		*field.target += field.value
	}
	return ""
}

func validateBenchmarkUsage(usage BenchmarkUsage) string {
	fields := []struct {
		name  string
		value int64
	}{
		{"totalTokens", usage.TotalTokens},
		{"inputTokens", usage.InputTokens},
		{"cachedInputTokens", usage.CachedInputTokens},
		{"cacheWriteInputTokens", usage.CacheWriteInputTokens},
		{"outputTokens", usage.OutputTokens},
		{"reasoningOutputTokens", usage.ReasoningOutputTokens},
	}
	for _, field := range fields {
		if field.value < 0 {
			return field.name + " is negative"
		}
	}
	if usage.CachedInputTokens > usage.InputTokens ||
		usage.CacheWriteInputTokens > usage.InputTokens-usage.CachedInputTokens {
		return "cached and cache-write input exceed inputTokens"
	}
	if usage.ReasoningOutputTokens > usage.OutputTokens {
		return "reasoningOutputTokens exceeds outputTokens"
	}
	if usage.InputTokens > math.MaxInt64-usage.OutputTokens {
		return "inputTokens + outputTokens overflow"
	}
	if usage.TotalTokens != usage.InputTokens+usage.OutputTokens {
		return "totalTokens does not equal inputTokens + outputTokens"
	}
	return ""
}

func benchmarkUsageAtLeast(current, previous BenchmarkUsage) bool {
	return current.TotalTokens >= previous.TotalTokens &&
		current.InputTokens >= previous.InputTokens &&
		current.CachedInputTokens >= previous.CachedInputTokens &&
		current.CacheWriteInputTokens >= previous.CacheWriteInputTokens &&
		current.OutputTokens >= previous.OutputTokens &&
		current.ReasoningOutputTokens >= previous.ReasoningOutputTokens
}

func benchmarkToolItem(itemType string) bool {
	switch itemType {
	case "plan", "commandExecution", "fileChange", "mcpToolCall", "dynamicToolCall",
		"collabAgentToolCall", "subAgentActivity", "webSearch", "imageView", "sleep", "imageGeneration":
		return true
	default:
		return false
	}
}

func fatalBenchmarkError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var methodError *benchmarkMethodError
	if errors.As(err, &methodError) {
		return nil
	}
	return err
}

func (s *appServerSession) readUntilNotification(ctx context.Context, accept func(string, json.RawMessage) bool) (json.RawMessage, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var envelope benchmarkEnvelope
		if len(s.pending) > 0 {
			envelope = s.pending[0]
			s.pending = s.pending[1:]
		} else {
			var err error
			envelope, err = s.nextEnvelope(ctx)
			if err != nil {
				return nil, err
			}
		}
		if envelope.Method != "" && accept(envelope.Method, envelope.Params) {
			return envelope.Params, nil
		}
	}
}

var benchmarkOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"code": map[string]any{"type": "string"},
	},
	"required":             []string{"code"},
	"additionalProperties": false,
}

func benchmarkCode(message string) (string, error) {
	if len(message) == 0 {
		return "", errors.New("Codex returned no final code")
	}
	if len(message) > benchmarkCodeLimit {
		return "", errors.New("returned code exceeds the 64 KiB safety limit")
	}
	var output struct {
		Code string `json:"code"`
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(message)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return "", fmt.Errorf("decode structured result: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", errors.New("structured result contains trailing data")
	}
	if strings.TrimSpace(output.Code) == "" {
		return "", errors.New("structured result contains no code")
	}
	if len(output.Code) > benchmarkCodeLimit {
		return "", errors.New("returned code exceeds the 64 KiB safety limit")
	}
	return output.Code, nil
}

type interval struct {
	start int64
	end   int64
}

func verifyMergeRanges(code string) error {
	thread, function, err := loadBenchmarkFunction(code, "merge_ranges")
	if err != nil {
		return err
	}

	for index, input := range intervalTestCases() {
		argument := intervalsToStarlark(input)
		before := argument.String()
		renewBenchmarkStepBudget(thread)
		value, err := starlark.Call(thread, function, starlark.Tuple{argument}, nil)
		if err != nil {
			return fmt.Errorf("case %d raised an error: %w", index+1, err)
		}
		if argument.String() != before {
			return fmt.Errorf("case %d mutated its input", index+1)
		}
		got, err := intervalsFromStarlark(value)
		if err != nil {
			return fmt.Errorf("case %d returned an invalid value: %w", index+1, err)
		}
		want := mergeIntervals(input)
		if !equalIntervals(got, want) {
			return fmt.Errorf("case %d returned %s, want %s", index+1, formatIntervals(got), formatIntervals(want))
		}
	}
	return nil
}

func intervalTestCases() [][]interval {
	cases := [][]interval{
		{},
		{{start: 4, end: 9}},
		{{start: 5, end: 7}, {start: 1, end: 3}, {start: 2, end: 4}, {start: 10, end: 10}, {start: 8, end: 9}},
		{{start: -8, end: -6}, {start: -5, end: -2}, {start: 0, end: 0}, {start: -3, end: 4}},
		{{start: 1, end: 100}, {start: 4, end: 8}, {start: 1, end: 100}, {start: 20, end: 30}},
		{{start: -1000, end: -999}, {start: 999, end: 1000}},
		{{start: math.MaxInt64 - 1, end: math.MaxInt64 - 1}, {start: math.MaxInt64, end: math.MaxInt64}},
		{{start: math.MinInt64, end: math.MinInt64}, {start: math.MinInt64 + 1, end: math.MinInt64 + 4}},
	}
	seed := uint64(0xC0DE10E7)
	for n := 0; n < 48; n++ {
		count := int(seed%9) + 1
		input := make([]interval, 0, count)
		for i := 0; i < count; i++ {
			seed = seed*6364136223846793005 + 1442695040888963407
			start := int64(seed%61) - 30
			seed = seed*6364136223846793005 + 1442695040888963407
			length := int64(seed % 12)
			input = append(input, interval{start: start, end: start + length})
		}
		cases = append(cases, input)
	}
	return cases
}

func intervalsToStarlark(input []interval) *starlark.List {
	values := make([]starlark.Value, 0, len(input))
	for _, item := range input {
		values = append(values, starlark.NewList([]starlark.Value{
			starlark.MakeInt64(item.start), starlark.MakeInt64(item.end),
		}))
	}
	return starlark.NewList(values)
}

func intervalsFromStarlark(value starlark.Value) ([]interval, error) {
	list, ok := value.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("expected list, got %s", value.Type())
	}
	if list.Len() > 256 {
		return nil, errors.New("result contains too many ranges")
	}
	output := make([]interval, 0, list.Len())
	iterator := list.Iterate()
	defer iterator.Done()
	var item starlark.Value
	for iterator.Next(&item) {
		pair, ok := item.(*starlark.List)
		if !ok || pair.Len() != 2 {
			return nil, errors.New("every range must be a two-item list")
		}
		start, ok := starlarkInt64(pair.Index(0))
		if !ok {
			return nil, errors.New("range start is not a 64-bit integer")
		}
		end, ok := starlarkInt64(pair.Index(1))
		if !ok {
			return nil, errors.New("range end is not a 64-bit integer")
		}
		output = append(output, interval{start: start, end: end})
	}
	return output, nil
}

func starlarkInt64(value starlark.Value) (int64, bool) {
	integer, ok := value.(starlark.Int)
	if !ok {
		return 0, false
	}
	return integer.Int64()
}

func mergeIntervals(input []interval) []interval {
	if len(input) == 0 {
		return nil
	}
	items := append([]interval(nil), input...)
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && (items[j].start < items[j-1].start || (items[j].start == items[j-1].start && items[j].end < items[j-1].end)); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	merged := []interval{items[0]}
	for _, item := range items[1:] {
		last := &merged[len(merged)-1]
		if item.start <= last.end || (last.end < math.MaxInt64 && item.start == last.end+1) {
			if item.end > last.end {
				last.end = item.end
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func equalIntervals(left, right []interval) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func formatIntervals(items []interval) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("[%d,%d]", item.start, item.end))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

type apiPrice struct {
	input           float64
	cached          float64
	cacheWrite      float64
	cacheWriteKnown bool
	output          float64
}

var standardAPIPrices = map[string]apiPrice{
	"gpt-5.6-sol":   {input: 5.00, cached: 0.50, cacheWrite: 6.25, cacheWriteKnown: true, output: 30.00},
	"gpt-5.6-terra": {input: 2.00, cached: 0.20, cacheWrite: 2.50, cacheWriteKnown: true, output: 12.00},
	"gpt-5.6-luna":  {input: 0.20, cached: 0.02, cacheWrite: 0.25, cacheWriteKnown: true, output: 1.20},
	"gpt-5.5":       {input: 5.00, cached: 0.50, output: 30.00},
	"gpt-5.4":       {input: 2.50, cached: 0.25, output: 15.00},
	"gpt-5.4-mini":  {input: 0.75, cached: 0.075, output: 4.50},
	"gpt-5.3-codex": {input: 1.75, cached: 0.175, output: 14.00},
}

func estimateAPICost(model string, usage BenchmarkUsage) (float64, bool) {
	cost, known, _ := estimateAPICostWithIssue(model, usage)
	return cost, known
}

func estimateAPICostWithIssue(model string, usage BenchmarkUsage) (float64, bool, string) {
	if issue := validateBenchmarkUsage(usage); issue != "" {
		return 0, false, "invalid usage: " + issue
	}
	price, ok := priceForModel(model)
	if !ok {
		return 0, false, "no published price for model " + strings.TrimSpace(model)
	}
	if usage.CacheWriteInputTokens > 0 && !price.cacheWriteKnown {
		return 0, false, "no published cache-write price for model " + strings.TrimSpace(model)
	}
	regularInput := usage.InputTokens - usage.CachedInputTokens - usage.CacheWriteInputTokens
	cost := float64(regularInput)*price.input +
		float64(usage.CachedInputTokens)*price.cached +
		float64(usage.CacheWriteInputTokens)*price.cacheWrite +
		float64(usage.OutputTokens)*price.output
	return cost / 1_000_000, true, ""
}

func priceForModel(model string) (apiPrice, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	if price, ok := standardAPIPrices[model]; ok {
		return price, true
	}
	for name, price := range standardAPIPrices {
		if suffix := strings.TrimPrefix(model, name+"-"); suffix != model {
			if _, err := time.Parse("2006-01-02", suffix); err != nil {
				continue
			}
			return price, true
		}
	}
	return apiPrice{}, false
}

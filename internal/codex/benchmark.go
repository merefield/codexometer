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
	"sort"
	"strings"
	"sync"
	"time"

	"go.starlark.net/starlark"
)

const (
	benchmarkTurnTimeout      = 5 * time.Minute
	benchmarkInterruptTimeout = 15 * time.Second
	benchmarkCodeLimit        = 64 * 1024
	benchmarkInteractionLimit = 64 * 1024
	benchmarkTranscriptLimit  = 1024 * 1024
	benchmarkInteractionCount = 4096
	benchmarkStepLimit        = 250_000
	benchmarkHardStepLimit    = 2_000_000

	// StandardAPIPricingSourceURL and StandardAPIPricingRetrievedOn identify
	// the published rates compiled into this release. Keep both in sync whenever
	// standardAPIPrices changes.
	StandardAPIPricingSourceURL   = "https://developers.openai.com/api/docs/pricing"
	StandardAPIPricingRetrievedOn = "2026-08-23"
)

// BenchmarkUsage is the app-server token breakdown for one isolated turn.
type BenchmarkUsage struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	CacheWriteInputTokens int64 `json:"cacheWriteInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
	schemaIssue           string
}

// UnmarshalJSON records unknown fields instead of silently discarding them.
// A future app-server token class may be billable, so costing must remain
// unavailable until Codexometer knows how to account for it.
func (u *BenchmarkUsage) UnmarshalJSON(data []byte) error {
	type wireUsage BenchmarkUsage
	var decoded wireUsage
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	known := map[string]struct{}{
		"totalTokens": {}, "inputTokens": {}, "cachedInputTokens": {},
		"cacheWriteInputTokens": {}, "outputTokens": {}, "reasoningOutputTokens": {},
	}
	unknown := make([]string, 0)
	for name := range fields {
		if _, ok := known[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	*u = BenchmarkUsage(decoded)
	if len(unknown) == 1 {
		u.schemaIssue = "unknown usage field: " + unknown[0]
	} else if len(unknown) > 1 {
		u.schemaIssue = "unknown usage fields: " + strings.Join(unknown, ", ")
	}
	return nil
}

// BenchmarkUsageSource identifies the telemetry used for a benchmark result.
type BenchmarkUsageSource string

const (
	BenchmarkUsageUnavailable  BenchmarkUsageSource = "unavailable"
	BenchmarkUsageCumulative   BenchmarkUsageSource = "cumulative"
	BenchmarkUsageRawResponses BenchmarkUsageSource = "raw-responses"
)

// BenchmarkBillingSource identifies whether benchmark model calls consume the
// Codex subscription quota displayed by Codexometer or a separately billed API
// key. Quota accounting must not infer this from identical token telemetry.
type BenchmarkBillingSource string

const (
	BenchmarkBillingUnknown      BenchmarkBillingSource = "unknown"
	BenchmarkBillingSubscription BenchmarkBillingSource = "subscription"
	BenchmarkBillingAPIKey       BenchmarkBillingSource = "api-key"
)

// BenchmarkBillingSource reports the billing path selected for benchmark
// app-server sessions created by this client.
func (c Client) BenchmarkBillingSource() BenchmarkBillingSource {
	if strings.TrimSpace(c.BenchmarkAPIKey) != "" {
		return BenchmarkBillingAPIKey
	}
	return BenchmarkBillingSubscription
}

// BenchmarkResponseUsage is the exact upstream usage for one Responses API
// completion when the current Codex app-server exposes experimental raw events.
type BenchmarkResponseUsage struct {
	ResponseID string
	Usage      BenchmarkUsage
}

type BenchmarkInteractionKind string

const (
	BenchmarkInteractionPrompt       BenchmarkInteractionKind = "prompt"
	BenchmarkInteractionResponse     BenchmarkInteractionKind = "response"
	BenchmarkInteractionPolicy       BenchmarkInteractionKind = "policy"
	BenchmarkInteractionTools        BenchmarkInteractionKind = "tools"
	BenchmarkInteractionTool         BenchmarkInteractionKind = "tool request"
	BenchmarkInteractionToolResponse BenchmarkInteractionKind = "tool response"
	BenchmarkInteractionVerifier     BenchmarkInteractionKind = "verifier"
	BenchmarkInteractionMove         BenchmarkInteractionKind = "move"
	BenchmarkInteractionState        BenchmarkInteractionKind = "state"
)

// BenchmarkInteraction is content emitted only by a benchmark turn that
// Codexometer created. Codexometer never adds app-server IDs, credentials,
// request headers, or reasoning events to this transcript.
type BenchmarkInteraction struct {
	Elapsed time.Duration
	Kind    BenchmarkInteractionKind
	Content string
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
	Stopped       bool
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
	Interactions  []BenchmarkInteraction
	Provider      string
	GameStatus    string
	CurrentLevel  int
	LevelsBeaten  int
	MaxLevel      int
	Steps         int
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
	Active        *BenchmarkResult
	Result        *BenchmarkResult
	Stopped       bool
	Done          bool
	Err           error
}

// BenchmarkPlan describes the model and reasoning-effort choices currently
// advertised by the user's Codex account.
type BenchmarkPlan struct {
	Models  []BenchmarkModelOption
	Efforts []string
	Games   []string
}

// BenchmarkModelOption is one selectable model and its supported efforts.
type BenchmarkModelOption struct {
	Model       string
	DisplayName string
	Efforts     []string
}

// BenchmarkScope selects the model/effort intersections included in a suite.
// Empty selections intentionally produce no combinations.
type BenchmarkScope struct {
	Models  []string
	Efforts []string
	Games   []string
}

// AllScope selects every model and effort in the plan.
func (p BenchmarkPlan) AllScope() BenchmarkScope {
	scope := BenchmarkScope{Efforts: append([]string(nil), p.Efforts...), Games: append([]string(nil), p.Games...)}
	for _, model := range p.Models {
		scope.Models = append(scope.Models, model.Model)
	}
	return scope
}

// CombinationCount returns the compatible model/effort pairs in scope.
func (p BenchmarkPlan) CombinationCount(scope BenchmarkScope) int {
	models := stringSet(scope.Models)
	efforts := stringSet(scope.Efforts)
	count := 0
	for _, model := range p.Models {
		if !models[model.Model] {
			continue
		}
		for _, effort := range model.Efforts {
			if efforts[effort] {
				count++
			}
		}
	}
	return count
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
	experimentalAPI       bool
	experimentalRawEvents bool
	turnTimeout           time.Duration
	interruptTimeout      time.Duration
	temporaryCodexHome    string
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
	plan, err := c.BenchmarkPlan(ctx)
	if err != nil {
		return 0, err
	}
	return plan.CombinationCount(plan.AllScope()), nil
}

// BenchmarkPlan returns the visible model/effort catalog without starting a
// model turn.
func (c Client) BenchmarkPlan(ctx context.Context) (BenchmarkPlan, error) {
	server, err := openBenchmarkAppServer(ctx, c.Binary, c.BenchmarkAPIKey)
	if err != nil {
		return BenchmarkPlan{}, err
	}
	defer server.close()
	models, err := server.models(ctx)
	if err != nil {
		return BenchmarkPlan{}, err
	}
	plan := benchmarkPlan(models)
	plan.Games = append([]string(nil), c.DigBenchGames...)
	return plan, nil
}

// RunBenchmarkSuite runs each selected deterministic task once for every
// visible model/reasoning-effort combination advertised by the current login.
var openBenchmarkAppServer = startAppServer

func (c Client) RunBenchmarkSuite(ctx context.Context, taskIDs []BenchmarkTaskID, emit func(BenchmarkEvent)) {
	c.runBenchmarkSuite(ctx, taskIDs, emit)
}

// RunBenchmarkSuiteScoped runs the selected tasks only for compatible pairs in
// scope. It is separate from RunBenchmarkSuite to preserve the all-model
// behavior for callers that do not expose scope controls.
func (c Client) RunBenchmarkSuiteScoped(ctx context.Context, taskIDs []BenchmarkTaskID, scope BenchmarkScope, emit func(BenchmarkEvent)) {
	c.runBenchmarkSuite(ctx, taskIDs, emit, scope)
}

func (c Client) runBenchmarkSuite(ctx context.Context, taskIDs []BenchmarkTaskID, emit func(BenchmarkEvent), scopes ...BenchmarkScope) {
	if emit == nil {
		emit = func(BenchmarkEvent) {}
	}
	if len(taskIDs) == 1 {
		if isDigBenchTask(taskIDs[0]) {
			c.runDigBenchBenchmarkSuite(ctx, taskIDs[0], emit, scopes...)
			return
		}
	}
	for _, id := range taskIDs {
		if isDigBenchTask(id) {
			emit(benchmarkTerminalEvent(ctx, 0, 0, 0, errors.New("DigBench must be run separately from deterministic benchmarks")))
			return
		}
	}
	definitions, err := resolveBenchmarkTasks(taskIDs)
	if err != nil {
		emit(benchmarkTerminalEvent(ctx, 0, 0, 0, err))
		return
	}
	server, err := openBenchmarkAppServer(ctx, c.Binary, c.BenchmarkAPIKey)
	if err != nil {
		emit(benchmarkTerminalEvent(ctx, 0, 0, 0, err))
		return
	}
	defer server.close()

	models, err := server.models(ctx)
	if err != nil {
		emit(benchmarkTerminalEvent(ctx, 0, 0, 0, err))
		return
	}
	combinations := benchmarkCombinations(models, scopes...)
	if len(combinations) == 0 {
		emit(BenchmarkEvent{Done: true, Err: errors.New("selected benchmark scope has no compatible model/effort pairs")})
		return
	}
	total := len(combinations) * len(definitions)
	emit(BenchmarkEvent{Total: total, Combinations: len(combinations)})

	completed := 0
	for _, definition := range definitions {
		for _, combination := range combinations {
			if err := ctx.Err(); err != nil {
				emit(benchmarkTerminalEvent(ctx, total, completed, len(combinations), err))
				return
			}
			event := BenchmarkEvent{
				Total: total, Completed: completed, Combinations: len(combinations),
				CurrentTaskID: definition.task.ID, CurrentTask: definition.task.Name,
				CurrentModel: combination.model.DisplayName, CurrentEffort: combination.effort,
			}
			result, fatalErr := server.runBenchmark(ctx, combination, definition, func(active BenchmarkResult) {
				active = benchmarkResultSnapshot(active)
				event.Active, event.Result = &active, nil
				emit(event)
			})
			if fatalErr != nil && errors.Is(ctx.Err(), context.Canceled) {
				markBenchmarkStopped(&result)
				event.Active, event.Result, event.Stopped = nil, &result, true
				emit(event)
				emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(combinations), Stopped: true, Done: true})
				return
			}
			completed++
			event.Completed, event.Active, event.Result = completed, nil, &result
			emit(event)
			if fatalErr != nil {
				emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(combinations), Done: true, Err: fatalErr})
				return
			}
		}
	}
	emit(BenchmarkEvent{Total: total, Completed: completed, Combinations: len(combinations), Done: true})
}

func startAppServer(ctx context.Context, binary, apiKey string) (*appServerSession, error) {
	return startAppServerWithExperimentalUsage(ctx, binary, strings.TrimSpace(apiKey), true)
}

func benchmarkTerminalEvent(ctx context.Context, total, completed, combinations int, err error) BenchmarkEvent {
	stopped := errors.Is(ctx.Err(), context.Canceled)
	if stopped {
		err = nil
	}
	return BenchmarkEvent{
		Total: total, Completed: completed, Combinations: combinations,
		Stopped: stopped, Done: true, Err: err,
	}
}

func markBenchmarkStopped(result *BenchmarkResult) {
	if result == nil {
		return
	}
	result.Correct = false
	result.Stopped = true
	if !strings.HasPrefix(result.Failure, "remote interruption could not be confirmed") {
		result.Failure = ""
	}
	startedAt := time.Now().Add(-result.Duration)
	appendBenchmarkInteraction(result, startedAt, BenchmarkInteractionVerifier, "Benchmark stopped before completion.")
}

func startAppServerWithExperimentalUsage(ctx context.Context, binary, apiKey string, experimental bool) (*appServerSession, error) {
	if strings.TrimSpace(binary) == "" {
		binary = "codex"
	}
	args := []string{"app-server", "--stdio"}
	temporaryCodexHome := ""
	if apiKey != "" {
		var err error
		temporaryCodexHome, err = os.MkdirTemp("", "codexometer-benchmark-auth-")
		if err != nil {
			return nil, fmt.Errorf("create isolated benchmark Codex home: %w", err)
		}
		args = append(args, "-c", `cli_auth_credentials_store="ephemeral"`)
	}
	removeTemporaryHome := func() {
		if temporaryCodexHome != "" {
			_ = os.RemoveAll(temporaryCodexHome)
		}
	}
	// Calls remain context-bound, but the short-lived process is closed
	// explicitly so a cancelled benchmark still has a chance to send and confirm
	// turn/interrupt before teardown.
	cmd := exec.Command(binary, args...)
	cmd.Env = environmentWithout(os.Environ(), "DIGBENCH_API_TOKEN", "CODEXOMETER_BENCHMARK_API_KEY", "OPENAI_API_KEY")
	if temporaryCodexHome != "" {
		cmd.Env = environmentWith(cmd.Env, "CODEX_HOME", temporaryCodexHome)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		removeTemporaryHome()
		return nil, fmt.Errorf("open Codex input: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		removeTemporaryHome()
		return nil, fmt.Errorf("open Codex output: %w", err)
	}
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		removeTemporaryHome()
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("Codex CLI not found; install it or pass --codex PATH")
		}
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}
	server := &appServerSession{
		cmd: cmd, stdin: stdin, encoder: json.NewEncoder(stdin),
		stderr: stderr, envelopes: make(chan benchmarkEnvelope, 64), readErrors: make(chan error, 1), done: make(chan struct{}),
		experimentalAPI:       experimental,
		experimentalRawEvents: experimental,
		temporaryCodexHome:    temporaryCodexHome,
	}
	go server.readLoop(json.NewDecoder(bufio.NewReader(stdout)))
	initialize := map[string]any{
		"clientInfo": codexometerClientInfo(),
	}
	if experimental {
		initialize["capabilities"] = map[string]bool{"experimentalApi": true}
	}
	if _, err := server.call(ctx, "initialize", initialize, nil); err != nil {
		server.close()
		if experimental && experimentalAPIUnsupported(err) {
			return startAppServerWithExperimentalUsage(ctx, binary, apiKey, false)
		}
		return nil, fmt.Errorf("initialize Codex app-server: %w", err)
	}
	if err := server.encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
		server.close()
		return nil, fmt.Errorf("acknowledge Codex app-server: %w", err)
	}
	if apiKey != "" {
		result, loginErr := server.call(ctx, "account/login/start", map[string]any{
			"type": "apiKey", "apiKey": apiKey,
		}, nil)
		if loginErr != nil {
			server.close()
			return nil, fmt.Errorf("authenticate benchmark Codex app-server with API key: %w", loginErr)
		}
		var login struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(result, &login) != nil || login.Type != "apiKey" {
			server.close()
			return nil, errors.New("Codex returned an invalid API-key login response")
		}
	}
	return server, nil
}

func environmentWithout(environment []string, names ...string) []string {
	excluded := make(map[string]struct{}, len(names))
	for _, name := range names {
		excluded[strings.ToUpper(name)] = struct{}{}
	}
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, remove := excluded[strings.ToUpper(key)]; remove {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func environmentWith(environment []string, name, value string) []string {
	return append(environmentWithout(environment, name), name+"="+value)
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
	envelope, _, err := s.nextEnvelopeOrHeartbeat(ctx, nil)
	return envelope, err
}

func (s *appServerSession) nextEnvelopeOrHeartbeat(ctx context.Context, heartbeat <-chan time.Time) (benchmarkEnvelope, bool, error) {
	// Drain decoded protocol messages before observing a terminal decoder error.
	// A short-lived server may write its final response and close stdout so close
	// together that both channels are ready at once.
	select {
	case envelope := <-s.envelopes:
		return envelope, false, nil
	default:
	}
	select {
	case <-ctx.Done():
		return benchmarkEnvelope{}, false, ctx.Err()
	case <-heartbeat:
		return benchmarkEnvelope{}, true, nil
	case <-s.done:
		return benchmarkEnvelope{}, false, errors.New("Codex app-server session closed")
	case err := <-s.readErrors:
		select {
		case envelope := <-s.envelopes:
			return envelope, false, nil
		default:
		}
		if errors.Is(err, io.EOF) {
			return benchmarkEnvelope{}, false, fmt.Errorf("Codex app-server closed unexpectedly: %s", strings.TrimSpace(s.stderr.String()))
		}
		return benchmarkEnvelope{}, false, err
	case envelope := <-s.envelopes:
		return envelope, false, nil
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
	if s.temporaryCodexHome != "" {
		_ = os.RemoveAll(s.temporaryCodexHome)
		s.temporaryCodexHome = ""
	}
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

func benchmarkCombinations(models []benchmarkModel, scopes ...BenchmarkScope) []benchmarkCombination {
	var selectedModels, selectedEfforts map[string]bool
	if len(scopes) > 0 {
		selectedModels = stringSet(scopes[0].Models)
		selectedEfforts = stringSet(scopes[0].Efforts)
	}
	var combinations []benchmarkCombination
	for _, model := range models {
		if strings.TrimSpace(model.Model) == "" {
			continue
		}
		if selectedModels != nil && !selectedModels[model.Model] {
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
			if selectedEfforts == nil || selectedEfforts[effort] {
				combinations = append(combinations, benchmarkCombination{model: model, effort: effort})
			}
			continue
		}
		for _, option := range model.SupportedReasoningEfforts {
			if effort := strings.TrimSpace(option.ReasoningEffort); effort != "" && (selectedEfforts == nil || selectedEfforts[effort]) {
				combinations = append(combinations, benchmarkCombination{model: model, effort: effort})
			}
		}
	}
	return combinations
}

func benchmarkPlan(models []benchmarkModel) BenchmarkPlan {
	combinations := benchmarkCombinations(models)
	plan := BenchmarkPlan{}
	modelIndexes := make(map[string]int)
	efforts := make(map[string]bool)
	for _, combination := range combinations {
		index, ok := modelIndexes[combination.model.Model]
		if !ok {
			index = len(plan.Models)
			modelIndexes[combination.model.Model] = index
			plan.Models = append(plan.Models, BenchmarkModelOption{
				Model: combination.model.Model, DisplayName: combination.model.DisplayName,
			})
		}
		plan.Models[index].Efforts = append(plan.Models[index].Efforts, combination.effort)
		if !efforts[combination.effort] {
			efforts[combination.effort] = true
			plan.Efforts = append(plan.Efforts, combination.effort)
		}
	}
	return plan
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func (s *appServerSession) runBenchmark(
	ctx context.Context,
	combination benchmarkCombination,
	definition benchmarkDefinition,
	progressCallbacks ...func(BenchmarkResult),
) (BenchmarkResult, error) {
	result := benchmarkPendingResult(combination, definition)
	var startedAt time.Time
	publish := func() {
		if len(progressCallbacks) > 0 && progressCallbacks[0] != nil {
			snapshot := benchmarkResultSnapshot(result)
			if !startedAt.IsZero() {
				snapshot.Duration = time.Since(startedAt)
			}
			progressCallbacks[0](snapshot)
		}
	}
	publish()
	turnTimeout := s.benchmarkTurnTimeout()

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
	setupCtx, cancelSetup := context.WithTimeout(ctx, turnTimeout)
	threadResult, err := s.call(setupCtx, "thread/start", threadParams, nil)
	if err != nil && s.experimentalRawEvents && experimentalAPIUnsupported(err) {
		// Older Codex versions may support the stable benchmark API but not raw
		// response telemetry. Retry this thread without the experimental field and
		// keep cumulative usage as the compatibility path for the rest of the suite.
		s.experimentalRawEvents = false
		delete(threadParams, "experimentalRawEvents")
		threadResult, err = s.call(setupCtx, "thread/start", threadParams, nil)
	}
	setupErr := fatalBenchmarkError(setupCtx, err)
	cancelSetup()
	if err != nil {
		result.Failure = fmt.Sprintf("start thread: %v", err)
		return result, setupErr
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
		publish()
	}
	startedAt = time.Now()
	prompt := benchmarkPrompt(definition)
	turnParams := map[string]any{
		"threadId":     startedThread.Thread.ID,
		"input":        []map[string]any{{"type": "text", "text": prompt}},
		"outputSchema": benchmarkOutputSchema,
	}
	if combination.effort != "default" && combination.effort != "" {
		turnParams["effort"] = combination.effort
	}
	turnCtx, cancelTurn := context.WithTimeout(ctx, turnTimeout)
	defer cancelTurn()
	turnResponse, err := s.call(turnCtx, "turn/start", turnParams, nil)
	if err != nil {
		result.Duration = time.Since(startedAt)
		result.Failure = fmt.Sprintf("start turn: %v", err)
		appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, "Run failed before the model turn started.")
		publish()
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
		appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, result.Failure)
		publish()
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
				preview := benchmarkResultSnapshot(result)
				preview.Duration = time.Since(startedAt)
				applyBenchmarkMeasurements(&preview, telemetry)
				if len(progressCallbacks) > 0 && progressCallbacks[0] != nil {
					progressCallbacks[0](preview)
				}
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
				publish()
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
				changed := false
				if benchmarkToolItem(event.Item.Type) && !result.ToolUsed {
					result.ToolUsed, result.ToolType = true, event.Item.Type
					appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionPolicy, "Prohibited tool event observed: "+event.Item.Type)
					changed = true
				}
				if method == "item/completed" && event.Item.Type == "agentMessage" {
					finalMessage = event.Item.Text
					appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionResponse, finalMessage)
					changed = true
				}
				if changed {
					publish()
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
							appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionResponse, finalMessage)
							publish()
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
			if errors.Is(ctx.Err(), context.Canceled) {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), s.benchmarkInterruptTimeout())
				interruptErr := s.interruptBenchmarkTurn(cleanupCtx, startedThread.Thread.ID, startedTurn.Turn.ID, &completed, handleNotification)
				cancel()
				applyBenchmarkMeasurements(&result, telemetry)
				if interruptErr != nil {
					result.Failure = "remote interruption could not be confirmed: " + interruptErr.Error()
					appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, "Stop requested; "+result.Failure)
				}
				return result, ctx.Err()
			}
			if recoverableBenchmarkTimeout(ctx, turnCtx, err) {
				result.Failure = fmt.Sprintf("turn timed out after %s", turnTimeout)
				if interruptErr := s.interruptBenchmarkTurn(ctx, startedThread.Thread.ID, startedTurn.Turn.ID, &completed, handleNotification); interruptErr != nil {
					result.Failure += "; cleanup failed: " + interruptErr.Error()
					appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, "Turn timed out and interruption could not be confirmed.")
					return result, fmt.Errorf("recover timed-out benchmark turn: %w", interruptErr)
				}
				applyBenchmarkMeasurements(&result, telemetry)
				appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, result.Failure)
				return result, nil
			}
			result.Failure = fmt.Sprintf("wait for turn: %v", err)
			appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, "App-server transport failed before benchmark completion.")
			return result, err
		}
	}
	result.Duration = time.Since(startedAt)
	applyBenchmarkMeasurements(&result, telemetry)
	if turnFailure != "" {
		result.Failure = turnFailure
		appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, "Codex turn did not complete successfully.")
		return result, nil
	}
	if result.ToolUsed {
		result.Failure = "tool use prohibited: " + result.ToolType
		appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, result.Failure)
		return result, nil
	}
	code, err := benchmarkCode(finalMessage)
	if err != nil {
		result.Failure = err.Error()
		appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, result.Failure)
		return result, nil
	}
	if err := definition.verify(code); err != nil {
		result.Failure = err.Error()
		appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, result.Failure)
		return result, nil
	}
	result.Correct = true
	appendBenchmarkInteraction(&result, startedAt, BenchmarkInteractionVerifier, "Submission passed the deterministic verifier.")
	return result, nil
}

func benchmarkPendingResult(combination benchmarkCombination, definition benchmarkDefinition) BenchmarkResult {
	result := BenchmarkResult{
		TaskID: definition.task.ID, TaskName: definition.task.Name,
		Model: combination.model.Model, DisplayName: combination.model.DisplayName,
		Effort: combination.effort, ActualModel: combination.model.Model,
	}
	appendBenchmarkInteraction(&result, time.Time{}, BenchmarkInteractionPrompt, benchmarkPrompt(definition))
	return result
}

func benchmarkResultSnapshot(result BenchmarkResult) BenchmarkResult {
	result.Interactions = append([]BenchmarkInteraction(nil), result.Interactions...)
	result.ResponseUsage = append([]BenchmarkResponseUsage(nil), result.ResponseUsage...)
	return result
}

func appendBenchmarkInteraction(result *BenchmarkResult, startedAt time.Time, kind BenchmarkInteractionKind, content string) {
	if result == nil || len(result.Interactions) >= benchmarkInteractionCount {
		return
	}
	used := 0
	for _, interaction := range result.Interactions {
		used += len(interaction.Content)
	}
	remaining := benchmarkTranscriptLimit - used
	if remaining <= 0 {
		return
	}
	content = strings.ToValidUTF8(content, "�")
	limit := min(benchmarkInteractionLimit, remaining)
	if len(content) > limit {
		const marker = "\n… [truncated by Codexometer]"
		contentLimit := max(limit-len(marker), 0)
		content = strings.ToValidUTF8(content[:contentLimit], "")
		if len(marker) <= limit {
			content += marker
		}
	}
	elapsed := time.Duration(0)
	if !startedAt.IsZero() {
		elapsed = time.Since(startedAt)
	}
	result.Interactions = append(result.Interactions, BenchmarkInteraction{Elapsed: elapsed, Kind: kind, Content: content})
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
		if result.UsageSource == BenchmarkUsageRawResponses {
			result.CostKnown = true
			for _, response := range result.ResponseUsage {
				cost, known, issue := estimateSingleResponseAPICostWithIssue(result.ActualModel, response.Usage)
				if !known {
					result.CostUSD, result.CostKnown, result.CostIssue = 0, false, issue
					break
				}
				result.CostUSD += cost
			}
		} else {
			result.CostUSD, result.CostKnown, result.CostIssue = estimateAPICostWithIssue(result.ActualModel, result.Usage)
		}
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
	if usage.schemaIssue != "" {
		return usage.schemaIssue
	}
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
		envelope, err := s.nextPendingEnvelope(ctx)
		if err != nil {
			return nil, err
		}
		if envelope.Method != "" && accept(envelope.Method, envelope.Params) {
			return envelope.Params, nil
		}
	}
}

func (s *appServerSession) nextPendingEnvelope(ctx context.Context) (benchmarkEnvelope, error) {
	envelope, _, err := s.nextPendingEnvelopeOrHeartbeat(ctx, nil)
	return envelope, err
}

func (s *appServerSession) nextPendingEnvelopeOrHeartbeat(ctx context.Context, heartbeat <-chan time.Time) (benchmarkEnvelope, bool, error) {
	if len(s.pending) > 0 {
		envelope := s.pending[0]
		s.pending = s.pending[1:]
		return envelope, false, nil
	}
	return s.nextEnvelopeOrHeartbeat(ctx, heartbeat)
}

func (s *appServerSession) respond(id json.RawMessage, result any) error {
	if len(id) == 0 {
		return errors.New("app-server request omitted id")
	}
	return s.encoder.Encode(map[string]any{"id": id, "result": result})
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
	longInput       float64
	longCached      float64
	longCacheWrite  float64
	longOutput      float64
	longKnown       bool
}

var standardAPIPrices = map[string]apiPrice{
	"gpt-5.6-sol":   {input: 4.00, cached: 0.40, cacheWrite: 5.00, cacheWriteKnown: true, output: 20.00, longInput: 8.00, longCached: 0.80, longCacheWrite: 10.00, longOutput: 30.00, longKnown: true},
	"gpt-5.6-terra": {input: 2.00, cached: 0.20, cacheWrite: 2.50, cacheWriteKnown: true, output: 12.00, longInput: 4.00, longCached: 0.40, longCacheWrite: 5.00, longOutput: 18.00, longKnown: true},
	"gpt-5.6-luna":  {input: 0.20, cached: 0.02, cacheWrite: 0.25, cacheWriteKnown: true, output: 1.20, longInput: 0.40, longCached: 0.04, longCacheWrite: 0.50, longOutput: 1.80, longKnown: true},
	"gpt-5.5":       {input: 5.00, cached: 0.50, output: 30.00, longInput: 10.00, longCached: 1.00, longOutput: 45.00, longKnown: true},
	"gpt-5.4":       {input: 2.50, cached: 0.25, output: 15.00, longInput: 5.00, longCached: 0.50, longOutput: 22.50, longKnown: true},
	"gpt-5.4-mini":  {input: 0.75, cached: 0.075, output: 4.50},
	"gpt-5.3-codex": {input: 1.75, cached: 0.175, output: 14.00},
}

func estimateAPICost(model string, usage BenchmarkUsage) (float64, bool) {
	cost, known, _ := estimateAPICostWithIssue(model, usage)
	return cost, known
}

func estimateAPICostWithIssue(model string, usage BenchmarkUsage) (float64, bool, string) {
	if usage.InputTokens > 272_000 {
		return 0, false, "long-context pricing requires per-response usage"
	}
	return estimateSingleResponseAPICostWithIssue(model, usage)
}

func estimateSingleResponseAPICostWithIssue(model string, usage BenchmarkUsage) (float64, bool, string) {
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
	if usage.InputTokens > 272_000 {
		if !price.longKnown {
			return 0, false, "no published long-context price for model " + strings.TrimSpace(model)
		}
		price.input = price.longInput
		price.cached = price.longCached
		price.cacheWrite = price.longCacheWrite
		price.output = price.longOutput
	}
	regularInput := usage.InputTokens - usage.CachedInputTokens - usage.CacheWriteInputTokens
	cost := float64(regularInput)*price.input +
		float64(usage.CachedInputTokens)*price.cached +
		float64(usage.CacheWriteInputTokens)*price.cacheWrite +
		float64(usage.OutputTokens)*price.output
	return cost / 1_000_000, true, ""
}

// EstimateStandardAPIEqCost prices one model response at the published
// standard API text-token rates embedded in this release. Unknown models,
// token shapes, or pricing classes fail closed so callers cannot silently
// understate an aggregate.
func EstimateStandardAPIEqCost(model string, usage BenchmarkUsage) (float64, bool, string) {
	return estimateSingleResponseAPICostWithIssue(model, usage)
}

// EstimateStandardAPIEqAggregateCost prices aggregate usage only when its
// standard API cost can be reconstructed without response boundaries. Long
// context usage therefore fails closed because its surcharge is per response.
func EstimateStandardAPIEqAggregateCost(model string, usage BenchmarkUsage) (float64, bool, string) {
	return estimateAPICostWithIssue(model, usage)
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

package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const correctStarlarkSubmission = `
def merge_ranges(ranges):
    items = []
    for pair in ranges:
        items.append([pair[0], pair[1]])
    items = sorted(items)
    result = []
    for pair in items:
        if not result or pair[0] > result[-1][1] + 1:
            result.append([pair[0], pair[1]])
        elif pair[1] > result[-1][1]:
            result[-1][1] = pair[1]
    return result
`

const correctLRUSubmission = `
def lru_cache(capacity, operations):
    entries = []
    gets = []
    for operation in operations:
        found = -1
        for i in range(len(entries)):
            if entries[i][0] == operation[1]:
                found = i
        if operation[0] == "get":
            if found < 0:
                gets.append(-1)
            else:
                entry = entries[found]
                gets.append(entry[1])
                entries = [entry] + entries[:found] + entries[found + 1:]
        else:
            if found >= 0:
                entries = entries[:found] + entries[found + 1:]
            entries = [[operation[1], operation[2]]] + entries
            if len(entries) > capacity:
                entries = entries[:capacity]
    return [gets, entries]
`

const correctExpressionSubmission = `
def _apply_operator(values, operators):
    right = values.pop()
    left = values.pop()
    operator = operators.pop()
    if operator == "+":
        values.append(left + right)
    elif operator == "-":
        values.append(left - right)
    else:
        values.append(left * right)

def evaluate_expression(tokens):
    values = []
    operators = []
    precedence = {"+": 1, "-": 1, "*": 2}
    for token in tokens:
        if token == "(":
            operators.append(token)
        elif token == ")":
            for _ in range(len(operators)):
                if operators[-1] == "(":
                    break
                _apply_operator(values, operators)
            operators.pop()
        elif token == "+" or token == "-" or token == "*":
            for _ in range(len(operators)):
                if operators[-1] == "(" or precedence[operators[-1]] < precedence[token]:
                    break
                _apply_operator(values, operators)
            operators.append(token)
        else:
            values.append(int(token))
    for _ in range(len(operators)):
        _apply_operator(values, operators)
    return values[0]
`

const correctShortestPathSubmission = `
def shortest_path(grid, start, end):
    distances = []
    for row in grid:
        distances.append([-1 for _ in row])
    queue = [[start[0], start[1]]]
    distances[start[0]][start[1]] = 0
    head = 0
    for _ in range(len(grid) * len(grid[0])):
        if head >= len(queue):
            break
        current = queue[head]
        head += 1
        if current == end:
            return distances[current[0]][current[1]]
        for delta in [[-1, 0], [1, 0], [0, -1], [0, 1]]:
            row = current[0] + delta[0]
            column = current[1] + delta[1]
            if row >= 0 and row < len(grid) and column >= 0 and column < len(grid[0]):
                if grid[row][column] == 0 and distances[row][column] < 0:
                    distances[row][column] = distances[current[0]][current[1]] + 1
                    queue.append([row, column])
    return -1
`

func TestVerifyMergeRangesAcceptsCorrectHermeticSubmission(t *testing.T) {
	if err := verifyMergeRanges(correctStarlarkSubmission); err != nil {
		t.Fatalf("correct submission failed: %v", err)
	}
}

func TestAdditionalBenchmarkVerifiersAcceptCorrectSubmissions(t *testing.T) {
	tests := []struct {
		name   string
		verify func(string) error
		code   string
	}{
		{name: "LRU cache", verify: verifyLRUCache, code: correctLRUSubmission},
		{name: "expression parser", verify: verifyExpressionParser, code: correctExpressionSubmission},
		{name: "shortest path", verify: verifyShortestPath, code: correctShortestPathSubmission},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.verify(test.code); err != nil {
				t.Fatalf("correct submission failed: %v", err)
			}
		})
	}
}

func TestAdditionalBenchmarkVerifiersRejectIncorrectSubmissions(t *testing.T) {
	tests := []struct {
		name   string
		verify func(string) error
		code   string
	}{
		{name: "LRU cache", verify: verifyLRUCache, code: "def lru_cache(capacity, operations):\n    return [[], []]\n"},
		{name: "expression parser", verify: verifyExpressionParser, code: "def evaluate_expression(tokens):\n    return 0\n"},
		{name: "shortest path", verify: verifyShortestPath, code: "def shortest_path(grid, start, end):\n    return -1\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.verify(test.code); err == nil {
				t.Fatal("incorrect submission unexpectedly passed")
			}
		})
	}
}

func TestBenchmarkTaskCatalogAndSelection(t *testing.T) {
	tasks := BenchmarkTasks()
	if len(tasks) != 7 || tasks[0].ID != BenchmarkMergeRanges || tasks[3].ID != BenchmarkShortestPath || tasks[6].ID != BenchmarkEventProcessor {
		t.Fatalf("task catalog = %#v", tasks)
	}
	definitions, err := resolveBenchmarkTasks([]BenchmarkTaskID{BenchmarkShortestPath, BenchmarkLRUCache, BenchmarkLRUCache})
	if err != nil || len(definitions) != 2 || definitions[0].task.ID != BenchmarkLRUCache || definitions[1].task.ID != BenchmarkShortestPath {
		t.Fatalf("resolved tasks = %#v, %v", definitions, err)
	}
	if _, err := resolveBenchmarkTasks(nil); err == nil {
		t.Fatal("empty benchmark selection was accepted")
	}
	if _, err := resolveBenchmarkTasks([]BenchmarkTaskID{"unknown"}); err == nil {
		t.Fatal("unknown benchmark selection was accepted")
	}
	for _, definition := range benchmarkTaskDefinitions {
		prompt := benchmarkPrompt(definition)
		for _, required := range []string{definition.function, "Starlark language contract", "while loops", `field named "code"`} {
			if !strings.Contains(prompt, required) {
				t.Fatalf("%s prompt is missing %q", definition.task.ID, required)
			}
		}
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func newFakeBenchmarkServer(envelopes ...benchmarkEnvelope) (*appServerSession, *bytes.Buffer) {
	requests := &bytes.Buffer{}
	server := &appServerSession{
		stdin: nopWriteCloser{requests}, encoder: json.NewEncoder(requests),
		envelopes:  make(chan benchmarkEnvelope, len(envelopes)+16),
		readErrors: make(chan error, 1), done: make(chan struct{}), stderr: &lockedBuffer{},
	}
	for _, envelope := range envelopes {
		server.envelopes <- envelope
	}
	return server, requests
}

func rawJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func TestRunBenchmarkConsumesStreamedResultAndUsage(t *testing.T) {
	message := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	server, requests := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-1"}, "model": "gpt-5.3-codex",
		})},
		// A fast notification arriving before the turn/start response must be retained.
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1",
			"item": map[string]string{"type": "agentMessage", "text": message},
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{
			"turn": map[string]string{"id": "turn-1"},
		})},
		benchmarkEnvelope{Method: "thread/tokenUsage/updated", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1",
			"tokenUsage": map[string]any{"total": BenchmarkUsage{TotalTokens: 1200, InputTokens: 1000, CachedInputTokens: 200, OutputTokens: 200}},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1",
			"turn":     map[string]any{"id": "turn-1", "status": "completed", "items": []any{}},
		})},
	)
	result, fatalErr := server.runBenchmark(context.Background(), benchmarkCombination{
		model: benchmarkModel{Model: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex"}, effort: "high",
	}, benchmarkTaskDefinitions[0])
	if fatalErr != nil || !result.Correct {
		t.Fatalf("benchmark result = %#v, fatal error = %v", result, fatalErr)
	}
	if result.Usage.TotalTokens != 1200 || !result.UsageObserved || !result.UsageKnown || result.UsageSource != BenchmarkUsageCumulative || !result.CostKnown || result.CostUSD <= 0 {
		t.Fatalf("usage/cost not captured: %#v", result)
	}
	if !strings.Contains(requests.String(), `"method":"thread/start"`) || !strings.Contains(requests.String(), `"effort":"high"`) {
		t.Fatalf("requests missing thread or effort: %s", requests.String())
	}
	if !strings.Contains(requests.String(), `"sandbox":"read-only"`) {
		t.Fatalf("thread request did not use the app-server sandbox spelling: %s", requests.String())
	}
	if len(result.Interactions) != 3 || result.Interactions[0].Kind != BenchmarkInteractionPrompt || result.Interactions[1].Kind != BenchmarkInteractionResponse || result.Interactions[2].Kind != BenchmarkInteractionVerifier {
		t.Fatalf("benchmark interaction transcript = %#v", result.Interactions)
	}
	if !strings.Contains(result.Interactions[0].Content, "Starlark language contract") || result.Interactions[1].Content != message || !strings.Contains(result.Interactions[2].Content, "passed") {
		t.Fatalf("benchmark interaction content = %#v", result.Interactions)
	}
	for _, interaction := range result.Interactions {
		if strings.Contains(interaction.Content, "thread-1") || strings.Contains(interaction.Content, "turn-1") {
			t.Fatalf("internal app-server ID leaked into benchmark transcript: %#v", result.Interactions)
		}
	}
}

func TestBenchmarkInteractionCaptureIsBoundedAndValidUTF8(t *testing.T) {
	countBounded := BenchmarkResult{}
	for index := 0; index < benchmarkInteractionCount+4; index++ {
		appendBenchmarkInteraction(&countBounded, time.Time{}, BenchmarkInteractionPolicy, "event")
	}
	if len(countBounded.Interactions) != benchmarkInteractionCount {
		t.Fatalf("captured %d interactions, want %d", len(countBounded.Interactions), benchmarkInteractionCount)
	}

	result := BenchmarkResult{}
	content := strings.Repeat("x", benchmarkInteractionLimit-1) + "£" + strings.Repeat("y", 10)
	for index := 0; index < benchmarkInteractionCount+4; index++ {
		appendBenchmarkInteraction(&result, time.Time{}, BenchmarkInteractionResponse, content)
	}
	total := 0
	for _, interaction := range result.Interactions {
		total += len(interaction.Content)
		if !strings.Contains(interaction.Content, "[truncated by Codexometer]") || !utf8.ValidString(interaction.Content) {
			t.Fatalf("bounded interaction was not valid, visibly truncated UTF-8: %q", interaction.Content[len(interaction.Content)-64:])
		}
	}
	if total > benchmarkTranscriptLimit {
		t.Fatalf("transcript captured %d bytes, limit is %d", total, benchmarkTranscriptLimit)
	}
}

func TestRunBenchmarkFailsClosedWhenUsageIsMissing(t *testing.T) {
	message := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	server, _ := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-1"}, "model": "gpt-5.3-codex",
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-1"}})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "item": map[string]string{"type": "agentMessage", "text": message},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed"},
		})},
	)
	result, fatalErr := server.runBenchmark(context.Background(), benchmarkCombination{
		model: benchmarkModel{Model: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex"}, effort: "low",
	}, benchmarkTaskDefinitions[0])
	if fatalErr != nil || !result.Correct {
		t.Fatalf("correct result was affected by missing telemetry: %#v, %v", result, fatalErr)
	}
	if result.UsageObserved || result.UsageKnown || result.CostKnown || result.UsageIssue != "matching usage event was not observed" || result.CostIssue != result.UsageIssue {
		t.Fatalf("missing usage did not fail closed: %#v", result)
	}
}

func TestRunBenchmarkSuiteInterruptsTimedOutTurnAndContinues(t *testing.T) {
	message := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	server, requests := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"data": []any{map[string]any{
				"model": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex",
				"supportedReasoningEfforts": []any{
					map[string]string{"reasoningEffort": "low"},
					map[string]string{"reasoningEffort": "high"},
				},
			}},
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-low"}, "model": "gpt-5.3-codex",
		})},
		benchmarkEnvelope{ID: rawJSON(3), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-low"}})},
	)
	server.turnTimeout = 20 * time.Millisecond
	server.interruptTimeout = 2 * time.Second
	go func() {
		time.Sleep(60 * time.Millisecond)
		server.envelopes <- benchmarkEnvelope{ID: rawJSON(4), Result: rawJSON(map[string]any{})}
		server.envelopes <- benchmarkEnvelope{Method: "thread/tokenUsage/updated", Params: rawJSON(map[string]any{
			"threadId": "thread-low", "turnId": "turn-low",
			"tokenUsage": map[string]any{"total": BenchmarkUsage{TotalTokens: 55, InputTokens: 50, OutputTokens: 5}},
		})}
		server.envelopes <- benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-low", "turn": map[string]any{"id": "turn-low", "status": "interrupted"},
		})}
		server.envelopes <- benchmarkEnvelope{ID: rawJSON(5), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-high"}, "model": "gpt-5.3-codex",
		})}
		server.envelopes <- benchmarkEnvelope{ID: rawJSON(6), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-high"}})}
		server.envelopes <- benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-high", "turnId": "turn-high",
			"item": map[string]string{"type": "agentMessage", "text": message},
		})}
		server.envelopes <- benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-high", "turn": map[string]any{"id": "turn-high", "status": "completed"},
		})}
	}()

	original := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string) (*appServerSession, error) { return server, nil }
	t.Cleanup(func() { openBenchmarkAppServer = original })

	var results []BenchmarkResult
	var final BenchmarkEvent
	Client{}.RunBenchmarkSuite(context.Background(), []BenchmarkTaskID{BenchmarkMergeRanges}, func(event BenchmarkEvent) {
		if event.Result != nil {
			results = append(results, *event.Result)
		}
		final = event
	})
	if len(results) != 2 {
		t.Fatalf("suite produced %d results, want 2: %#v", len(results), results)
	}
	if results[0].Correct || results[0].Failure != "turn timed out after 20ms" || !results[0].UsageKnown || results[0].Usage.TotalTokens != 55 {
		t.Fatalf("timed-out result was not retained as a measured failure: %#v", results[0])
	}
	if !results[1].Correct || results[1].Effort != "high" {
		t.Fatalf("suite did not continue to the next combination: %#v", results[1])
	}
	if !final.Done || final.Err != nil || final.Completed != 2 || final.Total != 2 {
		t.Fatalf("suite ended as a global fault: %#v", final)
	}
	requestLog := requests.String()
	if !strings.Contains(requestLog, `"method":"turn/interrupt"`) ||
		!strings.Contains(requestLog, `"threadId":"thread-low"`) ||
		!strings.Contains(requestLog, `"turnId":"turn-low"`) {
		t.Fatalf("timed-out turn was not interrupted: %s", requestLog)
	}
}

func TestRunBenchmarkStopsWhenTimedOutTurnCannotBeCleanedUp(t *testing.T) {
	server, _ := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-1"}, "model": "gpt-5.3-codex",
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-1"}})},
	)
	server.turnTimeout = 10 * time.Millisecond
	server.interruptTimeout = 20 * time.Millisecond
	result, fatalErr := server.runBenchmark(context.Background(), benchmarkCombination{
		model: benchmarkModel{Model: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex"}, effort: "low",
	}, benchmarkTaskDefinitions[0])
	if fatalErr == nil || !errors.Is(fatalErr, context.DeadlineExceeded) {
		t.Fatalf("failed cleanup error = %v, want deadline exceeded", fatalErr)
	}
	if result.Correct || !strings.Contains(result.Failure, "turn timed out after 10ms; cleanup failed") {
		t.Fatalf("failed cleanup result = %#v", result)
	}
}

func TestRunBenchmarkRejectsToolUseAndInvalidatesCost(t *testing.T) {
	message := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	usage := BenchmarkUsage{TotalTokens: 120, InputTokens: 100, OutputTokens: 20}
	server, _ := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-1"}, "model": "gpt-5.3-codex",
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-1"}})},
		benchmarkEnvelope{Method: "item/started", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "item": map[string]string{"type": "commandExecution"},
		})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "item": map[string]string{"type": "agentMessage", "text": message},
		})},
		benchmarkEnvelope{Method: "thread/tokenUsage/updated", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "tokenUsage": map[string]any{"total": usage},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed"},
		})},
	)
	result, fatalErr := server.runBenchmark(context.Background(), benchmarkCombination{
		model: benchmarkModel{Model: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex"}, effort: "low",
	}, benchmarkTaskDefinitions[0])
	if fatalErr != nil || result.Correct || !result.ToolUsed || result.ToolType != "commandExecution" {
		t.Fatalf("tool protocol violation was not rejected: %#v, %v", result, fatalErr)
	}
	if result.Failure != "tool use prohibited: commandExecution" || result.CostKnown || result.CostIssue != result.Failure || !result.UsageKnown {
		t.Fatalf("tool result accounting is invalid: %#v", result)
	}
}

func TestRunBenchmarkPrefersCompleteRawResponseUsage(t *testing.T) {
	message := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	first := BenchmarkUsage{TotalTokens: 110, InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10}
	second := BenchmarkUsage{TotalTokens: 55, InputTokens: 50, OutputTokens: 5}
	total := BenchmarkUsage{TotalTokens: 165, InputTokens: 150, CachedInputTokens: 20, OutputTokens: 15}
	server, requests := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-1"}, "model": "gpt-5.3-codex",
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-1"}})},
		benchmarkEnvelope{Method: "rawResponse/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "responseId": "response-1", "usage": first,
		})},
		benchmarkEnvelope{Method: "rawResponse/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "responseId": "response-2", "usage": second,
		})},
		benchmarkEnvelope{Method: "thread/tokenUsage/updated", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "tokenUsage": map[string]any{"total": total},
		})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "item": map[string]string{"type": "agentMessage", "text": message},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed"},
		})},
	)
	server.experimentalRawEvents = true
	result, fatalErr := server.runBenchmark(context.Background(), benchmarkCombination{
		model: benchmarkModel{Model: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex"}, effort: "low",
	}, benchmarkTaskDefinitions[0])
	if fatalErr != nil || !result.Correct || !result.UsageKnown || result.UsageSource != BenchmarkUsageRawResponses || result.Usage != total || len(result.ResponseUsage) != 2 {
		t.Fatalf("raw response usage was not selected: %#v, %v", result, fatalErr)
	}
	if !strings.Contains(requests.String(), `"experimentalRawEvents":true`) {
		t.Fatalf("thread did not request raw events: %s", requests.String())
	}
}

func TestRunBenchmarkFallsBackWhenRawEventsAreUnsupported(t *testing.T) {
	message := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	usage := BenchmarkUsage{TotalTokens: 120, InputTokens: 100, OutputTokens: 20}
	server, requests := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Error: &benchmarkRPCError{Code: -32602, Message: "unknown field experimentalRawEvents"}},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{
			"thread": map[string]string{"id": "thread-1"}, "model": "gpt-5.3-codex",
		})},
		benchmarkEnvelope{ID: rawJSON(3), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-1"}})},
		benchmarkEnvelope{Method: "thread/tokenUsage/updated", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "tokenUsage": map[string]any{"total": usage},
		})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "item": map[string]string{"type": "agentMessage", "text": message},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": "completed"},
		})},
	)
	server.experimentalRawEvents = true
	result, fatalErr := server.runBenchmark(context.Background(), benchmarkCombination{
		model: benchmarkModel{Model: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex"}, effort: "low",
	}, benchmarkTaskDefinitions[0])
	if fatalErr != nil || !result.Correct || !result.UsageKnown || result.UsageSource != BenchmarkUsageCumulative {
		t.Fatalf("stable cumulative fallback failed: %#v, %v", result, fatalErr)
	}
	if server.experimentalRawEvents || strings.Count(requests.String(), `"experimentalRawEvents":true`) != 1 {
		t.Fatalf("raw-event compatibility fallback did not disable the field: %s", requests.String())
	}
}

func TestBenchmarkTelemetryRejectsInconsistentStreams(t *testing.T) {
	valid := BenchmarkUsage{TotalTokens: 110, InputTokens: 100, OutputTokens: 10}
	for _, test := range []struct {
		name   string
		record func(*benchmarkTelemetry)
		want   string
	}{
		{name: "negative", record: func(state *benchmarkTelemetry) {
			state.recordCumulative(BenchmarkUsage{TotalTokens: -1})
		}, want: "negative"},
		{name: "invalid breakdown", record: func(state *benchmarkTelemetry) {
			state.recordCumulative(BenchmarkUsage{TotalTokens: 10, InputTokens: 5, CachedInputTokens: 6, OutputTokens: 5})
		}, want: "exceed"},
		{name: "regression", record: func(state *benchmarkTelemetry) {
			state.recordCumulative(valid)
			state.recordCumulative(BenchmarkUsage{TotalTokens: 55, InputTokens: 50, OutputTokens: 5})
		}, want: "regressed"},
		{name: "raw mismatch", record: func(state *benchmarkTelemetry) {
			state.recordRawResponse("response-1", &valid)
			state.recordCumulative(BenchmarkUsage{TotalTokens: 120, InputTokens: 100, OutputTokens: 20})
		}, want: "disagree"},
		{name: "duplicate response", record: func(state *benchmarkTelemetry) {
			state.recordRawResponse("response-1", nil)
			state.recordRawResponse("response-1", nil)
		}, want: "duplicate"},
		{name: "raw aggregate overflow", record: func(state *benchmarkTelemetry) {
			maximum := BenchmarkUsage{TotalTokens: math.MaxInt64, InputTokens: math.MaxInt64}
			state.recordRawResponse("response-1", &maximum)
			state.recordRawResponse("response-2", &maximum)
		}, want: "overflowed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newBenchmarkTelemetry()
			test.record(state)
			var result BenchmarkResult
			state.apply(&result)
			if result.UsageKnown || !strings.Contains(result.UsageIssue, test.want) {
				t.Fatalf("inconsistent telemetry did not fail closed: %#v", result)
			}
		})
	}
}

func TestBenchmarkTelemetryFallsBackWhenRawUsageIsOmitted(t *testing.T) {
	valid := BenchmarkUsage{TotalTokens: 110, InputTokens: 100, OutputTokens: 10}
	state := newBenchmarkTelemetry()
	state.recordRawResponse("response-1", nil)
	state.recordCumulative(valid)
	var result BenchmarkResult
	state.apply(&result)
	if !result.UsageObserved || !result.UsageKnown || result.UsageSource != BenchmarkUsageCumulative || result.Usage != valid || result.UsageIssue != "" {
		t.Fatalf("valid cumulative fallback was not used: %#v", result)
	}
}

func TestValidateBenchmarkUsageInvariants(t *testing.T) {
	valid := BenchmarkUsage{
		TotalTokens: 130, InputTokens: 100, CachedInputTokens: 20,
		CacheWriteInputTokens: 10, OutputTokens: 30, ReasoningOutputTokens: 15,
	}
	if issue := validateBenchmarkUsage(valid); issue != "" {
		t.Fatalf("valid usage rejected: %s", issue)
	}
	for _, mutation := range []func(*BenchmarkUsage){
		func(usage *BenchmarkUsage) { usage.OutputTokens = -1 },
		func(usage *BenchmarkUsage) { usage.CachedInputTokens = 95 },
		func(usage *BenchmarkUsage) { usage.ReasoningOutputTokens = 31 },
		func(usage *BenchmarkUsage) { usage.TotalTokens++ },
		func(usage *BenchmarkUsage) {
			*usage = BenchmarkUsage{
				TotalTokens: math.MaxInt64, InputTokens: math.MaxInt64,
				CachedInputTokens: math.MaxInt64, CacheWriteInputTokens: math.MaxInt64,
			}
		},
		func(usage *BenchmarkUsage) {
			*usage = BenchmarkUsage{TotalTokens: math.MaxInt64, InputTokens: math.MaxInt64, OutputTokens: 1}
		},
	} {
		usage := valid
		mutation(&usage)
		if issue := validateBenchmarkUsage(usage); issue == "" {
			t.Fatalf("invalid usage accepted: %#v", usage)
		}
	}
	invalid := valid
	invalid.TotalTokens++
	if _, known, issue := estimateAPICostWithIssue("gpt-5.3-codex", invalid); known || !strings.Contains(issue, "invalid usage") {
		t.Fatalf("invalid usage received a cost: known=%v issue=%q", known, issue)
	}
}

func TestBenchmarkUsageRejectsUnknownTokenFields(t *testing.T) {
	var usage BenchmarkUsage
	input := `{
		"totalTokens": 130,
		"inputTokens": 100,
		"cachedInputTokens": 20,
		"cacheWriteInputTokens": 10,
		"outputTokens": 30,
		"reasoningOutputTokens": 15,
		"vectorTokens": 2,
		"audioTokens": 1
	}`
	if err := json.Unmarshal([]byte(input), &usage); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	want := "unknown usage fields: audioTokens, vectorTokens"
	if issue := validateBenchmarkUsage(usage); issue != want {
		t.Fatalf("usage issue = %q, want %q", issue, want)
	}
	if _, known, issue := estimateAPICostWithIssue("gpt-5.6-sol", usage); known || !strings.Contains(issue, want) {
		t.Fatalf("unknown token fields received a cost: known=%v issue=%q", known, issue)
	}

	state := newBenchmarkTelemetry()
	state.recordCumulative(usage)
	var result BenchmarkResult
	state.apply(&result)
	if result.UsageKnown || !strings.Contains(result.UsageIssue, want) {
		t.Fatalf("unknown token fields did not fail telemetry closed: %#v", result)
	}
}

func TestBenchmarkUsageAcceptsDocumentedTokenFields(t *testing.T) {
	var usage BenchmarkUsage
	input := `{"totalTokens":130,"inputTokens":100,"cachedInputTokens":20,"cacheWriteInputTokens":10,"outputTokens":30,"reasoningOutputTokens":15}`
	if err := json.Unmarshal([]byte(input), &usage); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if issue := validateBenchmarkUsage(usage); issue != "" {
		t.Fatalf("documented usage fields rejected: %s", issue)
	}
}

func TestExperimentalAPIUnsupported(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{
		{err: &benchmarkMethodError{code: -32602, message: "invalid params"}, want: true},
		{err: &benchmarkMethodError{code: -32000, message: "unknown experimentalRawEvents"}, want: true},
		{err: &benchmarkMethodError{code: -32000, message: "account unavailable"}, want: false},
		{err: errors.New("transport failed"), want: false},
	} {
		if got := experimentalAPIUnsupported(test.err); got != test.want {
			t.Errorf("experimentalAPIUnsupported(%v) = %v, want %v", test.err, got, test.want)
		}
	}
}

func TestRunBenchmarkSuiteDiscoversAndRunsEveryCombination(t *testing.T) {
	message := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	server, _ := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"data": []any{map[string]any{
				"model": "gpt-5.6-luna", "displayName": "GPT-5.6 Luna",
				"supportedReasoningEfforts": []any{
					map[string]string{"reasoningEffort": "low"},
					map[string]string{"reasoningEffort": "high"},
				},
			}},
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{"thread": map[string]string{"id": "thread-low"}})},
		benchmarkEnvelope{ID: rawJSON(3), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-low"}})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-low", "turnId": "turn-low", "item": map[string]string{"type": "agentMessage", "text": message},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-low", "turn": map[string]any{"id": "turn-low", "status": "completed"},
		})},
		benchmarkEnvelope{ID: rawJSON(4), Result: rawJSON(map[string]any{"thread": map[string]string{"id": "thread-high"}})},
		benchmarkEnvelope{ID: rawJSON(5), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-high"}})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-high", "turnId": "turn-high", "item": map[string]string{"type": "agentMessage", "text": message},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-high", "turn": map[string]any{"id": "turn-high", "status": "completed"},
		})},
	)
	original := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string) (*appServerSession, error) { return server, nil }
	t.Cleanup(func() { openBenchmarkAppServer = original })

	var events []BenchmarkEvent
	Client{Binary: "ignored"}.RunBenchmarkSuite(context.Background(), []BenchmarkTaskID{BenchmarkMergeRanges}, func(event BenchmarkEvent) {
		events = append(events, event)
	})
	var efforts []string
	for _, event := range events {
		if event.Result != nil {
			if !event.Result.Correct {
				t.Fatalf("suite result failed: %#v", event.Result)
			}
			efforts = append(efforts, event.Result.Effort)
		}
	}
	if len(efforts) != 2 || efforts[0] != "low" || efforts[1] != "high" {
		t.Fatalf("suite efforts = %v, want [low high]", efforts)
	}
	last := events[len(events)-1]
	if !last.Done || last.Err != nil || last.Total != 2 || last.Completed != 2 {
		t.Fatalf("final suite event = %#v", last)
	}
}

func TestRunBenchmarkSuiteMultipliesSelectedTasksByCombinations(t *testing.T) {
	mergeMessage := string(rawJSON(map[string]string{"code": correctStarlarkSubmission}))
	lruMessage := string(rawJSON(map[string]string{"code": correctLRUSubmission}))
	server, _ := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"data": []any{map[string]any{
				"model": "gpt-5.6-luna", "displayName": "GPT-5.6 Luna",
				"supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "low"}},
			}},
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{"thread": map[string]string{"id": "thread-merge"}})},
		benchmarkEnvelope{ID: rawJSON(3), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-merge"}})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-merge", "turnId": "turn-merge", "item": map[string]string{"type": "agentMessage", "text": mergeMessage},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-merge", "turn": map[string]any{"id": "turn-merge", "status": "completed"},
		})},
		benchmarkEnvelope{ID: rawJSON(4), Result: rawJSON(map[string]any{"thread": map[string]string{"id": "thread-lru"}})},
		benchmarkEnvelope{ID: rawJSON(5), Result: rawJSON(map[string]any{"turn": map[string]string{"id": "turn-lru"}})},
		benchmarkEnvelope{Method: "item/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-lru", "turnId": "turn-lru", "item": map[string]string{"type": "agentMessage", "text": lruMessage},
		})},
		benchmarkEnvelope{Method: "turn/completed", Params: rawJSON(map[string]any{
			"threadId": "thread-lru", "turn": map[string]any{"id": "turn-lru", "status": "completed"},
		})},
	)
	original := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string) (*appServerSession, error) { return server, nil }
	t.Cleanup(func() { openBenchmarkAppServer = original })

	var results []BenchmarkResult
	var final BenchmarkEvent
	Client{}.RunBenchmarkSuite(context.Background(), []BenchmarkTaskID{BenchmarkLRUCache, BenchmarkMergeRanges}, func(event BenchmarkEvent) {
		if event.Result != nil {
			results = append(results, *event.Result)
		}
		final = event
	})
	if len(results) != 2 || results[0].TaskID != BenchmarkMergeRanges || results[1].TaskID != BenchmarkLRUCache {
		t.Fatalf("task results = %#v", results)
	}
	if !results[0].Correct || !results[1].Correct || !final.Done || final.Total != 2 || final.Completed != 2 || final.Combinations != 1 {
		t.Fatalf("suite result = %#v, final = %#v", results, final)
	}
}

func TestRunBenchmarkSuiteReportsStartupFailure(t *testing.T) {
	original := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string) (*appServerSession, error) {
		return nil, errors.New("offline")
	}
	t.Cleanup(func() { openBenchmarkAppServer = original })
	var final BenchmarkEvent
	Client{}.RunBenchmarkSuite(context.Background(), []BenchmarkTaskID{BenchmarkMergeRanges}, func(event BenchmarkEvent) { final = event })
	if !final.Done || final.Err == nil || final.Err.Error() != "offline" {
		t.Fatalf("startup failure event = %#v", final)
	}
}

func TestBenchmarkCombinationCountUsesVisibleCatalogWithoutTurns(t *testing.T) {
	server, requests := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"data": []any{
				map[string]any{"model": "a", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "low"}, map[string]string{"reasoningEffort": "high"}}},
				map[string]any{"model": "b", "defaultReasoningEffort": "medium"},
			},
		})},
	)
	original := openBenchmarkAppServer
	openBenchmarkAppServer = func(context.Context, string) (*appServerSession, error) { return server, nil }
	t.Cleanup(func() { openBenchmarkAppServer = original })
	count, err := (Client{}).BenchmarkCombinationCount(context.Background())
	if err != nil || count != 3 {
		t.Fatalf("combination count = %d, %v; want 3", count, err)
	}
	if strings.Contains(requests.String(), `"method":"turn/start"`) {
		t.Fatalf("catalog planning unexpectedly started a model turn: %s", requests.String())
	}
}

func TestAppServerReadLoopAndClose(t *testing.T) {
	server, _ := newFakeBenchmarkServer()
	input := strings.NewReader("{\"method\":\"first\"}\n{\"method\":\"second\"}\n")
	go server.readLoop(json.NewDecoder(input))
	for _, want := range []string{"first", "second"} {
		envelope, err := server.nextEnvelope(context.Background())
		if err != nil || envelope.Method != want {
			t.Fatalf("decoded envelope = %#v, %v; want %s", envelope, err, want)
		}
	}
	if _, err := server.nextEnvelope(context.Background()); err == nil || !strings.Contains(err.Error(), "closed unexpectedly") {
		t.Fatalf("decoder terminal error = %v, want unexpected-close diagnostic", err)
	}
	server.close()
}

func TestLockedBufferAndFatalErrorClassification(t *testing.T) {
	buffer := &lockedBuffer{}
	if _, err := buffer.Write([]byte("diagnostic")); err != nil || buffer.String() != "diagnostic" {
		t.Fatalf("locked buffer = %q, %v", buffer.String(), err)
	}
	if err := fatalBenchmarkError(context.Background(), &benchmarkMethodError{method: "turn/start", code: -1, message: "rejected"}); err != nil {
		t.Fatalf("RPC method failure was classified fatal: %v", err)
	}
	transport := errors.New("transport")
	if err := fatalBenchmarkError(context.Background(), transport); !errors.Is(err, transport) {
		t.Fatalf("transport error = %v, want original", err)
	}
}

func TestModelsPaginatesAndOmitsHiddenRows(t *testing.T) {
	next := "page-2"
	server, _ := newFakeBenchmarkServer(
		benchmarkEnvelope{ID: rawJSON(1), Result: rawJSON(map[string]any{
			"data": []any{
				map[string]any{"model": "visible-a", "displayName": "A"},
				map[string]any{"model": "hidden", "displayName": "Hidden", "hidden": true},
			},
			"nextCursor": next,
		})},
		benchmarkEnvelope{ID: rawJSON(2), Result: rawJSON(map[string]any{
			"data":       []any{map[string]any{"model": "visible-b", "displayName": "B"}},
			"nextCursor": nil,
		})},
	)
	models, err := server.models(context.Background())
	if err != nil || len(models) != 2 || models[0].Model != "visible-a" || models[1].Model != "visible-b" {
		t.Fatalf("models = %#v, error = %v", models, err)
	}
}

func TestNextEnvelopeHonorsCancellation(t *testing.T) {
	server, _ := newFakeBenchmarkServer()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := server.nextEnvelope(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("nextEnvelope error = %v, want deadline exceeded", err)
	}
}

func TestLiveBenchmarkModelCatalog(t *testing.T) {
	if os.Getenv("CODEXOMETER_LIVE_TEST") != "1" {
		t.Skip("set CODEXOMETER_LIVE_TEST=1 to exercise the installed Codex app-server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server, err := startAppServer(ctx, "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()
	models, err := server.models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if combinations := benchmarkCombinations(models); len(combinations) == 0 {
		t.Fatalf("model catalog contained no benchmark combinations: %#v", models)
	} else {
		t.Logf("discovered %d visible model(s) and %d model/effort combination(s)", len(models), len(combinations))
		for _, model := range models {
			t.Logf("model %s efforts=%v", model.Model, model.SupportedReasoningEfforts)
		}
	}
}

func TestLiveBenchmarkThreadStart(t *testing.T) {
	if os.Getenv("CODEXOMETER_LIVE_TEST") != "1" {
		t.Skip("set CODEXOMETER_LIVE_TEST=1 to exercise the installed Codex app-server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server, err := startAppServer(ctx, "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()
	workspace := t.TempDir()
	result, err := server.call(ctx, "thread/start", map[string]any{
		"cwd": workspace, "approvalPolicy": "never", "sandbox": "read-only", "ephemeral": true,
	}, nil)
	if err != nil {
		t.Fatalf("start ephemeral read-only benchmark thread: %v", err)
	}
	if !strings.Contains(string(result), `"thread"`) {
		t.Fatalf("thread/start returned %s", result)
	}
}

func TestLiveBenchmarkSingleLunaTrial(t *testing.T) {
	if os.Getenv("CODEXOMETER_LIVE_BENCHMARK") != "1" {
		t.Skip("set CODEXOMETER_LIVE_BENCHMARK=1 to consume quota for one live Luna/low trial")
	}
	ctx, cancel := context.WithTimeout(context.Background(), benchmarkTurnTimeout)
	defer cancel()
	server, err := startAppServer(ctx, "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()
	models, err := server.models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var selected *benchmarkCombination
	for _, combination := range benchmarkCombinations(models) {
		if combination.model.Model == "gpt-5.6-luna" && combination.effort == "low" {
			candidate := combination
			selected = &candidate
			break
		}
	}
	if selected == nil {
		t.Skip("gpt-5.6-luna/low is not visible to this account")
	}
	result, fatalErr := server.runBenchmark(ctx, *selected, benchmarkTaskDefinitions[0])
	if fatalErr != nil {
		t.Fatalf("live trial transport failure: %v", fatalErr)
	}
	if !result.Correct {
		t.Fatalf("live trial failed: %s (usage=%+v)", result.Failure, result.Usage)
	}
	if result.Usage.TotalTokens <= 0 {
		t.Fatalf("live trial passed without token telemetry: %+v", result)
	}
	t.Logf("live Luna/low passed in %s using %d tokens", result.Duration.Round(time.Millisecond), result.Usage.TotalTokens)
}

func TestVerifyMergeRangesRejectsWrongUnsafeAndMalformedSubmissions(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{name: "wrong answer", code: "def merge_ranges(ranges):\n    return ranges\n", want: "returned"},
		{name: "missing function", code: "answer = 42\n", want: "does not define"},
		{name: "not callable", code: "merge_ranges = 42\n", want: "not callable"},
		{name: "bad type", code: "def merge_ranges(ranges):\n    return 42\n", want: "expected list"},
		{name: "mutates input", code: "def merge_ranges(ranges):\n    ranges.append([99, 100])\n    return ranges\n", want: "mutated"},
		{name: "load denied", code: "load(\"outside.star\", \"x\")\ndef merge_ranges(ranges):\n    return []\n", want: "load not implemented"},
		{name: "step limit", code: "def merge_ranges(ranges):\n    x = 0\n    for i in range(10000000):\n        x += i\n    return []\n", want: "too many steps"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifyMergeRanges(test.code)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("verify error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestBenchmarkCodeRequiresStrictStructuredOutput(t *testing.T) {
	code, err := benchmarkCode(`{"code":"def merge_ranges(ranges):\n    return []"}`)
	if err != nil || !strings.Contains(code, "merge_ranges") {
		t.Fatalf("valid code result = %q, %v", code, err)
	}
	for _, message := range []string{"", "```python\npass\n```", `{"code":""}`, `{"code":"pass","extra":true}`, `{"code":"pass"} {"code":"pass"}`} {
		if _, err := benchmarkCode(message); err == nil {
			t.Fatalf("benchmarkCode(%q) unexpectedly succeeded", message)
		}
	}
	oversized := `{"code":"` + strings.Repeat("x", benchmarkCodeLimit) + `"}`
	if _, err := benchmarkCode(oversized); err == nil {
		t.Fatal("oversized result unexpectedly succeeded")
	}
}

func TestBenchmarkCombinationsPreserveCatalogOrder(t *testing.T) {
	models := []benchmarkModel{
		{
			Model: "model-a", DisplayName: "Model A", DefaultReasoningEffort: "medium",
			SupportedReasoningEfforts: []benchmarkEffortOption{{ReasoningEffort: "low"}, {ReasoningEffort: "high"}},
		},
		{Model: "model-b", DefaultReasoningEffort: "medium"},
		{Model: ""},
	}
	got := benchmarkCombinations(models)
	if len(got) != 3 {
		t.Fatalf("got %d combinations, want 3", len(got))
	}
	if got[0].effort != "low" || got[1].effort != "high" || got[2].effort != "medium" {
		t.Fatalf("effort order = %q, %q, %q", got[0].effort, got[1].effort, got[2].effort)
	}
	if got[2].model.DisplayName != "model-b" {
		t.Fatalf("fallback display name = %q", got[2].model.DisplayName)
	}
}

func TestEstimateAPICostUsesCachedAndOutputRates(t *testing.T) {
	usage := BenchmarkUsage{
		InputTokens: 2_000, CachedInputTokens: 500, CacheWriteInputTokens: 100,
		OutputTokens: 300, ReasoningOutputTokens: 200, TotalTokens: 2_300,
	}
	cost, ok := estimateAPICost("gpt-5.6-sol-2026-08-01", usage)
	if !ok {
		t.Fatal("known model was not priced")
	}
	want := (1_400*5.00 + 500*0.50 + 100*6.25 + 300*30.00) / 1_000_000
	if cost != want {
		t.Fatalf("cost = %.8f, want %.8f", cost, want)
	}
	if _, ok := estimateAPICost("future-unpriced-model", usage); ok {
		t.Fatal("unknown model received a guessed price")
	}
	if _, ok := estimateAPICost("gpt-5.3-codex-spark", usage); ok {
		t.Fatal("unlisted Spark pricing was guessed from a different model")
	}
}

func TestEstimateAPICostUsesPublishedLongContextRates(t *testing.T) {
	usage := BenchmarkUsage{
		InputTokens: 300_000, CachedInputTokens: 100_000, CacheWriteInputTokens: 20_000,
		OutputTokens: 10_000, TotalTokens: 310_000,
	}
	cost, known, issue := EstimateStandardAPIEqCost("gpt-5.6-terra", usage)
	want := (180_000*4.00 + 100_000*0.40 + 20_000*5.00 + 10_000*18.00) / 1_000_000.0
	if !known || issue != "" || math.Abs(cost-want) > 1e-12 {
		t.Fatalf("long-context cost = %f, %v, %q; want %f", cost, known, issue, want)
	}
	olderUsage := usage
	olderUsage.CacheWriteInputTokens = 0
	for model, want := range map[string]float64{
		"gpt-5.4": (200_000*5.00 + 100_000*0.50 + 10_000*22.50) / 1_000_000.0,
		"gpt-5.5": (200_000*10.00 + 100_000*1.00 + 10_000*45.00) / 1_000_000.0,
	} {
		cost, known, issue := EstimateStandardAPIEqCost(model, olderUsage)
		if !known || issue != "" || math.Abs(cost-want) > 1e-12 {
			t.Errorf("%s long-context cost = %f, %v, %q; want %f", model, cost, known, issue, want)
		}
	}
}

func TestBenchmarkPricesLongContextThresholdPerRawResponse(t *testing.T) {
	response := BenchmarkUsage{InputTokens: 200_000, OutputTokens: 1_000, TotalTokens: 201_000}
	telemetry := newBenchmarkTelemetry()
	telemetry.recordRawResponse("response-1", &response)
	telemetry.recordRawResponse("response-2", &response)
	total := BenchmarkUsage{InputTokens: 400_000, OutputTokens: 2_000, TotalTokens: 402_000}
	telemetry.recordCumulative(total)
	result := BenchmarkResult{ActualModel: "gpt-5.6-terra"}
	applyBenchmarkMeasurements(&result, telemetry)
	want := 2 * (200_000*2.00 + 1_000*12.00) / 1_000_000.0
	if !result.CostKnown || math.Abs(result.CostUSD-want) > 1e-12 {
		t.Fatalf("per-response cost = %#v; want %f", result, want)
	}

	cumulativeOnly := newBenchmarkTelemetry()
	cumulativeOnly.recordCumulative(total)
	result = BenchmarkResult{ActualModel: "gpt-5.6-terra"}
	applyBenchmarkMeasurements(&result, cumulativeOnly)
	if result.CostKnown || !strings.Contains(result.CostIssue, "per-response") {
		t.Fatalf("ambiguous cumulative long-context cost = %#v", result)
	}
}

func TestEstimateAPICostRequiresPublishedCacheWriteRate(t *testing.T) {
	usage := BenchmarkUsage{InputTokens: 1_000, CacheWriteInputTokens: 100, OutputTokens: 50, TotalTokens: 1_050}
	if _, ok := estimateAPICost("gpt-5.5", usage); ok {
		t.Fatal("model with no published cache-write rate received a guessed price")
	}
	usage.CacheWriteInputTokens = 0
	cost, ok := estimateAPICost("gpt-5.5", usage)
	if !ok {
		t.Fatal("published GPT-5.5 rates were not used")
	}
	want := (1_000*5.00 + 50*30.00) / 1_000_000
	if cost != want {
		t.Fatalf("cost = %.8f, want %.8f", cost, want)
	}
}

func TestFormatIntervalsAndReferenceMerge(t *testing.T) {
	input := []interval{{5, 7}, {1, 3}, {2, 4}, {10, 10}, {8, 9}}
	got := mergeIntervals(input)
	if formatted := formatIntervals(got); formatted != "[[1,10]]" {
		t.Fatalf("merge result = %s", formatted)
	}
	if input[0] != (interval{5, 7}) {
		t.Fatal("reference merge mutated input")
	}
}

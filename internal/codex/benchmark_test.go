package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
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
	if len(tasks) != 4 || tasks[0].ID != BenchmarkMergeRanges || tasks[3].ID != BenchmarkShortestPath {
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
		envelopes:  make(chan benchmarkEnvelope, len(envelopes)+1),
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
	if result.Usage.TotalTokens != 1200 || !result.CostKnown || result.CostUSD <= 0 {
		t.Fatalf("usage/cost not captured: %#v", result)
	}
	if !strings.Contains(requests.String(), `"method":"thread/start"`) || !strings.Contains(requests.String(), `"effort":"high"`) {
		t.Fatalf("requests missing thread or effort: %s", requests.String())
	}
	if !strings.Contains(requests.String(), `"sandbox":"read-only"`) {
		t.Fatalf("thread request did not use the app-server sandbox spelling: %s", requests.String())
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
		OutputTokens: 300, ReasoningOutputTokens: 200,
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

func TestEstimateAPICostRequiresPublishedCacheWriteRate(t *testing.T) {
	usage := BenchmarkUsage{InputTokens: 1_000, CacheWriteInputTokens: 100, OutputTokens: 50}
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

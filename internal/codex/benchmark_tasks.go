package codex

import (
	"errors"
	"fmt"
	"strconv"

	"go.starlark.net/starlark"
)

// BenchmarkTaskID is the stable identifier for a deterministic benchmark.
type BenchmarkTaskID string

const (
	BenchmarkMergeRanges         BenchmarkTaskID = "merge-ranges"
	BenchmarkLRUCache            BenchmarkTaskID = "lru-cache"
	BenchmarkExpressionParser    BenchmarkTaskID = "expression-parser"
	BenchmarkShortestPath        BenchmarkTaskID = "shortest-path"
	BenchmarkDependencyScheduler BenchmarkTaskID = "dependency-scheduler"
	BenchmarkVersionResolver     BenchmarkTaskID = "version-resolver"
	BenchmarkEventProcessor      BenchmarkTaskID = "event-processor"
)

// BenchmarkTask is the public description used by the terminal selector.
type BenchmarkTask struct {
	ID   BenchmarkTaskID
	Name string
}

type benchmarkDefinition struct {
	task     BenchmarkTask
	function string
	prompt   string
	verify   func(string) error
}

var benchmarkTaskDefinitions = []benchmarkDefinition{
	{
		task:     BenchmarkTask{ID: BenchmarkMergeRanges, Name: "MERGE RANGES"},
		function: "merge_ranges",
		prompt: `Write Starlark code defining this function:

    merge_ranges(ranges)

The input is a list of inclusive integer ranges, each represented by a two-item list [start, end]. Every input range is valid (start <= end). Return a new list containing the same integer union in canonical form: sorted by start, with overlapping and adjacent ranges merged. Do not mutate the input. The function must work for empty input, duplicates, nested ranges, negative values, and arbitrary ordering. Do not use load().`,
		verify: verifyMergeRanges,
	},
	{
		task:     BenchmarkTask{ID: BenchmarkLRUCache, Name: "LRU CACHE"},
		function: "lru_cache",
		prompt: `Write Starlark code defining this function:

    lru_cache(capacity, operations)

Simulate a least-recently-used cache. Each operation is ["put", key, value] or ["get", key], where keys and values are integers. A put inserts or updates a key and makes it most recently used. A get returns its value and makes it most recently used; a missing get returns -1. Evict the least recently used key when capacity is exceeded. Capacity may be zero. Return [get_results, final_entries], where final_entries is a list of [key, value] ordered most-recently-used first. Do not mutate the input and do not use load().`,
		verify: verifyLRUCache,
	},
	{
		task:     BenchmarkTask{ID: BenchmarkExpressionParser, Name: "EXPRESSION"},
		function: "evaluate_expression",
		prompt: `Write Starlark code defining this function:

    evaluate_expression(tokens)

The input is a valid token list containing non-negative base-10 integer literals, "+", "-", "*", "(" and ")". Evaluate it using normal arithmetic precedence: multiplication before addition and subtraction, parentheses first, and left associativity. Return the integer result. Do not mutate the input, do not call eval, and do not use load().`,
		verify: verifyExpressionParser,
	},
	{
		task:     BenchmarkTask{ID: BenchmarkShortestPath, Name: "SHORTEST PATH"},
		function: "shortest_path",
		prompt: `Write Starlark code defining this function:

    shortest_path(grid, start, end)

The grid is a non-empty rectangular list of lists containing 0 for open cells and 1 for blocked cells. Start and end are open [row, column] coordinates. Moving one cell up, down, left, or right costs one. Return the minimum move count from start to end, or -1 if no route exists. Do not mutate any input and do not use load().`,
		verify: verifyShortestPath,
	},
	{
		task:     BenchmarkTask{ID: BenchmarkDependencyScheduler, Name: "DEPENDENCY SCHEDULER"},
		function: "minimum_schedule_time",
		prompt: `Write Starlark code defining this function:

    minimum_schedule_time(worker_count, jobs)

There are at most seven jobs and one to three workers. Each job is [duration, dependencies], where duration is from 1 through 3 and dependencies is a list of valid zero-based job indexes. The dependency graph is acyclic. A job may start only after all its dependencies finish. Jobs are non-preemptive, each worker can run at most one job at a time, and different workers are identical. Return the minimum possible integer completion time for all jobs. Return 0 for an empty job list. Do not mutate the input and do not use load().`,
		verify: verifyDependencyScheduler,
	},
	{
		task:     BenchmarkTask{ID: BenchmarkVersionResolver, Name: "VERSION RESOLVER"},
		function: "resolve_versions",
		prompt: `Write Starlark code defining this function:

    resolve_versions(catalog)

The catalog contains one to five packages, each with two or three version records. Catalog entry i is the version list for package i. Each version is [number, requirements, conflicts], with a unique positive number within that package. A requirement [package, minimum, maximum] names a valid package and is inclusive. A conflict [package, version] forbids that exact pairing. Select exactly one listed version for every package so that every selected version's requirements and conflicts are satisfied. Return the selected version numbers in package order. If several solutions exist, return the lexicographically greatest version list; return [] when none exists. Version entries may be unsorted. Do not mutate the input and do not use load().`,
		verify: verifyVersionResolver,
	},
	{
		task:     BenchmarkTask{ID: BenchmarkEventProcessor, Name: "EVENT PROCESSOR"},
		function: "process_ledger",
		prompt: `Write Starlark code defining this function:

    process_ledger(accounts, events)

Accounts are [account_id, opening_balance], with unique integer IDs and non-negative balances. Events are [sequence, event_id, kind, arg1, arg2, amount], arrive in arbitrary order, and must be processed by ascending unique sequence. Kinds are "transfer", "freeze", "unfreeze", and "reverse". All account references are valid, transfer amounts are positive, and a transfer's accounts differ. Process the first event with each event_id normally; a later duplicate has status "duplicate" and changes nothing. Every first occurrence claims its ID even when it fails.

A transfer moves amount from account arg1 to account arg2. It has status "frozen" if either account is frozen, then "insufficient" if arg1 lacks funds, otherwise "applied". Freeze and unfreeze use arg1; changing the state returns "applied", while requesting the existing state returns "noop". A reverse uses arg1 as the target event_id. It is "invalid" unless that target was a successful, not-yet-reversed transfer; otherwise it checks "frozen" for either original account, then "insufficient" if the original recipient can no longer return the amount, and otherwise reverses the transfer, marks it reversed, and returns "applied".

Return [balances, frozen_accounts, audit]. Balances are [account_id, balance] sorted by account_id; frozen_accounts is a sorted list of IDs; audit contains [sequence, event_id, status] in processing order. Do not mutate the input and do not use load().`,
		verify: verifyEventProcessor,
	},
}

// BenchmarkTasks returns the complete task catalog in display order.
func BenchmarkTasks() []BenchmarkTask {
	tasks := make([]BenchmarkTask, 0, len(benchmarkTaskDefinitions))
	for _, definition := range benchmarkTaskDefinitions {
		tasks = append(tasks, definition.task)
	}
	return tasks
}

func resolveBenchmarkTasks(ids []BenchmarkTaskID) ([]benchmarkDefinition, error) {
	if len(ids) == 0 {
		return nil, errors.New("select at least one benchmark task")
	}
	requested := make(map[BenchmarkTaskID]bool, len(ids))
	for _, id := range ids {
		requested[id] = true
	}
	definitions := make([]benchmarkDefinition, 0, len(requested))
	for _, definition := range benchmarkTaskDefinitions {
		if requested[definition.task.ID] {
			definitions = append(definitions, definition)
			delete(requested, definition.task.ID)
		}
	}
	if len(requested) > 0 {
		for id := range requested {
			return nil, fmt.Errorf("unknown benchmark task %q", id)
		}
	}
	return definitions, nil
}

func benchmarkPrompt(definition benchmarkDefinition) string {
	return definition.prompt + `

Starlark language contract for this task:
- Starlark is Python-like but deliberately smaller.
- You may use top-level def functions, if/elif/else, for loops, break/continue, range, len, int, sorted, lists, dictionaries, indexing, slicing, append, pop, dictionary get/keys, and membership tests.
- Integer arithmetic includes +, -, *, //, and %; booleans and None are available.
- while loops, recursive calls, eval, imports, and load are unavailable.
- Keep all work deterministic and return only the specified value from the named function.

Return a JSON object containing one field named "code" whose value is the complete Starlark source. Do not include Markdown fences or commentary.`
}

func loadBenchmarkFunction(code, functionName string) (*starlark.Thread, starlark.Callable, error) {
	thread := &starlark.Thread{Name: "codexometer-verifier"}
	thread.SetMaxExecutionSteps(benchmarkStepLimit)
	globals, err := starlark.ExecFile(thread, "submission.star", code, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("code does not load: %w", err)
	}
	value, ok := globals[functionName]
	if !ok {
		return nil, nil, fmt.Errorf("code does not define %s", functionName)
	}
	function, ok := value.(starlark.Callable)
	if !ok {
		return nil, nil, fmt.Errorf("%s is not callable", functionName)
	}
	return thread, function, nil
}

func renewBenchmarkStepBudget(thread *starlark.Thread) {
	thread.SetMaxExecutionSteps(thread.ExecutionSteps() + benchmarkStepLimit)
}

func renewHardBenchmarkStepBudget(thread *starlark.Thread) {
	thread.SetMaxExecutionSteps(thread.ExecutionSteps() + benchmarkHardStepLimit)
}

type lruOperation struct {
	kind  string
	key   int64
	value int64
}

type lruCase struct {
	capacity int
	items    []lruOperation
}

type lruEntry struct {
	key   int64
	value int64
}

func verifyLRUCache(code string) error {
	thread, function, err := loadBenchmarkFunction(code, "lru_cache")
	if err != nil {
		return err
	}
	for index, test := range lruTestCases() {
		operations := lruOperationsToStarlark(test.items)
		before := operations.String()
		renewBenchmarkStepBudget(thread)
		value, err := starlark.Call(thread, function, starlark.Tuple{starlark.MakeInt(test.capacity), operations}, nil)
		if err != nil {
			return fmt.Errorf("case %d raised an error: %w", index+1, err)
		}
		if operations.String() != before {
			return fmt.Errorf("case %d mutated its input", index+1)
		}
		gets, entries, err := lruResultFromStarlark(value)
		if err != nil {
			return fmt.Errorf("case %d returned an invalid value: %w", index+1, err)
		}
		wantGets, wantEntries := simulateLRU(test.capacity, test.items)
		if !equalInt64s(gets, wantGets) || entries != formatLRUEntries(wantEntries) {
			return fmt.Errorf("case %d returned gets=%v entries=%s, want gets=%v entries=%s", index+1, gets, entries, wantGets, formatLRUEntries(wantEntries))
		}
	}
	return nil
}

func lruTestCases() []lruCase {
	cases := []lruCase{
		{capacity: 0, items: []lruOperation{{kind: "put", key: 1, value: 10}, {kind: "get", key: 1}}},
		{capacity: 1, items: []lruOperation{{kind: "get", key: 4}}},
		{capacity: 1, items: []lruOperation{{kind: "put", key: 1, value: 10}, {kind: "put", key: 2, value: 20}, {kind: "get", key: 1}, {kind: "get", key: 2}}},
		{capacity: 2, items: []lruOperation{{kind: "put", key: 1, value: 10}, {kind: "put", key: 2, value: 20}, {kind: "get", key: 1}, {kind: "put", key: 3, value: 30}, {kind: "get", key: 2}}},
		{capacity: 2, items: []lruOperation{{kind: "put", key: -1, value: -10}, {kind: "put", key: -1, value: 99}, {kind: "get", key: -1}}},
	}
	seed := uint64(0x1A2B3C4D)
	for n := 0; n < 40; n++ {
		seed = nextBenchmarkSeed(seed)
		capacity := int(seed % 6)
		seed = nextBenchmarkSeed(seed)
		count := int(seed%24) + 6
		operations := make([]lruOperation, 0, count)
		for i := 0; i < count; i++ {
			seed = nextBenchmarkSeed(seed)
			key := int64(seed%9) - 4
			if seed%3 == 0 {
				operations = append(operations, lruOperation{kind: "get", key: key})
				continue
			}
			seed = nextBenchmarkSeed(seed)
			operations = append(operations, lruOperation{kind: "put", key: key, value: int64(seed%201) - 100})
		}
		cases = append(cases, lruCase{capacity: capacity, items: operations})
	}
	return cases
}

func lruOperationsToStarlark(operations []lruOperation) *starlark.List {
	values := make([]starlark.Value, 0, len(operations))
	for _, operation := range operations {
		parts := []starlark.Value{starlark.String(operation.kind), starlark.MakeInt64(operation.key)}
		if operation.kind == "put" {
			parts = append(parts, starlark.MakeInt64(operation.value))
		}
		values = append(values, starlark.NewList(parts))
	}
	return starlark.NewList(values)
}

func lruResultFromStarlark(value starlark.Value) ([]int64, string, error) {
	outer, ok := value.(*starlark.List)
	if !ok || outer.Len() != 2 {
		return nil, "", errors.New("expected [get_results, final_entries]")
	}
	gets, err := intListFromStarlark(outer.Index(0), 256)
	if err != nil {
		return nil, "", fmt.Errorf("get results: %w", err)
	}
	entries, err := intPairsFromStarlark(outer.Index(1), 256)
	if err != nil {
		return nil, "", fmt.Errorf("final entries: %w", err)
	}
	return gets, formatIntervals(entries), nil
}

func simulateLRU(capacity int, operations []lruOperation) ([]int64, []lruEntry) {
	var gets []int64
	var entries []lruEntry
	for _, operation := range operations {
		found := -1
		for index, entry := range entries {
			if entry.key == operation.key {
				found = index
				break
			}
		}
		if operation.kind == "get" {
			if found < 0 {
				gets = append(gets, -1)
				continue
			}
			entry := entries[found]
			gets = append(gets, entry.value)
			entries = append([]lruEntry{entry}, append(entries[:found], entries[found+1:]...)...)
			continue
		}
		entry := lruEntry{key: operation.key, value: operation.value}
		if found >= 0 {
			entries = append(entries[:found], entries[found+1:]...)
		}
		entries = append([]lruEntry{entry}, entries...)
		if len(entries) > capacity {
			entries = entries[:capacity]
		}
	}
	return gets, entries
}

func formatLRUEntries(entries []lruEntry) string {
	items := make([]interval, 0, len(entries))
	for _, entry := range entries {
		items = append(items, interval{start: entry.key, end: entry.value})
	}
	return formatIntervals(items)
}

type expressionCase struct {
	tokens []string
	want   int64
}

func verifyExpressionParser(code string) error {
	thread, function, err := loadBenchmarkFunction(code, "evaluate_expression")
	if err != nil {
		return err
	}
	for index, test := range expressionTestCases() {
		values := make([]starlark.Value, 0, len(test.tokens))
		for _, token := range test.tokens {
			values = append(values, starlark.String(token))
		}
		argument := starlark.NewList(values)
		before := argument.String()
		renewBenchmarkStepBudget(thread)
		value, err := starlark.Call(thread, function, starlark.Tuple{argument}, nil)
		if err != nil {
			return fmt.Errorf("case %d raised an error: %w", index+1, err)
		}
		if argument.String() != before {
			return fmt.Errorf("case %d mutated its input", index+1)
		}
		got, ok := starlarkInt64(value)
		if !ok {
			return fmt.Errorf("case %d returned a non-64-bit integer", index+1)
		}
		if got != test.want {
			return fmt.Errorf("case %d returned %d, want %d", index+1, got, test.want)
		}
	}
	return nil
}

func expressionTestCases() []expressionCase {
	raw := [][]string{
		{"0"}, {"42"}, {"2", "+", "3", "*", "4"}, {"(", "2", "+", "3", ")", "*", "4"},
		{"9", "-", "3", "-", "2"}, {"1", "-", "8", "*", "2"}, {"(", "7", ")"},
		{"2", "*", "(", "3", "+", "4", "*", "5", ")", "-", "6"},
	}
	cases := make([]expressionCase, 0, len(raw)+40)
	for _, tokens := range raw {
		want, _ := evaluateTokens(tokens)
		cases = append(cases, expressionCase{tokens: tokens, want: want})
	}
	seed := uint64(0xE7E55)
	for n := 0; n < 40; n++ {
		tokens := generateExpressionTokens(&seed, 0)
		want, err := evaluateTokens(tokens)
		if err != nil {
			panic(err)
		}
		cases = append(cases, expressionCase{tokens: tokens, want: want})
	}
	return cases
}

func generateExpressionTokens(seed *uint64, depth int) []string {
	parts := generateTermTokens(seed, depth)
	*seed = nextBenchmarkSeed(*seed)
	count := int(*seed % 2)
	for i := 0; i < count; i++ {
		*seed = nextBenchmarkSeed(*seed)
		op := "+"
		if *seed%2 == 0 {
			op = "-"
		}
		parts = append(parts, op)
		parts = append(parts, generateTermTokens(seed, depth)...)
	}
	return parts
}

func generateTermTokens(seed *uint64, depth int) []string {
	parts := generateFactorTokens(seed, depth)
	*seed = nextBenchmarkSeed(*seed)
	count := int(*seed % 2)
	for i := 0; i < count; i++ {
		parts = append(parts, "*")
		parts = append(parts, generateFactorTokens(seed, depth)...)
	}
	return parts
}

func generateFactorTokens(seed *uint64, depth int) []string {
	*seed = nextBenchmarkSeed(*seed)
	if depth < 2 && *seed%4 == 0 {
		return append(append([]string{"("}, generateExpressionTokens(seed, depth+1)...), ")")
	}
	return []string{strconv.FormatUint(*seed%10, 10)}
}

type tokenParser struct {
	tokens []string
	index  int
}

func evaluateTokens(tokens []string) (int64, error) {
	parser := tokenParser{tokens: tokens}
	value, err := parser.expression()
	if err != nil {
		return 0, err
	}
	if parser.index != len(tokens) {
		return 0, errors.New("trailing expression tokens")
	}
	return value, nil
}

func (p *tokenParser) expression() (int64, error) {
	value, err := p.term()
	for err == nil && p.index < len(p.tokens) && (p.tokens[p.index] == "+" || p.tokens[p.index] == "-") {
		op := p.tokens[p.index]
		p.index++
		var right int64
		right, err = p.term()
		if op == "+" {
			value += right
		} else {
			value -= right
		}
	}
	return value, err
}

func (p *tokenParser) term() (int64, error) {
	value, err := p.factor()
	for err == nil && p.index < len(p.tokens) && p.tokens[p.index] == "*" {
		p.index++
		var right int64
		right, err = p.factor()
		value *= right
	}
	return value, err
}

func (p *tokenParser) factor() (int64, error) {
	if p.index >= len(p.tokens) {
		return 0, errors.New("missing expression factor")
	}
	token := p.tokens[p.index]
	p.index++
	if token == "(" {
		value, err := p.expression()
		if err != nil || p.index >= len(p.tokens) || p.tokens[p.index] != ")" {
			return 0, errors.New("unclosed expression group")
		}
		p.index++
		return value, nil
	}
	return strconv.ParseInt(token, 10, 64)
}

type pathCase struct {
	grid       [][]int64
	start, end [2]int64
}

func verifyShortestPath(code string) error {
	thread, function, err := loadBenchmarkFunction(code, "shortest_path")
	if err != nil {
		return err
	}
	for index, test := range pathTestCases() {
		grid := intGridToStarlark(test.grid)
		start := coordinateToStarlark(test.start)
		end := coordinateToStarlark(test.end)
		before := grid.String() + start.String() + end.String()
		renewBenchmarkStepBudget(thread)
		value, err := starlark.Call(thread, function, starlark.Tuple{grid, start, end}, nil)
		if err != nil {
			return fmt.Errorf("case %d raised an error: %w", index+1, err)
		}
		if grid.String()+start.String()+end.String() != before {
			return fmt.Errorf("case %d mutated its input", index+1)
		}
		got, ok := starlarkInt64(value)
		if !ok {
			return fmt.Errorf("case %d returned a non-64-bit integer", index+1)
		}
		want := shortestPath(test)
		if got != want {
			return fmt.Errorf("case %d returned %d, want %d", index+1, got, want)
		}
	}
	return nil
}

func pathTestCases() []pathCase {
	cases := []pathCase{
		{grid: [][]int64{{0}}, start: [2]int64{0, 0}, end: [2]int64{0, 0}},
		{grid: [][]int64{{0, 0, 0}}, start: [2]int64{0, 0}, end: [2]int64{0, 2}},
		{grid: [][]int64{{0, 1}, {1, 0}}, start: [2]int64{0, 0}, end: [2]int64{1, 1}},
		{grid: [][]int64{{0, 0, 1}, {1, 0, 1}, {1, 0, 0}}, start: [2]int64{0, 0}, end: [2]int64{2, 2}},
		{grid: [][]int64{{0}, {0}, {0}, {0}}, start: [2]int64{3, 0}, end: [2]int64{0, 0}},
	}
	seed := uint64(0x5A07E57)
	for n := 0; n < 40; n++ {
		seed = nextBenchmarkSeed(seed)
		rows := int(seed%7) + 2
		seed = nextBenchmarkSeed(seed)
		columns := int(seed%7) + 2
		grid := make([][]int64, rows)
		for row := range grid {
			grid[row] = make([]int64, columns)
			for column := range grid[row] {
				seed = nextBenchmarkSeed(seed)
				if seed%4 == 0 {
					grid[row][column] = 1
				}
			}
		}
		seed = nextBenchmarkSeed(seed)
		start := [2]int64{int64(seed % uint64(rows)), int64((seed / 7) % uint64(columns))}
		seed = nextBenchmarkSeed(seed)
		end := [2]int64{int64(seed % uint64(rows)), int64((seed / 7) % uint64(columns))}
		grid[start[0]][start[1]], grid[end[0]][end[1]] = 0, 0
		cases = append(cases, pathCase{grid: grid, start: start, end: end})
	}
	return cases
}

func intGridToStarlark(grid [][]int64) *starlark.List {
	rows := make([]starlark.Value, 0, len(grid))
	for _, row := range grid {
		values := make([]starlark.Value, 0, len(row))
		for _, cell := range row {
			values = append(values, starlark.MakeInt64(cell))
		}
		rows = append(rows, starlark.NewList(values))
	}
	return starlark.NewList(rows)
}

func coordinateToStarlark(coordinate [2]int64) *starlark.List {
	return starlark.NewList([]starlark.Value{starlark.MakeInt64(coordinate[0]), starlark.MakeInt64(coordinate[1])})
}

func shortestPath(test pathCase) int64 {
	rows, columns := len(test.grid), len(test.grid[0])
	distance := make([][]int64, rows)
	for row := range distance {
		distance[row] = make([]int64, columns)
		for column := range distance[row] {
			distance[row][column] = -1
		}
	}
	queue := [][2]int64{test.start}
	distance[test.start[0]][test.start[1]] = 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == test.end {
			return distance[current[0]][current[1]]
		}
		for _, delta := range [][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			next := [2]int64{current[0] + delta[0], current[1] + delta[1]}
			if next[0] < 0 || next[0] >= int64(rows) || next[1] < 0 || next[1] >= int64(columns) ||
				test.grid[next[0]][next[1]] != 0 || distance[next[0]][next[1]] >= 0 {
				continue
			}
			distance[next[0]][next[1]] = distance[current[0]][current[1]] + 1
			queue = append(queue, next)
		}
	}
	return -1
}

func intListFromStarlark(value starlark.Value, limit int) ([]int64, error) {
	list, ok := value.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("expected list, got %s", value.Type())
	}
	if list.Len() > limit {
		return nil, errors.New("result contains too many items")
	}
	output := make([]int64, 0, list.Len())
	for index := 0; index < list.Len(); index++ {
		item, ok := starlarkInt64(list.Index(index))
		if !ok {
			return nil, errors.New("result item is not a 64-bit integer")
		}
		output = append(output, item)
	}
	return output, nil
}

func intPairsFromStarlark(value starlark.Value, limit int) ([]interval, error) {
	list, ok := value.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("expected list, got %s", value.Type())
	}
	if list.Len() > limit {
		return nil, errors.New("result contains too many pairs")
	}
	output := make([]interval, 0, list.Len())
	for index := 0; index < list.Len(); index++ {
		pair, ok := list.Index(index).(*starlark.List)
		if !ok || pair.Len() != 2 {
			return nil, errors.New("every entry must be a two-item list")
		}
		first, firstOK := starlarkInt64(pair.Index(0))
		second, secondOK := starlarkInt64(pair.Index(1))
		if !firstOK || !secondOK {
			return nil, errors.New("entry values must be 64-bit integers")
		}
		output = append(output, interval{start: first, end: second})
	}
	return output, nil
}

func equalInt64s(left, right []int64) bool {
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

func nextBenchmarkSeed(seed uint64) uint64 {
	return seed*6364136223846793005 + 1442695040888963407
}

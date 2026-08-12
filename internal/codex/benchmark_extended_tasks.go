package codex

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.starlark.net/starlark"
)

type schedulerJob struct {
	duration     int
	dependencies []int
}

type schedulerCase struct {
	workers int
	jobs    []schedulerJob
}

func verifyDependencyScheduler(code string) error {
	thread, function, err := loadBenchmarkFunction(code, "minimum_schedule_time")
	if err != nil {
		return err
	}
	for index, test := range schedulerTestCases() {
		jobs := schedulerJobsToStarlark(test.jobs)
		before := jobs.String()
		renewHardBenchmarkStepBudget(thread)
		value, err := starlark.Call(thread, function, starlark.Tuple{starlark.MakeInt(test.workers), jobs}, nil)
		if err != nil {
			return fmt.Errorf("case %d raised an error: %w", index+1, err)
		}
		if jobs.String() != before {
			return fmt.Errorf("case %d mutated its input", index+1)
		}
		got, ok := starlarkInt64(value)
		if !ok {
			return fmt.Errorf("case %d returned a non-64-bit integer", index+1)
		}
		want := minimumScheduleTime(test)
		if got != int64(want) {
			return fmt.Errorf("case %d returned %d, want %d", index+1, got, want)
		}
	}
	return nil
}

func schedulerTestCases() []schedulerCase {
	cases := []schedulerCase{
		{workers: 2},
		{workers: 2, jobs: []schedulerJob{{duration: 3}}},
		{workers: 2, jobs: []schedulerJob{{duration: 2}, {duration: 3}, {duration: 1, dependencies: []int{0, 1}}}},
		{workers: 1, jobs: []schedulerJob{{duration: 2}, {duration: 1}, {duration: 3, dependencies: []int{0}}, {duration: 2, dependencies: []int{1}}}},
		{workers: 2, jobs: []schedulerJob{{duration: 3}, {duration: 2}, {duration: 2}, {duration: 1, dependencies: []int{0}}, {duration: 3, dependencies: []int{1, 2}}}},
		{workers: 3, jobs: []schedulerJob{{duration: 2}, {duration: 2}, {duration: 2}, {duration: 2, dependencies: []int{0, 1}}, {duration: 1, dependencies: []int{1, 2}}, {duration: 2, dependencies: []int{3, 4}}}},
	}
	seed := uint64(0x5C4ED01E)
	for caseIndex := 0; caseIndex < 8; caseIndex++ {
		seed = nextBenchmarkSeed(seed)
		count := int(seed%4) + 3
		seed = nextBenchmarkSeed(seed)
		workers := int(seed%3) + 1
		jobs := make([]schedulerJob, count)
		for job := range jobs {
			seed = nextBenchmarkSeed(seed)
			jobs[job].duration = int(seed%3) + 1
			for dependency := 0; dependency < job; dependency++ {
				seed = nextBenchmarkSeed(seed)
				if seed%5 == 0 {
					jobs[job].dependencies = append(jobs[job].dependencies, dependency)
				}
			}
		}
		cases = append(cases, schedulerCase{workers: workers, jobs: jobs})
	}
	return cases
}

func schedulerJobsToStarlark(jobs []schedulerJob) *starlark.List {
	values := make([]starlark.Value, 0, len(jobs))
	for _, job := range jobs {
		dependencies := make([]starlark.Value, 0, len(job.dependencies))
		for _, dependency := range job.dependencies {
			dependencies = append(dependencies, starlark.MakeInt(dependency))
		}
		values = append(values, starlark.NewList([]starlark.Value{
			starlark.MakeInt(job.duration), starlark.NewList(dependencies),
		}))
	}
	return starlark.NewList(values)
}

func minimumScheduleTime(test schedulerCase) int {
	if len(test.jobs) == 0 {
		return 0
	}
	maximumTime := 0
	for _, job := range test.jobs {
		maximumTime += job.duration
	}
	frontier := map[int]struct{}{0: {}}
	base := 5
	for elapsed := 0; elapsed < maximumTime; elapsed++ {
		next := make(map[int]struct{})
		for encoded := range frontier {
			status := decodeSchedulerState(encoded, len(test.jobs), base)
			running := 0
			available := make([]bool, len(test.jobs))
			for job := range test.jobs {
				if status[job] > 1 {
					running++
					continue
				}
				if status[job] != 0 {
					continue
				}
				available[job] = true
				for _, dependency := range test.jobs[job].dependencies {
					if status[dependency] != 1 {
						available[job] = false
						break
					}
				}
			}
			free := test.workers - running
			for selection := 0; selection < 1<<len(test.jobs); selection++ {
				selected := 0
				valid := true
				for job := range test.jobs {
					if selection&(1<<job) == 0 {
						continue
					}
					selected++
					if selected > free || !available[job] {
						valid = false
						break
					}
				}
				if !valid {
					continue
				}
				advanced := append([]int(nil), status...)
				for job := range advanced {
					if selection&(1<<job) != 0 {
						advanced[job] = test.jobs[job].duration + 1
					}
					if advanced[job] == 2 {
						advanced[job] = 1
					} else if advanced[job] > 2 {
						advanced[job]--
					}
				}
				if schedulerComplete(advanced) {
					return elapsed + 1
				}
				next[encodeSchedulerState(advanced, base)] = struct{}{}
			}
		}
		frontier = next
	}
	return maximumTime
}

func decodeSchedulerState(encoded, count, base int) []int {
	status := make([]int, count)
	for index := range status {
		status[index] = encoded % base
		encoded /= base
	}
	return status
}

func encodeSchedulerState(status []int, base int) int {
	encoded, multiplier := 0, 1
	for _, value := range status {
		encoded += value * multiplier
		multiplier *= base
	}
	return encoded
}

func schedulerComplete(status []int) bool {
	for _, value := range status {
		if value != 1 {
			return false
		}
	}
	return true
}

type versionRequirement struct {
	packageIndex int
	minimum      int64
	maximum      int64
}

type versionConflict struct {
	packageIndex int
	version      int64
}

type versionOption struct {
	number       int64
	requirements []versionRequirement
	conflicts    []versionConflict
}

type versionCase struct {
	catalog [][]versionOption
}

func verifyVersionResolver(code string) error {
	thread, function, err := loadBenchmarkFunction(code, "resolve_versions")
	if err != nil {
		return err
	}
	for index, test := range versionTestCases() {
		catalog := versionCatalogToStarlark(test.catalog)
		before := catalog.String()
		renewHardBenchmarkStepBudget(thread)
		value, err := starlark.Call(thread, function, starlark.Tuple{catalog}, nil)
		if err != nil {
			return fmt.Errorf("case %d raised an error: %w", index+1, err)
		}
		if catalog.String() != before {
			return fmt.Errorf("case %d mutated its input", index+1)
		}
		got, err := intListFromStarlark(value, 32)
		if err != nil {
			return fmt.Errorf("case %d returned an invalid value: %w", index+1, err)
		}
		want := resolveVersionCatalog(test.catalog)
		if !equalInt64s(got, want) {
			return fmt.Errorf("case %d returned %v, want %v", index+1, got, want)
		}
	}
	return nil
}

func versionTestCases() []versionCase {
	cases := []versionCase{
		{catalog: [][]versionOption{{{number: 1}, {number: 3}, {number: 2}}}},
		{catalog: [][]versionOption{
			{{number: 1}, {number: 2, requirements: []versionRequirement{{packageIndex: 1, minimum: 2, maximum: 3}}}},
			{{number: 1}, {number: 2}},
		}},
		{catalog: [][]versionOption{
			{{number: 2, conflicts: []versionConflict{{packageIndex: 1, version: 2}}}, {number: 1}},
			{{number: 2}, {number: 1}},
		}},
		{catalog: [][]versionOption{
			{{number: 1, requirements: []versionRequirement{{packageIndex: 1, minimum: 3, maximum: 4}}}},
			{{number: 1}, {number: 2}},
		}},
		{catalog: [][]versionOption{
			{{number: 4, requirements: []versionRequirement{{packageIndex: 1, minimum: 2, maximum: 2}}}, {number: 3}},
			{{number: 3, conflicts: []versionConflict{{packageIndex: 2, version: 2}}}, {number: 2}},
			{{number: 2}, {number: 1}},
		}},
	}
	seed := uint64(0x0E25017E)
	for caseIndex := 0; caseIndex < 12; caseIndex++ {
		seed = nextBenchmarkSeed(seed)
		packageCount := int(seed%4) + 2
		catalog := make([][]versionOption, packageCount)
		for packageIndex := range catalog {
			seed = nextBenchmarkSeed(seed)
			versionCount := int(seed%2) + 2
			for versionIndex := 0; versionIndex < versionCount; versionIndex++ {
				number := int64(versionCount - versionIndex)
				option := versionOption{number: number}
				seed = nextBenchmarkSeed(seed)
				if packageCount > 1 && seed%3 == 0 {
					target := (packageIndex + int(seed%uint64(packageCount-1)) + 1) % packageCount
					seed = nextBenchmarkSeed(seed)
					minimum := int64(seed%2) + 1
					option.requirements = append(option.requirements, versionRequirement{
						packageIndex: target, minimum: minimum, maximum: minimum + int64(seed%2),
					})
				}
				seed = nextBenchmarkSeed(seed)
				if packageCount > 1 && seed%5 == 0 {
					target := (packageIndex + int(seed%uint64(packageCount-1)) + 1) % packageCount
					option.conflicts = append(option.conflicts, versionConflict{packageIndex: target, version: int64(seed%2) + 1})
				}
				catalog[packageIndex] = append(catalog[packageIndex], option)
			}
		}
		cases = append(cases, versionCase{catalog: catalog})
	}
	return cases
}

func versionCatalogToStarlark(catalog [][]versionOption) *starlark.List {
	packages := make([]starlark.Value, 0, len(catalog))
	for _, options := range catalog {
		versions := make([]starlark.Value, 0, len(options))
		for _, option := range options {
			requirements := make([]starlark.Value, 0, len(option.requirements))
			for _, requirement := range option.requirements {
				requirements = append(requirements, starlark.NewList([]starlark.Value{
					starlark.MakeInt(requirement.packageIndex), starlark.MakeInt64(requirement.minimum), starlark.MakeInt64(requirement.maximum),
				}))
			}
			conflicts := make([]starlark.Value, 0, len(option.conflicts))
			for _, conflict := range option.conflicts {
				conflicts = append(conflicts, starlark.NewList([]starlark.Value{
					starlark.MakeInt(conflict.packageIndex), starlark.MakeInt64(conflict.version),
				}))
			}
			versions = append(versions, starlark.NewList([]starlark.Value{
				starlark.MakeInt64(option.number), starlark.NewList(requirements), starlark.NewList(conflicts),
			}))
		}
		packages = append(packages, starlark.NewList(versions))
	}
	return starlark.NewList(packages)
}

func resolveVersionCatalog(catalog [][]versionOption) []int64 {
	if len(catalog) == 0 {
		return []int64{}
	}
	total := 1
	for _, options := range catalog {
		total *= len(options)
	}
	var best []int64
	for encoded := 0; encoded < total; encoded++ {
		remaining := encoded
		selected := make([]versionOption, len(catalog))
		versions := make([]int64, len(catalog))
		for packageIndex, options := range catalog {
			choice := remaining % len(options)
			remaining /= len(options)
			selected[packageIndex] = options[choice]
			versions[packageIndex] = options[choice].number
		}
		if versionSelectionValid(selected, versions) && (best == nil || lexicographicallyGreater(versions, best)) {
			best = append([]int64(nil), versions...)
		}
	}
	if best == nil {
		return []int64{}
	}
	return best
}

func versionSelectionValid(selected []versionOption, versions []int64) bool {
	for _, option := range selected {
		for _, requirement := range option.requirements {
			version := versions[requirement.packageIndex]
			if version < requirement.minimum || version > requirement.maximum {
				return false
			}
		}
		for _, conflict := range option.conflicts {
			if versions[conflict.packageIndex] == conflict.version {
				return false
			}
		}
	}
	return true
}

func lexicographicallyGreater(left, right []int64) bool {
	for index := range left {
		if left[index] != right[index] {
			return left[index] > right[index]
		}
	}
	return false
}

type ledgerEvent struct {
	sequence int64
	id       int64
	kind     string
	arg1     int64
	arg2     int64
	amount   int64
}

type ledgerCase struct {
	accounts []interval
	events   []ledgerEvent
}

type ledgerAudit struct {
	sequence int64
	id       int64
	status   string
}

type ledgerResult struct {
	balances []interval
	frozen   []int64
	audit    []ledgerAudit
}

type appliedTransfer struct {
	from, to int64
	amount   int64
	reversed bool
}

func verifyEventProcessor(code string) error {
	thread, function, err := loadBenchmarkFunction(code, "process_ledger")
	if err != nil {
		return err
	}
	for index, test := range ledgerTestCases() {
		accounts := intervalsToStarlark(test.accounts)
		events := ledgerEventsToStarlark(test.events)
		before := accounts.String() + events.String()
		renewHardBenchmarkStepBudget(thread)
		value, err := starlark.Call(thread, function, starlark.Tuple{accounts, events}, nil)
		if err != nil {
			return fmt.Errorf("case %d raised an error: %w", index+1, err)
		}
		if accounts.String()+events.String() != before {
			return fmt.Errorf("case %d mutated its input", index+1)
		}
		got, err := ledgerResultFromStarlark(value)
		if err != nil {
			return fmt.Errorf("case %d returned an invalid value: %w", index+1, err)
		}
		want := processLedger(test)
		if formatLedgerResult(got) != formatLedgerResult(want) {
			return fmt.Errorf("case %d returned %s, want %s", index+1, formatLedgerResult(got), formatLedgerResult(want))
		}
	}
	return nil
}

func ledgerTestCases() []ledgerCase {
	cases := []ledgerCase{
		{accounts: []interval{{start: 1, end: 100}, {start: 2, end: 20}}},
		{accounts: []interval{{start: 1, end: 100}, {start: 2, end: 20}}, events: []ledgerEvent{
			{sequence: 2, id: 11, kind: "transfer", arg1: 2, arg2: 1, amount: 40},
			{sequence: 1, id: 10, kind: "transfer", arg1: 1, arg2: 2, amount: 30},
		}},
		{accounts: []interval{{start: 1, end: 50}, {start: 2, end: 50}}, events: []ledgerEvent{
			{sequence: 1, id: 7, kind: "freeze", arg1: 1},
			{sequence: 2, id: 8, kind: "transfer", arg1: 1, arg2: 2, amount: 10},
			{sequence: 3, id: 9, kind: "unfreeze", arg1: 1},
			{sequence: 4, id: 8, kind: "transfer", arg1: 1, arg2: 2, amount: 10},
		}},
		{accounts: []interval{{start: 1, end: 80}, {start: 2, end: 20}}, events: []ledgerEvent{
			{sequence: 1, id: 1, kind: "transfer", arg1: 1, arg2: 2, amount: 40},
			{sequence: 2, id: 2, kind: "transfer", arg1: 2, arg2: 1, amount: 30},
			{sequence: 3, id: 3, kind: "reverse", arg1: 1},
			{sequence: 4, id: 4, kind: "reverse", arg1: 1},
		}},
		{accounts: []interval{{start: -1, end: 30}, {start: 4, end: 10}}, events: []ledgerEvent{
			{sequence: 1, id: 20, kind: "transfer", arg1: -1, arg2: 4, amount: 20},
			{sequence: 2, id: 21, kind: "transfer", arg1: 4, arg2: -1, amount: 25},
			{sequence: 3, id: 22, kind: "reverse", arg1: 999},
		}},
	}
	seed := uint64(0x1ED6E2)
	for caseIndex := 0; caseIndex < 10; caseIndex++ {
		accounts := []interval{{start: 0, end: 70}, {start: 1, end: 55}, {start: 2, end: 40}}
		events := make([]ledgerEvent, 0, 14)
		for eventIndex := 0; eventIndex < 14; eventIndex++ {
			seed = nextBenchmarkSeed(seed)
			kindIndex := seed % 6
			event := ledgerEvent{sequence: int64(eventIndex + 1), id: int64(caseIndex*100 + eventIndex + 1)}
			seed = nextBenchmarkSeed(seed)
			event.arg1 = int64(seed % 3)
			seed = nextBenchmarkSeed(seed)
			event.arg2 = int64(seed % 3)
			if event.arg2 == event.arg1 {
				event.arg2 = (event.arg2 + 1) % 3
			}
			seed = nextBenchmarkSeed(seed)
			event.amount = int64(seed%35) + 1
			switch kindIndex {
			case 0, 1, 2:
				event.kind = "transfer"
			case 3:
				event.kind = "freeze"
				event.arg2, event.amount = 0, 0
			case 4:
				event.kind = "unfreeze"
				event.arg2, event.amount = 0, 0
			default:
				event.kind = "reverse"
				if eventIndex > 0 {
					seed = nextBenchmarkSeed(seed)
					event.arg1 = int64(caseIndex*100 + int(seed%uint64(eventIndex)) + 1)
				} else {
					event.arg1 = -1
				}
				event.arg2, event.amount = 0, 0
			}
			if eventIndex > 2 && seed%11 == 0 {
				event.id = events[eventIndex-2].id
			}
			events = append(events, event)
		}
		for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
			events[left], events[right] = events[right], events[left]
		}
		cases = append(cases, ledgerCase{accounts: accounts, events: events})
	}
	return cases
}

func ledgerEventsToStarlark(events []ledgerEvent) *starlark.List {
	values := make([]starlark.Value, 0, len(events))
	for _, event := range events {
		values = append(values, starlark.NewList([]starlark.Value{
			starlark.MakeInt64(event.sequence), starlark.MakeInt64(event.id), starlark.String(event.kind),
			starlark.MakeInt64(event.arg1), starlark.MakeInt64(event.arg2), starlark.MakeInt64(event.amount),
		}))
	}
	return starlark.NewList(values)
}

func processLedger(test ledgerCase) ledgerResult {
	balances := make(map[int64]int64, len(test.accounts))
	for _, account := range test.accounts {
		balances[account.start] = account.end
	}
	events := append([]ledgerEvent(nil), test.events...)
	sort.Slice(events, func(left, right int) bool { return events[left].sequence < events[right].sequence })
	frozen := make(map[int64]bool)
	seen := make(map[int64]bool)
	transfers := make(map[int64]*appliedTransfer)
	audit := make([]ledgerAudit, 0, len(events))
	for _, event := range events {
		status := "invalid"
		if seen[event.id] {
			status = "duplicate"
			audit = append(audit, ledgerAudit{sequence: event.sequence, id: event.id, status: status})
			continue
		}
		seen[event.id] = true
		switch event.kind {
		case "transfer":
			if frozen[event.arg1] || frozen[event.arg2] {
				status = "frozen"
			} else if balances[event.arg1] < event.amount {
				status = "insufficient"
			} else {
				balances[event.arg1] -= event.amount
				balances[event.arg2] += event.amount
				transfers[event.id] = &appliedTransfer{from: event.arg1, to: event.arg2, amount: event.amount}
				status = "applied"
			}
		case "freeze":
			if frozen[event.arg1] {
				status = "noop"
			} else {
				frozen[event.arg1] = true
				status = "applied"
			}
		case "unfreeze":
			if !frozen[event.arg1] {
				status = "noop"
			} else {
				delete(frozen, event.arg1)
				status = "applied"
			}
		case "reverse":
			transfer, ok := transfers[event.arg1]
			if !ok || transfer.reversed {
				status = "invalid"
			} else if frozen[transfer.from] || frozen[transfer.to] {
				status = "frozen"
			} else if balances[transfer.to] < transfer.amount {
				status = "insufficient"
			} else {
				balances[transfer.to] -= transfer.amount
				balances[transfer.from] += transfer.amount
				transfer.reversed = true
				status = "applied"
			}
		}
		audit = append(audit, ledgerAudit{sequence: event.sequence, id: event.id, status: status})
	}
	accountIDs := make([]int64, 0, len(balances))
	for accountID := range balances {
		accountIDs = append(accountIDs, accountID)
	}
	sort.Slice(accountIDs, func(left, right int) bool { return accountIDs[left] < accountIDs[right] })
	result := ledgerResult{audit: audit}
	for _, accountID := range accountIDs {
		result.balances = append(result.balances, interval{start: accountID, end: balances[accountID]})
		if frozen[accountID] {
			result.frozen = append(result.frozen, accountID)
		}
	}
	return result
}

func ledgerResultFromStarlark(value starlark.Value) (ledgerResult, error) {
	outer, ok := value.(*starlark.List)
	if !ok || outer.Len() != 3 {
		return ledgerResult{}, errors.New("expected [balances, frozen_accounts, audit]")
	}
	balances, err := intPairsFromStarlark(outer.Index(0), 64)
	if err != nil {
		return ledgerResult{}, fmt.Errorf("balances: %w", err)
	}
	frozen, err := intListFromStarlark(outer.Index(1), 64)
	if err != nil {
		return ledgerResult{}, fmt.Errorf("frozen accounts: %w", err)
	}
	auditList, ok := outer.Index(2).(*starlark.List)
	if !ok || auditList.Len() > 256 {
		return ledgerResult{}, errors.New("audit must be a list of at most 256 entries")
	}
	audit := make([]ledgerAudit, 0, auditList.Len())
	for index := 0; index < auditList.Len(); index++ {
		entry, ok := auditList.Index(index).(*starlark.List)
		if !ok || entry.Len() != 3 {
			return ledgerResult{}, errors.New("every audit entry must be [sequence, event_id, status]")
		}
		sequence, sequenceOK := starlarkInt64(entry.Index(0))
		id, idOK := starlarkInt64(entry.Index(1))
		status, statusOK := entry.Index(2).(starlark.String)
		if !sequenceOK || !idOK || !statusOK {
			return ledgerResult{}, errors.New("audit entry has an invalid field type")
		}
		audit = append(audit, ledgerAudit{sequence: sequence, id: id, status: string(status)})
	}
	return ledgerResult{balances: balances, frozen: frozen, audit: audit}, nil
}

func formatLedgerResult(result ledgerResult) string {
	parts := make([]string, 0, len(result.audit))
	for _, entry := range result.audit {
		parts = append(parts, fmt.Sprintf("%d:%d:%s", entry.sequence, entry.id, entry.status))
	}
	return fmt.Sprintf("%s|%v|%s", formatIntervals(result.balances), result.frozen, strings.Join(parts, ","))
}

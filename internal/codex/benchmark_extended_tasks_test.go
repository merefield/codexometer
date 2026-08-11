package codex

import (
	"strings"
	"testing"
)

const correctDependencySchedulerSubmission = `
def _scheduler_decode(encoded, count):
    status = []
    for _ in range(count):
        status.append(encoded % 5)
        encoded //= 5
    return status

def _scheduler_encode(status):
    encoded = 0
    multiplier = 1
    for value in status:
        encoded += value * multiplier
        multiplier *= 5
    return encoded

def minimum_schedule_time(worker_count, jobs):
    if len(jobs) == 0:
        return 0
    maximum_time = 0
    selections = 1
    for job in jobs:
        maximum_time += job[0]
        selections *= 2
    if worker_count == 1:
        return maximum_time
    frontier = {0: True}
    for elapsed in range(maximum_time):
        next_states = {}
        for encoded in frontier:
            status = _scheduler_decode(encoded, len(jobs))
            running = 0
            available = []
            for job_index in range(len(jobs)):
                if status[job_index] > 1:
                    running += 1
                    available.append(False)
                elif status[job_index] != 0:
                    available.append(False)
                else:
                    ready = True
                    for dependency in jobs[job_index][1]:
                        if status[dependency] != 1:
                            ready = False
                    available.append(ready)
            free = worker_count - running
            for selection in range(selections):
                bits = selection
                selected = 0
                valid = True
                for job_index in range(len(jobs)):
                    take = bits % 2
                    bits //= 2
                    if take == 1:
                        selected += 1
                        if selected > free or not available[job_index]:
                            valid = False
                if running == 0 and selected == 0:
                    valid = False
                if not valid:
                    continue
                advanced = status[:]
                bits = selection
                complete = True
                for job_index in range(len(jobs)):
                    take = bits % 2
                    bits //= 2
                    if take == 1:
                        advanced[job_index] = jobs[job_index][0] + 1
                    if advanced[job_index] == 2:
                        advanced[job_index] = 1
                    elif advanced[job_index] > 2:
                        advanced[job_index] -= 1
                    if advanced[job_index] != 1:
                        complete = False
                if complete:
                    return elapsed + 1
                next_states[_scheduler_encode(advanced)] = True
        frontier = next_states
    return maximum_time
`

const correctVersionResolverSubmission = `
def _versions_greater(left, right):
    for index in range(len(left)):
        if left[index] != right[index]:
            return left[index] > right[index]
    return False

def resolve_versions(catalog):
    if len(catalog) == 0:
        return []
    total = 1
    for options in catalog:
        total *= len(options)
    best = None
    for encoded in range(total):
        remaining = encoded
        selected = []
        versions = []
        for options in catalog:
            choice = remaining % len(options)
            remaining //= len(options)
            selected.append(options[choice])
            versions.append(options[choice][0])
        valid = True
        for option in selected:
            for requirement in option[1]:
                version = versions[requirement[0]]
                if version < requirement[1] or version > requirement[2]:
                    valid = False
            for conflict in option[2]:
                if versions[conflict[0]] == conflict[1]:
                    valid = False
        if valid and (best == None or _versions_greater(versions, best)):
            best = versions
    if best == None:
        return []
    return best
`

const correctEventProcessorSubmission = `
def process_ledger(accounts, events):
    balances = {}
    for account in accounts:
        balances[account[0]] = account[1]
    frozen = {}
    seen = {}
    transfers = {}
    audit = []
    for event in sorted(events):
        sequence = event[0]
        event_id = event[1]
        kind = event[2]
        arg1 = event[3]
        arg2 = event[4]
        amount = event[5]
        status = "invalid"
        if event_id in seen:
            status = "duplicate"
        else:
            seen[event_id] = True
            if kind == "transfer":
                if frozen.get(arg1, False) or frozen.get(arg2, False):
                    status = "frozen"
                elif balances[arg1] < amount:
                    status = "insufficient"
                else:
                    balances[arg1] -= amount
                    balances[arg2] += amount
                    transfers[event_id] = [arg1, arg2, amount, False]
                    status = "applied"
            elif kind == "freeze":
                if frozen.get(arg1, False):
                    status = "noop"
                else:
                    frozen[arg1] = True
                    status = "applied"
            elif kind == "unfreeze":
                if not frozen.get(arg1, False):
                    status = "noop"
                else:
                    frozen[arg1] = False
                    status = "applied"
            elif kind == "reverse":
                if arg1 not in transfers or transfers[arg1][3]:
                    status = "invalid"
                else:
                    transfer = transfers[arg1]
                    if frozen.get(transfer[0], False) or frozen.get(transfer[1], False):
                        status = "frozen"
                    elif balances[transfer[1]] < transfer[2]:
                        status = "insufficient"
                    else:
                        balances[transfer[1]] -= transfer[2]
                        balances[transfer[0]] += transfer[2]
                        transfer[3] = True
                        status = "applied"
        audit.append([sequence, event_id, status])
    account_ids = sorted(balances.keys())
    final_balances = []
    frozen_accounts = []
    for account_id in account_ids:
        final_balances.append([account_id, balances[account_id]])
        if frozen.get(account_id, False):
            frozen_accounts.append(account_id)
    return [final_balances, frozen_accounts, audit]
`

func TestExtendedBenchmarkVerifiersAcceptCorrectSubmissions(t *testing.T) {
	for _, test := range []struct {
		name   string
		verify func(string) error
		code   string
	}{
		{name: "dependency scheduler", verify: verifyDependencyScheduler, code: correctDependencySchedulerSubmission},
		{name: "version resolver", verify: verifyVersionResolver, code: correctVersionResolverSubmission},
		{name: "event processor", verify: verifyEventProcessor, code: correctEventProcessorSubmission},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.verify(test.code); err != nil {
				t.Fatalf("correct submission failed: %v", err)
			}
		})
	}
}

func TestExtendedBenchmarkVerifiersRejectIncorrectSubmissions(t *testing.T) {
	for _, test := range []struct {
		name   string
		verify func(string) error
		code   string
	}{
		{name: "dependency scheduler", verify: verifyDependencyScheduler, code: "def minimum_schedule_time(worker_count, jobs):\n    return 0\n"},
		{name: "version resolver", verify: verifyVersionResolver, code: "def resolve_versions(catalog):\n    return []\n"},
		{name: "event processor", verify: verifyEventProcessor, code: "def process_ledger(accounts, events):\n    return [accounts, [], []]\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.verify(test.code); err == nil {
				t.Fatal("incorrect submission unexpectedly passed")
			}
		})
	}
}

func TestExtendedBenchmarkVerifiersRejectMutation(t *testing.T) {
	for _, test := range []struct {
		verify func(string) error
		code   string
	}{
		{verify: verifyDependencyScheduler, code: "def minimum_schedule_time(worker_count, jobs):\n    jobs.append([1, []])\n    return 0\n"},
		{verify: verifyVersionResolver, code: "def resolve_versions(catalog):\n    catalog.append([])\n    return []\n"},
		{verify: verifyEventProcessor, code: "def process_ledger(accounts, events):\n    accounts.append([99, 0])\n    return [accounts, [], []]\n"},
	} {
		if err := test.verify(test.code); err == nil || !strings.Contains(err.Error(), "mutated") {
			t.Fatalf("mutation error = %v", err)
		}
	}
}

func TestBenchmarkSuitesAndDifficultyLabels(t *testing.T) {
	core := BenchmarkTasksForSuite(BenchmarkSuiteCore)
	extended := BenchmarkTasksForSuite(BenchmarkSuiteExtended)
	if len(core) != 4 || len(extended) != 3 {
		t.Fatalf("suite sizes = core %d, extended %d", len(core), len(extended))
	}
	if core[0].ID != BenchmarkMergeRanges || extended[0].ID != BenchmarkDependencyScheduler || extended[2].ID != BenchmarkEventProcessor {
		t.Fatalf("suite order = core %#v, extended %#v", core, extended)
	}
	for _, task := range BenchmarkTasks() {
		if !strings.HasSuffix(task.Name, ")") || (!strings.Contains(task.Name, "(EASY)") && !strings.Contains(task.Name, "(MODERATE)") && !strings.Contains(task.Name, "(HARD)")) {
			t.Errorf("task %q has no difficulty suffix", task.Name)
		}
	}
}

func TestExtendedReferenceOraclesOnHandWrittenCases(t *testing.T) {
	wantSchedules := []int{0, 3, 4, 8, 6, 6}
	for index, want := range wantSchedules {
		if got := minimumScheduleTime(schedulerTestCases()[index]); got != want {
			t.Errorf("scheduler case %d = %d, want %d", index+1, got, want)
		}
	}
	wantVersions := [][]int64{{3}, {2, 2}, {2, 1}, {}, {4, 2, 2}}
	for index, want := range wantVersions {
		if got := resolveVersionCatalog(versionTestCases()[index].catalog); !equalInt64s(got, want) {
			t.Errorf("version case %d = %v, want %v", index+1, got, want)
		}
	}

	ledger := processLedger(ledgerTestCases()[3])
	if got, want := formatLedgerResult(ledger), "[[1,70],[2,30]]|[]|1:1:applied,2:2:applied,3:3:insufficient,4:4:insufficient"; got != want {
		t.Errorf("ledger reversal case = %s, want %s", got, want)
	}
}

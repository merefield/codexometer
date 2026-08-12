package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveUsageReaderBaselinesAndConsumesAppendedTelemetry(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "baseline")
	writeRollout(t, path, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}

	initial, err := reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if initial.TotalTokens != 0 {
		t.Fatalf("historical baseline counted as new usage: %#v", initial)
	}

	appendRollout(t, path,
		`{"timestamp":"ignored","type":"response_item","payload":{"content":"private text is ignored"}}`+"\n"+
			tokenCountLine(now, 150)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 50 || usage.LastActivity.IsZero() || usage.SessionCount != 1 {
		t.Fatalf("unexpected first live delta: %#v", usage)
	}

	appendRollout(t, path, tokenCountLine(now.Add(time.Second), 150)+"\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 50 {
		t.Fatalf("repeated cumulative total changed usage: %#v, %v", usage, err)
	}

	partial := tokenCountLine(now.Add(2*time.Second), 225)
	appendRollout(t, path, partial)
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 50 {
		t.Fatalf("partial JSONL record was consumed: %#v, %v", usage, err)
	}
	appendRollout(t, path, "\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 125 {
		t.Fatalf("completed JSONL record was not consumed: %#v, %v", usage, err)
	}
}

func TestAppendBoundedRetainsNewestValuesWithoutCopyingOnOverflow(t *testing.T) {
	history := make([]int, 3, 4)
	copy(history, []int{1, 2, 3})
	firstRetained := &history[1]

	history = appendBounded(history, 4, 3)
	if len(history) != 3 || history[0] != 2 || history[1] != 3 || history[2] != 4 {
		t.Fatalf("bounded history = %#v; want [2 3 4]", history)
	}
	if &history[0] != firstRetained {
		t.Fatal("bounded history copied its backing array on overflow")
	}

	history = appendBounded(history, 5, 3)
	if len(history) != 3 || history[0] != 3 || history[1] != 4 || history[2] != 5 {
		t.Fatalf("bounded history after reuse = %#v; want [3 4 5]", history)
	}
}

func TestLiveUsageReaderHandlesNewOldAndResetSessions(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	baselinePath := testRolloutPath(t, home, now.Add(-time.Hour), "known")
	writeRollout(t, baselinePath, tokenCountLine(now.Add(-time.Hour), 500)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	newPath := testRolloutPath(t, home, now.Add(2*time.Second), "new")
	writeRollout(t, newPath, tokenCountLine(now.Add(2*time.Second), 40)+"\n")
	reader.lastDiscovery = time.Time{}
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 40 {
		t.Fatalf("new session usage = %#v, %v; want 40", usage, err)
	}

	oldPath := testRolloutPath(t, home, now.Add(-2*time.Hour), "resumed")
	writeRollout(t, oldPath, tokenCountLine(now.Add(-2*time.Hour), 900)+"\n")
	reader.lastDiscovery = time.Time{}
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 40 {
		t.Fatalf("late-discovered old baseline counted historical usage: %#v, %v", usage, err)
	}
	appendRollout(t, oldPath, tokenCountLine(now.Add(3*time.Second), 925)+"\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 65 {
		t.Fatalf("resumed session delta = %#v, %v; want 65", usage, err)
	}

	appendRollout(t, baselinePath, tokenCountLine(now.Add(4*time.Second), 10)+"\n")
	appendRollout(t, baselinePath, tokenCountLine(now.Add(5*time.Second), 35)+"\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 90 {
		t.Fatalf("counter reset was mishandled: %#v, %v", usage, err)
	}
}

func TestLiveUsageReaderThrottlesDiscoveryButConsumesKnownFiles(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	knownPath := testRolloutPath(t, home, now.Add(-time.Hour), "known-throttle")
	writeRollout(t, knownPath, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	appendRollout(t, knownPath, tokenCountLine(now, 150)+"\n")
	newPath := testRolloutPath(t, home, now.Add(time.Second), "new-throttle")
	writeRollout(t, newPath, tokenCountLine(now.Add(time.Second), 40)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 50 {
		t.Fatalf("known cursor was not consumed independently of discovery: %#v, %v", usage, err)
	}
	if _, exists := reader.files[newPath]; exists {
		t.Fatal("new rollout was discovered before the discovery interval elapsed")
	}

	reader.lastDiscovery = time.Now().Add(-discoveryEvery)
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 90 {
		t.Fatalf("due discovery did not add new rollout usage: %#v, %v", usage, err)
	}
}

func TestFreshLiveUsageReadNoticesAResumedDormantSession(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	created := now.Add(-72 * time.Hour)
	path := testRolloutPath(t, home, created, "dormant")
	writeRollout(t, path, tokenCountLine(created, 1_000)+"\n")
	if err := os.Chtimes(path, created, created); err != nil {
		t.Fatal(err)
	}
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	appendRollout(t, path, tokenCountLine(now, 1_075)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 0 {
		t.Fatalf("ordinary read unexpectedly rediscovered dormant session: %#v, %v", usage, err)
	}
	usage, err = reader.FetchTokenUsageFresh(context.Background())
	if err != nil || usage.TotalTokens != 75 {
		t.Fatalf("fresh read resumed dormant session usage = %#v, %v; want 75", usage, err)
	}
}

func TestLiveUsageReaderExpiresInactiveSessionCount(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "inactive")
	writeRollout(t, path, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path, tokenCountLine(now, 125)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.SessionCount != 1 {
		t.Fatalf("active session count = %#v, %v; want one", usage, err)
	}

	inactiveAt := now.Add(-6 * time.Minute)
	if err := os.Chtimes(path, inactiveAt, inactiveAt); err != nil {
		t.Fatal(err)
	}
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil || usage.SessionCount != 0 {
		t.Fatalf("inactive session count = %#v, %v; want zero", usage, err)
	}
}

func TestLiveUsageReaderKeepsNewestActivityAcrossEvents(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := testRolloutPath(t, home, now.Add(-time.Hour), "activity-order")
	writeRollout(t, path, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path, tokenCountLine(now, 150)+"\n"+tokenCountLine(now.Add(-time.Minute), 175)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || !usage.LastActivity.Equal(now) {
		t.Fatalf("last activity = %s, %v; want newest %s", usage.LastActivity, err, now)
	}
}

func TestLiveUsageReaderCapturesContentFreeModelCallAndTurnTimingPulses(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := testRolloutPath(t, home, now.Add(-time.Hour), "response-pulses")
	writeRollout(t, path, sessionMetaLine("root", `"cli"`, "/work/root", nil)+"\n"+tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	appendRollout(t, path,
		tokenCountLineWithOutput(now, 150, 2_013)+"\n"+
			turnTimingLine(now.Add(time.Second), 11_600)+"\n"+
			tokenCountLineWithOutput(now.Add(2*time.Second), 225, 842)+"\n"+
			turnTimingLine(now.Add(3*time.Second), 2_400)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || len(usage.Sessions) != 1 {
		t.Fatalf("pulse telemetry fetch = %#v, %v", usage, err)
	}
	session := usage.Sessions[0]
	if len(session.ModelCalls) != 2 || session.ModelCalls[0].OutputTokens != 2_013 || session.ModelCalls[1].OutputTokens != 842 ||
		!session.ModelCalls[0].OutputAvailable || !session.ModelCalls[1].OutputAvailable {
		t.Fatalf("model-call pulses = %#v", session.ModelCalls)
	}
	if len(session.TurnTimings) != 2 || session.TurnTimings[0].TimeToFirstToken != 11_600*time.Millisecond ||
		session.TurnTimings[1].TimeToFirstToken != 2_400*time.Millisecond ||
		!session.TurnTimings[0].Available || !session.TurnTimings[1].Available {
		t.Fatalf("turn timings = %#v", session.TurnTimings)
	}
	if session.ModelCalls[0].Sequence >= session.TurnTimings[0].Sequence ||
		session.TurnTimings[0].Sequence >= session.ModelCalls[1].Sequence {
		t.Fatalf("telemetry sequence did not preserve rollout order: calls=%#v turns=%#v", session.ModelCalls, session.TurnTimings)
	}

	appendRollout(t, path,
		tokenCountLine(now.Add(4*time.Second), 250)+"\n"+
			turnTimingUnavailableLine(now.Add(5*time.Second))+"\n")
	usage, err = reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session = usage.Sessions[0]
	if len(session.ModelCalls) != 3 || session.ModelCalls[2].OutputAvailable {
		t.Fatalf("legacy model call was not retained as unavailable: %#v", session.ModelCalls)
	}
	if len(session.TurnTimings) != 3 || session.TurnTimings[2].Available {
		t.Fatalf("legacy turn timing was not retained as unavailable: %#v", session.TurnTimings)
	}
}

func TestLiveUsageReaderGroupsSpawnedDescendantsUnderIndependentRoots(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	created := now.Add(-time.Hour)
	paths := map[string]string{
		"root-a":   testRolloutPath(t, home, created, "root-a"),
		"root-b":   testRolloutPath(t, home, created, "root-b"),
		"child-a":  testRolloutPath(t, home, created, "child-a"),
		"nested-a": testRolloutPath(t, home, created, "nested-a"),
		"review":   testRolloutPath(t, home, created, "review"),
	}
	writeRollout(t, paths["root-a"], sessionMetaLine("root-a", `"cli"`, "/work/alpha", nil)+"\n"+tokenCountLine(created, 100)+"\n")
	writeRollout(t, paths["root-b"], sessionMetaLine("root-b", `"cli"`, "/work/bravo", nil)+"\n"+tokenCountLine(created, 200)+"\n")
	writeRollout(t, paths["child-a"], sessionMetaLine("child-a", threadSpawnSource("root-a"), "/work/alpha", nil)+"\n"+tokenCountLine(created, 300)+"\n")
	writeRollout(t, paths["nested-a"], sessionMetaLine("nested-a", threadSpawnSource("child-a"), "/work/alpha", nil)+"\n"+tokenCountLine(created, 400)+"\n")
	writeRollout(t, paths["review"], sessionMetaLine("review", `{"subagent":"review"}`, "/work/alpha", nil)+"\n"+tokenCountLine(created, 500)+"\n")

	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	for path, total := range map[string]int64{
		paths["root-a"]: 150, paths["root-b"]: 230, paths["child-a"]: 325,
		paths["nested-a"]: 410, paths["review"]: 505,
	} {
		appendRollout(t, path, tokenCountLine(now, total)+"\n")
	}

	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 120 || usage.SessionCount != 3 {
		t.Fatalf("aggregate usage = %#v; want 120 tokens across three logical roots", usage)
	}
	byID := make(map[string]LiveUsageSession)
	for _, session := range usage.Sessions {
		byID[session.ID] = session
	}
	if got := byID["root-a"]; got.TotalTokens != 85 || got.AgentCount != 2 || got.WorkingDirectory != "/work/alpha" || got.Unattributed {
		t.Fatalf("root A grouping = %#v; want root plus two descendants and 85 tokens", got)
	} else if len(got.ModelCalls) != 3 {
		t.Fatalf("root A model calls = %#v; want three root/descendant pulses", got.ModelCalls)
	}
	if got := byID["root-b"]; got.TotalTokens != 30 || got.AgentCount != 0 || got.WorkingDirectory != "/work/bravo" {
		t.Fatalf("root B grouping = %#v; want independent 30-token root", got)
	}
	if got := byID[unattributedSessionID]; got.TotalTokens != 5 || !got.Unattributed {
		t.Fatalf("unlinked review grouping = %#v; want explicit unattributed bucket", got)
	}
}

func TestLiveUsageReaderSkipsInheritedChildTokenHistory(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	rootPath := testRolloutPath(t, home, now.Add(-time.Hour), "parent")
	writeRollout(t, rootPath, sessionMetaLine("parent", `"cli"`, "/work/root", nil)+"\n"+tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	boundary := uint64(3)
	childPath := testRolloutPath(t, home, now.Add(2*time.Second), "child-with-prefix")
	writeRollout(t, childPath,
		sessionMetaLine("child", threadSpawnSource("parent"), "/work/root", &boundary)+"\n"+
			tokenCountLineWithOrdinal(now, 1_000, 1)+"\n"+
			tokenCountLineWithOrdinal(now.Add(time.Second), 1_200, 3)+"\n")
	reader.lastDiscovery = time.Time{}
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 200 || len(usage.Sessions) != 1 || usage.Sessions[0].ID != "parent" || usage.Sessions[0].AgentCount != 1 {
		t.Fatalf("inherited child history was double counted or detached: %#v", usage)
	}
}

func TestLiveUsageReaderBaselinesAChildThatInitiallyHasOnlyInheritedHistory(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	rootPath := testRolloutPath(t, home, now.Add(-time.Hour), "parent-before-monitor")
	writeRollout(t, rootPath, sessionMetaLine("parent", `"cli"`, "/work/root", nil)+"\n"+tokenCountLine(now.Add(-time.Hour), 1_000)+"\n")

	boundary := uint64(3)
	childPath := testRolloutPath(t, home, now.Add(-time.Minute), "child-before-monitor")
	writeRollout(t, childPath,
		sessionMetaLine("child", threadSpawnSource("parent"), "/work/root", &boundary)+"\n"+
			tokenCountLineWithOrdinal(now.Add(-time.Hour), 1_000, 1)+"\n")

	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, childPath, tokenCountLineWithOrdinal(now, 1_200, 3)+"\n")

	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 200 || len(usage.Sessions) != 1 || usage.Sessions[0].ID != "parent" {
		t.Fatalf("first owned child record included inherited history: %#v", usage)
	}
}

func TestLiveUsageReaderUsesLegacyInheritedTotalsOnlyAsChildBaseline(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	rootPath := testRolloutPath(t, home, now.Add(-time.Hour), "legacy-parent")
	writeRollout(t, rootPath, sessionMetaLineAt("legacy-parent", `"cli"`, "/work/root", nil, now.Add(-time.Hour))+"\n"+tokenCountLine(now.Add(-time.Hour), 1_000)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	childPath := testRolloutPath(t, home, now.Add(2*time.Second), "legacy-child")
	writeRollout(t, childPath,
		sessionMetaLineAt("legacy-child", threadSpawnSource("legacy-parent"), "/work/root", nil, now)+"\n"+
			tokenCountLine(now.Add(-time.Minute), 1_000)+"\n"+
			tokenCountLine(now.Add(time.Second), 1_200)+"\n")
	reader.lastDiscovery = time.Time{}
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 200 || len(usage.Sessions) != 1 || usage.Sessions[0].ID != "legacy-parent" {
		t.Fatalf("legacy inherited usage was not treated as a baseline: %#v", usage)
	}
}

func TestSessionSourceParentRecognizesCodexSourceShapes(t *testing.T) {
	for _, test := range []struct {
		name       string
		source     string
		wantParent string
		wantAgent  bool
	}{
		{name: "missing", source: "null"},
		{name: "root", source: `"cli"`},
		{name: "legacy agent string", source: `"subagent_review"`, wantAgent: true},
		{name: "spawned", source: threadSpawnSource("parent-id"), wantParent: "parent-id", wantAgent: true},
		{name: "unlinked review", source: `{"subagent":"review"}`, wantAgent: true},
		{name: "internal", source: `{"internal":"memory_consolidation"}`, wantAgent: true},
		{name: "malformed", source: `{not-json`},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, agent := sessionSourceParent(json.RawMessage(test.source))
			if parent != test.wantParent || agent != test.wantAgent {
				t.Fatalf("sessionSourceParent(%s) = %q, %v; want %q, %v", test.source, parent, agent, test.wantParent, test.wantAgent)
			}
		})
	}
}

func TestRolloutRootRejectsOrphanedAndCyclicAgentLineage(t *testing.T) {
	root := &rolloutCursor{threadID: "root"}
	child := &rolloutCursor{threadID: "child", parentThreadID: "root", nonRoot: true}
	index := map[string]*rolloutCursor{"root": root, "child": child}
	if id, unattributed := rolloutRoot(child, index); id != "root" || unattributed {
		t.Fatalf("linked child resolved to %q, %v; want root", id, unattributed)
	}

	orphan := &rolloutCursor{threadID: "orphan", parentThreadID: "missing", nonRoot: true}
	if id, unattributed := rolloutRoot(orphan, index); id != unattributedSessionID || !unattributed {
		t.Fatalf("orphan resolved to %q, %v; want unattributed", id, unattributed)
	}

	cycleA := &rolloutCursor{threadID: "cycle-a", parentThreadID: "cycle-b", nonRoot: true}
	cycleB := &rolloutCursor{threadID: "cycle-b", parentThreadID: "cycle-a", nonRoot: true}
	cycleIndex := map[string]*rolloutCursor{"cycle-a": cycleA, "cycle-b": cycleB}
	if id, unattributed := rolloutRoot(cycleA, cycleIndex); id != unattributedSessionID || !unattributed {
		t.Fatalf("cycle resolved to %q, %v; want unattributed", id, unattributed)
	}
}

func TestLiveUsageReaderContinuesPastUnreadableCursor(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "readable")
	writeRollout(t, path, tokenCountLine(now.Add(-time.Hour), 100)+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}

	badPath := filepath.Join(home, "unreadable-rollout")
	if err := os.Mkdir(badPath, 0o700); err != nil {
		t.Fatal(err)
	}
	probe := &rolloutCursor{lastModified: now}
	if err := reader.consume(badPath, probe); err == nil {
		t.Skip("platform permits directory cursor reads")
	}
	reader.files[badPath] = &rolloutCursor{lastModified: now}
	appendRollout(t, path, tokenCountLine(now, 150)+"\n")
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 50 {
		t.Fatalf("readable telemetry was lost beside bad cursor: %#v, %v", usage, err)
	}
}

func TestLiveUsageReaderIgnoresMalformedAndNullTokenEvents(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "malformed")
	writeRollout(t, path, "{not json token_count}\n"+
		`{"type":"event_msg","payload":{"type":"token_count","info":null}}`+"\n")
	reader, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := reader.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 0 {
		t.Fatalf("malformed events affected usage: %#v, %v", usage, err)
	}
}

func TestLatestTokenTotalSearchesBackwardAcrossLargeRecords(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	path := testRolloutPath(t, home, now, "large-tail")
	writeRollout(t, path, tokenCountLine(now, 4321)+"\n"+
		`{"type":"response_item","payload":{"text":"`+strings.Repeat("x", 130_000)+`"}}`+"\n")
	total, err := latestTokenTotal(path)
	if err != nil || total != 4321 {
		t.Fatalf("latestTokenTotal() = %d, %v; want 4321", total, err)
	}
}

func TestLiveUsageReaderHonorsCodexHomeAndCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	resolved, err := DefaultCodexHome()
	if err != nil || resolved != filepath.Clean(home) {
		t.Fatalf("DefaultCodexHome() = %q, %v", resolved, err)
	}
	reader, err := NewLiveUsageReader("")
	if err != nil || reader.SessionsRoot != filepath.Join(home, "sessions") {
		t.Fatalf("reader root = %q, %v", reader.SessionsRoot, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.FetchTokenUsage(ctx); err == nil {
		t.Fatal("cancelled discovery returned no error")
	}
}

func tokenCountLine(at time.Time, total int64) string {
	return fmt.Sprintf(
		`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d},"last_token_usage":{"total_tokens":%d}}}}`,
		at.UTC().Format(time.RFC3339Nano), total, total,
	)
}

func tokenCountLineWithOutput(at time.Time, total, output int64) string {
	return fmt.Sprintf(
		`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d},"last_token_usage":{"output_tokens":%d,"total_tokens":%d}}}}`,
		at.UTC().Format(time.RFC3339Nano), total, output, total,
	)
}

func turnTimingLine(at time.Time, ttftMS int64) string {
	return fmt.Sprintf(
		`{"timestamp":%q,"type":"event_msg","payload":{"type":"task_complete","completed_at":%d,"duration_ms":12000,"time_to_first_token_ms":%d}}`,
		at.UTC().Format(time.RFC3339Nano), at.Unix(), ttftMS,
	)
}

func turnTimingUnavailableLine(at time.Time) string {
	return fmt.Sprintf(
		`{"timestamp":%q,"type":"event_msg","payload":{"type":"task_complete","completed_at":%d,"duration_ms":12000}}`,
		at.UTC().Format(time.RFC3339Nano), at.Unix(),
	)
}

func tokenCountLineWithOrdinal(at time.Time, total int64, ordinal uint64) string {
	return fmt.Sprintf(
		`{"timestamp":%q,"ordinal":%d,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d}}}}`,
		at.UTC().Format(time.RFC3339Nano), ordinal, total,
	)
}

func sessionMetaLine(id, source, cwd string, boundary *uint64) string {
	return sessionMetaLineAt(id, source, cwd, boundary, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
}

func sessionMetaLineAt(id, source, cwd string, boundary *uint64, at time.Time) string {
	boundaryJSON := "null"
	if boundary != nil {
		boundaryJSON = fmt.Sprintf("%d", *boundary)
	}
	return fmt.Sprintf(
		`{"timestamp":%q,"type":"session_meta","payload":{"id":%q,"timestamp":%q,"cwd":%q,"source":%s,"subagent_history_start_ordinal":%s}}`,
		at.UTC().Format(time.RFC3339Nano), id, at.UTC().Format(time.RFC3339Nano), cwd, source, boundaryJSON,
	)
}

func threadSpawnSource(parent string) string {
	return fmt.Sprintf(`{"subagent":{"thread_spawn":{"parent_thread_id":%q,"depth":1}}}`, parent)
}

func testRolloutPath(t *testing.T, home string, created time.Time, suffix string) string {
	t.Helper()
	directory := filepath.Join(home, "sessions", created.Format("2006"), created.Format("01"), created.Format("02"))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rollout-" + created.In(time.Local).Format("2006-01-02T15-04-05") + "-" + suffix + ".jsonl"
	return filepath.Join(directory, name)
}

func writeRollout(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendRollout(t *testing.T, path, contents string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(contents); err != nil {
		t.Fatal(err)
	}
}

func TestRolloutHelpers(t *testing.T) {
	now := time.Now()
	name := "rollout-" + now.Add(time.Second).Format("2006-01-02T15-04-05") + "-id.jsonl"
	if !isRolloutFile(name) || !rolloutCreatedAfter(name, now) {
		t.Fatalf("valid rollout was not recognized: %s", name)
	}
	if isRolloutFile("notes.jsonl") || rolloutCreatedAfter("rollout-bad.jsonl", now) {
		t.Fatal("invalid rollout was recognized")
	}
	if _, _, ok := tokenTotal([]byte(strings.ReplaceAll(tokenCountLine(now, 7), "event_msg", "other"))); ok {
		t.Fatal("non-event token payload was accepted")
	}
}

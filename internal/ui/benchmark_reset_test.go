package ui

import (
	"reflect"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestBenchmarkStartAndClearResetPresentationWithoutLosingPreferences(t *testing.T) {
	for _, start := range []bool{false, true} {
		t.Run(map[bool]string{false: "clear", true: "start"}[start], func(t *testing.T) {
			old := codex.BenchmarkResult{TaskID: codex.BenchmarkLRUCache, TaskName: "LRU CACHE", Model: "saved-model", Effort: "high"}
			scope := codex.BenchmarkScope{Models: []string{"saved-model"}, Efforts: []string{"high"}}
			model := Model{
				benchmarkRunner: benchmarkCaptureFetcher{}, benchmarkCombinations: 2,
				benchmarkState: benchmarkFinished, benchmarkResults: []codex.BenchmarkResult{old},
				benchmarkTotal: 99, benchmarkCompleted: 42, benchmarkCurrentModel: "stale", benchmarkCurrentEffort: "stale", benchmarkCurrentTask: "stale", benchmarkError: "stale",
				benchmarkScroll: 7, benchmarkActive: &old, benchmarkActiveSince: time.Now(), benchmarkSelectedRun: "stale", benchmarkHoveredRun: "stale", benchmarkRunHovered: true,
				benchmarkDetail: &old, benchmarkDetailActive: true, benchmarkDetailScroll: 9, benchmarkDetailCache: benchmarkDetailTranscriptCache{valid: true, lines: []string{"cached"}},
				benchmarkAllArmed: true, benchmarkSelectedArmed: true, benchmarkScopeOpen: true, benchmarkScopeKeyboard: true,
				benchmarkScope: scope, benchmarkFilter: benchmarkFilterFail, benchmarkRankMode: benchmarkRankCost, benchmarkSort: benchmarkSortTokens, benchmarkSortDescending: true,
			}
			if start {
				updated, cmd := model.startBenchmark([]codex.BenchmarkTaskID{codex.BenchmarkMergeRanges}, scope, false)
				model = updated
				defer model.cancelBenchmark()
				if cmd == nil || model.benchmarkState != benchmarkRunning || model.benchmarkTotal != 2 || len(model.benchmarkResults) != 1 || model.benchmarkResults[0].TaskID != old.TaskID || model.benchmarkRunResults == nil || model.benchmarkRunIndexes == nil {
					t.Fatal("start changed run/history lifecycle")
				}
			} else {
				model.clearBenchmarkResults()
				if model.benchmarkState != benchmarkIdle || model.benchmarkTotal != 0 || model.benchmarkResults != nil || model.benchmarkResultIndexes != nil || model.benchmarkRunResults != nil || model.benchmarkRunIndexes != nil {
					t.Fatal("clear did not discard results and run indexes")
				}
			}
			if model.benchmarkCompleted != 0 || model.benchmarkCurrentModel != "" || model.benchmarkCurrentEffort != "" || model.benchmarkCurrentTask != "" || model.benchmarkError != "" || model.benchmarkScroll != 0 || model.benchmarkActive != nil || !model.benchmarkActiveSince.IsZero() || model.benchmarkSelectedRun != "" || model.benchmarkHoveredRun != "" || model.benchmarkRunHovered || model.benchmarkDetail != nil || model.benchmarkDetailActive || model.benchmarkDetailScroll != 0 || !reflect.DeepEqual(model.benchmarkDetailCache, benchmarkDetailTranscriptCache{}) || model.benchmarkAllArmed || model.benchmarkSelectedArmed {
				t.Fatal("stale benchmark presentation survived")
			}
			if !reflect.DeepEqual(model.benchmarkScope, scope) || model.benchmarkFilter != benchmarkFilterFail || model.benchmarkRankMode != benchmarkRankCost || model.benchmarkSort != benchmarkSortTokens || !model.benchmarkSortDescending {
				t.Fatal("reset changed scope/filter/ranking preferences")
			}
			if model.benchmarkScopeOpen == start || model.benchmarkScopeKeyboard == start {
				t.Fatal("start/clear scope-form behaviours were conflated")
			}
		})
	}
}

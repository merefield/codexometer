package codex

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func exactLine(t *testing.T, at time.Time, thread, turn, response string, usage BenchmarkUsage) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": "token_usage_record", "timestamp": at, "payload": map[string]any{
		"thread_id": thread, "turn_id": turn, "response_id": response,
		"usage": map[string]any{"input_tokens": usage.InputTokens, "cached_input_tokens": usage.CachedInputTokens,
			"cache_write_input_tokens": usage.CacheWriteInputTokens, "output_tokens": usage.OutputTokens,
			"reasoning_output_tokens": usage.ReasoningOutputTokens, "total_tokens": usage.TotalTokens}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestExactUsageIgnoresContextCountersAndDeduplicatesAcrossPolls(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	r.statusProvider = nil
	u := BenchmarkUsage{InputTokens: 1000, CachedInputTokens: 200, CacheWriteInputTokens: 30, OutputTokens: 100, ReasoningOutputTokens: 80, TotalTokens: 1100}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "r1", u)+"\n"+richTokenCountLine(now, 9000, 1000, 200, 30, 100)+"\n")
	first, err := r.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want, _, _ := EstimateStandardAPIEqCost("gpt-6-astra", u)
	if first.TotalTokens != 1100 || first.APIEqPricedCalls != 1 || first.APIEqUnpricedCalls != 0 || first.APIEqInconsistentUsageCalls != 0 || math.Abs(first.APIEqUSD-want) > 1e-12 {
		t.Fatal("exact response depended on context counter", first)
	}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "r1", u)+"\n"+contextFillLine(t, now, 128000, 119000)+"\n")
	duplicate, err := r.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.TotalTokens != first.TotalTokens || duplicate.APIEqPricedCalls != 1 || duplicate.APIEqAccountingAdjustments != 0 || len(duplicate.Sessions[0].ModelCalls) != 1 {
		t.Fatal("replay/bookkeeping affected exact accounting", duplicate)
	}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "r2", u)+"\n")
	next, err := r.FetchTokenUsage(context.Background())
	if err != nil || next.TotalTokens != 2200 || next.APIEqPricedCalls != 2 || math.Abs(next.APIEqUSD-2*want) > 1e-12 {
		t.Fatal("next exact response was lost", next, err)
	}
}

func TestExactUsageStartupBetweenResponseAndContextSnapshot(t *testing.T) {
	_, path, now := astraUsageReader(t, 100)
	u := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "old", u)+"\n")

	home := strings.Split(path, string(filepath.Separator)+"sessions"+string(filepath.Separator))[0]
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.statusProvider = nil
	baseline, err := r.FetchTokenUsage(context.Background())
	if err != nil || baseline.TotalTokens != 0 || baseline.APIEqPricedCalls != 0 {
		t.Fatal("historical exact response counted", baseline, err)
	}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "old", u)+"\n"+richTokenCountLine(now, 1200, 1000, 0, 0, 100)+"\n")
	replay, err := r.FetchTokenUsage(context.Background())
	if err != nil || replay.TotalTokens != 0 || replay.APIEqPricedCalls != 0 {
		t.Fatal("startup replay/context duplicate counted", replay, err)
	}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "new", u)+"\n")
	fresh, err := r.FetchTokenUsage(context.Background())
	if err != nil || fresh.TotalTokens != 1100 || fresh.APIEqPricedCalls != 1 {
		t.Fatal("fresh exact response lost", fresh, err)
	}

}

func TestExactUsageOwnedRecordsExcludeInheritedHistory(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	r.statusProvider = nil
	cursor := r.files[path]
	ordinal := uint64(10)
	cursor.nonRoot = true
	cursor.subagentHistoryStartOrdinal = &ordinal
	u := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	inherited := exactLine(t, now, "astra-accounting", "t", "inherited", u)
	var frame map[string]any
	_ = json.Unmarshal([]byte(inherited), &frame)
	frame["ordinal"] = 9
	raw, _ := json.Marshal(frame)
	appendRollout(t, path, exactLine(t, now, "parent-thread", "t", "parent", u)+"\n"+string(raw)+"\n")
	first, err := r.FetchTokenUsage(context.Background())
	if err != nil || first.APIEqPricedCalls != 0 || first.TotalTokens != 0 {
		t.Fatal("inherited response was charged", first, err)
	}
	frame["ordinal"] = 11
	frame["payload"].(map[string]any)["response_id"] = "owned"
	raw, _ = json.Marshal(frame)
	appendRollout(t, path, string(raw)+"\n")
	owned, err := r.FetchTokenUsage(context.Background())
	if err != nil || owned.APIEqPricedCalls != 1 || owned.TotalTokens != 1100 {
		t.Fatal("owned response was not charged", owned, err)
	}
}

func TestExactUsageSeparatesMissingInvalidAndUnpricedWithDiagnostics(t *testing.T) {
	for _, kind := range []string{"missing", "invalid", "unknown-field", "reserve"} {
		t.Run(kind, func(t *testing.T) {
			r, path, now := astraUsageReader(t, 100)
			r.statusProvider = nil
			u := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
			line := exactLine(t, now, "astra-accounting", "t", "r", u)
			var frame map[string]any
			_ = json.Unmarshal([]byte(line), &frame)
			fields := frame["payload"].(map[string]any)["usage"].(map[string]any)
			switch kind {
			case "missing":
				delete(fields, "cached_input_tokens")
			case "invalid":
				fields["cached_input_tokens"] = 1001
			case "unknown-field":
				fields["future_billable_tokens"] = 20
			case "reserve":
				appendRollout(t, path, turnContextLine(now, "gpt-reserve")+"\n")
			}
			raw, _ := json.Marshal(frame)
			appendRollout(t, path, string(raw)+"\n")
			got, err := r.FetchTokenUsage(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			wanted := UsageIssueInconsistent
			if kind == "missing" {
				wanted = UsageIssueMissing
			}
			if kind == "reserve" {
				wanted = UsageIssueUnpriced
			}
			if len(got.APIEqDiagnostics) != 1 || got.APIEqDiagnostics[0].Kind != wanted || got.APIEqDiagnostics[0].SessionID != "astra-accounting" || got.APIEqDiagnostics[0].ResponseID != "r" || got.APIEqDiagnostics[0].Reason == "" {
				t.Fatal("failure attribution lost", got)
			}
			if kind != "reserve" && got.APIEqUnpricedCalls != 0 {
				t.Fatal("bad usage became missing pricing", got)
			}
		})
	}
}

func TestExactUsageKeepsDaemonModelResolution(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	u := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	r.statusProvider = stubSessionStatusProvider{subscribed: map[string]struct{}{"astra-accounting": {}}}
	appendRollout(t, path, turnContextWithTurnIDLine(now, "turn", "gpt-6-astra")+"\n"+exactLine(t, now, "astra-accounting", "turn", "r", u)+"\n"+richTokenCountLine(now, 9999, 1000, 0, 0, 100)+"\n")
	first, err := r.FetchTokenUsage(context.Background())
	if err != nil || first.APIEqPendingCalls != 1 || first.APIEqPricedCalls != 0 {
		t.Fatal("response did not wait for attribution", first, err)
	}
	r.statusProvider = stubSessionStatusProvider{subscribed: map[string]struct{}{"astra-accounting": {}}, observations: []resolvedModelObservation{{Sequence: 1, ThreadID: "astra-accounting", TurnID: "turn", Model: "gpt-5.2", Usage: u, CumulativeTotal: 9999}}}
	resolved, err := r.FetchTokenUsage(context.Background())
	if err != nil || resolved.APIEqPendingCalls != 0 || resolved.APIEqPricedCalls != 1 || resolved.Sessions[0].ModelCalls[0].Model != "gpt-5.2" {
		t.Fatal("exact response lost reroute attribution", resolved, err)
	}
}

func TestExactConflictingReplayIsReportedOnceWithoutAdditionalCost(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	r.statusProvider = nil
	u := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "r", u)+"\n")
	first, _ := r.FetchTokenUsage(context.Background())
	u.InputTokens++
	u.TotalTokens++
	line := exactLine(t, now, "astra-accounting", "t", "r", u) + "\n"
	appendRollout(t, path, line+line)
	got, err := r.FetchTokenUsage(context.Background())
	if err != nil || got.APIEqUSD != first.APIEqUSD || got.TotalTokens != first.TotalTokens || got.APIEqInconsistentUsageCalls != 1 || len(got.APIEqDiagnostics) != 1 || !strings.Contains(got.APIEqDiagnostics[0].Reason, "conflicting usage") {
		t.Fatal("conflicting replay charged or hidden", got, err)
	}
}

func TestExactDiscoveryDoesNotBaselineConcurrentAppendOrPartialRecord(t *testing.T) {
	for _, partial := range []bool{false, true} {
		r, path, now := astraUsageReader(t, 100)
		r.statusProvider = nil
		u := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
		line := exactLine(t, now, "astra-accounting", "t", "concurrent", u) + "\n"
		if partial {
			appendRollout(t, path, line[:len(line)/2])
		}
		file, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if partial {
			appendRollout(t, path, line[len(line)/2:])
		} else {
			appendRollout(t, path, line)
		}
		delete(r.files, path)
		if err := r.addFile(path, file); err != nil {
			t.Fatal(err)
		}
		got, err := r.FetchTokenUsage(context.Background())
		if err != nil || got.APIEqPricedCalls != 1 || got.TotalTokens != 1100 {
			t.Fatal("concurrent/partial append was silently baselined", partial, got, err)
		}
	}
}

func TestExactReplacementRolloutDoesNotBillCopiedThreadHistory(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	r.statusProvider = nil
	u := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	line := exactLine(t, now, "astra-accounting", "t", "old", u) + "\n"
	appendRollout(t, path, line)
	first, _ := r.FetchTokenUsage(context.Background())
	home := strings.Split(path, string(filepath.Separator)+"sessions"+string(filepath.Separator))[0]
	replacement := testRolloutPath(t, home, time.Now().Add(time.Second), "replacement-rollout")
	writeRollout(t, replacement, sessionMetaLine("astra-accounting", `"cli"`, "/synthetic", nil)+"\n"+turnContextLine(now, "gpt-6-sol")+"\n"+line)
	info, err := os.Stat(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.addFile(replacement, info); err != nil {
		t.Fatal(err)
	}
	got, err := r.FetchTokenUsage(context.Background())
	if err != nil || got.TotalTokens != first.TotalTokens || got.APIEqPricedCalls != 1 || got.APIEqAccountingAdjustments != 1 {
		t.Fatal("replacement history billed again", got, err)
	}
	appendRollout(t, replacement, exactLine(t, now, "astra-accounting", "t", "new", u)+"\n")
	next, err := r.FetchTokenUsage(context.Background())
	if err != nil || next.TotalTokens != 2200 || next.APIEqPricedCalls != 2 || len(next.Sessions) != 1 || next.Sessions[0].TotalTokens != 2200 || next.Sessions[0].ModelSettings.Model != "gpt-6-sol" {
		t.Fatal("fresh replacement response was lost", next, err)
	}
}

func TestExactUsageOlderThanStartupTailCannotEnableLegacyBilling(t *testing.T) {
	for _, historicalExact := range []bool{false, true} {
		_, path, now := astraUsageReader(t, 100)
		u := BenchmarkUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
		if historicalExact {
			appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "old", u)+"\n")
		}
		// The last owned exact response, if present, is outside the discovery
		// window. An otherwise identical large legacy file is also uncertain.
		padding := `{"type":"response_item","payload":"` + strings.Repeat("x", 4096) + `"}` + "\n"
		appendRollout(t, path, strings.Repeat(padding, 1100))
		home := strings.Split(path, string(filepath.Separator)+"sessions"+string(filepath.Separator))[0]
		r, err := NewLiveUsageReader(home)
		if err != nil {
			t.Fatal(err)
		}
		r.statusProvider = nil
		if _, err := r.FetchTokenUsage(context.Background()); err != nil {
			t.Fatal(err)
		}
		checkpoint := richTokenCountLine(now, 1200, 1000, 0, 0, 100) + "\n"
		appendRollout(t, path, checkpoint+checkpoint)
		uncertain, err := r.FetchTokenUsage(context.Background())
		if err != nil || uncertain.TotalTokens != 0 || uncertain.APIEqPricedCalls != 0 || uncertain.APIEqUSD != 0 || uncertain.APIEqMissingUsageCalls != 1 || len(uncertain.APIEqDiagnostics) != 1 || !strings.Contains(uncertain.APIEqDiagnostics[0].Reason, "without establishing response usage mode") {
			t.Fatal("truncated baseline enabled legacy billing or hid uncertainty", historicalExact, uncertain, err)
		}
		appendRollout(t, path, richTokenCountLine(now, 2300, 1000, 0, 0, 100)+"\n")
		continued, err := r.FetchTokenUsage(context.Background())
		if err != nil || continued.APIEqMissingUsageCalls != 2 || continued.APIEqPricedCalls != 0 {
			t.Fatal("ongoing uncertain usage failed to invalidate quota learning", continued, err)
		}
		appendRollout(t, path, exactLine(t, now, "astra-accounting", "t", "fresh", u)+"\n"+richTokenCountLine(now, 3400, 1000, 0, 0, 100)+"\n")
		fresh, err := r.FetchTokenUsage(context.Background())
		if err != nil || fresh.APIEqPricedCalls != 1 || fresh.TotalTokens != 1100 || fresh.APIEqMissingUsageCalls != 2 || r.files[path].exactUsageUncertain {
			t.Fatal("exact record did not restore reliable accounting", fresh, err)
		}
	}
}

// Codex 0.163+ persists spending from incomplete Responses API requests even
// when the turn fails. These records must not depend on a successful turn or
// a matching (potentially recomputed) context counter.
func TestExactIncompleteResponseSpendingSurvivesFailedTurn(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	r.statusProvider = nil
	usage := BenchmarkUsage{InputTokens: 1000, CachedInputTokens: 200, OutputTokens: 100, ReasoningOutputTokens: 80, TotalTokens: 1100}
	response := exactLine(t, now, "astra-accounting", "failed-turn", "incomplete-response", usage) + "\n"
	appendRollout(t, path, response+`{"type":"event_msg","payload":{"type":"error","message":"Incomplete response returned, reason: max_output_tokens"}}`+"\n"+richTokenCountLine(now, 123456, 1000, 200, 0, 100)+"\n")
	failed, err := r.FetchTokenUsage(context.Background())
	want, _, _ := EstimateStandardAPIEqCost("gpt-6-astra", usage)
	if err != nil || failed.APIEqPricedCalls != 1 || failed.TotalTokens != 1100 || math.Abs(failed.APIEqUSD-want) > 1e-12 || failed.APIEqInconsistentUsageCalls != 0 {
		t.Fatal("failed response spending was dropped or tied to context", failed, err)
	}
	// A reconnect replay and a retained-reasoning context correction are not
	// additional spending, even though the context total moves backwards.
	appendRollout(t, path, response+richTokenCountLine(now, 120000, 1000, 200, 0, 100)+"\n")
	replayed, err := r.FetchTokenUsage(context.Background())
	if err != nil || replayed.TotalTokens != failed.TotalTokens || replayed.APIEqUSD != failed.APIEqUSD || replayed.APIEqPricedCalls != 1 || replayed.APIEqAccountingAdjustments != 0 {
		t.Fatal("failed response replay or context correction was billed twice", replayed, err)
	}
}

func TestExactIncompleteResponseWithoutUsageRemainsMissing(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	r.statusProvider = nil
	line := exactLine(t, now, "astra-accounting", "failed-turn", "incomplete-response", BenchmarkUsage{})
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatal(err)
	}
	record["payload"].(map[string]any)["usage"] = nil
	raw, _ := json.Marshal(record)
	appendRollout(t, path, string(raw)+"\n"+richTokenCountLine(now, 1200, 1000, 0, 0, 100)+"\n")
	got, err := r.FetchTokenUsage(context.Background())
	if err != nil || got.APIEqMissingUsageCalls != 1 || got.APIEqPricedCalls != 0 || got.APIEqUnpricedCalls != 0 || got.TotalTokens != 0 || len(got.APIEqDiagnostics) != 1 || got.APIEqDiagnostics[0].ResponseID != "incomplete-response" {
		t.Fatal("missing incomplete-response usage was guessed from context", got, err)
	}
}

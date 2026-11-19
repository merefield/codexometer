package codex

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGuardianPlanQuotaAccounting(t *testing.T) {
	const guardian = `{"subagent":{"other":"guardian"}}`
	const pro = `{"limit_id":"codex","plan_type":"pro"}`
	u := BenchmarkUsage{InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, TotalTokens: 110}
	for _, tc := range []struct {
		name, source, provider, limits string
		legacy, split, daemon          bool
		ordinal                        int
		otherTurn, mismatchedUsage     bool
		reroute                        string
		wantUnpriced, wantPriced       int64
	}{
		{name: "exact plan review", source: guardian, provider: "openai", limits: pro, ordinal: 11},
		{name: "split across polls", source: guardian, provider: "openai", limits: pro, ordinal: 11, split: true},
		{name: "legacy plan review", source: guardian, provider: "openai", limits: pro, legacy: true},
		{name: "shared daemon", source: guardian, provider: "openai", limits: pro, ordinal: 11, daemon: true},
		{name: "live reroute is priced", source: guardian, provider: "openai", limits: pro, ordinal: 11, daemon: true, reroute: "gpt-5.2", wantPriced: 1},
		{name: "model name alone", source: `"cli"`, provider: "openai", limits: pro, ordinal: 11, wantUnpriced: 1},
		{name: "custom provider", source: guardian, provider: "proxy", limits: pro, ordinal: 11, wantUnpriced: 1},
		{name: "missing limits", source: guardian, provider: "openai", limits: "null", ordinal: 11, wantUnpriced: 1},
		{name: "unknown plan", source: guardian, provider: "openai", limits: `{"limit_id":"codex","plan_type":"unknown"}`, ordinal: 11, wantUnpriced: 1},
		{name: "other limit", source: guardian, provider: "openai", limits: `{"limit_id":"other","plan_type":"pro"}`, ordinal: 11, wantUnpriced: 1},
		{name: "malformed limits keep tokens", source: guardian, provider: "openai", limits: `false`, legacy: true, wantUnpriced: 1},
		{name: "no checkpoint", source: guardian, provider: "openai", wantUnpriced: 1},
		{name: "not adjacent", source: guardian, provider: "openai", limits: pro, ordinal: 12, wantUnpriced: 1},
		{name: "different turn", source: guardian, provider: "openai", limits: pro, ordinal: 11, otherTurn: true, wantUnpriced: 1},
		{name: "different usage", source: guardian, provider: "openai", limits: pro, ordinal: 11, mismatchedUsage: true, wantUnpriced: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, home := time.Now().UTC(), t.TempDir()
			path := testRolloutPath(t, home, now.Add(-time.Hour), "review")
			meta := sessionMetaLine("review", tc.source, "/synthetic", nil)
			meta = strings.Replace(meta, `"source":`, fmt.Sprintf(`"model_provider":%q,"source":`, tc.provider), 1)
			writeRollout(t, path, meta+"\n")
			r, err := NewLiveUsageReader(home)
			if err != nil {
				t.Fatal(err)
			}
			r.statusProvider = nil
			status := stubSessionStatusProvider{subscribed: map[string]struct{}{"review": {}}}
			if tc.daemon {
				r.statusProvider = status
			}
			fetch := func() LiveUsageSnapshot {
				t.Helper()
				got, err := r.FetchTokenUsage(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			fetch()
			appendRollout(t, path, turnContextWithTurnIDLine(now, "turn", autoReviewModel)+"\n")
			if !tc.legacy {
				exact := exactLine(t, now, "review", "turn", "response", u)
				appendRollout(t, path, `{"ordinal":10,`+exact[1:]+"\n")
				if tc.split {
					if first := fetch(); first.TotalTokens != 110 || first.APIEqPendingCalls != 1 || first.APIEqUnpricedCalls != 0 {
						t.Fatal("split review was lost or finalised before its checkpoint", first)
					}
				}
			}
			if tc.limits != "" {
				cached := int64(20)
				if tc.otherTurn {
					appendRollout(t, path, turnContextWithTurnIDLine(now, "other", autoReviewModel)+"\n")
				}
				if tc.mismatchedUsage {
					cached--
				}
				appendRollout(t, path, guardianCheckpoint(now, tc.ordinal, 110, cached, tc.limits))
			}
			first := fetch()
			if tc.reroute != "" {
				status.observations = []resolvedModelObservation{{Sequence: 1, ThreadID: "review", TurnID: "turn", Model: tc.reroute, Usage: u, CumulativeTotal: 110}}
				r.statusProvider = status
			}
			got := fetch()
			if got.TotalTokens != 110 || got.APIEqPendingCalls != 0 || got.APIEqPricedCalls != tc.wantPriced ||
				got.APIEqUnpricedCalls != tc.wantUnpriced || got.APIEqMissingUsageCalls != 0 || got.APIEqInconsistentUsageCalls != 0 ||
				len(got.APIEqDiagnostics) != int(tc.wantUnpriced) || len(r.files[path].modelCalls) != 1 || r.files[path].observedTokens != 110 {
				t.Fatalf("unexpected accounting: %+v", got)
			}
			call := r.files[path].modelCalls[0]
			if tc.wantUnpriced != 0 && !strings.Contains(got.APIEqDiagnostics[0].Reason, "plan safety check not confirmed") {
				t.Fatal("diagnostic did not explain why the exemption was withheld", got.APIEqDiagnostics)
			}
			if !call.apiEqFinalized || call.OutputTokens != 10 || (got.APIEqUSD > 0) != (tc.wantPriced != 0) || call.APIEqKnown != (tc.wantPriced != 0) {
				t.Fatalf("lost telemetry or invented API price: %+v", call)
			}
			if tc.wantUnpriced == 0 && !tc.daemon && first.APIEqPendingCalls != 0 {
				t.Fatal("complete local evidence unnecessarily deferred quota learning")
			}
			// A subsequent paid response contributes its own price and tokens;
			// neither a review nor a replay should leak into its quota estimate.
			r.statusProvider = nil
			paid := exactLine(t, now, "review", "next", "paid", u) + "\n"
			appendRollout(t, path, turnContextWithTurnIDLine(now, "next", "gpt-5.2")+"\n"+paid+paid)
			mixed := fetch()
			price, known, _ := EstimateStandardAPIEqCost("gpt-5.2", u)
			if !known || mixed.TotalTokens != 220 || mixed.APIEqUSD != got.APIEqUSD+price ||
				mixed.APIEqPricedCalls != tc.wantPriced+1 || mixed.APIEqUnpricedCalls != tc.wantUnpriced {
				t.Fatal("paid call after safety check incorrectly accounted", mixed)
			}
		})
	}
	if _, known, _ := EstimateStandardAPIEqCost(autoReviewModel, u); known {
		t.Fatal("quota exemption must not publish a zero-dollar benchmark/API price")
	}
}

func guardianCheckpoint(now time.Time, ordinal int, total, cached int64, limits string) string {
	s := richTokenCountLine(now, total, 100, cached, 0, 10)
	s = strings.Replace(s, `"info":`, `"rate_limits":`+limits+`,"info":`, 1)
	s = strings.Replace(s, `"output_tokens":`, `"reasoning_output_tokens":0,"output_tokens":`, 1)
	return fmt.Sprintf(`{"ordinal":%d,`, ordinal) + s[1:] + "\n"
}

func TestReusedGuardianThreadAcrossReconnect(t *testing.T) {
	now, home := time.Now(), t.TempDir()
	path := testRolloutPath(t, home, now, "review")
	meta := sessionMetaLine("review", `{"subagent":{"other":"guardian"}}`, "/synthetic", nil)
	writeRollout(t, path, strings.Replace(meta, `"source":`, `"model_provider":"openai","source":`, 1)+"\n")
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	connected := stubSessionStatusProvider{subscribed: map[string]struct{}{"review": {}}}
	r.statusProvider = connected
	check := func(want int64) {
		t.Helper()
		s, err := r.FetchTokenUsage(context.Background())
		if err != nil || s.TotalTokens != want*110 || s.APIEqUSD != 0 || s.APIEqPricedCalls+s.APIEqUnpricedCalls+s.APIEqPendingCalls != 0 || int64(len(r.files[path].modelCalls)) != want {
			t.Fatalf("want %d distinct, accounted plan reviews: %+v %v", want, s, err)
		}
		for _, row := range s.Sessions {
			if row.Context.Text != "" {
				t.Fatal("private Guardian prose became a session reply", row.Context)
			}
		}
	}
	check(0)
	for i, turn := range []string{"first", "second"} {
		u := BenchmarkUsage{InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, TotalTokens: 110}
		exact := exactLine(t, now, "review", turn, "response-"+turn, u)
		ordinal := 10 + i*3
		record := fmt.Sprintf(`{"ordinal":%d,`, ordinal) + exact[1:] + "\n"
		checkpoint := guardianCheckpoint(now, ordinal+1, int64(i+1)*110, 20, `{"limit_id":"codex","plan_type":"pro"}`)
		appendRollout(t, path, turnContextWithTurnIDLine(now, turn, autoReviewModel)+"\n"+record+checkpoint+
			`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"private reviewer prose"}}`+"\n")
		r.statusProvider = nil
		check(int64(i + 1))
		r.statusProvider = connected
		appendRollout(t, path, record+checkpoint)
		check(int64(i + 1))
	}
}

package codex

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func contextFillLine(t *testing.T, at time.Time, window, delta int64) string {
	t.Helper()
	zero := func(total int64) map[string]any {
		return map[string]any{
			"input_tokens": 0, "cached_input_tokens": 0, "output_tokens": 0,
			"reasoning_output_tokens": 0, "total_tokens": total,
		}
	}
	raw, err := json.Marshal(map[string]any{"timestamp": at, "type": "event_msg", "payload": map[string]any{
		"type": "token_count", "info": map[string]any{"total_token_usage": zero(window), "last_token_usage": zero(delta), "model_context_window": window},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func astraUsageReader(t *testing.T, baseline int64) (*LiveUsageReader, string, time.Time) {
	t.Helper()
	home := t.TempDir()
	now := time.Now().UTC()
	path := testRolloutPath(t, home, now.Add(-time.Hour), "astra-accounting")
	writeRollout(t, path, sessionMetaLine("astra-accounting", `"cli"`, "/synthetic", nil)+"\n"+
		tierSettingsLine(now, `"fast"`)+"\n"+`{"type":"turn_context","payload":{"model":"gpt-6-astra","effort":"xhigh"}}`+"\n"+tokenCountLine(now, baseline)+"\n")
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r, path, now
}

func TestContextWindowFillRebasesWithoutResponseTokensOrPricing(t *testing.T) {
	r, path, now := astraUsageReader(t, 100)
	appendRollout(t, path, richTokenCountLine(now, 1200, 1000, 200, 0, 100)+"\n")
	before, err := r.FetchTokenUsage(context.Background())
	if err != nil || before.APIEqPricedCalls != 1 || before.APIEqUnpricedCalls != 0 || before.APIEqTierPremiumUSD != before.APIEqUSD {
		t.Fatal("normal Astra xhigh Fast failed", before, err)
	}
	fill := contextFillLine(t, now.Add(time.Second), 128000, 126800)
	appendRollout(t, path, fill+"\n")
	after, err := r.FetchTokenUsage(context.Background())
	if err != nil || after.TotalTokens != before.TotalTokens || after.Sessions[0].TotalTokens != before.Sessions[0].TotalTokens ||
		!after.LastActivity.Equal(before.LastActivity) || after.APIEqPricedCalls != 1 || after.APIEqUSD != before.APIEqUSD ||
		after.APIEqTierPremiumUSD != before.APIEqTierPremiumUSD || after.APIEqUnpricedCalls != 0 || after.APIEqMissingUsageCalls != 0 ||
		after.APIEqInconsistentUsageCalls != 0 || after.APIEqPendingCalls != 0 || after.APIEqAccountingAdjustments != 1 || len(after.Sessions[0].ModelCalls) != 1 {
		t.Fatalf("bookkeeping became usage, price error, activity or graph pulse: %+v %v", after, err)
	}
	// Repeated notification and polling must not adjust twice.
	appendRollout(t, path, fill+"\n")
	repeated, _ := r.FetchTokenUsage(context.Background())
	if repeated.APIEqAccountingAdjustments != 1 || repeated.TotalTokens != before.TotalTokens {
		t.Fatal("duplicate adjustment", repeated)
	}
	appendRollout(t, path, richTokenCountLine(now.Add(2*time.Second), 129100, 1000, 200, 0, 100)+"\n")
	resumed, err := r.FetchTokenUsage(context.Background())
	if err != nil || resumed.TotalTokens != 2200 || resumed.APIEqPricedCalls != 2 || resumed.APIEqUnpricedCalls != 0 || resumed.APIEqMissingUsageCalls != 0 ||
		resumed.APIEqInconsistentUsageCalls != 0 || resumed.APIEqAccountingAdjustments != 1 || len(resumed.Sessions[0].ModelCalls) != 2 ||
		math.Abs(resumed.APIEqUSD-2*before.APIEqUSD) > 1e-12 || resumed.APIEqTierPremiumUSD != resumed.APIEqUSD {
		t.Fatal("normal response after rebase lost/double counted", resumed, err)
	}
}

func TestContextWindowFillCanReduceCounterWithoutLosingNextResponse(t *testing.T) {
	r, path, now := astraUsageReader(t, 200000)
	appendRollout(t, path, contextFillLine(t, now, 128000, 0)+"\n"+richTokenCountLine(now, 129100, 1000, 0, 0, 100)+"\n")
	usage, err := r.FetchTokenUsage(context.Background())
	if err != nil || usage.TotalTokens != 1100 || usage.APIEqAccountingAdjustments != 1 || usage.APIEqPricedCalls != 1 || usage.APIEqUnpricedCalls != 0 {
		t.Fatal("downward fill lost next response", usage, err)
	}
}

func TestContextWindowFillRequiresExactExplicitBreakdowns(t *testing.T) {
	for _, kind := range []string{"missing-window", "different-window", "missing-input", "missing-output", "missing-cached", "missing-reasoning", "missing-last-total", "nonzero-input", "nonzero-total-cache", "wrong-delta"} {
		t.Run(kind, func(t *testing.T) {
			r, path, now := astraUsageReader(t, 100)
			var event map[string]any
			if err := json.Unmarshal([]byte(contextFillLine(t, now, 128000, 127900)), &event); err != nil {
				t.Fatal(err)
			}
			info := event["payload"].(map[string]any)["info"].(map[string]any)
			total, last := info["total_token_usage"].(map[string]any), info["last_token_usage"].(map[string]any)
			switch kind {
			case "missing-window":
				delete(info, "model_context_window")
			case "different-window":
				info["model_context_window"] = 256000
			case "missing-input":
				delete(last, "input_tokens")
			case "missing-output":
				delete(total, "output_tokens")
			case "missing-cached":
				delete(last, "cached_input_tokens")
			case "missing-reasoning":
				delete(total, "reasoning_output_tokens")
			case "missing-last-total":
				delete(last, "total_tokens")
			case "nonzero-input":
				last["input_tokens"] = 10
			case "nonzero-total-cache":
				total["cached_input_tokens"] = 1
			case "wrong-delta":
				last["total_tokens"] = 127000
			}
			raw, _ := json.Marshal(event)
			appendRollout(t, path, string(raw)+"\n")
			usage, err := r.FetchTokenUsage(context.Background())
			if err != nil || usage.APIEqAccountingAdjustments != 0 || usage.APIEqPricedCalls != 0 || usage.APIEqUnpricedCalls != 0 ||
				usage.APIEqMissingUsageCalls+usage.APIEqInconsistentUsageCalls != 1 || usage.TotalTokens != 127900 {
				t.Fatal("incomplete/ambiguous record silently discarded", usage, err)
			}
		})
	}
}

func TestResponseUsageProblemsAreSeparateFromUnknownPrices(t *testing.T) {
	for _, kind := range []string{"missing-input", "missing-output", "cumulative-mismatch", "invalid-breakdown", "unknown-model", "unsupported-tier"} {
		t.Run(kind, func(t *testing.T) {
			r, path, now := astraUsageReader(t, 100)
			event := richTokenCountLine(now, 1200, 1000, 0, 0, 100)
			switch kind {
			case "missing-input":
				event = strings.Replace(event, `"input_tokens":1000,`, "", 1)
			case "missing-output":
				event = strings.Replace(event, `"output_tokens":100,`, "", 1)
			case "cumulative-mismatch":
				event = richTokenCountLine(now, 5000, 1000, 0, 0, 100)
			case "invalid-breakdown":
				event = richTokenCountLine(now, 1200, 1000, 1001, 0, 100)
			case "unknown-model":
				event = turnContextLine(now, "gpt-6-astra-preview") + "\n" + event
			case "unsupported-tier":
				event = tierSettingsLine(now, `"flex"`) + "\n" + turnContextLine(now, "gpt-6-astra") + "\n" + event
			}
			appendRollout(t, path, event+"\n")
			usage, err := r.FetchTokenUsage(context.Background())
			if err != nil || usage.APIEqPricedCalls != 0 || usage.APIEqUSD != 0 || usage.APIEqTierPremiumUSD != 0 || usage.APIEqPendingCalls != 0 || usage.APIEqAccountingAdjustments != 0 {
				t.Fatal("unsafe cost or wrong accounting", usage, err)
			}
			missing, inconsistent, unpriced := int64(0), int64(0), int64(0)
			switch kind {
			case "missing-input", "missing-output":
				missing = 1
			case "cumulative-mismatch", "invalid-breakdown":
				inconsistent = 1
			default:
				unpriced = 1
			}
			if usage.APIEqMissingUsageCalls != missing || usage.APIEqInconsistentUsageCalls != inconsistent || usage.APIEqUnpricedCalls != unpriced ||
				len(usage.Sessions[0].ModelCalls) != 1 || usage.Sessions[0].ModelCalls[0].APIEqIssue == "" {
				t.Fatalf("cause lost: %+v", usage)
			}
			again, _ := r.FetchTokenUsage(context.Background())
			if again.APIEqMissingUsageCalls != missing || again.APIEqInconsistentUsageCalls != inconsistent || again.APIEqUnpricedCalls != unpriced {
				t.Fatal("double counted", again)
			}
		})
	}
}

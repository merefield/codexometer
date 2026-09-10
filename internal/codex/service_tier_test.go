package codex

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestRequestedTierPremium(t *testing.T) {
	for _, test := range []struct {
		model, tier string
		premium     float64
		known       bool
	}{
		{"gpt-6-astra", "fast", 3, true},
		{"gpt-5.6-sol", "priority", 3, true},
		{"gpt-5.6-terra", "fast", 3, true},
		{"gpt-5.6-luna", "fast", 3, true},
		{" GPT-6-ASTRA-2026-09-01 ", " PRIORITY ", 3, true},
		{"gpt-6-astra", "default", 0, true},
		{"gpt-6-astra", "", 0, true},
		{"gpt-6-astra", "flex", 0, false},
		{"gpt-6-astra-preview", "fast", 0, false},
		{"gpt-5.4", "fast", 0, false},
		{"unknown", "fast", 0, false},
	} {
		t.Run(test.model+test.tier, func(t *testing.T) {
			premium, known := requestedTierPremium(test.model, test.tier, 3)
			if premium != test.premium || known != test.known {
				t.Fatalf("got %v, %v", premium, known)
			}
		})
	}
}

func TestFastPricingPreservesTokenClassesAndLongContext(t *testing.T) {
	for _, input := range []int64{272000, 272001} {
		r := &LiveUsageReader{}
		call := LiveModelCall{Model: "gpt-6-astra", RequestedServiceTier: "fast"}
		usage := BenchmarkUsage{InputTokens: input, CachedInputTokens: 1000, CacheWriteInputTokens: 2000, OutputTokens: 100, ReasoningOutputTokens: 50, TotalTokens: input + 100}
		r.finalizeCallPricing(&call, usage)
		in, cached, write, out := 10.0, 1.0, 12.5, 50.0
		if input > 272000 {
			in, cached, write, out = 20, 2, 25, 75
		}
		standard := (float64(input-3000)*in + 1000*cached + 2000*write + 100*out) / 1e6
		if !call.APIEqKnown || math.Abs(r.apiEqUSD-standard) > 1e-12 || math.Abs(r.apiEqTierPremiumUSD-standard) > 1e-12 {
			t.Fatalf("input %d: %+v", input, call)
		}
	}
}

func tierSettingsLine(at time.Time, tier string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-6-astra","service_tier":%s}}}`, at.Format(time.RFC3339Nano), tier)
}

func TestRolloutTierRecord(t *testing.T) {
	now := time.Now()
	for _, tier := range []string{`"fast"`, `null`, `""`} {
		got, _, _, ok := rolloutTierRecord([]byte(tierSettingsLine(now, tier)))
		want := "default"
		if tier == `"fast"` {
			want = "fast"
		}
		if !ok || got != want {
			t.Fatalf("%s: %s %v", tier, got, ok)
		}
	}
	for _, line := range []string{`{`, `{"type":"event_msg","payload":{"type":"thread_settings_applied"}}`, `{"type":"response_item","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-6-astra","service_tier":"fast"}}}`} {
		if _, _, _, ok := rolloutTierRecord([]byte(line)); ok {
			t.Fatalf("accepted %s", line)
		}
	}
}

func TestLiveTierChangesAndStartup(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := testRolloutPath(t, home, now.Add(-time.Hour), "tier")
	writeRollout(t, path, sessionMetaLine("tier", `"cli"`, "/work/tier", nil)+"\n"+
		tierSettingsLine(now, `"fast"`)+"\n"+turnContextLine(now, "gpt-6-astra")+"\n"+
		tokenCountLine(now, 100)+"\n"+
		// A setting changed during the current turn must only price the next
		// turn; startup must recover both the active and pending settings.
		tierSettingsLine(now, `null`)+"\n")
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path, richTokenCountLine(now, 1200, 1000, 200, 100, 100)+"\n")
	first, err := r.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.APIEqTierPremiumUSD <= 0 || first.APIEqTierPremiumUSD != first.APIEqUSD || first.APIEqUnknownTierCalls != 0 {
		t.Fatalf("fast %+v", first)
	}
	appendRollout(t, path, turnContextLine(now, "gpt-6-astra")+"\n"+richTokenCountLine(now, 2300, 1000, 200, 100, 100)+"\n")
	second, err := r.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.APIEqTierPremiumUSD != first.APIEqTierPremiumUSD || second.APIEqUSD != 2*first.APIEqUSD || second.APIEqPricedCalls != 2 {
		t.Fatalf("default %+v", second)
	}
	// Repeated polling is idempotent.
	third, err := r.FetchTokenUsage(context.Background())
	if err != nil || third.APIEqTierPremiumUSD != second.APIEqTierPremiumUSD {
		t.Fatalf("repeat %+v %v", third, err)
	}
}

func TestPendingResolutionRetainsCallTier(t *testing.T) {
	u := BenchmarkUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}
	r := &LiveUsageReader{files: map[string]*rolloutCursor{"a": {modelCalls: []LiveModelCall{{Sequence: 1, Model: "gpt-5.6-sol", RequestedServiceTier: "fast"}}}},
		pendingModelResolutions: []pendingModelResolution{{callSequence: 1, threadID: "a", turnID: "t", usage: u, cumulativeTotal: 110}},
		resolvedObservations:    []resolvedModelObservation{{ThreadID: "a", TurnID: "t", Model: "gpt-6-astra", Usage: u, CumulativeTotal: 110}}}
	r.reconcilePendingModelResolutions()
	if r.apiEqTierPremiumUSD <= 0 || r.apiEqTierPremiumUSD != r.apiEqUSD || r.apiEqPricedCalls != 1 {
		t.Fatalf("reader %+v", r)
	}
	r.reconcilePendingModelResolutions()
	if r.apiEqPricedCalls != 1 {
		t.Fatal("double counted")
	}
}

func TestTierBootstrapLookbackAndOwnership(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name    string
		padding int
		child   bool
		want    string
	}{
		{"chunk boundary", 70000, false, "fast"},
		{"bounded startup", 5 * 1024 * 1024, false, ""},
		{"inherited settings", 0, true, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := testRolloutPath(t, t.TempDir(), now, "bootstrap")
			// A child's copied legacy history precedes its own creation.
			writeRollout(t, path, tierSettingsLine(now.Add(-time.Hour), `"fast"`)+"\n"+
				strings.Repeat("{}\n", test.padding/3)+turnContextLine(now, "gpt-6-astra")+"\n")
			cursor := &rolloutCursor{nonRoot: test.child, startedAt: now}
			model, err := latestRolloutModel(path, cursor)
			if err != nil || model != "gpt-6-astra" || cursor.currentServiceTier != test.want {
				t.Fatalf("%s %+v %v", model, cursor, err)
			}
		})
	}
}

func TestTierAccountingIsPerSession(t *testing.T) {
	now := time.Now()
	home := t.TempDir()
	paths := make(map[string]string)
	for _, id := range []string{"fast", "standard"} {
		paths[id] = testRolloutPath(t, home, now.Add(-time.Hour), id)
		writeRollout(t, paths[id], sessionMetaLine(id, `"cli"`, "/work/"+id, nil)+"\n")
	}
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	for id, path := range paths {
		tier := `null`
		if id == "fast" {
			tier = `"fast"`
		}
		appendRollout(t, path, tierSettingsLine(now, tier)+"\n"+turnContextLine(now, "gpt-6-astra")+"\n"+richTokenCountLine(now, 1100, 1000, 200, 100, 100)+"\n")
	}
	u, err := r.FetchTokenUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.APIEqPricedCalls != 2 || u.APIEqUSD != 2*u.APIEqTierPremiumUSD {
		t.Fatalf("%+v", u)
	}
	for _, s := range u.Sessions {
		if len(s.ModelCalls) != 1 {
			t.Fatalf("%+v", s)
		}
		if (s.ModelCalls[0].APIEqTierPremiumUSD > 0) != (s.ID == "fast") {
			t.Fatalf("tier leaked: %+v", s)
		}
	}
}

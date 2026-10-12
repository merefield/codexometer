package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// FastAPIPricingRetrievedOn is separate from the standard-price review date.
// Sources: https://developers.openai.com/api/docs/pricing and
// https://learn.chatgpt.com/docs/agent-configuration/speed (GPT-5.6 API rates).
const FastAPIPricingRetrievedOn = "2026-10-12"

// requestedTierPremium prices a single response's requested tier. It does not
// claim that the backend delivered/billed that tier. Empty means unavailable,
// not confirmed standard. Unknown explicit tiers fail closed.
func requestedTierPremium(model, tier string, standard float64) (float64, bool) {
	multiplier, known := requestedTierMultiplier(model, tier)
	return standard * (multiplier - 1), known
}

// Only published model/tier combinations are maintained. The adjustment may
// be negative for Flex; Standard totals remain independently monotonic.
func requestedTierMultiplier(model, tier string) (float64, bool) {
	name := pricedModelName(model)
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "", "default", "standard":
		return 1, true
	case "fast", "priority":
		switch name {
		case "gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.2", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex":
			return 2, true
		case "gpt-5.5":
			return 2.5, true
		}
	case "ultrafast":
		if name == "gpt-6-astra" || name == "gpt-6.1-sol" {
			return 6, true
		}
	case "flex":
		switch name {
		case "gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.2", "gpt-5.5":
			return 0.5, true
		}
	}
	return 1, false
}

// Codex persists settings separately from turn_context. A complete settings
// snapshot with an omitted/null tier means the default; no snapshot means
// unknown. Never consult today's config to price yesterday's responses.
func rolloutTierRecord(line []byte) (string, *uint64, time.Time, bool) {
	if !bytes.Contains(line, []byte(`"thread_settings_applied"`)) {
		return "", nil, time.Time{}, false
	}
	var event struct {
		Timestamp time.Time `json:"timestamp"`
		Ordinal   *uint64   `json:"ordinal"`
		Type      string    `json:"type"`
		Payload   struct {
			Type     string `json:"type"`
			Settings *struct {
				Model string `json:"model"`
				Tier  string `json:"service_tier"`
			} `json:"thread_settings"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &event) != nil || event.Type != "event_msg" ||
		event.Payload.Type != "thread_settings_applied" || event.Payload.Settings == nil || event.Payload.Settings.Model == "" {
		return "", nil, time.Time{}, false
	}
	tier := strings.ToLower(strings.TrimSpace(event.Payload.Settings.Tier))
	if tier == "" {
		tier = "default"
	}
	return tier, event.Ordinal, event.Timestamp, true
}

func (r *LiveUsageReader) finalizeCallPricing(call *LiveModelCall, usage BenchmarkUsage) {
	if issue := validateBenchmarkUsage(usage); issue != "" {
		call.issueKind = UsageIssueInconsistent
		call.APIEqIssue = issue
		call.apiEqFinalized = true
		r.apiEqInconsistentUsageCalls++
		return
	}
	call.APIEqUSD, call.APIEqKnown, call.APIEqIssue = EstimateStandardAPIEqCost(call.Model, usage)
	if call.APIEqKnown && usage.InputTokens > 272_000 &&
		(strings.EqualFold(strings.TrimSpace(call.RequestedServiceTier), "fast") || strings.EqualFold(strings.TrimSpace(call.RequestedServiceTier), "priority")) &&
		(pricedModelName(call.Model) == "gpt-5.4" || pricedModelName(call.Model) == "gpt-5.5") {
		call.APIEqKnown = false
		call.APIEqIssue = "no maintained long-context Fast price for model " + call.Model
	}
	if call.APIEqKnown {
		call.APIEqTierPremiumUSD, call.APIEqKnown = requestedTierPremium(call.Model, call.RequestedServiceTier, call.APIEqUSD)
		if !call.APIEqKnown {
			call.APIEqIssue = "unavailable pricing for requested service tier"
		}
	}
	call.apiEqFinalized = true
	if call.APIEqKnown {
		r.apiEqUSD += call.APIEqUSD
		r.apiEqTierPremiumUSD += call.APIEqTierPremiumUSD
		r.apiEqPricedCalls++
		if call.RequestedServiceTier == "" {
			r.apiEqUnknownTierCalls++
		}
	} else {
		r.apiEqUnknownCalls++
	}
}

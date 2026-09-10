package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// FastAPIPricingRetrievedOn is separate from the standard-price review date.
// Sources: https://developers.openai.com/api/docs/models/gpt-6-astra and
// https://learn.chatgpt.com/docs/agent-configuration/speed (GPT-5.6 API rates).
const FastAPIPricingRetrievedOn = "2026-09-10"

// requestedTierPremium prices a single response's requested tier. It does not
// claim that the backend delivered/billed that tier. Empty means unavailable,
// not confirmed standard. Unknown explicit tiers fail closed.
func requestedTierPremium(model, tier string, standard float64) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "", "default", "standard":
		return 0, true
	case "fast", "priority":
		// Only apply verified model-specific rates, never subscription credit
		// multipliers. Astra / GPT-5.6 Fast is 2x applicable rates, including long
		// context and both cache classes. Dated snapshots share base pricing.
		switch pricedModelName(model) {
		case "gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna":
			return standard, true
		default:
			return 0, false
		}
	default:
		return 0, false
	}
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
	call.APIEqUSD, call.APIEqKnown, _ = EstimateStandardAPIEqCost(call.Model, usage)
	if call.APIEqKnown {
		call.APIEqTierPremiumUSD, call.APIEqKnown = requestedTierPremium(call.Model, call.RequestedServiceTier, call.APIEqUSD)
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

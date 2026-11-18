package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// FastAPIPricingRetrievedOn is separate from the standard-price review date.
// Source: https://developers.openai.com/api/docs/pricing (Fast, Flex and Ultrafast).
const FastAPIPricingRetrievedOn = "2026-11-17"

const autoReviewModel = "codex-auto-review"

func (c *rolloutCursor) planReviewCandidate() bool {
	return c.guardianReviewer && c.openAIProvider && c.currentModel == autoReviewModel
}

// Known PlanType wire values from Codex. An unknown or absent plan does not
// establish ChatGPT billing; neither does the user's current login.
func reportsChatGPTQuota(line []byte) bool {
	// Decode optional evidence separately: malformed limits shouldn't destroy
	// an otherwise valid token record. Ordinary model calls don't pay this cost.
	var event struct {
		Payload struct {
			Limits struct {
				ID   string `json:"limit_id"`
				Plan string `json:"plan_type"`
			} `json:"rate_limits"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &event) != nil || event.Payload.Limits.ID != "codex" {
		return false
	}
	switch event.Payload.Limits.Plan {
	case "free", "go", "plus", "pro", "prolite", "promax", "team", "self_serve_business_prolite",
		"self_serve_business_usage_based", "business", "ent26", "enterprise_cbp_automation",
		"enterprise_cbp_usage_based", "enterprise", "edu", "edu_plus", "edu_pro", "sci", "sci_plus":
		return true
	}
	return false
}

// requestedTierPremium prices a single response's requested tier. It does not
// claim that the backend delivered/billed that tier. Empty means unavailable,
// not confirmed standard. Unknown explicit tiers fail closed.
func requestedTierPremium(model, tier string, standard float64, at time.Time) (float64, bool) {
	multiplier, known := requestedTierMultiplier(model, tier, at)
	return standard * (multiplier - 1), known
}

// Only published model/tier combinations are maintained. The adjustment may
// be negative for Flex; Standard totals remain independently monotonic.
func requestedTierMultiplier(model, tier string, at time.Time) (float64, bool) {
	name := pricedModelName(model)
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "", "default", "standard":
		return 1, true
	case "fast", "priority":
		switch name {
		case "gpt-6.2-astra", "gpt-6.2-sol", "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.2", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex":
			return 2, true
		case "gpt-5.5":
			return 2.5, true
		}
	case "ultrafast":
		if name == "gpt-6.2-astra" || name == "gpt-6.2-sol" {
			// The promotion ends at midnight March 1, 2027 Pacific (PST).
			// Price historical responses by their own timestamp, not startup time.
			if at.IsZero() {
				return 1, false
			}
			if at.Before(time.Date(2027, time.March, 1, 8, 0, 0, 0, time.UTC)) {
				return 3, true
			}
			return 6, true
		}
		if name == "gpt-6-astra" || name == "gpt-6.1-sol" {
			return 6, true
		}
	case "flex":
		switch name {
		case "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.2", "gpt-5.5":
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
	if call.planReviewEvidence && call.Model == autoReviewModel {
		// ChatGPT plan safety checks do not consume quota. The session pulse
		// and tokens are real, but this is neither a priced API call nor a gap.
		call.apiEqFinalized = true
		return
	}
	call.APIEqUSD, call.APIEqKnown, call.APIEqIssue = EstimateStandardAPIEqCost(call.Model, usage)
	if !call.APIEqKnown && call.Model == autoReviewModel {
		call.APIEqIssue = "no maintained API price for codex-auto-review; ChatGPT plan safety check not confirmed"
	}
	if call.APIEqKnown && usage.InputTokens > 272_000 &&
		(strings.EqualFold(strings.TrimSpace(call.RequestedServiceTier), "fast") || strings.EqualFold(strings.TrimSpace(call.RequestedServiceTier), "priority")) &&
		(pricedModelName(call.Model) == "gpt-5.4" || pricedModelName(call.Model) == "gpt-5.5") {
		call.APIEqKnown = false
		call.APIEqIssue = "no maintained long-context Fast price for model " + call.Model
	}
	if call.APIEqKnown {
		at := call.At
		if call.timestampEstimated {
			at = time.Time{}
		}
		call.APIEqTierPremiumUSD, call.APIEqKnown = requestedTierPremium(call.Model, call.RequestedServiceTier, call.APIEqUSD, at)
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

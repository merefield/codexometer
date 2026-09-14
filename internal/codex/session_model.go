package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// SessionModelSettings describes the root's latest locally observed selection,
// not a guarantee of the backend-resolved model or delivered service tier.
type SessionModelSettings struct {
	Model           string
	ReasoningEffort string
	ServiceTier     string
}

func rolloutSessionSettingsRecord(line []byte, previous SessionModelSettings) (SessionModelSettings, *uint64, time.Time, bool) {
	if !bytes.Contains(line, []byte(`"turn_context"`)) && !bytes.Contains(line, []byte(`"thread_settings_applied"`)) {
		return previous, nil, time.Time{}, false
	}
	var event struct {
		Type      string    `json:"type"`
		Ordinal   *uint64   `json:"ordinal"`
		Timestamp time.Time `json:"timestamp"`
		Payload   struct {
			Type     string `json:"type"`
			Model    string `json:"model"`
			Effort   string `json:"effort"`
			Settings *struct {
				Model  string `json:"model"`
				Effort string `json:"reasoning_effort"`
				Tier   string `json:"service_tier"`
			} `json:"thread_settings"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &event) != nil {
		return previous, nil, time.Time{}, false
	}
	settings := previous
	switch {
	case event.Type == "turn_context":
		settings.Model, settings.ReasoningEffort = event.Payload.Model, event.Payload.Effort
	case event.Type == "event_msg" && event.Payload.Type == "thread_settings_applied" && event.Payload.Settings != nil:
		s := event.Payload.Settings
		settings = SessionModelSettings{Model: s.Model, ReasoningEffort: s.Effort, ServiceTier: strings.ToLower(strings.TrimSpace(s.Tier))}
		if settings.ServiceTier == "" {
			settings.ServiceTier = "default"
		}
	default:
		return previous, nil, time.Time{}, false
	}
	settings.Model = strings.TrimSpace(settings.Model)
	settings.ReasoningEffort = strings.TrimSpace(settings.ReasoningEffort)
	return settings, event.Ordinal, event.Timestamp, settings.Model != ""
}

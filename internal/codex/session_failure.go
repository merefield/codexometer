package codex

import (
	"encoding/json"
	"strings"
)

// Failure labels are shared by both interfaces; backend messages remain bounded,
// sanitised observations, never commands or permission to retry a write.
func SessionFailureLabel(c SessionContext) string {
	switch normalizedFailureCode(c.FailureCode) {
	case "serveroverloaded":
		return "MODEL BUSY"
	case "modelnotfound":
		return "MODEL UNAVAILABLE"
	case "policycapacityunavailable":
		return "CAPACITY RESTRICTED"
	}
	return "TURN FAILED"
}

func normalizedFailureCode(code string) string {
	return strings.ToLower(strings.ReplaceAll(code, "_", ""))
}

func failureAffectsTurn(c SessionContext) bool {
	switch normalizedFailureCode(c.FailureCode) {
	case "activeturnnotsteerable", "threadrollbackfailed":
		return false
	}
	return true
}

func sessionFailureContext(c SessionContext, raw json.RawMessage) SessionContext {
	var e struct {
		Message    string          `json:"message"`
		Code       json.RawMessage `json:"codexErrorInfo"`
		LegacyCode json.RawMessage `json:"codex_error_info"`
		Details    string          `json:"additionalDetails"`
	}
	_ = json.Unmarshal(raw, &e)
	code := e.Code
	if len(code) == 0 {
		code = e.LegacyCode
	}
	var name string
	if json.Unmarshal(code, &name) != nil {
		var variant map[string]json.RawMessage
		if json.Unmarshal(code, &variant) == nil && len(variant) == 1 {
			for key := range variant {
				name = key
			}
		}
	}
	switch normalizedFailureCode(name) {
	case "serveroverloaded":
		name = "serverOverloaded"
	case "modelnotfound":
		name = "modelNotFound"
	case "policycapacityunavailable":
		name = "policyCapacityUnavailable"
	}
	c.Kind, c.Streaming, c.Activity = SessionContextFailure, false, SessionActivity{}
	c.FailureCode = SanitizeSessionContext(name)
	c.Text = SanitizeSessionContext(strings.TrimSpace(e.Message + "\n" + e.Details))
	if c.Text == "" {
		c.Text = "Codex reported a failed turn."
	}
	return c
}

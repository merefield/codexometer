package codex

import (
	"strings"
	"time"
)

type APIEqIssueKind string

const (
	UsageIssueUnpriced     APIEqIssueKind = "UNPRICED USAGE"
	UsageIssueMissing      APIEqIssueKind = "MISSING RESPONSE USAGE"
	UsageIssueInconsistent APIEqIssueKind = "INCONSISTENT RESPONSE USAGE"
)

// A bounded, process-local diagnostic contains accounting metadata only.
// Counter belongs to Kind; it prevents old failures attaching to new intervals.
type APIEqDiagnostic struct {
	Kind       APIEqIssueKind
	ObservedAt time.Time
	At         time.Time
	Counter    int64
	SessionID  string
	Model      string
	ModelBasis string
	Tier       string
	Effort     string
	Source     string
	ResponseID string
	Reason     string
}

func (r *LiveUsageReader) rememberCallIssue(call LiveModelCall) {
	kind := call.issueKind
	if kind == "" {
		kind = UsageIssueUnpriced
	}
	counter := r.apiEqUnknownCalls
	if kind == UsageIssueMissing {
		counter = r.apiEqMissingUsageCalls
	}
	if kind == UsageIssueInconsistent {
		counter = r.apiEqInconsistentUsageCalls
	}
	r.apiEqDiagnostics = appendBounded(r.apiEqDiagnostics, APIEqDiagnostic{
		Kind: kind, Counter: counter, ObservedAt: time.Now(), At: call.At, SessionID: call.SessionID, Model: call.Model, ModelBasis: call.ModelBasis, Tier: call.RequestedServiceTier,
		Effort: call.Effort, Source: call.UsageSource, ResponseID: call.ResponseID, Reason: call.APIEqIssue}, 32)
}

// DiagnosticText is safe for terminal output and browser text. Preserve model
// and tier identifiers without retaining request content or arbitrary errors.
func DiagnosticText(issue APIEqDiagnostic) string {
	field := func(value string, limit int) string {
		value = strings.Join(strings.Fields(SanitizeSessionContext(value)), " ")
		runes := []rune(value)
		if len(runes) > limit {
			value = string(runes[:limit]) + "…"
		}
		if value == "" {
			return "unknown"
		}
		return value
	}
	basis := issue.ModelBasis
	if basis == "" {
		basis = "requested"
	}
	result := string(issue.Kind) + " // " + field(issue.Reason, 180) + "\n" +
		"MODEL " + field(issue.Model, 96) + " (" + field(basis, 32) + ") // TIER " + field(issue.Tier, 32) + " // EFFORT " + field(issue.Effort, 16) + "\n" +
		"SESSION " + field(issue.SessionID, 12) + " // SOURCE " + field(issue.Source, 48)
	if issue.ResponseID != "" {
		result += " // RESPONSE " + field(issue.ResponseID, 16)
	}
	if !issue.At.IsZero() {
		result += " // AT " + issue.At.UTC().Format(time.RFC3339)
	}
	return result
}

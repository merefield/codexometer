package web

import (
	"github.com/merefield/codexometer/internal/codex"
	"strings"
	"testing"
	"time"
)

func TestUsageDiagnosticProjectionIsBoundedSafeAndChoosesNewest(t *testing.T) {
	s := newStore()
	now := time.Now()
	newer := codex.APIEqDiagnostic{Kind: codex.UsageIssueUnpriced, ObservedAt: now, Model: "gpt-reserve", Tier: "fast", Effort: "xhigh", SessionID: "child", Source: "exact response", Reason: "\x1b[31mprice missing <script>alert(1)</script>\x1b[0m"}
	older := codex.APIEqDiagnostic{Kind: codex.UsageIssueMissing, ObservedAt: now.Add(-time.Hour), Model: "old-model", Reason: "old failure"}
	s.live(codex.LiveUsageSnapshot{APIEqDiagnostics: []codex.APIEqDiagnostic{newer, older}}, nil, now)
	data, _ := s.snapshot()
	text := string(data)
	if strings.Contains(text, "old-model") || strings.Contains(text, `\u001b`) || !strings.Contains(text, "child") || !strings.Contains(text, "gpt-reserve") || !strings.Contains(text, "xhigh") {
		t.Fatal("unsafe or stale projection", text)
	}
	s.live(codex.LiveUsageSnapshot{}, nil, now)
	if s.state.UsageDiagnostic != "" {
		t.Fatal("new source retained an old diagnostic")
	}
}

func TestUsageCoverageDiagnosticOverridesStaleResponseFailures(t *testing.T) {
	s := newStore()
	now := time.Now()
	s.live(codex.LiveUsageSnapshot{APIEqCoverageIssue: "SESSION child: background compaction usage is unobserved", APIEqDiagnostics: []codex.APIEqDiagnostic{{Kind: codex.UsageIssueUnpriced, Reason: "old failure"}}}, nil, now)
	if !strings.Contains(s.state.UsageDiagnostic, "INCOMPLETE COVERAGE") || !strings.Contains(s.state.UsageDiagnostic, "child") || strings.Contains(s.state.UsageDiagnostic, "old failure") {
		t.Fatal(s.state.UsageDiagnostic)
	}
	s.live(codex.LiveUsageSnapshot{}, nil, now)
	if s.state.UsageDiagnostic != "" {
		t.Fatal("coverage diagnostic did not clear")
	}
}

package ui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
)

func TestProfileResultAfterThresholdChangeRescans(t *testing.T) {
	m, f := profileTestModel(t)
	m, _ = m.pressQuotaSession("one", false)
	m, apply := m.pressQuotaSession("one", true)
	m.snapshot.RateLimits.Secondary.UsedPercent = 96
	if cmd := m.evaluateQuotaStep(m.snapshot); cmd != nil {
		t.Fatal("scan during apply")
	}
	next, scan := m.Update(apply())
	m = next.(Model)
	if scan == nil || len(m.quota.handled) != 0 || len(m.quota.sessions) != 0 {
		t.Fatal("old result accepted under new threshold")
	}
	next, _ = m.Update(scan())
	m = next.(Model)
	for _, s := range m.quotaCandidates() {
		if s.ID == "one" && (s.Model != "small" || s.Effort != "medium") {
			t.Fatal("stale current profile")
		}
	}
	if f.updates != 1 {
		t.Fatal("unexpected retry")
	}
}

func TestProfileFailureNoticeDistinguishesAcceptance(t *testing.T) {
	for _, err := range []error{errors.New("settings changed since review; skipped"), codex.ErrQuotaProfileUnverified} {
		m, _ := profileTestModel(t)
		next, _ := m.Update(quotaStepResult{revision: m.quota.revision, step: *m.quotaStepPending, window: m.quotaStepWindow, targets: m.quotaCandidates()[:1], err: err})
		notice := next.(Model).quota.notices["one"]
		if strings.Contains(notice, "Codex accepted") != errors.Is(err, codex.ErrQuotaProfileUnverified) {
			t.Fatalf("wrong notice: %s", notice)
		}
		if !errors.Is(err, codex.ErrQuotaProfileUnverified) && !strings.Contains(notice, err.Error()) {
			t.Fatal("lost rejection cause")
		}
	}
}

func TestProfileNoticeWithoutContextWraps(t *testing.T) {
	m, _ := profileTestModel(t)
	m.quotaStepPending = nil
	m.monitorSessionData[0].preview = codex.SessionContext{}
	before := m.contextDetailDocument(15)
	if before[len(before)-1].kind == "warning" {
		t.Fatal("empty warning row")
	}
	m.setQuotaSessionNotice("one", strings.Repeat("long notice ", 12))
	after := m.contextDetailDocument(15)
	if len(after) <= len(before)+1 {
		t.Fatal("notice not wrapped")
	}
	for _, line := range after {
		if lipgloss.Width(line.text) > 15 {
			t.Fatal("notice overflow")
		}
	}
}

func TestProfileChoiceClearsOnResolutionAndThresholdChange(t *testing.T) {
	for _, event := range []string{"skip", "apply", "threshold", "window", "matched"} {
		t.Run(event, func(t *testing.T) {
			m, _ := profileTestModel(t)
			m.openMonitorAttention("attention-profile:one")
			switch event {
			case "skip":
				m.declineQuotaSession("one")
			case "apply":
				next, _ := m.Update(quotaStepResult{revision: m.quota.revision, step: *m.quotaStepPending, window: m.quotaStepWindow, targets: m.quotaCandidates()[:1]})
				m = next.(Model)
			case "threshold":
				m.snapshot.RateLimits.Secondary.UsedPercent = 96
				m.evaluateQuotaStep(m.snapshot)
			case "window":
				reset := *m.snapshot.RateLimits.Secondary.ResetsAt + 3600
				m.snapshot.RateLimits.Secondary.ResetsAt = &reset
				m.evaluateQuotaStep(m.snapshot)
			case "matched":
				next, _ := m.Update(quotaScanResult{revision: m.quota.revision, step: *m.quotaStepPending, matched: []string{"one"}})
				m = next.(Model)
			}
			if m.monitorContextRows["one"].review != "" {
				t.Fatal("review choice survived resolution")
			}
		})
	}
}

func TestProfilePillPreservesComposer(t *testing.T) {
	for _, busy := range []bool{false, true} {
		m, _ := profileTestModel(t)
		m.monitorPrompt.input = newMonitorEditor()
		m.monitorPrompt.input.SetValue("keep this draft")
		m.monitorPrompt.session = "one"
		m.monitorPrompt.busy = busy
		if !busy {
			m.monitorPrompt.input.Focus()
		}
		m.openMonitorAttention("attention-profile:two")
		if m.monitorPrompt.session != "one" || m.monitorPrompt.input.Value() != "keep this draft" || m.monitorPrompt.busy != busy || m.monitorContextDetail != "one" {
			t.Fatal("profile pill discarded composer")
		}
	}
}

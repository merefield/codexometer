package ui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"github.com/merefield/codexometer/internal/codex"
	"testing"
	"time"
)

func drainQuotaCommands(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 20 {
			t.Fatal("quota command loop")
		}
		next, more := m.Update(cmd())
		m = next.(Model)
		cmd = more
	}
	return m
}

func TestAutoQuotaAppliesOnceAndDiscoversNewSessions(t *testing.T) {
	m, f := profileTestModel(t)
	m.quotaSteps[0].Mode = "auto"
	m = drainQuotaCommands(t, m, m.evaluateQuotaStep(m.snapshot))
	if f.updates != 2 || len(m.monitorAttentionSessions()) != 0 {
		t.Fatal("auto did not apply per session or showed approval")
	}
	f.sessions[0].Model = "manual-choice"
	f.sessions = append(f.sessions, codex.QuotaSession{ID: "new", Model: "large", Effort: "high"})
	m = drainQuotaCommands(t, m, m.evaluateQuotaStep(m.snapshot))
	if f.updates != 3 || f.sessions[0].Model != "manual-choice" || f.sessions[2].Model != "small" {
		t.Fatal("manual override lost or new session ignored")
	}
	m.snapshot.RateLimits.Secondary.UsedPercent = 96
	m = drainQuotaCommands(t, m, m.evaluateQuotaStep(m.snapshot))
	if f.updates != 3 || len(m.quotaCandidates()) != 3 {
		t.Fatal("next ask threshold applied automatically")
	}
}

func TestAutoQuotaPartialInventoryContinuesKnownSessions(t *testing.T) {
	m, f := profileTestModel(t)
	m.quotaSteps[0].Mode = "auto"
	f.readError = errors.New("unreadable third session")
	m = drainQuotaCommands(t, m, m.evaluateQuotaStep(m.snapshot))
	if f.updates != 2 || m.quota.scanError == "" {
		t.Fatal("partial inventory blocked known sessions or hid read error")
	}
}

func TestAutoQuotaMatchingAndStaleInventory(t *testing.T) {
	for _, stale := range []bool{false, true} {
		m, f := profileTestModel(t)
		m.quotaSteps[0].Mode = "auto"
		f.sessions[0].Model = "small"
		f.sessions[0].Effort = "medium"
		if stale {
			m.snapshot.FetchedAt = time.Now().Add(-time.Hour)
		}
		m = drainQuotaCommands(t, m, m.evaluateQuotaStep(m.snapshot))
		if stale {
			if f.updates != 0 {
				t.Fatal("stale quota caused write")
			}
			continue
		}
		if f.updates != 1 {
			t.Fatal("matching session updated")
		}
		f.sessions[0].Model = "manual"
		m = drainQuotaCommands(t, m, m.evaluateQuotaStep(m.snapshot))
		if f.updates != 1 {
			t.Fatal("matching session not considered handled")
		}
	}
}

func TestAutoQuotaFailureDoesNotRetryAndContinues(t *testing.T) {
	m, f := profileTestModel(t)
	m.quotaSteps[0].Mode = "auto"
	scan := m.evaluateQuotaStep(m.snapshot)
	next, apply := m.Update(scan())
	m = next.(Model)
	if apply == nil {
		t.Fatal("no automatic attempt")
	}
	// A rejected or uncertain attempt leaves the original session unchanged.
	result := quotaStepResult{revision: m.quota.revision, step: *m.quotaStepPending, window: m.quotaStepWindow, targets: []codex.QuotaSession{f.sessions[0]}, err: errors.New("rejected")}
	next, scan = m.Update(result)
	m = next.(Model)
	m = drainQuotaCommands(t, m, scan)
	if f.updates != 1 || f.targets[0].ID != "two" || m.quota.notices["one"] == "" {
		t.Fatal("failure retried or blocked next session")
	}
	m = drainQuotaCommands(t, m, m.evaluateQuotaStep(m.snapshot))
	if f.updates != 1 {
		t.Fatal("refresh retried failure")
	}
}

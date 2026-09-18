package ui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"errors"
	"github.com/merefield/codexometer/internal/codex"
	"strings"
	"testing"
	"time"
)

type quotaStepTestFetcher struct {
	steps    []codex.QuotaStep
	sessions []codex.QuotaSession
	targets  []codex.QuotaSession
	updates  int
}

func (f *quotaStepTestFetcher) Fetch(context.Context) (codex.Snapshot, error) {
	return codex.Snapshot{}, nil
}
func (f *quotaStepTestFetcher) ConsumeReset(context.Context, string, string) (string, error) {
	return "reset", nil
}
func (f *quotaStepTestFetcher) QuotaStepPolicy() []codex.QuotaStep { return f.steps }
func (f *quotaStepTestFetcher) QuotaSessions(context.Context) ([]codex.QuotaSession, error) {
	return append([]codex.QuotaSession(nil), f.sessions...), nil
}
func (f *quotaStepTestFetcher) ApplyQuotaProfile(_ context.Context, targets []codex.QuotaSession, step codex.QuotaStep) (int, error) {
	f.updates++
	f.targets = targets
	for i, session := range f.sessions {
		for _, target := range targets {
			if session.ID == target.ID {
				f.sessions[i].Model = step.Model
				f.sessions[i].Effort = step.Effort
				if step.ServiceTier == "default" {
					f.sessions[i].Tier = nil
				} else if step.ServiceTier != "" {
					tier := step.ServiceTier
					f.sessions[i].Tier = &tier
				}
			}
		}
	}
	return len(targets), nil
}
func (f *quotaStepTestFetcher) CloseQuotaProfiles() {}
func (f *quotaStepTestFetcher) ResolveQuotaStep(_ context.Context, step codex.QuotaStep) (codex.QuotaStep, error) {
	if step.ServiceTier == "fast" {
		step.ServiceTier = "priority"
	}
	return step, nil
}
func quotaStepSnapshot(used int, reset int64) codex.Snapshot {
	s := codex.DemoSnapshot()
	s.AccountFingerprint = "account"
	s.FetchedAt = time.Now()
	s.RateLimits.Secondary.UsedPercent = used
	s.RateLimits.Secondary.ResetsAt = &reset
	return s
}
func quotaTestModel(t *testing.T) (Model, *quotaStepTestFetcher) {
	t.Helper()
	f := &quotaStepTestFetcher{steps: []codex.QuotaStep{{Threshold: 80, Model: "small", Effort: "medium"}, {Threshold: 95, Model: "small", Effort: "low"}}, sessions: []codex.QuotaSession{{ID: "one", Model: "large", Effort: "high"}, {ID: "two", Model: "large", Effort: "high"}}}
	m := New(f, time.Minute)
	m.width = 100
	m.height = 60
	m.loading = false
	m.snapshot = quotaStepSnapshot(85, time.Now().Add(time.Hour).Unix())
	cmd := m.evaluateQuotaStep(m.snapshot)
	if cmd == nil {
		t.Fatal("no inventory scan")
	}
	next, _ := m.Update(cmd())
	return next.(Model), f
}
func quotaPress(m Model, _ bool) (Model, tea.Cmd) {
	targets := m.quotaCandidates()
	if len(targets) == 0 {
		return m, nil
	}
	return m.pressQuotaSession(targets[0].ID, m.profileArmed(targets[0].ID))
}
func TestQuotaApprovalIsPerSessionAndNewSessionsNeedApproval(t *testing.T) {
	m, f := quotaTestModel(t)
	m, cmd := quotaPress(m, false)
	if cmd != nil || len(m.quota.confirm) != 1 {
		t.Fatal("first press must only review one session")
	}
	m, cmd = quotaPress(m, false)
	if cmd == nil {
		t.Fatal("second press should apply")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if f.updates != 1 || len(f.targets) != 1 || f.targets[0].ID != "one" {
		t.Fatalf("targets %#v", f.targets)
	}
	f.sessions = append(f.sessions, codex.QuotaSession{ID: "new", Model: "large", Effort: "high"})
	cmd = m.evaluateQuotaStep(m.snapshot)
	next, _ = m.Update(cmd())
	m = next.(Model)
	if f.updates != 1 || len(m.quotaCandidates()) != 2 {
		t.Fatal("refresh must only discover unapproved sessions")
	}
	restarted := New(f, time.Minute)
	if len(restarted.quota.handled) != 0 {
		t.Fatal("approval persisted")
	}
}
func TestQuotaConfirmationRejectsStaleOrChangedSnapshots(t *testing.T) {
	for _, kind := range []string{"stale", "loading", "resetting", "error", "expired window", "threshold", "inventory"} {
		t.Run(kind, func(t *testing.T) {
			m, f := quotaTestModel(t)
			m, _ = quotaPress(m, false)
			switch kind {
			case "stale":
				m.snapshot.FetchedAt = time.Now().Add(-time.Hour)
			case "loading":
				m.loading = true
			case "resetting":
				m.resetBusy = true
			case "error":
				m.err = errors.New("offline")
			case "expired window":
				reset := time.Now().Add(-time.Second).Unix()
				m.snapshot.RateLimits.Secondary.ResetsAt = &reset
			case "threshold":
				m.snapshot.RateLimits.Secondary.UsedPercent = 96
				cmd := m.evaluateQuotaStep(m.snapshot)
				next, _ := m.Update(cmd())
				m = next.(Model)
			case "inventory":
				cmd := m.evaluateQuotaStep(m.snapshot)
				next, _ := m.Update(cmd())
				m = next.(Model)
			}
			_, cmd := quotaPress(m, false)
			if cmd != nil || f.updates != 0 {
				t.Fatal("changed confirmation was applied")
			}
		})
	}
}

func TestQuotaWindowChangeLeavesSettingsAndIgnoresLateResults(t *testing.T) {
	m, f := quotaTestModel(t)
	m, _ = quotaPress(m, false)
	m, apply := quotaPress(m, false)
	old := apply().(quotaStepResult)
	m.snapshot = quotaStepSnapshot(85, time.Now().Add(2*time.Hour).Unix())
	scan := m.evaluateQuotaStep(m.snapshot)
	if scan == nil || !m.quotaStepBusy {
		t.Fatal("window change did not rescan")
	}
	next, _ := m.Update(old)
	m = next.(Model)
	if !m.quotaStepBusy {
		t.Fatal("old result cleared busy state")
	}
	next, _ = m.Update(scan())
	m = next.(Model)
	if f.updates != 1 || len(m.quota.handled) != 0 {
		t.Fatal("window state not reset")
	}
}
func TestQuotaSkipAndControlsIndependentOfReset(t *testing.T) {
	m, _ := quotaTestModel(t)
	m.declineQuotaSession("one")
	if got := m.quotaCandidates(); len(got) != 1 || got[0].ID != "two" {
		t.Fatalf("candidates %#v", got)
	}
	for _, width := range []int{24, 60, 100} {
		if m.resetControlsLayout(width) != m.baseResetControlsLayout(width) {
			t.Fatal("profile changes reset layout")
		}
	}
}

func TestQuotaControlsKeepResetVisibleAndClickable(t *testing.T) {
	m, _ := quotaTestModel(t)
	m.snapshot.RateLimitResetCredits = &codex.ResetCredits{AvailableCount: 2}
	for _, width := range []int{28, 64, 100} {
		m.width = width + 4
		rendered := m.renderMainTabs(width, paletteFor(m.theme))
		if !strings.Contains(rendered, "RESET") || strings.Contains(rendered, "REVIEW") {
			t.Fatalf("missing controls %q", rendered)
		}
		if lipgloss.Width(rendered) > width {
			t.Fatalf("width %d overflow: %d", width, lipgloss.Width(rendered))
		}
		g := m.dashboardLayout()
		c := m.baseResetControlsLayout(g.contentWidth)
		if !m.resetAt(2+c.buttonX, g.tabsY+c.buttonY) {
			t.Fatal("reset click mismatch")
		}

	}
}

func TestQuotaRestartUsesCurrentSettingsWithoutApprovalHistory(t *testing.T) {
	for _, speed := range []string{"", "default", "fast"} {
		t.Run(speed, func(t *testing.T) {
			m, f := quotaTestModel(t)
			f.steps = f.steps[:1]
			f.steps[0].ServiceTier = speed
			priority := "priority"
			for i := range f.sessions {
				f.sessions[i].Model = "small"
				f.sessions[i].Effort = "medium"
				f.sessions[i].Tier = &priority
				if speed == "default" {
					// Reproduce app-server's reconnect representation: standard
					// routing may be returned explicitly rather than as null.
					standard := "default"
					f.sessions[i].Tier = &standard
				}
			}
			restarted := New(f, time.Minute)
			restarted.loading = false
			restarted.snapshot = m.snapshot
			cmd := restarted.evaluateQuotaStep(restarted.snapshot)
			next, _ := restarted.Update(cmd())
			restarted = next.(Model)
			if len(restarted.quotaCandidates()) != 0 || len(restarted.quota.handled) != 0 {
				t.Fatal("already matching sessions should require no approval history")
			}
			restarted, cmd = quotaPress(restarted, true)
			if cmd != nil || len(restarted.quota.confirm) != 0 || f.updates != 0 {
				t.Fatal("matching sessions prompted or wrote settings")
			}
			// One different session needs approval; the matching session remains excluded.
			f.sessions[1].Effort = "high"
			cmd = restarted.evaluateQuotaStep(restarted.snapshot)
			next, _ = restarted.Update(cmd())
			restarted = next.(Model)
			candidates := restarted.quotaCandidates()
			if len(candidates) != 1 || candidates[0].ID != "two" {
				t.Fatalf("candidates %#v", candidates)
			}
		})
	}
}

func TestQuotaDisarmingClearsReviewNotice(t *testing.T) {
	for _, action := range []string{"escape", "timeout", "navigation", "resize", "next"} {
		t.Run(action, func(t *testing.T) {
			m, _ := quotaTestModel(t)
			m, _ = quotaPress(m, false)
			if len(m.quota.confirm) != 1 {
				t.Fatal("review missing")
			}
			switch action {
			case "escape":
				m, _, _ = m.updateProfileKey("esc")
			case "next":
				m.setRowContext("two", contextWide)
			case "timeout":
				m.quotaStepConfirmUntil = time.Now().Add(-time.Second)
				next, _ := m.Update(secondMsg(time.Now()))
				m = next.(Model)
			case "navigation":
				next, _ := m.pressViewTab(viewUsage)
				m = next.(Model)
			case "resize":
				next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 50})
				m = next.(Model)
			}
			if len(m.quota.confirm) != 0 {
				t.Fatalf("stale confirmation: %#v", m.quota.confirm)
			}
		})
	}
	m, _ := quotaTestModel(t)
	m.setQuotaSessionNotice("one", "Result notice")
	m.clearQuotaConfirmation()
	if m.quota.notices["one"] != "Result notice" {
		t.Fatal("unrelated result notice was erased")
	}
}

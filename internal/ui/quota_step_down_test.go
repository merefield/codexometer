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
	steps             []codex.QuotaStep
	sessions          []codex.QuotaSession
	targets           []codex.QuotaSession
	updates, restores int
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
func (f *quotaStepTestFetcher) ApplyQuotaProfile(_ context.Context, targets []codex.QuotaSession, _ codex.QuotaStep) (int, error) {
	f.updates++
	f.targets = targets
	return len(targets), nil
}
func (f *quotaStepTestFetcher) RestoreSessionSettings(context.Context) (int, error) {
	f.restores++
	return 1, nil
}
func (f *quotaStepTestFetcher) CloseQuotaProfiles(ctx context.Context) (int, error) {
	return f.RestoreSessionSettings(ctx)
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
func quotaPress(m Model, all bool) (Model, tea.Cmd) {
	next, cmd := m.pressQuotaChoice(all)
	return next.(Model), cmd
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
func TestQuotaApproveAllBindsReviewedInventory(t *testing.T) {
	m, f := quotaTestModel(t)
	m, _ = quotaPress(m, true)
	f.sessions = append(f.sessions, codex.QuotaSession{ID: "new"})
	m, cmd := quotaPress(m, true)
	if cmd == nil {
		t.Fatal("no apply")
	}
	cmd()
	if len(f.targets) != 2 {
		t.Fatal("included unreviewed session")
	}
	m, _ = quotaTestModel(t)
	m.height = 10
	m, _ = quotaPress(m, true)
	if len(m.quota.confirm) != 0 {
		t.Fatal("approved list that cannot fit")
	}
}
func TestQuotaWindowChangeRestoresAndIgnoresLateResults(t *testing.T) {
	m, f := quotaTestModel(t)
	m, _ = quotaPress(m, false)
	m, apply := quotaPress(m, false)
	old := apply().(quotaStepResult)
	m.snapshot = quotaStepSnapshot(85, time.Now().Add(2*time.Hour).Unix())
	restore := m.evaluateQuotaStep(m.snapshot)
	if restore == nil || !m.quotaStepBusy {
		t.Fatal("window change did not restore")
	}
	next, _ := m.Update(old)
	m = next.(Model)
	if !m.quotaStepBusy {
		t.Fatal("old result cleared busy state")
	}
	next, _ = m.Update(restore())
	m = next.(Model)
	if f.restores != 1 || m.quotaStepActive != nil || len(m.quota.handled) != 0 {
		t.Fatal("window state not reset")
	}
}
func TestQuotaSkipAndControlsIndependentOfReset(t *testing.T) {
	m, _ := quotaTestModel(t)
	m.declineQuotaStep()
	if got := m.quotaCandidates(); len(got) != 1 || got[0].ID != "two" {
		t.Fatalf("candidates %#v", got)
	}
	for _, width := range []int{24, 60, 100} {
		rows := m.quotaActionRows(width)
		if len(rows) == 0 || !strings.Contains(strings.Join(rows, ""), "G") {
			t.Fatal("missing controls")
		}
		if m.resetControlsLayout(width).extraRows != m.baseResetControlsLayout(width).extraRows+len(rows) {
			t.Fatal("action rows not reserved")
		}
	}
}

func TestQuotaControlsKeepResetVisibleAndClickable(t *testing.T) {
	m, _ := quotaTestModel(t)
	m.snapshot.RateLimitResetCredits = &codex.ResetCredits{AvailableCount: 2}
	for _, width := range []int{28, 64, 100} {
		m.width = width + 4
		rendered := m.renderMainTabs(width, paletteFor(m.theme))
		if !strings.Contains(rendered, "RESET") || !strings.Contains(rendered, "REVIEW") {
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
		for _, a := range m.quotaActions(g.contentWidth) {
			if got := m.quotaActionAt(2+a.x, g.tabsY+c.extraRows+1+a.y); got != a.key {
				t.Fatalf("action click mismatch %s %s", got, a.key)
			}
		}
	}
}

package ui

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type quotaControl struct {
	engine        *codex.QuotaProfiles
	revision      uint64
	cancel        context.CancelFunc
	sessions      []codex.QuotaSession
	handled       map[string]int
	notices       map[string]string
	busySession   string
	scanError     string
	confirm       []codex.QuotaSession
	confirmStep   codex.QuotaStep
	confirmWindow string
}

func (m Model) CancelQuotaWork() {
	if m.quota.cancel != nil {
		m.quota.cancel()
	}
}

type quotaScanResult struct {
	revision uint64
	step     codex.QuotaStep
	sessions []codex.QuotaSession
	matched  []string
	err      error
}
type quotaStepResult struct {
	revision uint64
	step     codex.QuotaStep
	window   string
	targets  []codex.QuotaSession
	updated  int
	err      error
}

func quotaStepProfile(step codex.QuotaStep) string {
	result := step.Model + " / " + step.Effort
	if step.ServiceTier != "" {
		result += " / " + step.ServiceTier
	} else {
		result += " / " + i18n.Text("speed unchanged")
	}
	return result
}
func quotaPolicyWindow(s codex.Snapshot) (codex.Meter, string, bool) {
	return codex.QuotaPolicyWindow(s)
}
func (m Model) quotaFresh() bool {
	meter, window, ok := quotaPolicyWindow(m.snapshot)
	if !ok || m.quotaStepPending == nil {
		return false
	}
	var candidate *codex.QuotaStep
	if step, _, selected := codex.SelectQuotaProfile(m.snapshot, m.quotaSteps); selected {
		candidate = &step
	}
	if !reflect.DeepEqual(candidate, m.quotaStepPending) {
		return false
	}
	return window == m.quotaStepWindow && !m.loading && !m.resetBusy && m.err == nil &&
		*meter.Window.ResetsAt > time.Now().Unix() && !m.snapshot.FetchedAt.IsZero() && time.Since(m.snapshot.FetchedAt) >= 0 && time.Since(m.snapshot.FetchedAt) <= 2*m.refreshEvery
}
func (m *Model) clearQuotaConfirmation() {
	m.quota.confirm = nil
	m.quotaStepConfirmUntil = time.Time{}
}

func (m *Model) clearQuotaReviewChoice(id string) {
	m.monitorContextRows = maps.Clone(m.monitorContextRows)
	for key, row := range m.monitorContextRows {
		if id == "" || key == id {
			row.review = ""
			m.monitorContextRows[key] = row
		}
	}
}
func (m *Model) evaluateQuotaStep(snapshot codex.Snapshot) tea.Cmd {
	if len(m.quotaSteps) == 0 {
		return nil
	}
	_, window, ok := quotaPolicyWindow(snapshot)
	if !ok {
		m.clearQuotaReviewChoice("")
		m.clearQuotaConfirmation()
		m.quotaStepPending = nil
		return nil
	}
	if m.quotaStepWindow != "" && m.quotaStepWindow != window {
		m.clearQuotaReviewChoice("")
		if m.quota.cancel != nil {
			m.quota.cancel()
		}
		m.quota.revision++
		m.clearQuotaConfirmation()
		m.quota.sessions = nil
		m.quota.handled = nil
		m.quota.notices = nil
		m.quota.busySession = ""
		m.quota.scanError = ""
		m.quotaStepPending = nil
		m.quotaStepBusy = false
	}
	m.quotaStepWindow = window
	var candidate *codex.QuotaStep
	if step, _, selected := codex.SelectQuotaProfile(snapshot, m.quotaSteps); selected {
		candidate = &step
	}
	if !reflect.DeepEqual(candidate, m.quotaStepPending) {
		m.clearQuotaConfirmation()
		m.clearQuotaReviewChoice("")
	}
	m.quotaStepPending = candidate
	if candidate == nil {
		m.quota.scanError = ""
	}
	if candidate == nil || m.quotaStepBusy {
		return nil
	}
	m.quotaStepBusy = true
	m.quota.revision++
	revision := m.quota.revision
	step := *candidate
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.quota.cancel = cancel
	if c, ok := m.fetcher.(codex.SessionSettingsClient); ok && m.quota.engine == nil {
		m.quota.engine = codex.NewQuotaProfiles(c)
	}
	engine := m.quota.engine
	return func() tea.Msg {
		defer cancel()
		if engine == nil {
			return quotaScanResult{revision: revision, step: step, err: fmt.Errorf("%s", i18n.Text("Session controls unavailable."))}
		}
		inventory, err := engine.Inventory(ctx, snapshot, m.quotaSteps, m.refreshEvery)
		var candidates []codex.QuotaSession
		for _, session := range inventory.Sessions {
			if threshold, handled := inventory.Handled[session.ID]; !handled || threshold < step.Threshold {
				candidates = append(candidates, session)
			}
		}
		return quotaScanResult{revision: revision, step: step, sessions: candidates, matched: inventory.Matched, err: err}
	}
}
func (m Model) quotaCandidates() []codex.QuotaSession {
	if m.quotaStepPending == nil {
		return nil
	}
	var result []codex.QuotaSession
	for _, s := range m.quota.sessions {
		if threshold, handled := m.quota.handled[s.ID]; !handled || threshold < m.quotaStepPending.Threshold {
			result = append(result, s)
		}
	}
	return result
}

// A confirmation binds exactly one reviewed session, settings and quota window.
func (m Model) pressQuotaSession(id string, confirm bool) (Model, tea.Cmd) {
	var targets []codex.QuotaSession
	for _, s := range m.quotaCandidates() {
		if s.ID == id {
			targets = []codex.QuotaSession{s}
			break
		}
	}
	if m.quotaStepBusy || len(targets) != 1 || !m.quotaFresh() {
		m.clearQuotaConfirmation()
		return m, nil
	}
	m.resetConfirmUntil = time.Time{}
	if !confirm {
		m.quota.confirm = targets
		m.quota.confirmStep = *m.quotaStepPending
		m.quota.confirmWindow = m.quotaStepWindow
		m.quotaStepConfirmUntil = time.Now().Add(10 * time.Second)
		return m, nil
	}
	if time.Now().After(m.quotaStepConfirmUntil) || !reflect.DeepEqual(targets, m.quota.confirm) ||
		m.quota.confirmStep != *m.quotaStepPending || m.quota.confirmWindow != m.quotaStepWindow {
		m.clearQuotaConfirmation()
		return m, nil
	}
	return m.startQuotaUpdate(targets)
}

// Both reviewed and launch-authorized updates share the same backend checks.
func (m Model) startQuotaUpdate(targets []codex.QuotaSession) (Model, tea.Cmd) {
	snapshot := codex.CaptureQuotaProfile(m.snapshot)
	m.quota.busySession = targets[0].ID
	step := *m.quotaStepPending
	window := m.quotaStepWindow
	if step.Mode == "auto" {
		if m.quota.handled == nil {
			m.quota.handled = map[string]int{}
		}
		m.quota.handled[targets[0].ID] = step.Threshold
	}
	m.clearQuotaConfirmation()
	m.quotaStepBusy = true
	m.quota.revision++
	revision := m.quota.revision
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.quota.cancel = cancel
	return m, func() tea.Msg {
		defer cancel()
		c, ok := m.fetcher.(codex.SessionSettingsClient)
		if !ok {
			return quotaStepResult{revision: revision, window: window, step: step, targets: targets, err: fmt.Errorf("%s", i18n.Text("Session controls unavailable."))}
		}
		engine := m.quota.engine
		if engine == nil {
			engine = codex.NewQuotaProfiles(c)
		}
		n, err := engine.Apply(ctx, snapshot, m.quotaSteps, m.refreshEvery, targets[0], step, window, false)
		return quotaStepResult{revision: revision, window: window, step: step, targets: targets, updated: n, err: err}
	}
}

func (m Model) applyNextAutoQuotaSession() (Model, tea.Cmd) {
	if m.quotaStepPending == nil || m.quotaStepPending.Mode != "auto" ||
		m.quotaStepBusy || m.quota.scanError != "" || !m.quotaFresh() {
		return m, nil
	}
	targets := m.quotaCandidates()
	if len(targets) == 0 {
		return m, nil
	}
	return m.startQuotaUpdate(targets[:1])
}
func (m *Model) declineQuotaSession(id string) {
	if m.quotaStepBusy {
		return
	}
	for _, s := range m.quotaCandidates() {
		if s.ID != id {
			continue
		}
		if m.quota.handled == nil {
			m.quota.handled = map[string]int{}
		}
		m.quota.handled[id] = m.quotaStepPending.Threshold
		if m.quota.engine != nil {
			m.quota.engine.Skip(id, m.quotaStepPending.Threshold, m.quotaStepWindow)
		}
		m.clearQuotaReviewChoice(id)
		m.clearQuotaConfirmation()
		m.setQuotaSessionNotice(id, i18n.Text("Session skipped for this threshold."))
		return
	}
}

func (m *Model) setQuotaSessionNotice(id, notice string) {
	if m.quota.notices == nil {
		m.quota.notices = map[string]string{}
	}
	m.quota.notices[id] = notice
}

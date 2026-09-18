package ui

import (
	"context"
	"fmt"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type quotaControl struct {
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
func windowMinutes(w codex.Window) int64 {
	if w.WindowDurationMins == nil {
		return 0
	}
	return *w.WindowDurationMins
}
func quotaPolicyWindow(s codex.Snapshot) (codex.Meter, string, bool) {
	var selected codex.Meter
	found := false
	for _, m := range s.Meters() {
		if m.Kind != codex.MeterQuotaWindow || m.LimitID != "codex" {
			continue
		}
		if !found || windowMinutes(m.Window) > windowMinutes(selected.Window) {
			selected = m
			found = true
		}
	}
	if !found || selected.Window.ResetsAt == nil || s.AccountFingerprint == "" {
		return selected, "", false
	}
	return selected, fmt.Sprintf("%s:%d:%d", s.AccountFingerprint, windowMinutes(selected.Window), *selected.Window.ResetsAt), true
}
func (m Model) quotaFresh() bool {
	meter, window, ok := quotaPolicyWindow(m.snapshot)
	if !ok || m.quotaStepPending == nil {
		return false
	}
	var candidate *codex.QuotaStep
	for _, step := range m.quotaSteps {
		if step.Threshold <= meter.Window.UsedPercent {
			copy := step
			candidate = &copy
		}
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
func (m *Model) evaluateQuotaStep(snapshot codex.Snapshot) tea.Cmd {
	if len(m.quotaSteps) == 0 {
		return nil
	}
	meter, window, ok := quotaPolicyWindow(snapshot)
	if !ok {
		m.clearQuotaConfirmation()
		m.quotaStepPending = nil
		return nil
	}
	if m.quotaStepWindow != "" && m.quotaStepWindow != window {
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
	for _, step := range m.quotaSteps {
		if step.Threshold <= meter.Window.UsedPercent {
			copy := step
			candidate = &copy
		}
	}
	if !reflect.DeepEqual(candidate, m.quotaStepPending) {
		m.clearQuotaConfirmation()
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
	return func() tea.Msg {
		defer cancel()
		c, ok := m.fetcher.(codex.SessionSettingsClient)
		if !ok {
			return quotaScanResult{revision: revision, step: step, err: fmt.Errorf("%s", i18n.Text("Session controls unavailable."))}
		}
		resolved, err := c.ResolveQuotaStep(ctx, step)
		if err != nil {
			return quotaScanResult{revision: revision, step: step, err: err}
		}
		sessions, err := c.QuotaSessions(ctx)
		var candidates []codex.QuotaSession
		for _, session := range sessions {
			if !session.MatchesQuotaStep(resolved) {
				candidates = append(candidates, session)
			}
		}
		return quotaScanResult{revision: revision, step: step, sessions: candidates, err: err}
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
	m.quota.busySession = id
	step := m.quota.confirmStep
	window := m.quota.confirmWindow
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
		n, err := c.ApplyQuotaProfile(ctx, targets, step)
		return quotaStepResult{revision: revision, window: window, step: step, targets: targets, updated: n, err: err}
	}
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

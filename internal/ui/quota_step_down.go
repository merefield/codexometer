package ui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"reflect"
	"strings"
	"time"
)

type quotaControl struct {
	revision      uint64
	cancel        context.CancelFunc
	sessions      []codex.QuotaSession
	handled       map[string]int
	selected      int
	confirm       []codex.QuotaSession
	confirmStep   codex.QuotaStep
	confirmWindow string
	confirmAll    bool
}

func (m Model) CancelQuotaWork() {
	if m.quota.cancel != nil {
		m.quota.cancel()
	}
}

type quotaScanResult struct {
	revision uint64
	step     codex.QuotaStep
	matched  int
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

func quotaStepThreshold(step *codex.QuotaStep) int {
	if step == nil {
		return 0
	}
	return step.Threshold
}
func quotaStepProfile(step codex.QuotaStep) string {
	result := step.Model + " / " + step.Effort
	if step.ServiceTier != "" {
		result += " / " + step.ServiceTier
	} else {
		result += " / speed unchanged"
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
	return ok && window == m.quotaStepWindow && !m.loading && !m.resetBusy && m.err == nil &&
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
		m.quotaStepPending = nil
		m.quotaStepBusy = false
		m.quotaStepActive = nil
		m.quotaStepNotice = "Quota window changed. Existing session settings are unchanged."
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
			return quotaScanResult{revision: revision, step: step, err: fmt.Errorf("shared session control unavailable")}
		}
		resolved, err := c.ResolveQuotaStep(ctx, step)
		if err != nil {
			return quotaScanResult{revision: revision, step: step, err: err}
		}
		sessions, err := c.QuotaSessions(ctx)
		var candidates []codex.QuotaSession
		matched := 0
		for _, session := range sessions {
			if session.MatchesQuotaStep(resolved) {
				matched++
			} else {
				candidates = append(candidates, session)
			}
		}
		return quotaScanResult{revision: revision, step: step, sessions: candidates, matched: matched, err: err}
	}
}
func (m Model) quotaCandidates() []codex.QuotaSession {
	if m.quotaStepPending == nil {
		return nil
	}
	var result []codex.QuotaSession
	for _, s := range m.quota.sessions {
		if m.quota.handled[s.ID] < m.quotaStepPending.Threshold {
			result = append(result, s)
		}
	}
	return result
}
func (m Model) pressQuotaStep() (tea.Model, tea.Cmd) { return m.pressQuotaChoice(false) }
func (m Model) pressQuotaChoice(all bool) (tea.Model, tea.Cmd) {
	targets := m.quotaCandidates()
	if m.quotaStepBusy || len(targets) == 0 || !m.quotaFresh() {
		m.clearQuotaConfirmation()
		return m, nil
	}
	if !all {
		targets = []codex.QuotaSession{targets[m.quota.selected%len(targets)]}
	}
	m.resetConfirmUntil = time.Time{}
	if time.Now().After(m.quotaStepConfirmUntil) || all != m.quota.confirmAll || !reflect.DeepEqual(targets, m.quota.confirm) ||
		m.quota.confirmStep != *m.quotaStepPending || m.quota.confirmWindow != m.quotaStepWindow {
		m.quota.confirm = append([]codex.QuotaSession(nil), targets...)
		m.quota.confirmStep = *m.quotaStepPending
		m.quota.confirmAll = all
		m.quota.confirmWindow = m.quotaStepWindow
		m.quotaStepConfirmUntil = time.Now().Add(10 * time.Second)
		m.quotaStepNotice = fmt.Sprintf("Review %d session(s). Repeat the SAME approval action to confirm; Esc cancels. New sessions are not included.", len(targets))
		if all && (len(targets) > 8 || len(m.renderQuotaStepNotice(max(m.width-4, 1))) > 3500 || m.quotaStepNoticeHeight(max(m.width-4, 1)) > m.height/2) {
			m.clearQuotaConfirmation()
			m.quotaStepNotice = "Too many sessions to review together in this pane. Use G to approve individually."
		}
		return m, nil
	}
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
			return quotaStepResult{revision: revision, err: fmt.Errorf("shared session control unavailable")}
		}
		n, err := c.ApplyQuotaProfile(ctx, targets, step)
		return quotaStepResult{revision: revision, window: window, step: step, targets: targets, updated: n, err: err}
	}
}
func (m *Model) declineQuotaStep() {
	if m.quotaStepBusy {
		return
	}
	candidates := m.quotaCandidates()
	if len(candidates) == 0 {
		return
	}
	if m.quota.handled == nil {
		m.quota.handled = map[string]int{}
	}
	m.quota.handled[candidates[m.quota.selected%len(candidates)].ID] = m.quotaStepPending.Threshold
	m.clearQuotaConfirmation()
	m.quotaStepNotice = "Session skipped at this gate for this run."
}
func (m Model) quotaStepLabel() string {
	if !m.meterView.isQuota() || len(m.quotaSteps) == 0 {
		return ""
	}
	if m.quotaStepBusy {
		return "QUOTA PROFILE: checking / updating…"
	}
	if len(m.quotaCandidates()) > 0 {
		if len(m.quota.confirm) > 0 {
			return "[G: CONFIRM ONE] [A: CONFIRM ALL] [N: NEXT] [D: SKIP]"
		}
		return "[G: REVIEW ONE] [A: REVIEW ALL] [N: NEXT] [D: SKIP]"
	}
	if m.quotaStepActive != nil {
		return "QUOTA PROFILE: " + quotaStepProfile(*m.quotaStepActive) + " (approved sessions only)"
	}
	return "QUOTA PROFILE: waiting for threshold / new sessions"
}
func (m Model) renderQuotaStepNotice(width int) string {
	c := paletteFor(m.theme)
	body := m.quotaStepNotice
	if body == "" {
		body = m.quotaStepLabel()
	}
	body += "\nChanges remain after Codexometer closes."
	candidates := m.quotaCandidates()
	if len(candidates) > 0 {
		s := candidates[m.quota.selected%len(candidates)]
		speed := "unset"
		if s.Tier != nil {
			speed = *s.Tier
		}
		body += fmt.Sprintf("\nGate %d%% — Session %d/%d: %s\n%s / %s / %s → %s", m.quotaStepPending.Threshold, m.quota.selected%len(candidates)+1, len(candidates), s.ID, s.Model, s.Effort, speed, quotaStepProfile(*m.quotaStepPending))
	}
	if len(m.quota.confirm) > 1 {
		for _, s := range m.quota.confirm {
			speed := "unset"
			if s.Tier != nil {
				speed = *s.Tier
			}
			body += "\n" + s.ID + ": " + s.Model + " / " + s.Effort + " / " + speed
		}
	}
	return frame(width, "QUOTA PROFILE", c.label().Render(ansi.Hardwrap(codex.SanitizeSessionContext(body), max(width-4, 1), true)), c.warning, c)
}
func (m Model) quotaStepNoticeHeight(width int) int {
	if !m.meterView.isQuota() || len(m.quotaSteps) == 0 {
		return 0
	}
	return lipgloss.Height(m.renderQuotaStepNotice(width))
}
func (m Model) quotaProfileKey(key string) (Model, tea.Cmd, bool) {
	if !m.meterView.isQuota() || len(m.quotaSteps) == 0 {
		return m, nil, false
	}
	switch strings.ToLower(key) {
	case "g", "a":
		next, cmd := m.pressQuotaChoice(key == "a")
		return next.(Model), cmd, true
	case "n":
		m.quota.selected++
		m.clearQuotaConfirmation()
		return m, nil, true
	case "d":
		m.declineQuotaStep()
		return m, nil, true
	case "esc":
		if len(m.quota.confirm) > 0 {
			m.clearQuotaConfirmation()
			return m, nil, true
		}
	}
	return m, nil, false
}

type quotaAction struct {
	key, label string
	x, y       int
}

func (m Model) quotaActions(width int) []quotaAction {
	if m.quotaStepLabel() == "" {
		return nil
	}
	labels := []string{"[G: REVIEW ONE]", "[A: REVIEW ALL]", "[N: NEXT]", "[D: SKIP]"}
	if len(m.quota.confirm) > 0 {
		labels[0] = "[G: CONFIRM ONE]"
		labels[1] = "[A: CONFIRM ALL]"
	}
	keys := []string{"g", "a", "n", "d"}
	x, y := 0, 0
	var actions []quotaAction
	for i, label := range labels {
		if width < len(label) {
			label = "[" + strings.ToUpper(keys[i]) + "]"
		}
		if x > 0 && x+len(label) > width {
			x = 0
			y++
		}
		actions = append(actions, quotaAction{keys[i], label, x, y})
		x += len(label) + 1
	}
	return actions
}
func (m Model) quotaActionRows(width int) []string {
	actions := m.quotaActions(width)
	if len(actions) == 0 {
		return nil
	}
	rows := make([]string, actions[len(actions)-1].y+1)
	for _, a := range actions {
		rows[a.y] += strings.Repeat(" ", max(a.x-len(rows[a.y]), 0)) + a.label
	}
	return rows
}
func (m Model) appendQuotaActions(tabs string, width int) string {
	rows := m.quotaActionRows(width)
	if len(rows) == 0 {
		return tabs
	}
	return tabs + "\n" + strings.Join(rows, "\n")
}
func (m Model) quotaActionAt(x, y int) string {
	g := m.dashboardLayout()
	base := m.baseResetControlsLayout(g.contentWidth)
	for _, a := range m.quotaActions(g.contentWidth) {
		if y == g.tabsY+base.extraRows+1+a.y && x >= 2+a.x && x < 2+a.x+len(a.label) {
			return a.key
		}
	}
	return ""
}

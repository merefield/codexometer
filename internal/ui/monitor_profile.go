package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

// Eligibility is independent of the detail being viewed: one session may have
// a Codex request and a separate quota review at the same time.
func (m Model) quotaSessionCandidate(s monitorSession) (codex.QuotaSession, bool) {
	if m.quotaStepPending == nil || m.quotaStepPending.Mode == "auto" {
		return codex.QuotaSession{}, false
	}
	if threshold, handled := m.quota.handled[s.id]; handled && threshold >= m.quotaStepPending.Threshold {
		return codex.QuotaSession{}, false
	}
	for _, candidate := range m.quota.sessions {
		if candidate.ID == s.id {
			return candidate, true
		}
	}
	return codex.QuotaSession{}, false
}

// Default to Codex requests, but respect an explicit click on a quota pill.
func (m Model) sessionProfile(s monitorSession) (codex.QuotaSession, bool) {
	if m.monitorPrompt.session == s.id && (m.monitorPrompt.input.Focused() || m.monitorPrompt.busy) {
		return codex.QuotaSession{}, false
	}
	review := m.monitorContextRows[s.id].review
	if review == "context" {
		return codex.QuotaSession{}, false
	}
	if review != "profile" && (s.attention == codex.SessionAttentionApproval || s.attention == codex.SessionAttentionInput ||
		s.preview.Kind == codex.SessionContextApproval || s.preview.Kind == codex.SessionContextQuestion) {
		return codex.QuotaSession{}, false
	}
	return m.quotaSessionCandidate(s)
}

func (m Model) hasSessionProfile(s monitorSession) bool { _, ok := m.sessionProfile(s); return ok }

func (m Model) profileDocument(s monitorSession, width int) []detailLine {
	current, ok := m.sessionProfile(s)
	if !ok {
		return nil
	}
	speed := i18n.Text("unset")
	if current.Tier != nil {
		speed = *current.Tier
	}
	width = max(width, 1)
	var lines []detailLine
	appendText := func(text, kind string) {
		for _, line := range strings.Split(ansi.Hardwrap(codex.SanitizeSessionContext(text), width, true), "\n") {
			lines = append(lines, detailLine{ansi.Truncate(line, width, ""), kind})
		}
	}
	section := func(title string) {
		lines = append(lines, detailLine{})
		title = ansi.Truncate(title, width, "")
		if rule := width - lipgloss.Width(title) - 1; rule > 0 {
			title += " " + strings.Repeat("─", rule)
		}
		lines = append(lines, detailLine{title, "heading"})
	}
	appendText(terminalLabel(s.id)+" // "+terminalLabel(s.workingDirectory), "metadata")
	section(i18n.Text("WHY THIS CHANGE"))
	appendText(i18n.Format("Your %d%% quota threshold has been reached. Approve the profile switch below for this session's next turns, or skip it.", m.quotaStepPending.Threshold), "body")
	section(i18n.Text("CURRENT PROFILE"))
	appendText(i18n.Text("MODEL / REASONING LEVEL / SPEED"), "metadata")
	appendText(current.Model+" / "+current.Effort+" / "+speed, "body")
	section(i18n.Text("PROPOSED PROFILE"))
	appendText(i18n.Text("MODEL / REASONING LEVEL / SPEED"), "metadata")
	appendText(quotaStepProfile(*m.quotaStepPending), "body")
	section(i18n.Text("PLEASE NOTE"))
	appendText(i18n.Text("Changes remain after Codexometer closes."), "warning")
	if notice := m.quota.notices[s.id]; notice != "" {
		appendText(notice, "warning")
	}
	if m.quota.busySession == s.id {
		appendText(i18n.Text("Checking / updating…"), "body")
	} else if !m.quotaFresh() {
		appendText(i18n.Text("Refresh quota before approving."), "warning")
	}
	return lines
}

func (m Model) profileArmed(id string) bool {
	return len(m.quota.confirm) == 1 && m.quota.confirm[0].ID == id && time.Now().Before(m.quotaStepConfirmUntil)
}

// The complete settings review must fit above the buttons, even in full detail.
// Both hit testing and keyboard dispatch use these same visible controls.
func (m Model) profileButtons(width, height int, s monitorSession) []monitorApprovalButton {
	if m.quotaStepBusy || !m.quotaFresh() || m.monitorPrompt.input.Focused() ||
		m.monitorState != monitorRunning || m.monitorError != "" {
		return nil
	}
	if _, ok := m.sessionProfile(s); !ok {
		return nil
	}
	labels := []string{i18n.Text("[ (1) APPLY PROFILE ]"), i18n.Text("[ (2) SKIP ]")}
	if m.profileArmed(s.id) {
		labels[0] = i18n.Text("[ (C) CONFIRM PROFILE ]")
	}
	actions := []string{"profile-apply:" + s.id, "profile-skip:" + s.id}
	x, y := 0, 0
	var buttons []monitorApprovalButton
	for i, label := range labels {
		slot := lipgloss.Width(label)
		if i == 0 {
			slot = max(slot, lipgloss.Width(i18n.Text("[ (1) APPLY PROFILE ]")), lipgloss.Width(i18n.Text("[ (C) CONFIRM PROFILE ]")))
		}
		if slot > width-4 {
			return nil
		}
		if x+slot > width-4 {
			x, y = 0, y+1
		}
		buttons = append(buttons, monitorApprovalButton{action: actions[i], label: label, x: x, y: y, slot: slot})
		x += slot + 2
	}
	rows, _, _ := monitorContextBodyLayout(height, y+1)
	if len(m.profileDocument(s, max(width-4, 1))) > rows {
		return nil
	}
	return buttons
}

func (m Model) renderProfileControls(buttons []monitorApprovalButton, colors palette) string {
	if len(buttons) == 0 {
		return ""
	}
	rows := make([]string, buttons[len(buttons)-1].y+1)
	for _, b := range buttons {
		rows[b.y] += strings.Repeat(" ", max(b.x-lipgloss.Width(rows[b.y]), 0)) + m.renderContextAction(b.action, b.label, colors)
	}
	return strings.Join(rows, "\n")
}

func (m Model) renderSessionProfile(width, height int, s monitorSession, colors palette) string {
	doc := m.profileDocument(s, max(width-4, 1))
	buttons := m.profileButtons(width, height, s)
	n := 0
	if len(buttons) > 0 {
		n = buttons[len(buttons)-1].y + 1
	}
	rows, gap, _ := monitorContextBodyLayout(height, n)
	var body []string
	for _, line := range doc[:min(len(doc), rows)] {
		body = append(body, line.render(colors))
	}
	if len(doc) > rows && rows > 0 {
		body[rows-1] = colors.label().Render(ansi.Truncate(i18n.Text("QUOTA THRESHOLD")+" →", max(width-4, 1), ""))
	}
	if n > 0 {
		for len(body) < rows+gap {
			body = append(body, "")
		}
		body = append(body, m.renderProfileControls(buttons, colors))
	}
	navigation := m.renderMonitorNavigation(width, s.id, false, colors)
	if m.rowContextMode(s.id) == contextSplit {
		g := m.dashboardLayout()
		_, _, gw := m.contextColumns(g.contentWidth, s)
		if gw > 0 {
			navigation = ""
		}
	}
	return frameSizedWithActions(width, max(height-2, 1), i18n.Text("QUOTA THRESHOLD"), navigation, "", strings.Join(body, "\n"), colors.primary, colors)
}

func profileButtonsAt(buttons []monitorApprovalButton, height, x, y int) string {
	if len(buttons) == 0 {
		return ""
	}
	_, _, cy := monitorContextBodyLayout(height, buttons[len(buttons)-1].y+1)
	for _, b := range buttons {
		if y == cy+b.y && x >= 2+b.x && x < 2+b.x+lipgloss.Width(b.label) {
			return b.action
		}
	}
	return ""
}

func (m Model) visibleProfileButtons() []monitorApprovalButton {
	if m.meterView != viewMonitor || m.monitorPrompt.input.Focused() {
		return nil
	}
	id := m.monitorSelectedID
	g := m.monitorDashboardLayout()
	if m.monitorContextDetail != "" {
		if s, ok := m.contextDetailSession(); ok {
			return m.profileButtons(g.contentWidth, g.meterHeight, s)
		}
		return nil
	}
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	for i, s := range sessions {
		if s.id != id || m.rowContextMode(id) == contextGraph {
			continue
		}
		_, cw, _ := monitorSessionColumnWidths(a.width)
		if m.rowContextMode(id) == contextSplit {
			_, cw, _ = m.contextColumns(a.width, s)
		}
		return m.profileButtons(cw, heights[i], s)
	}
	return nil
}

func (m Model) updateProfileKey(key string) (Model, tea.Cmd, bool) {
	if key == "esc" && len(m.quota.confirm) > 0 {
		m.clearQuotaConfirmation()
		return m, nil, true
	}
	buttons := m.visibleProfileButtons()
	if len(buttons) == 0 {
		return m, nil, false
	}
	switch key {
	case "1":
		return m.profileAction(buttons[0].action, false)
	case "c":
		id := strings.TrimPrefix(buttons[0].action, "profile-apply:")
		if m.profileArmed(id) {
			return m.profileAction(buttons[0].action, true)
		}
	case "2":
		return m.profileAction(buttons[1].action, false)
	}
	return m, nil, false
}

func (m Model) profileAction(action string, confirm bool) (Model, tea.Cmd, bool) {
	id, skip := strings.CutPrefix(action, "profile-skip:")
	if !skip {
		id = strings.TrimPrefix(action, "profile-apply:")
	}
	if m.monitorContextDetail != "" && m.monitorContextDetail != id {
		return m, nil, true
	}
	if m.monitorSelectedID != id {
		m.clearQuotaConfirmation()
		m.monitorSelectedID = id
	}
	// Recheck the exact rendered target; stale clicks cannot approve another row.
	found := false
	for _, b := range m.visibleProfileButtons() {
		if b.action == action {
			found = true
			break
		}
	}
	if !found {
		m.clearQuotaConfirmation()
		return m, nil, true
	}
	if skip {
		m.declineQuotaSession(id)
		return m, nil, true
	}
	next, cmd := m.pressQuotaSession(id, confirm)
	return next, cmd, true
}

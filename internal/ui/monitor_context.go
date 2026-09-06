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

const monitorContextInfo = "[i]"

func monitorSessionAttentionLabel(s monitorSession) string {
	if s.attention == codex.SessionAttentionInput && s.preview.Kind == codex.SessionContextReply {
		return i18n.Text("TURN COMPLETE")
	}
	return monitorAttentionLabel(s.attention)
}

func contextTitle(c codex.SessionContext) string {
	switch c.Kind {
	case codex.SessionContextReply:
		return i18n.Text("LAST REPLY")
	case codex.SessionContextQuestion:
		return i18n.Text("QUESTION")
	case codex.SessionContextApproval:
		return i18n.Text("REQUEST")
	default:
		return i18n.Text("LAST ACTIVITY")
	}
}

func (m Model) monitorPrivacyLabel() string {
	if m.monitorContextHidden {
		return i18n.Text("[H:SHOW]")
	}
	return i18n.Text("[H:HIDE]")
}

func (m Model) renderContextAction(id, label string, colors palette) string {
	s := colors.label()
	if m.monitorContextHover == id {
		s = s.Foreground(colors.background).Background(colors.primary)
	}
	return s.Render(label)
}

func contextActionRect(width, y int, label string) (monitorRect, bool) {
	w := lipgloss.Width(label)
	return monitorRect{x: width - w - 2, y: y, width: w, height: 1}, width >= w+8
}

// Retain the metrics width so its dismiss target never shifts. Split only the
// space previously owned by the graph; narrow terminals prioritise the text.
func contextColumns(width int, s monitorSession) (metrics, context, graph int) {
	metrics, graph, _ = monitorSessionColumnWidths(width)
	if s.preview.Kind == codex.SessionContextActivity && s.attention == codex.SessionAttentionNone {
		return
	}
	if width >= 100 {
		context = graph / 2
		graph -= context + 1
	} else {
		context, graph = graph, 0
	}
	return
}

func contextAge(c codex.SessionContext) string {
	if c.At.IsZero() {
		return "--"
	}
	return compactDuration(max(time.Since(c.At), 0))
}

func (m Model) renderMonitorContextRow(width, height int, metrics string, s monitorSession, colors palette) string {
	_, cw, gw := contextColumns(width, s)
	info := m.renderContextAction(s.id, monitorContextInfo, colors)
	if cw == 0 {
		graph := m.renderMonitorGraphWithAction(gw, height, s.samples, i18n.Text("TOKEN BARS"), info, colors)
		return lipgloss.JoinHorizontal(lipgloss.Top, metrics, " ", graph)
	}
	inner := max(cw-4, 1)
	text := codex.SanitizeSessionContext(s.preview.Text)
	lines := strings.Split(ansi.Hardwrap(text, inner, true), "\n")
	bodyRows := max(height-2, 1)
	// At most two text rows keep the preview compact; the detail has the rest.
	textRows := min(bodyRows, 2)
	if len(lines) > textRows {
		lines = lines[:textRows]
		lines[textRows-1] = ansi.Truncate(lines[textRows-1], max(inner-1, 0), "") + "…"
	}
	if bodyRows > textRows {
		lines = append(lines, s.preview.Source+" // "+shortSessionID(s.preview.ThreadID)+" // "+contextAge(s.preview))
	}
	for i := range lines {
		lines[i] = colors.label().Render(ansi.Truncate(lines[i], inner, ""))
	}
	panel := frameSizedWithTitleAction(cw, max(height-2, 1), contextTitle(s.preview), info, strings.Join(lines, "\n"), colors.primary, colors)
	row := lipgloss.JoinHorizontal(lipgloss.Top, metrics, " ", panel)
	if gw > 0 {
		row = lipgloss.JoinHorizontal(lipgloss.Top, row, " ", m.renderMonitorGraphSamples(gw, height, s.samples, i18n.Text("TOKEN BARS"), colors))
	}
	return row
}

func (m Model) contextDetailSession() (monitorSession, bool) {
	for _, s := range m.monitorSessionData {
		if s.id == m.monitorContextDetail && m.monitorSessionVisible(s) {
			return s, true
		}
	}
	return monitorSession{}, false
}

func (m Model) contextDetailLines(width int) []string {
	s, ok := m.contextDetailSession()
	if !ok || s.preview.Text == "" {
		return []string{i18n.Text("NO CONTEXT")}
	}
	header := contextTitle(s.preview) + " // " + shortSessionID(s.preview.ThreadID) + " // " + s.preview.Source + " // " + contextAge(s.preview)
	lines := []string{ansi.Truncate(header, max(width, 1), "")}
	if (s.preview.Kind == codex.SessionContextApproval || s.preview.Kind == codex.SessionContextQuestion) && m.monitorApprovalToken() == "" {
		lines = append(lines, ansi.Truncate(i18n.Text("REPLY IN CODEX"), max(width, 1), ""))
	}
	return append(lines, strings.Split(ansi.Hardwrap(codex.SanitizeSessionContext(s.preview.Text), max(width, 1), true), "\n")...)
}

func (m Model) renderMonitorContextDetail(width, height int, colors palette) string {
	lines := m.contextDetailLines(max(width-4, 1))
	rows := max(height-2, 1)
	controls := ""
	if m.monitorApprovalControls(width, height) {
		a, b := m.monitorApprovalLabels()
		controls = m.renderContextAction("approve", a, colors) + strings.Repeat(" ", monitorApprovalSlotWidth()-lipgloss.Width(a)+2) + m.renderContextAction("decline", b, colors)
	} else if m.monitorApprovalNotice != "" {
		controls = colors.label().Render(ansi.Truncate(m.monitorApprovalNotice, max(width-4, 1), ""))
	}
	textRows := rows
	if controls != "" {
		textRows--
	}
	start := min(max(m.monitorContextScroll, 0), max(len(lines)-textRows, 0))
	end := min(start+textRows, len(lines))
	for i := start; i < end; i++ {
		lines[i] = colors.label().Render(lines[i])
	}
	body := strings.Join(lines[start:end], "\n")
	if controls != "" {
		body = controls + "\n" + body
	}
	return frameSizedWithTitleAction(width, rows, i18n.Text("SESSION CONTEXT"), m.renderContextAction("close", monitorDismissLabel, colors), body, colors.primary, colors)
}

func (m *Model) toggleMonitorContext() {
	m.monitorApprovalConfirm = ""
	m.monitorApprovalNotice = ""
	m.monitorContextHidden = !m.monitorContextHidden
	m.monitorContextDetail = ""
	m.monitorContextHover = ""
	m.monitorContextScroll = 0
	m.persistPreferences()
}

func (m *Model) openMonitorContext(id string) {
	if m.monitorContextHidden {
		return
	}
	for _, s := range m.monitorSessionData {
		if s.id == id && s.preview.Text != "" && m.monitorSessionVisible(s) {
			m.monitorApprovalConfirm = ""
			m.monitorApprovalNotice = ""
			m.monitorContextDetail = id
			m.monitorContextScroll = 0
			return
		}
	}
}

func (m *Model) scrollMonitorContext(delta int) {
	g := m.dashboardLayout()
	rows := max(g.meterHeight-2, 1)
	if m.monitorApprovalControls(g.contentWidth, g.meterHeight) || m.monitorApprovalNotice != "" {
		rows--
	}
	limit := max(len(m.contextDetailLines(max(g.contentWidth-4, 1)))-rows, 0)
	m.monitorContextScroll = min(max(m.monitorContextScroll+delta, 0), limit)
}

func (m Model) updateMonitorContextKey(key string) (Model, tea.Cmd, bool) {
	if key == "h" {
		m.toggleMonitorContext()
		return m, nil, true
	}
	if m.monitorContextDetail != "" && !m.monitorContextHidden {
		switch key {
		case "esc", "x", "enter", "i":
			m.monitorContextDetail = ""
			return m, nil, true
		case "up":
			m.scrollMonitorContext(-1)
		case "down":
			m.scrollMonitorContext(1)
		case "pgup":
			m.scrollMonitorContext(-max(m.dashboardLayout().meterHeight-4, 1))
		case "pgdown":
			m.scrollMonitorContext(max(m.dashboardLayout().meterHeight-4, 1))
		case "q", "ctrl+c", "tab", "shift+tab":
			m.monitorContextDetail = ""
			return m, nil, false
		default:
			return m, nil, true
		}
		return m, nil, true
	}
	if key == "i" || key == "enter" {
		id := m.monitorSelectedID
		if id == "" {
			for _, s := range m.monitorSessionData {
				if m.monitorSessionVisible(s) {
					id = s.id
					break
				}
			}
		}
		m.openMonitorContext(id)
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) monitorContextAt(x, y int) string {
	if m.meterView != viewMonitor || m.loading && len(m.snapshot.Meters()) == 0 {
		return ""
	}
	g := m.dashboardLayout()
	x -= 2
	y -= g.meterY
	if m.monitorContextDetail != "" && !m.monitorContextHidden {
		if y == 1 && m.monitorApprovalControls(g.contentWidth, g.meterHeight) {
			a, b := m.monitorApprovalLabels()
			if x >= 2 && x < 2+lipgloss.Width(a) {
				return "approve"
			}
			if x >= 4+monitorApprovalSlotWidth() && x < 4+monitorApprovalSlotWidth()+lipgloss.Width(b) {
				return "decline"
			}
		}
		if r, ok := contextActionRect(g.contentWidth, 0, monitorDismissLabel); ok && r.contains(x, y) {
			return "close"
		}
		return ""
	}
	a := layoutMonitorArea(g.contentWidth, g.meterHeight)
	if len(m.monitorSessionData) > 0 && a.readoutWidth >= 16 {
		if r, ok := contextActionRect(a.readoutWidth, 0, m.monitorPrivacyLabel()); ok && r.contains(x, y) {
			return "privacy"
		}
	}
	if m.monitorContextHidden {
		return ""
	}
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	rowY := a.topHeight + a.gap - 1
	for i, s := range sessions {
		mw, cw, gw := contextColumns(a.width, s)
		if cw == 0 {
			cw = gw
		}
		if s.preview.Text != "" {
			if r, ok := contextActionRect(cw, rowY, monitorContextInfo); ok {
				r.x += mw + 1
				if r.contains(x, y) {
					return s.id
				}
			}
		}
		rowY += heights[i]
	}
	return ""
}

func (m Model) updateMonitorContextMouse(msg tea.MouseMsg) (Model, tea.Cmd, bool) {
	mouse := msg.Mouse()
	_, click := msg.(tea.MouseClickMsg)
	m.monitorContextHover = m.monitorContextAt(mouse.X, mouse.Y)
	if click && mouse.Button == tea.MouseLeft && m.monitorContextHover != "" {
		switch m.monitorContextHover {
		case "approve", "decline":
			return m.monitorApprovalAction(m.monitorContextHover)
		case "privacy":
			m.toggleMonitorContext()
		case "close":
			m.monitorContextDetail = ""
		default:
			m.openMonitorContext(m.monitorContextHover)
		}
		return m, nil, true
	}
	if m.monitorContextDetail != "" && !m.monitorContextHidden {
		g := m.dashboardLayout()
		if mouse.Y >= g.meterY && mouse.Y < g.meterY+g.meterHeight {
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.scrollMonitorContext(-3)
			case tea.MouseWheelDown:
				m.scrollMonitorContext(3)
			}
			return m, nil, true
		}
	}
	return m, nil, false
}

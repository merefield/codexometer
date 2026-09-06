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
	if s.preview.Text == "" || s.preview.Kind == codex.SessionContextActivity && s.attention == codex.SessionAttentionNone {
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
	if m.monitorContextExpanded == s.id {
		_, cw, _ := monitorSessionColumnWidths(width)
		return lipgloss.JoinHorizontal(lipgloss.Top, metrics, " ", m.renderExpandedContext(cw, height, s, colors))
	}
	_, cw, gw := contextColumns(width, s)
	info := ""
	if m.monitorContextActionVisible(s) {
		info = m.renderContextAction(s.id, monitorContextInfo, colors)
	}
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
		if s.id == m.monitorContextTarget() && m.monitorSessionVisible(s) {
			return s, true
		}
	}
	return monitorSession{}, false
}

func (m Model) renderMonitorContextDetail(width, height int, colors palette) string {
	document := m.contextDetailDocument(max(width-4, 1))
	lines := make([]string, len(document))
	for i, line := range document {
		lines[i] = line.render(colors)
	}
	rows := max(height-2, 1)
	controls := ""
	if m.monitorApprovalControls(width, height) {
		controls = m.renderMonitorApprovalControls(width, height, colors)
	} else if m.monitorPromptRows(width, height) > 0 {
		controls = m.renderMonitorPrompt(width, height, colors)
	} else if m.monitorApprovalNotice != "" {
		controls = colors.label().Render(ansi.Truncate(m.monitorApprovalNotice, max(width-4, 1), ""))
	}
	controlRows := 0
	if controls != "" {
		controlRows = strings.Count(controls, "\n") + 1
	}
	textRows, gap, _ := monitorContextBodyLayout(height, controlRows)
	start := min(max(m.monitorContextScroll, 0), max(len(lines)-textRows, 0))
	end := min(start+textRows, len(lines))
	bodyLines := append([]string(nil), lines[start:end]...)
	if controls != "" {
		for len(bodyLines) < textRows+gap {
			bodyLines = append(bodyLines, "")
		}
		bodyLines = append(bodyLines, controls)
	}
	body := strings.Join(bodyLines, "\n")
	action := m.renderContextAction("close", monitorDismissLabel, colors)
	if width >= lipgloss.Width(monitorContextInfo+" "+monitorDismissLabel)+8 {
		action = m.renderContextAction("cycle", monitorContextInfo, colors) + " " + action
	}
	return frameSizedWithTitleAction(width, rows, i18n.Text("SESSION CONTEXT"), action, body, colors.primary, colors)
}

// Reserve the footer before allocating the scroll viewport. Short terminals
// drop the spacer first, preserving at least three text rows for buttons.
func monitorContextBodyLayout(height, controls int) (textRows, gap, controlY int) {
	bodyRows := max(height-2, 1)
	textRows = max(bodyRows-controls, 0)
	if controls > 0 && textRows >= 4 {
		gap = 1
		textRows--
	}
	controlY = 1 + textRows + gap // one title row, relative to the detail frame
	return
}

func (m *Model) toggleMonitorContext() {
	m.monitorPrompt = monitorPromptState{}
	m.monitorApprovalConfirm = ""
	m.monitorApprovalNotice = ""
	m.monitorContextHidden = !m.monitorContextHidden
	m.monitorContextDetail = ""
	m.monitorContextExpanded = ""
	m.monitorContextHover = ""
	m.monitorContextScroll = 0
	m.persistPreferences()
}

func (m *Model) openMonitorContext(id string) {
	if m.monitorContextHidden {
		return
	}
	for _, s := range m.monitorSessionData {
		if s.id == id && (s.preview.Text != "" || id == m.monitorContextExpanded) && m.monitorSessionVisible(s) {
			m.monitorPrompt = monitorPromptState{}
			m.monitorApprovalConfirm = ""
			m.monitorApprovalNotice = ""
			m.monitorContextDetail = id
			m.monitorContextExpanded = ""
			m.monitorContextScroll = 0
			return
		}
	}
}

func (m *Model) scrollMonitorContext(delta int) {
	g := m.dashboardLayout()
	n := m.monitorApprovalControlRows(g.contentWidth, g.meterHeight)
	if n == 0 {
		n = m.monitorPromptRows(g.contentWidth, g.meterHeight)
	}
	if n == 0 && m.monitorApprovalNotice != "" {
		n = 1
	}
	rows, _, _ := monitorContextBodyLayout(g.meterHeight, n)
	limit := max(len(m.contextDetailLines(max(g.contentWidth-4, 1)))-rows, 0)
	m.monitorContextScroll = min(max(m.monitorContextScroll+delta, 0), limit)
}

func (m Model) updateMonitorContextKey(key string) (Model, tea.Cmd, bool) {
	if next, cmd, handled := m.updateMonitorApprovalKey(key); handled {
		return next, cmd, true
	}
	if key == "h" {
		m.toggleMonitorContext()
		return m, nil, true
	}
	if m.monitorContextDetail != "" && !m.monitorContextHidden {
		switch key {
		case "t", "r":
			// These controls remain visible in the global footer. Keep detail
			// open and let the standard handler apply the action and flash.
			return m, nil, false
		case "enter":
			g := m.dashboardLayout()
			if m.monitorPromptRows(g.contentWidth, g.meterHeight) > 0 && m.monitorPromptOffer().Token != "" {
				cmd := m.focusMonitorPrompt()
				return m, cmd, true
			}
			m.cycleMonitorContext("")
			return m, nil, true
		case "esc", "x":
			m.stepBackMonitorContext()
			return m, nil, true
		case "i":
			m.cycleMonitorContext("")
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
			m.monitorContextExpanded = ""
			return m, nil, false
		default:
			return m, nil, true
		}
		return m, nil, true
	}
	if key == "esc" && m.monitorContextExpanded != "" {
		m.stepBackMonitorContext()
		return m, nil, true
	}
	if key == "i" || key == "enter" {
		m.cycleMonitorContext("")
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
		if rows := m.monitorPromptRows(g.contentWidth, g.meterHeight); rows > 0 && m.monitorPromptOffer().Token != "" {
			_, _, controlY := monitorContextBodyLayout(g.meterHeight, rows)
			if y >= controlY+1 && y < controlY+rows-1 && x >= 2 && x < g.contentWidth-2 {
				return "prompt"
			}
		}
		label := monitorContextInfo + " " + monitorDismissLabel
		if r, ok := contextActionRect(g.contentWidth, 0, label); ok {
			r.width = lipgloss.Width(monitorContextInfo)
			if r.contains(x, y) {
				return "cycle"
			}
		}
		buttons := m.monitorApprovalButtons(g.contentWidth, g.meterHeight)
		controlRows := 0
		if len(buttons) > 0 {
			controlRows = buttons[len(buttons)-1].y + 1
		}
		_, _, controlY := monitorContextBodyLayout(g.meterHeight, controlRows)
		for _, b := range buttons {
			if y == controlY+b.y && x >= b.x+2 && x < b.x+2+lipgloss.Width(b.label) {
				return b.action
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
	if hit := m.expandedContextAt(x, y); hit != "" {
		return hit
	}
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	rowY := a.topHeight + a.gap - 1
	for i, s := range sessions {
		if s.id == m.monitorContextExpanded {
			rowY += heights[i]
			continue
		}
		mw, cw, gw := contextColumns(a.width, s)
		if cw == 0 {
			cw = gw
		}
		if m.monitorContextActionVisible(s) {
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
		if strings.HasPrefix(m.monitorContextHover, "decision:") {
			return m.monitorApprovalAction(m.monitorContextHover)
		}
		switch m.monitorContextHover {
		case "prompt":
			cmd := m.focusMonitorPrompt()
			return m, cmd, true
		case "cycle":
			m.cycleMonitorContext("")
		case "privacy":
			m.toggleMonitorContext()
		case "close":
			m.stepBackMonitorContext()
		default:
			m.cycleMonitorContext(m.monitorContextHover)
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

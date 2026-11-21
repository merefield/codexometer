package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/i18n"
)

// Limit the fallback to cramped viewports, not just short rows in a long list.
func (m Model) compactFollowups() bool {
	return (m.width > 0 && m.width < 62) || (m.height > 0 && m.height < 25)
}

func (m Model) monitorQueueTarget() string {
	id := m.monitorContextTarget()
	if m.monitorQueue.compact != "" || m.monitorContextDetail != "" {
		return id
	}
	if !m.contextTargetHidden() && id != "" && id == m.monitorSelectedID && m.rowContextMode(id) == contextWide {
		return id
	}
	if m.compactFollowups() {
		return m.monitorSelectedID
	}
	return ""
}

func compactFollowupLabel(room, count int) string {
	if count == 0 {
		return ""
	}
	label := i18n.Format("[ FOLLOW-UPS · %d ]", count)
	if ansi.StringWidth(label) > room {
		label = fmt.Sprintf("[Q:%d]", count)
	}
	if ansi.StringWidth(label) > room {
		return ""
	}
	return label
}

func (m Model) compactFollowupCount(id string) int {
	return len(m.followupEntriesFor(id, false))
}

// An inline list with at least one item is already a scrollable surface.
func (m Model) rowFollowupBadge(width, height, rightWidth int, s monitorSession) string {
	if !m.compactFollowups() {
		return ""
	}
	if m.rowContextMode(s.id) == contextWide && s.id == m.monitorContextTarget() &&
		m.monitorQueueRows(rightWidth, height, m.expandedControlRows(rightWidth, height, s)) > 1 {
		return ""
	}
	return compactFollowupLabel(width-4, m.compactFollowupCount(s.id))
}

func (m Model) renderFollowupBadge(id, label string, colors palette) string {
	if m.monitorQueue.compactFocus == id && m.monitorContextHover != "followups:"+id {
		return colors.label().Underline(true).Foreground(colors.primary).Render(label)
	}
	return m.renderContextAction("followups:"+id, label, colors)
}

func (m Model) detailFollowupFooter(width, height, rows int, colors palette) (string, monitorRect) {
	id := m.monitorContextDetail
	copyLabel := m.renderMonitorCopy(width, id, colors)
	actions := m.monitorGoalFooter(width, copyLabel, colors)
	if id == "" || !m.compactFollowups() || rows > 1 ||
		m.scheduleUI.open || m.monitorQueue.open {
		return actions, monitorRect{}
	}
	count := m.compactFollowupCount(id)
	suffix := followupFooterSuffix(actions)
	label := compactFollowupLabel(width-4-ansi.StringWidth(suffix), count)
	if label == "" && actions != copyLabel {
		// Optional goal telemetry yields before either clickable action.
		suffix = followupFooterSuffix(copyLabel)
		label = compactFollowupLabel(width-4-ansi.StringWidth(suffix), count)
	}
	if label == "" {
		return actions, monitorRect{}
	}
	w := ansi.StringWidth(label)
	return m.renderFollowupBadge(id, label, colors) + suffix, monitorRect{
		x: width - 2 - w - ansi.StringWidth(suffix), y: height - 1, width: w, height: 1,
	}
}

func followupFooterSuffix(actions string) string {
	if actions == "" {
		return ""
	}
	return "  " + actions
}

func (m Model) compactFollowupAt(x, y int) string {
	if !m.compactFollowups() {
		return ""
	}
	g := m.monitorDashboardLayout()
	x, y = x-2, y-g.meterY
	if id := m.monitorContextDetail; id != "" {
		layout := m.layoutDetailControls(g.contentWidth, g.meterHeight)
		_, hit := m.detailFollowupFooter(g.contentWidth, g.meterHeight, layout.queueRows, paletteFor(m.theme))
		if hit.contains(x, y) {
			return id
		}
		return ""
	}
	return m.monitorFollowupRowAt(g, x, y, true)
}

func (m Model) compactPanelHeader(width int) (label, close string) {
	close = i18n.Text("[ CLOSE ]")
	if width < ansi.StringWidth(close)+14 {
		close = monitorDismissLabel
	}
	label = compactFollowupLabel(width-6-ansi.StringWidth(close), m.compactFollowupCount(m.monitorQueue.compact))
	if label == "" {
		label = "[Q]"
	}
	return label, close
}

func (m Model) renderCompactFollowups(width, height int, colors palette) string {
	label, close := m.compactPanelHeader(width)
	title := m.renderContextAction("followups:toggle", label, colors)
	if s, ok := m.contextDetailSession(); ok {
		title += colors.dimmed().Render(" // " + monitorSessionIdentity(s))
	}
	body := m.renderMonitorQueue(width, max(height-1, 1), colors)
	return frameSizedWithTitleAction(width, max(height-2, 1), title,
		m.renderContextAction("followups:close", close, colors), body, colors.primary, colors)
}

func (m *Model) closeCompactFollowups() {
	q := &m.monitorQueue
	q.compactFocus, q.compact, q.focused = q.compact, "", false
	m.monitorContextHover = ""
}

func (m *Model) compactFollowupAction(index int, action string) tea.Cmd {
	entries := m.followupEntries()
	if index < 0 || index >= len(entries) {
		return nil
	}
	entry := entries[index]
	if action == "delete" && entry.trigger != nil {
		return m.followupAction(index, action)
	}
	if action == "edit" && !entry.editable || action == "now" && !entry.now || action == "delete" && !entry.removable {
		return nil
	}
	id, key := m.monitorQueue.compact, entry.key()
	m.setRowContext(id, contextFull)
	for i, e := range m.followupEntries() {
		if e.key() == key {
			return m.followupAction(i, action)
		}
	}
	return nil
}

func (m Model) updateCompactFollowups(msg tea.Msg) (Model, tea.Cmd, bool) {
	q := &m.monitorQueue
	compact := m.compactFollowups()
	if e, resized := msg.(tea.WindowSizeMsg); resized {
		next := m
		next.width, next.height = e.Width, e.Height
		compact = next.compactFollowups()
	}
	if m.meterView != viewMonitor || !compact {
		q.compact, q.compactFocus, q.focused = "", "", q.focused && q.compact == ""
		return m, nil, false
	}
	if q.compact == "" {
		id := ""
		switch e := msg.(type) {
		case tea.MouseClickMsg:
			if e.Button == tea.MouseLeft {
				id = m.compactFollowupAt(e.X, e.Y)
			}
			q.compactFocus = ""
		case tea.KeyPressMsg:
			if e.String() == "alt+q" {
				id = m.monitorQueueTarget()
			} else if e.String() == "enter" {
				id = q.compactFocus
			}
			q.compactFocus = ""
		}
		if id == "" || m.compactFollowupCount(id) == 0 {
			return m, nil, false
		}
		q.compact, q.focusSession, q.focused, q.offset = id, id, true, 0
		m.monitorPrompt.input.Blur()
		m.closeMonitorCommands()
		cmd := m.pollMonitorQueue()
		return m, cmd, true
	}
	g := m.monitorDashboardLayout()
	rows := max(g.meterHeight-1, 1)
	scroll := func(delta int) {
		q.offset = min(max(q.offset+delta, 0), max(len(m.followupEntries())-1, 0))
		q.focused = true
	}
	switch e := msg.(type) {
	case tea.KeyPressMsg:
		key := e.String()
		if action := followupKeyAction(key); action != "" {
			cmd := m.compactFollowupAction(q.offset, action)
			return m, cmd, true
		}
		switch key {
		case "esc", "alt+q":
			m.closeCompactFollowups()
		case "up":
			scroll(-1)
		case "down":
			scroll(1)
		case "pgup":
			scroll(-max(rows-1, 1))
		case "pgdown":
			scroll(max(rows-1, 1))
		case "t":
			next, cmd := m.pressFooterButton(footerButtonTheme)
			return next.(Model), cmd, true
		case "tab", "shift+tab", "q", "ctrl+c":
			m.closeCompactFollowups()
			return m, nil, false
		}
		return m, nil, true
	case tea.MouseMsg:
		p := e.Mouse()
		x, y := p.X-2, p.Y-g.meterY
		label, close := m.compactPanelHeader(g.contentWidth)
		switch {
		case y == 0 && x >= 3 && x < 3+ansi.StringWidth(label):
			m.monitorContextHover = "followups:toggle"
		default:
			if r, ok := contextActionRect(g.contentWidth, 0, close); ok && r.contains(x, y) {
				m.monitorContextHover = "followups:close"
			} else {
				m.monitorContextHover = m.monitorQueueHit(g.contentWidth, rows, 0, x, y)
			}
		}
		switch msg.(type) {
		case tea.MouseWheelMsg:
			if p.Button == tea.MouseWheelUp {
				scroll(-1)
			} else if p.Button == tea.MouseWheelDown {
				scroll(1)
			}
		case tea.MouseClickMsg:
			if p.Button != tea.MouseLeft {
				break
			}
			if strings.HasPrefix(m.monitorContextHover, "followups:") {
				m.closeCompactFollowups()
			} else if parts := strings.Split(m.monitorContextHover, ":"); len(parts) == 3 {
				i, _ := strconv.Atoi(parts[2])
				cmd := m.compactFollowupAction(i, parts[1])
				return m, cmd, true
			} else if _, tab := m.mainTabAt(p.X, p.Y); tab || m.headerActionAt(p.X, p.Y) != "" {
				m.closeCompactFollowups()
				return m, nil, false
			} else if button := m.footerButtonAt(p.X, p.Y); button != footerButtonNone {
				next, cmd := m.pressFooterButton(button)
				return next.(Model), cmd, true
			}
		}
		return m, nil, true
	case tea.PasteMsg:
		return m, nil, true
	}
	return m, nil, false
}

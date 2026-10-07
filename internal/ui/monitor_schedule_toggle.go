package ui

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/i18n"
)

// Reserve the whole button before truncating the session heading, and share
// those exact display columns with hover/click handling.
func scheduleToggleParts(width int, identity string, open bool) (prefix, button string) {
	prefix = i18n.Text("FOLLOW-UP") + " // " + identity
	button = i18n.Text("[ Ctrl+S: SCHEDULE ]")
	if open {
		button = i18n.Text("[ Ctrl+S: CLOSE SCHEDULE ]")
	}
	inner := max(width-4, 1)
	if inner < ansi.StringWidth(button) {
		return ansi.Truncate(prefix, inner, "…"), ""
	}
	room := inner - ansi.StringWidth(button) - 4
	if room <= 0 {
		return "", button
	}
	return ansi.Truncate(prefix, room, "…") + " // ", button
}

func (m Model) renderScheduleToggleHeader(width int, identity string, open bool, colors palette) string {
	prefix, button := scheduleToggleParts(width, identity, open)
	style := colors.label().Foreground(colors.primary)
	if m.scheduleUI.toggleHover {
		style = style.Foreground(colors.background).Background(colors.primary)
	}
	return colors.label().Render(prefix) + style.Render(button)
}

func (m Model) scheduleToggleAt(x, y int) bool {
	if m.meterView != viewMonitor || m.monitorContextDetail == "" || m.contextTargetHidden() {
		return false
	}
	g := m.monitorDashboardLayout()
	identity := ""
	if s, ok := m.contextDetailSession(); ok {
		identity = monitorSessionIdentity(s)
	} else {
		return false
	}
	var row int
	if m.scheduleUI.open {
		layout := m.schedulePaneLayout(g.contentWidth, g.meterHeight)
		if layout.composerRows < 2 {
			return false
		}
		row = layout.composerY
	} else {
		o := m.monitorPromptOffer()
		if o.Token == "" || o.TurnID != "" || len(o.Questions) > 0 {
			return false
		}
		rows := m.monitorPromptRows(g.contentWidth, g.meterHeight)
		if rows == 0 {
			return false
		}
		_, _, row = monitorContextBodyLayout(g.meterHeight, rows)
	}
	prefix, button := scheduleToggleParts(g.contentWidth, identity, m.scheduleUI.open)
	start := 4 + ansi.StringWidth(prefix)
	return button != "" && y == g.meterY+row && x >= start && x < start+ansi.StringWidth(button)
}

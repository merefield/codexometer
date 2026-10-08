package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/i18n"
)

// Reserve the whole button before truncating the key hints, and share
// those exact display columns with hover/click handling.
func scheduleToggleParts(width int, hint string, open bool) (prefix, button string) {
	button = i18n.Text("[ Ctrl+S: SCHEDULE ]")
	if open {
		button = i18n.Text("[ Ctrl+S: CLOSE SCHEDULE ]")
	}
	inner := max(width-4, 1)
	if inner < ansi.StringWidth(button) {
		return ansi.Truncate(hint, inner, "…"), ""
	}
	room := inner - ansi.StringWidth(button)
	prefix = ansi.Truncate(hint, max(room-2, 0), "…")
	return prefix + strings.Repeat(" ", max(room-ansi.StringWidth(prefix), 0)), button
}

func (m Model) renderScheduleToggleHint(width int, hint string, open bool, hintStyle lipgloss.Style, colors palette) string {
	prefix, button := scheduleToggleParts(width, hint, open)
	style := colors.label().Foreground(colors.primary)
	if m.scheduleUI.toggleHover {
		style = style.Foreground(colors.background).Background(colors.primary)
	}
	return hintStyle.Render(prefix) + style.Render(button)
}

func (m Model) scheduleToggleAt(x, y int) bool {
	if m.meterView != viewMonitor || m.monitorContextDetail == "" || m.contextTargetHidden() {
		return false
	}
	g := m.monitorDashboardLayout()
	if _, ok := m.contextDetailSession(); !ok {
		return false
	}
	var row int
	if m.scheduleUI.open {
		layout := m.schedulePaneLayout(g.contentWidth, g.meterHeight)
		if layout.composerRows < 2 {
			return false
		}
		row = layout.composerY + layout.composerRows - 1
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
		row += rows - 1
	}
	prefix, button := scheduleToggleParts(g.contentWidth, "", m.scheduleUI.open)
	start := 4 + ansi.StringWidth(prefix)
	return button != "" && y == g.meterY+row && x >= start && x < start+ansi.StringWidth(button)
}

package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Nested command menus share the slash suggestions' bottom anchor. Grow upward
// with the choices, but leave conversation visible above the popup.
func (m Model) monitorCommandPopup() monitorRect {
	if !m.monitorCommands.open || m.meterView != viewMonitor || m.monitorContextTarget() != m.monitorCommands.session {
		return monitorRect{}
	}
	area := m.monitorComposerPopupArea()
	if area.height < 5 {
		return monitorRect{}
	}
	wanted := len(m.monitorCommands.menu.Choices) + 5
	if m.monitorCommands.menu.Multiple || m.monitorCommands.reviewedChoice != "" {
		wanted++
	}
	if m.monitorCommands.detail || m.monitorCommands.notice != "" {
		wanted = area.height
	} else if m.monitorCommands.menu.Input {
		wanted = 8
	} else if m.monitorCommands.busy {
		wanted = 5
	}
	height := min(max(wanted, 4), area.height-1, max(area.height/2, 4))
	return monitorRect{x: area.x, y: area.y + area.height - height, width: area.width, height: height}
}

func monitorCommandContentRows(height int) int {
	rows := height - 3 // borders and the action row
	if height >= 6 {
		rows-- // optional keyboard hint
	}
	return max(rows, 0)
}

func renderMonitorPopup(title, body string, width int, colors palette) string {
	border := lipgloss.NormalBorder()
	box := lipgloss.NewStyle().Border(border).BorderForeground(colors.primary).
		Background(colors.background).BorderBackground(colors.background).Width(width).Render(body)
	if title == "" || width < 5 {
		return box
	}
	lines := strings.Split(box, "\n")
	title = ansi.Truncate(terminalLabel(title), width-4, "…")
	lines[0] = colors.label().Background(colors.background).Render(border.TopLeft + " " + title + " " + strings.Repeat(border.Top, max(width-4-ansi.StringWidth(title), 0)) + border.TopRight)
	return strings.Join(lines, "\n")
}

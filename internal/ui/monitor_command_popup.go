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
	width := m.monitorCommandMenuWidth(area)
	if m.monitorCommands.inline {
		wanted := len(m.monitorCommandRows(width, area.height)) + 3
		height := min(max(wanted, 4), area.height-1)
		return monitorRect{x: area.x, y: area.y + area.height - height, width: width, height: height}
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
	return monitorRect{x: area.x, y: area.y + area.height - height, width: width, height: height}
}

func (m Model) monitorCommandMenuWidth(area monitorRect) int {
	p := m.monitorCommands
	width := 4
	for _, i := range m.commandChoiceIndices() {
		o := p.menu.Choices[i]
		label := o.Label
		if p.menu.Multiple {
			label = "[x] " + label
		} else if (p.menu.Picker || p.menu.Path == "theme") && o.Selected {
			label += " // CURRENT"
		} else if o.Next != "" {
			label += " →"
		}
		width = max(width, ansi.StringWidth(label+" // "+strings.Join(strings.Fields(o.Help), " "))+4)
	}
	if p.menu.Input {
		width = max(width, ansi.StringWidth(p.menu.InputLabel)+4, ansi.StringWidth(strings.Join(strings.Fields(p.menu.Help), " "))+4)
	}
	if p.busy {
		width = max(width, ansi.StringWidth("Loading / sending…")+4)
	}
	if p.notice != "" {
		width = max(width, ansi.StringWidth(strings.Join(strings.Fields(p.notice), " "))+4)
	}
	// Keep confirmation and navigation controls discoverable on one row.
	used := 2
	for _, button := range m.commandFooter(area.width) {
		used += ansi.StringWidth(button.text) + 2
	}
	return m.monitorPopupWidth(area, max(width, used))
}

// A row menu uses its entire detail pane. Full-page menus fit their contents
// up to 66% of the available composer width; rendering ellipsizes the excess.
func (m Model) monitorPopupWidth(area monitorRect, wanted int) int {
	if m.monitorContextDetail == "" {
		return area.width
	}
	return min(wanted, max(area.width*66/100, 1))
}

func (m Model) commandContentRows(height int) int {
	if m.monitorCommands.inline {
		return max(height-3, 0)
	}
	return monitorCommandContentRows(height)
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

package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type monitorNavigationButton struct {
	label, action string
	enabled       bool
	rect          monitorRect
}

// Rendering and hit testing share the same right-aligned header layout.
// An empty id means the Monitor readout's session-selection controls.
func (m Model) monitorNavigationButtons(width int, id string, full bool) []monitorNavigationButton {
	var buttons []monitorNavigationButton
	if id == "" {
		count, selected := 0, -1
		for _, s := range m.monitorSessionData {
			if m.monitorSessionVisible(s) {
				if s.id == m.monitorSelectedID {
					selected = count
				}
				count++
			}
		}
		if len(m.monitorSessionData) == 0 || width < 16 {
			return nil
		}
		buttons = []monitorNavigationButton{
			{label: "[↑]", action: "session-up", enabled: count > 0 && selected != 0},
			{label: "[↓]", action: "session-down", enabled: count > 0 && (selected < 0 || selected < count-1)},
			{label: m.monitorPrivacyLabel(width - 8), action: "privacy", enabled: true},
		}
	} else {
		mode := m.rowContextMode(id)
		buttons = []monitorNavigationButton{
			{label: "[←]", action: "less:" + id, enabled: mode > contextGraph},
			{label: "[→]", action: "more:" + id, enabled: mode < contextFull},
		}
		if full {
			buttons = append(buttons, monitorNavigationButton{label: monitorDismissLabel, action: "close", enabled: true})
		}
	}
	groupWidth := func() int {
		n := max(len(buttons)-1, 0)
		for _, b := range buttons {
			n += lipgloss.Width(b.label)
		}
		return n
	}
	if width < groupWidth()+8 {
		// Preserve the existing Hide/Show or Close control on small boxes.
		if len(buttons) == 3 {
			buttons = buttons[2:]
			if id == "" {
				buttons[0].label = m.monitorPrivacyLabel(width)
			}
		} else {
			return nil
		}
	}
	if width < groupWidth()+8 {
		return nil
	}
	x := width - groupWidth() - 2
	for i := range buttons {
		buttons[i].rect = monitorRect{x: x, y: 0, width: lipgloss.Width(buttons[i].label), height: 1}
		x += buttons[i].rect.width + 1
	}
	return buttons
}

func (m Model) renderMonitorNavigation(width int, id string, full bool, colors palette) string {
	return m.renderMonitorNavigationButtons(m.monitorNavigationButtons(width, id, full), colors)
}

func (m Model) renderMonitorNavigationButtons(buttons []monitorNavigationButton, colors palette) string {
	var labels []string
	for _, b := range buttons {
		if !b.enabled {
			labels = append(labels, colors.dimmed().Render(b.label))
		} else if strings.HasPrefix(b.action, "detail:") {
			style := colors.label().Foreground(colors.warning).Bold(true)
			if m.monitorContextHover == b.action {
				style = style.Foreground(colors.background).Background(colors.warning)
			}
			labels = append(labels, style.Render(b.label))
		} else {
			labels = append(labels, m.renderContextAction(b.action, b.label, colors))
		}
	}
	return strings.Join(labels, " ")
}

func (m Model) monitorNavigationHit(width int, id string, full bool, x, y int) string {
	return monitorNavigationButtonsHit(m.monitorNavigationButtons(width, id, full), x, y)
}

func monitorNavigationButtonsHit(buttons []monitorNavigationButton, x, y int) string {
	for _, b := range buttons {
		if b.rect.contains(x, y) {
			if !b.enabled {
				return "navigation-disabled"
			}
			return b.action
		}
	}
	return ""
}

package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/i18n"
)

func (m Model) monitorGoalStatus(s monitorSession, colors palette) string {
	goal := s.goal.Clone()
	if goal == nil {
		return ""
	}
	if m.monitorError != "" || m.monitorState != monitorRunning {
		goal.Stale = true
	}
	style := colors.label().Bold(true).Foreground(colors.primary)
	if goal.Stale {
		style = colors.dimmed()
	} else {
		switch goal.Status {
		case "active":
			style = style.Foreground(colors.success)
		case "blocked", "usageLimited", "budgetLimited":
			style = style.Foreground(colors.warning)
		}
	}
	return style.Render(i18n.Text(goal.Label()))
}

// Keep existing buttons anchored at the far right. Goal status is display-only
// and occupies only spare border space before those controls.
func (m Model) monitorGoalFooter(width int, controls string, colors palette) string {
	s, ok := m.contextDetailSession()
	if !ok || m.hasSessionProfile(s) {
		return controls
	}
	goal := m.monitorGoalStatus(s, colors)
	if goal == "" {
		return controls
	}
	parts := []string{goal}
	if controls != "" {
		parts = append(parts, controls)
	}
	footer := strings.Join(parts, "  ")
	spare := width - ansi.StringWidth(footer) - 6
	if spare < 0 || spare < 12 && m.monitorStatusLineText(width, controls) != "" {
		return controls
	}
	return footer
}

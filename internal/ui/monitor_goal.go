package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/i18n"
)

// Work in seconds rather than time.Duration so long-lived counters cannot
// overflow. This is Codex's reported pursuit time, not time since creation.
func formatGoalDuration(seconds int64) string {
	if seconds < 60 {
		return "<1m"
	}
	parts := make([]string, 0, 3)
	for _, unit := range []struct {
		seconds int64
		suffix  string
	}{{86400, "d"}, {3600, "h"}, {60, "m"}} {
		if count := seconds / unit.seconds; count > 0 {
			parts = append(parts, fmt.Sprintf("%d%s", count, unit.suffix))
		}
		seconds %= unit.seconds
	}
	return strings.Join(parts, " ")
}

func (m Model) monitorGoalStatus(s monitorSession, width int, colors palette) string {
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
	label := i18n.Text(goal.Label())
	if !goal.Stale {
		withDuration := label + " // " + formatGoalDuration(goal.TimeUsedSeconds)
		if ansi.StringWidth(withDuration) <= width {
			label = withDuration
		}
	}
	return style.Render(label)
}

// Keep existing buttons anchored at the far right. Goal status is display-only
// and occupies only spare border space before those controls.
func (m Model) monitorGoalFooter(width int, controls string, colors palette) string {
	s, ok := m.contextDetailSession()
	if !ok || m.hasSessionProfile(s) {
		return controls
	}
	available := width - 6
	if controls != "" {
		available -= ansi.StringWidth(controls) + 2
	}
	if m.monitorStatusLineText(width, controls) != "" {
		available -= 12
	}
	goal := m.monitorGoalStatus(s, available, colors)
	if goal == "" || ansi.StringWidth(goal) > available {
		return controls
	}
	parts := []string{goal}
	if controls != "" {
		parts = append(parts, controls)
	}
	return strings.Join(parts, "  ")
}

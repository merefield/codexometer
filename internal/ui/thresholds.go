package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func (m Model) thresholdDetailLines(width int, colors palette) []string {
	statuses, used := codex.QuotaStepStatuses(m.snapshot, m.quotaSteps)

	lines := []string{colors.dimmed().Render(i18n.Text("MODEL STEP POLICY"))}
	if used == nil {
		lines = append(lines, colors.dimmed().Render(i18n.Text("QUOTA WINDOW UNAVAILABLE")))
	} else {
		lines = append(lines, colors.label().Render(i18n.Format("QUOTA WINDOW // %d%% USED", *used)))
	}
	lines = append(lines, "")

	wide := width >= 72
	if wide {
		lines = append(lines, colors.dimmed().Render(fitTableCell(i18n.Text("TRIGGER"), 10)+fitTableCell(i18n.Text("MODEL / REASONING LEVEL / SPEED"), width-40)+fitTableCell(i18n.Text("ACTION"), 10)+fitTableCell(i18n.Text("STATE"), 20)))
	}
	for _, status := range statuses {
		step := status.Step
		speed := step.ServiceTier
		if speed == "" {
			speed = i18n.Text("speed unchanged")
		} else if speed == "default" {
			speed = "standard"
		}
		mode := i18n.Text("ASK")
		if step.Mode == "auto" {
			mode = i18n.Text("AUTO")
		}
		state := i18n.Text(string(status.Stage))
		style := colors.dimmed()
		switch status.Stage {
		case codex.QuotaStepActive:
			style = colors.label()
		case codex.QuotaStepNext:
			state, style = i18n.Format("NEXT // %d PP", status.Remaining), colors.label()
		}
		profile := codex.SanitizeSessionContext(step.Model + " / " + step.Effort + " / " + speed)
		if wide {
			line := fitTableCell(fmt.Sprintf("%d%%", step.Threshold), 10) + fitTableCell(profile, width-40) + fitTableCell(mode, 10) + fitTableCell(state, 20)
			lines = append(lines, style.Render(ansi.Truncate(line, width, "")))
		} else {
			lines = append(lines,
				style.Render(fmt.Sprintf("%d%% // %s // %s", step.Threshold, mode, state)),
				colors.dimmed().Render(ansi.Truncate(profile, width, "")),
			)
		}
	}
	return strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), max(width, 1), true), "\n")
}

func (m Model) renderThresholds(width, height int, colors palette) string {
	lines := m.thresholdDetailLines(max(width-4, 1), colors)
	rows := max(height-2, 1)
	start := min(m.thresholdScroll, max(len(lines)-rows, 0))
	title := i18n.Text("THRESHOLDS")
	if len(lines) > rows {
		title += " // ↑↓ PgUp/PgDn"
	}
	body := strings.Join(lines[start:min(start+rows, len(lines))], "\n")
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(frameSized(width, rows, title, body, colors.primary, colors))
}

func (m *Model) scrollThresholds(delta int) {
	g := m.dashboardLayout()
	limit := max(len(m.thresholdDetailLines(max(g.contentWidth-4, 1), paletteFor(m.theme)))-max(g.meterHeight-2, 1), 0)
	m.thresholdScroll = min(max(m.thresholdScroll+delta, 0), limit)
}

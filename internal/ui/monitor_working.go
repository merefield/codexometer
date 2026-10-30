package ui

import (
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func hasWorkingCommand(c codex.SessionContext) bool {
	return c.Kind == codex.SessionContextActivity && c.Activity.Command != ""
}

func (m Model) workingCommandTitle(a codex.SessionActivity) string {
	status := "UNKNOWN"
	switch a.CommandStatus {
	case "running":
		status = "RUNNING"
	case "completed":
		status = "COMPLETED"
	case "failed":
		status = "FAILED"
	case "declined":
		status = "DECLINED"
	}
	title := i18n.Text("COMMAND") + " // " + i18n.Text(status)
	// Completed duration is historical evidence. Running elapsed time must not
	// keep advancing when the retained command is only a stale observation.
	if a.CommandStatus != "running" || m.monitorState == monitorRunning && m.monitorError == "" {
		if timing := a.CommandTiming(time.Now()); timing != "" {
			title += " // " + timing
		}
	}
	extra := a.RunningCommands
	if a.CommandStatus == "running" {
		extra--
	}
	if extra > 0 {
		title += " // " + i18n.Format("+%d RUNNING", extra)
	}
	if a.RunningLimited {
		title += "+"
	}
	return title
}

// Reserve space for the command on roomy rows; tiny previews prioritise prose.
func (m Model) workingContextLines(c codex.SessionContext, width, rows int, compact bool) []string {
	width, rows = max(width, 1), max(rows, 1)
	fit := func(text string, n int, cook bool) []string {
		lines := wrappedSessionWebText(codex.SanitizeSessionContext(text), width, cook)
		if len(lines) > n {
			lines = lines[:n]
			lines[n-1] = ansi.Truncate(lines[n-1], max(width-1, 0), "") + "…"
		}
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], width, "")
		}
		return lines
	}
	a := c.Activity
	var lines []string
	if a.Prose != "" {
		proseRows := max(rows-3, 1)
		if compact {
			proseRows = min(proseRows, 2)
		}
		lines = fit(a.Prose, proseRows, true)
	}
	if rows-len(lines) >= 3 && len(lines) > 0 {
		lines = append(lines, "")
	}
	if rows-len(lines) >= 2 {
		lines = append(lines, ansi.Truncate(m.workingCommandTitle(a), width, "…"))
		lines = append(lines, fit(a.Command, rows-len(lines), false)...)
	} else if len(lines) == 0 {
		lines = fit(a.Command, rows, false)
	}
	return lines
}

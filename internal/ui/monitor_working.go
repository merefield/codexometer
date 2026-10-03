package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func hasWorkingCommand(c codex.SessionContext) bool {
	return c.Kind == codex.SessionContextActivity && c.Activity.Command != ""
}

func workingCommandTitle(a codex.SessionActivity) string {
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
func workingContextLines(c codex.SessionContext, width, rows int, compact bool) []string {
	width, rows = max(width, 1), max(rows, 1)
	fit := func(text string, n int) []string {
		lines := strings.Split(ansi.Hardwrap(codex.SanitizeSessionContext(text), width, true), "\n")
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
		lines = fit(a.Prose, proseRows)
	}
	if rows-len(lines) >= 3 && len(lines) > 0 {
		lines = append(lines, "")
	}
	if rows-len(lines) >= 2 {
		lines = append(lines, ansi.Truncate(workingCommandTitle(a), width, "…"))
		lines = append(lines, fit(a.Command, rows-len(lines))...)
	} else if len(lines) == 0 {
		lines = fit(a.Command, rows)
	}
	return lines
}

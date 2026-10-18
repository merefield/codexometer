package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/statusline"
)

func (m Model) statusLineMenu() codex.SessionCommandMenu {
	menu := codex.SessionCommandMenu{Title: "/statusline", Path: "statusline", Multiple: true, Help: "Choose priority fields for session telemetry boxes and the detail footer. Space/Enter toggles; C applies; Escape cancels. Left/Right reorders. This does not change Codex CLI configuration. Missing telemetry is omitted."}
	selected := statusline.Normalize(m.monitorStatusLine)
	fields := statusline.Fields()
	// Keep selected fields in display order, followed by the remaining fields.
	for _, id := range selected {
		for _, f := range fields {
			if f.ID == id {
				menu.Choices = append(menu.Choices, codex.SessionCommandChoice{ID: f.ID, Label: f.Label, Help: f.Help, Selected: true})
			}
		}
	}
	for _, f := range fields {
		found := false
		for _, id := range selected {
			found = found || id == f.ID
		}
		if !found {
			menu.Choices = append(menu.Choices, codex.SessionCommandChoice{ID: f.ID, Label: f.Label, Help: f.Help})
		}
	}
	return menu
}

func statusLineSelection(menu codex.SessionCommandMenu) []string {
	selected := []string{}
	for _, o := range menu.Choices {
		if o.Selected {
			selected = append(selected, o.ID)
		}
	}
	return selected
}

func (m Model) monitorStatusValues() map[string]string {
	s, ok := m.contextDetailSession()
	if !ok {
		return nil
	}
	return m.monitorStatusValuesFor(s)
}

// Both the detail footer and every telemetry box use the same per-session
// values. Rendering an unselected row must never borrow the selected session.
func (m Model) monitorStatusValuesFor(s monitorSession) map[string]string {
	state := monitorSessionAttentionLabel(s)
	if state == "" {
		if s.working {
			state = "WORKING"
		} else {
			state = "IDLE"
		}
	}
	if m.monitorError != "" || m.monitorState != monitorRunning {
		state = "STALE"
	}
	return statusline.Values(statusline.Data{Model: s.modelSettings.Model, Effort: s.modelSettings.ReasoningEffort, Speed: s.modelSettings.ServiceTier, Directory: s.workingDirectory, Name: s.name, ID: s.id, State: state, Source: s.preview.Source, Tokens: s.latest, Agents: s.agentCount})
}

func (m Model) monitorPriorityFields(s monitorSession) ([]string, map[string]bool) {
	values := m.monitorStatusValuesFor(s)
	selected := make(map[string]bool)
	var lines []string
	for _, id := range statusline.Normalize(m.monitorStatusLine) {
		if value := values[id]; value != "" {
			selected[id] = true
			if id != "run-state" { // The essential state line is already first.
				lines = append(lines, value)
			}
		}
	}
	return lines, selected
}

func (m Model) monitorStatusLineText(width int, copyLabel string) string {
	end := width - 2
	if copyLabel != "" {
		end = width - ansi.StringWidth(copyLabel) - 3
	}
	if end < 12 {
		return ""
	}
	text := statusline.Text(m.monitorStatusLine, m.monitorStatusValues())
	if text == "" {
		return ""
	}
	return ansi.Truncate(text, max(end-3, 1), "…")
}

func (m Model) monitorStatusLineVisible(width int) bool {
	if m.meterView != viewMonitor || m.monitorContextDetail == "" || m.contextTargetHidden() || m.monitorCommands.open || m.monitorQueue.open {
		return false
	}
	return m.monitorStatusLineText(width, m.renderMonitorCopy(width, m.monitorContextDetail, paletteFor(m.theme))) != ""
}

// Write into the existing bottom border; dashboard geometry reserves a blank
// row below it only when this text is actually visible.
func (m Model) withMonitorStatusLine(panel string, width int, copyLabel string, colors palette) string {
	text := m.monitorStatusLineText(width, copyLabel)
	if text == "" {
		return panel
	}
	lines := strings.Split(panel, "\n")
	last := lines[len(lines)-1]
	value := colors.label().Foreground(colors.primary).Background(colors.background).Render(" " + text + " ")
	lines[len(lines)-1] = ansi.Cut(last, 0, 1) + value + ansi.Cut(last, 1+ansi.StringWidth(value), width)
	return strings.Join(lines, "\n")
}

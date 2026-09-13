package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

// The token counter retains the existing measurement baseline, including work
// from dismissed rows. State counts describe visible rows and never add agents
// again: linked agents are already included in their parent session.
func (m Model) monitorSummaryLines(width, rows int, colors palette) []string {
	counts := [5]int{}
	for _, s := range m.monitorSessionData {
		if !m.monitorSessionVisible(s) {
			continue
		}
		counts[0]++
		switch s.attention {
		case codex.SessionAttentionApproval:
			counts[2]++
		case codex.SessionAttentionInput:
			counts[3]++
		case codex.SessionAttentionCheck:
			counts[4]++
		case codex.SessionAttentionNone:
			if s.working {
				counts[1]++
			}
		}
	}
	labels := []string{"TOKENS", "SESSIONS", "WORKING", "APPROVAL", "INPUT", "CHECK"}
	values := []string{formatTokens(m.monitorRecordedTokens())}
	for i, n := range counts {
		value := strconv.Itoa(n)
		if i > 0 && (m.monitorError != "" || m.monitorState != monitorRunning) {
			value = "—"
		}
		values = append(values, value)
	}
	columns := 6
	if width < 78 && rows >= 4 {
		columns = 3
	}
	if width < 44 && rows >= 6 {
		columns = 2
	}
	cell := max((width-(columns-1))/columns, 1)
	var lines []string
	for start := 0; start < 6; start += columns {
		var titles, numbers []string
		for i := start; i < min(start+columns, 6); i++ {
			title := i18n.Text(labels[i])
			if i == 5 {
				title += "*"
			} // inferred, never an authoritative input request
			titles = append(titles, colors.dimmed().Render(ansi.Truncate(title, cell, "…")+strings.Repeat(" ", max(cell-lipgloss.Width(ansi.Truncate(title, cell, "…")), 0))))
			style := colors.label().Bold(true)
			if i >= 2 {
				style = colors.dimmed().Bold(true)
				if m.monitorState == monitorRunning && m.monitorError == "" && counts[i-1] > 0 {
					if i == 2 {
						style = style.Foreground(colors.success)
					} else {
						style = style.Foreground(colors.warning)
					}
				}
			}
			value := ansi.Truncate(values[i], cell, "")
			numbers = append(numbers, style.Render(value+strings.Repeat(" ", max(cell-lipgloss.Width(value), 0))))
		}
		lines = append(lines, strings.Join(titles, " "), strings.Join(numbers, " "))
	}
	return lines
}

func (m Model) monitorAttentionSessions() []monitorSession {
	if m.monitorState != monitorRunning || m.monitorError != "" {
		return nil
	}
	var sessions []monitorSession
	for _, attention := range []codex.SessionAttention{codex.SessionAttentionApproval, codex.SessionAttentionInput, codex.SessionAttentionComplete} {
		for _, s := range m.monitorSessionData {
			if m.monitorSessionVisible(s) && s.attention == attention && !(attention == codex.SessionAttentionComplete && s.working) {
				sessions = append(sessions, s)
			}
		}
	}
	return sessions
}

// A bounded, paged flow layout shared by rendering and hit testing. IDs remain
// untouched for navigation; directory names are sanitized before shortening.
func (m Model) monitorAttentionButtons(width, maxRows int) ([]monitorNavigationButton, int) {
	sessions := m.monitorAttentionSessions()
	if len(sessions) == 0 || width < 24 || maxRows < 1 {
		return nil, 0
	}
	start := m.monitorAttentionPage % len(sessions)
	if start < 0 {
		start = 0
	}
	reserve := lipgloss.Width(fmt.Sprintf("[+%d →]", len(sessions))) + 1
	limit := max(width-reserve, 1)
	x, y := 0, 0
	var buttons []monitorNavigationButton
	for i := start; i < len(sessions); i++ {
		s := sessions[i]
		id := shortSessionID(s.id)
		name := filepath.Base(terminalLabel(s.workingDirectory))
		budget := min(limit, max(width/3, 24))
		// Preserve the shared short ID before spending remaining space on the
		// directory. Long translated state labels shrink first on narrow screens.
		state := ansi.Truncate(monitorAttentionStatus(s.attention), max(budget-lipgloss.Width(id)-4, 1), "…")
		caption := state + " " + id
		if name != "." && name != "" {
			caption += " // " + name
		}
		label := "[" + ansi.Truncate(caption, max(budget-2, 1), "…") + "]"
		w := lipgloss.Width(label)
		if x+w > limit {
			x = 0
			y++
		}
		if y >= maxRows {
			break
		}
		buttons = append(buttons, monitorNavigationButton{label: label, action: "attention:" + s.id, enabled: true, rect: monitorRect{x: x, y: y, width: w, height: 1}})
		x += w + 1
	}
	if len(buttons) == 0 {
		return nil, 0
	}
	rows := buttons[len(buttons)-1].rect.y + 1
	if len(buttons) < len(sessions) {
		next := (start + len(buttons)) % len(sessions)
		label := fmt.Sprintf("[+%d →]", len(sessions)-len(buttons))
		buttons = append(buttons, monitorNavigationButton{label: label, action: "attention-next:" + strconv.Itoa(next), enabled: true, rect: monitorRect{x: width - lipgloss.Width(label), y: rows - 1, width: lipgloss.Width(label), height: 1}})
	}
	return buttons, rows
}

func (m Model) renderMonitorAttention(width, rows int, buttons []monitorNavigationButton, colors palette) string {
	lines := make([]string, rows)
	completed := make(map[string]bool)
	for _, s := range m.monitorAttentionSessions() {
		if s.attention == codex.SessionAttentionComplete {
			completed["attention:"+s.id] = true
		}
	}
	for _, b := range buttons {
		color := colors.warning
		if completed[b.action] {
			color = colors.primary
		}
		style := colors.label().Foreground(color).Bold(!completed[b.action])
		if m.monitorContextHover == b.action {
			style = style.Foreground(colors.background).Background(color)
		}
		lines[b.rect.y] += strings.Repeat(" ", max(b.rect.x-lipgloss.Width(lines[b.rect.y]), 0)) + style.Render(b.label)
	}
	for i := range lines {
		lines[i] += strings.Repeat(" ", max(width-lipgloss.Width(lines[i]), 0))
	}
	return strings.Join(lines, "\n")
}

// Overview attention fits between the summary and session rows; retain at least
// one useful row. The full detail budget additionally protects controls/text.
func (m Model) monitorArea(width, height int) monitorGeometry {
	a := layoutMonitorArea(width, height)
	// Reserve one stable navigation row, even when no session needs attention.
	// Overflow is paged rather than shifting the session content downwards.
	a.attentionRows = min(1, max(a.graphHeight-3, 0))
	a.attention, _ = m.monitorAttentionButtons(width, a.attentionRows)
	a.gap += a.attentionRows
	a.graphHeight -= a.attentionRows
	return a
}

func (m Model) monitorDetailHeader(width, height int) (summary, rows int, buttons []monitorNavigationButton) {
	if m.monitorContextDetail == "" || m.contextTargetHidden() {
		return
	}
	// Use the unabridged control allocation, including a growing composer. The
	// optional chrome cannot evict controls or leave fewer than six text rows.
	minimum := max(10, m.layoutDetailControls(width, height).rows+9)
	rows = min(1, max(height-minimum, 0))
	buttons, _ = m.monitorAttentionButtons(width, rows)
	needed := monitorSummaryHeight(width)
	if height-rows-needed >= minimum {
		summary = needed
	}
	return
}

func monitorSummaryHeight(width int) int {
	if width < 48 {
		return 9
	}
	if width < 82 {
		return 7
	}
	return 5
}

func (m Model) monitorDashboardLayout() dashboardGeometry {
	g := m.dashboardLayout()
	summary, rows, _ := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
	g.meterY += summary + rows
	g.meterHeight -= summary + rows
	return g
}

func (m Model) monitorAttentionAt(x, y int) string {
	if m.meterView != viewMonitor || m.loading && len(m.snapshot.Meters()) == 0 {
		return ""
	}
	g := m.dashboardLayout()
	x -= 2
	y -= g.meterY
	var buttons []monitorNavigationButton
	if m.monitorContextDetail != "" && !m.contextTargetHidden() {
		summary, _, b := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
		buttons = b
		y -= summary
	} else {
		a := m.monitorArea(g.contentWidth, g.meterHeight)
		buttons = a.attention
		y -= a.topHeight
	}
	return monitorNavigationButtonsHit(buttons, x, y)
}

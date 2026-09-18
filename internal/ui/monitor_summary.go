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

type monitorAttentionItem struct {
	monitorSession
	profile bool
}

func (item monitorAttentionItem) action() string {
	if item.profile {
		return "attention-profile:" + item.id
	}
	return "attention:" + item.id
}

func (m Model) monitorAttentionSessions() []monitorAttentionItem {
	if m.monitorState != monitorRunning || m.monitorError != "" {
		return nil
	}
	var items []monitorAttentionItem
	for priority := 0; priority < 4; priority++ {
		for _, s := range m.monitorSessionData {
			if !m.monitorSessionVisible(s) {
				continue
			}
			_, profile := m.quotaSessionCandidate(s)
			include := false
			switch priority {
			case 0:
				include = s.attention == codex.SessionAttentionApproval
			case 1:
				include = s.attention == codex.SessionAttentionInput
			case 2:
				include = profile
			case 3:
				include = s.attention == codex.SessionAttentionComplete && !s.working && !profile
			}
			if include {
				items = append(items, monitorAttentionItem{monitorSession: s, profile: priority == 2})
			}
		}
	}
	return items
}

// Validate the action, not only the session ID; sibling pills must open
// different documents and disarm each other's confirmation.
func (m *Model) openMonitorAttention(action string) {
	for _, item := range m.monitorAttentionSessions() {
		if item.action() != action {
			continue
		}
		review := "context"
		if item.profile {
			review = "profile"
		}
		if m.monitorContextDetail == item.id && m.selectedAttentionAction() == action {
			return
		}
		m.setRowContext(item.id, contextFull)
		row := m.monitorContextRows[item.id]
		row.review = review
		m.monitorContextRows[item.id] = row
		return
	}
}

// A bounded, paged flow layout shared by rendering and hit testing. IDs remain
// untouched for navigation; directory names are sanitized before shortening.
func (m Model) monitorAttentionButtons(width, maxRows int) ([]monitorNavigationButton, int) {
	sessions := m.monitorAttentionSessions()
	if len(sessions) == 0 || width < 24 || maxRows < 1 {
		return nil, 0
	}
	// Choose the least compressed presentation that fits the entire list.
	// A resize that makes everything fit also returns to the first page.
	for compact := 0; compact < 4; compact++ {
		buttons := m.layoutMonitorAttention(sessions, width, maxRows, compact, false)
		if len(buttons) == len(sessions) {
			m.markSelectedAttention(buttons)
			return buttons, buttons[len(buttons)-1].rect.y + 1
		}
	}
	start := m.monitorAttentionPage % len(sessions)
	if start < 0 {
		start = 0
	}
	reserve := lipgloss.Width(fmt.Sprintf("[+%d →]", len(sessions))) + 1
	limit := max(width-reserve, 1)
	buttons := m.layoutMonitorAttention(sessions[start:], limit, maxRows, 3, true)
	if len(buttons) == 0 {
		return nil, 0
	}
	rows := buttons[len(buttons)-1].rect.y + 1
	if len(buttons) < len(sessions) {
		next := (start + len(buttons)) % len(sessions)
		label := fmt.Sprintf("[+%d →]", len(sessions)-len(buttons))
		buttons = append(buttons, monitorNavigationButton{label: label, action: "attention-next:" + strconv.Itoa(next), enabled: true, rect: monitorRect{x: width - lipgloss.Width(label), y: rows - 1, width: lipgloss.Width(label), height: 1}})
	}
	m.markSelectedAttention(buttons)
	return buttons, rows
}

func (m Model) selectedAttentionAction() string {
	id := m.monitorContextDetail
	if id == "" {
		return ""
	}
	if s, ok := m.contextDetailSession(); ok && m.hasSessionProfile(s) {
		return "attention-profile:" + id
	}
	return "attention:" + id
}

func (m Model) markSelectedAttention(buttons []monitorNavigationButton) {
	action := m.selectedAttentionAction()
	if action == "" {
		return
	}
	for i := range buttons {
		if buttons[i].action == action {
			label := buttons[i].label
			buttons[i].label = ">" + label[1:len(label)-1] + "<"
		}
	}
}

func (m Model) layoutMonitorAttention(sessions []monitorAttentionItem, width, rows, compact int, truncate bool) []monitorNavigationButton {
	x, y := 0, 0
	markerWidth := 0
	if m.monitorContextDetail != "" {
		markerWidth = 2
	}
	var buttons []monitorNavigationButton
	for _, s := range sessions {
		id := shortSessionID(s.id)
		state := monitorAttentionStatus(s.attention)
		if s.profile {
			state = i18n.Text("QUOTA THRESHOLD")
		}
		if compact == 3 && s.attention == codex.SessionAttentionComplete && !s.profile {
			state = i18n.Text("DONE")
		}
		if truncate {
			state = ansi.Truncate(state, max(width-lipgloss.Width(id)-3-markerWidth, 1), "…")
		}
		caption := state + " " + id
		name := filepath.Base(terminalLabel(s.workingDirectory))
		if compact < 2 && name != "." && name != "" {
			separator := " // "
			if compact == 1 {
				separator = " "
			}
			caption += separator + name
		}
		label := "[" + caption + "]"
		// In full detail reserve both cells on every pill, so changing the
		// detail target cannot alter compression or shift its neighbours.
		if markerWidth > 0 {
			label = " " + label + " "
		}
		w := lipgloss.Width(label)
		if w > width {
			break
		}
		if x+w > width {
			x, y = 0, y+1
		}
		if y >= rows {
			break
		}
		buttons = append(buttons, monitorNavigationButton{label: label, action: s.action(), enabled: true, rect: monitorRect{x: x, y: y, width: w, height: 1}})
		x += w + 1
	}
	return buttons
}

func (m Model) renderMonitorAttention(width, rows int, buttons []monitorNavigationButton, colors palette) string {
	lines := make([]string, rows)
	completed := make(map[string]bool)
	for _, s := range m.monitorAttentionSessions() {
		if s.attention == codex.SessionAttentionComplete && !s.profile {
			completed[s.action()] = true
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
	if a.attentionRows > 0 && m.monitorAttentionHasRoom(width, height) {
		a.attentionRows += 2
		for i := range a.attention {
			a.attention[i].rect.y++
		}
	}
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
	if rows > 0 && height-summary-rows-2 >= max(minimum, 33) {
		rows += 2
		for i := range buttons {
			buttons[i].rect.y++
		}
	}
	return
}

// Reserve overview padding only if every visible session would still receive
// at least eleven rows, including its borders.
func (m Model) monitorAttentionHasRoom(width, height int) bool {
	count := m.visibleMonitorSessionCount()
	return count > 0 && (layoutMonitorArea(width, height).graphHeight-3)/count >= 11
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

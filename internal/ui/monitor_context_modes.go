package ui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func (m Model) monitorContextTarget() string {
	if m.monitorContextDetail != "" {
		return m.monitorContextDetail
	}
	return m.monitorContextExpanded
}

func (m Model) initialMonitorContextTarget() string {
	var first, newest string
	var at time.Time
	for _, s := range m.monitorSessionData {
		if !m.monitorSessionVisible(s) {
			continue
		}
		if s.id == m.monitorSelectedID {
			return s.id
		}
		if s.preview.Text == "" {
			continue
		}
		if first == "" {
			first = s.id
		}
		if s.attention == codex.SessionAttentionApproval && (newest == "" || s.preview.At.After(at)) {
			newest = s.id
			at = s.preview.At
		}
	}
	if newest != "" {
		return newest
	}
	return first
}

func (m *Model) cycleMonitorContext(id string) {
	m.monitorPrompt = monitorPromptState{}
	if m.monitorContextHidden {
		m.toggleMonitorContext()
	}
	if id == "" {
		id = m.monitorContextTarget()
		if m.monitorContextDetail == "" && m.monitorSelectedID != "" {
			id = m.initialMonitorContextTarget()
		}
		if id == "" {
			id = m.initialMonitorContextTarget()
		}
	}
	if id == "" {
		return
	}
	m.monitorApprovalConfirm = ""
	m.monitorApprovalNotice = ""
	m.monitorContextScroll = 0
	if m.monitorContextDetail == id {
		m.stepBackMonitorContext()
		return
	}
	if m.monitorContextExpanded == id {
		if m.monitorContextReturning {
			m.toggleMonitorContext()
		} else {
			m.openMonitorContext(id)
		}
		return
	}
	for _, s := range m.monitorSessionData {
		if s.id == id && m.monitorSessionVisible(s) {
			m.monitorContextDetail = ""
			m.monitorContextExpanded = id
			m.monitorContextReturning = false
			return
		}
	}
}

// With an explicit selection, expose the keyboard action only on that row.
// An empty preview is still a valid destination (and may offer a live prompt).
func (m Model) monitorContextActionVisible(s monitorSession) bool {
	return !m.monitorContextHidden && (m.monitorSelectedID == s.id ||
		m.monitorSelectedID == "" && s.preview.Text != "")
}

func (m *Model) stepBackMonitorContext() {
	m.monitorPrompt = monitorPromptState{}
	m.monitorApprovalConfirm = ""
	m.monitorContextScroll = 0
	if m.monitorContextDetail != "" {
		m.monitorContextExpanded = m.monitorContextDetail
		m.monitorSelectedID = m.monitorContextDetail
		m.monitorContextDetail = ""
		m.monitorContextReturning = true
	} else {
		m.toggleMonitorContext()
	}
}

func expandedContextLines(width int, s monitorSession) []string {
	if s.preview.Text == "" {
		return []string{i18n.Text("NO CONTEXT")}
	}
	header := contextTitle(s.preview) + " // " + s.preview.ThreadID + " // " + s.preview.Source
	text := header + "\n" + codex.SanitizeSessionContext(s.preview.Text)
	return strings.Split(ansi.Hardwrap(text, max(width-4, 1), true), "\n")
}

// Inline decisions are only offered when the complete source/request fits
// above them. Long commands and rules must be reviewed in the full detail.
func (m Model) expandedApprovalButtons(width, height int, s monitorSession) []monitorApprovalButton {
	buttons := m.monitorApprovalButtons(width, height)
	if len(buttons) == 0 {
		return nil
	}
	n := buttons[len(buttons)-1].y + 1
	rows, _, _ := monitorContextBodyLayout(height, n)
	if len(expandedContextLines(width, s)) > rows {
		return nil
	}
	return buttons
}

func (m Model) expandedContextAction(width, height int, s monitorSession) string {
	if len(m.expandedApprovalButtons(width, height, s)) == 0 && s.preview.Kind == codex.SessionContextApproval {
		label := monitorContextInfo + " " + i18n.Text("OPEN DETAIL")
		if width >= lipgloss.Width(label)+8 {
			return label
		}
	}
	return monitorContextInfo
}

func (m Model) renderExpandedContext(width, height int, s monitorSession, colors palette) string {
	lines := expandedContextLines(width, s)
	buttons := m.expandedApprovalButtons(width, height, s)
	n := 0
	controls := ""
	if len(buttons) > 0 {
		n = buttons[len(buttons)-1].y + 1
		controls = m.renderMonitorApprovalControls(width, height, colors)
	} else if m.monitorApprovalHasOutcome() {
		n = 1
		controls = colors.label().Render(ansi.Truncate(m.monitorApprovalNotice, max(width-4, 1), ""))
	}
	if dots := m.sessionActivityDots(s); n == 0 && dots != "" && height >= 5 && width >= 7 {
		n = 1
		controls = colors.label().Render(dots)
	}
	textRows, gap, _ := monitorContextBodyLayout(height, n)
	if len(lines) > textRows {
		lines = lines[:textRows]
		if textRows > 0 {
			lines[textRows-1] = ansi.Truncate(lines[textRows-1], max(width-5, 0), "") + "…"
		}
	}
	for i := range lines {
		lines[i] = colors.label().Render(lines[i])
	}
	if n > 0 {
		for len(lines) < textRows+gap {
			lines = append(lines, "")
		}
		lines = append(lines, controls)
	}
	return frameSizedWithTitleAction(width, max(height-2, 1), contextTitle(s.preview), m.renderContextAction(s.id, m.expandedContextAction(width, height, s), colors), strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) expandedContextAt(x, y int) string {
	if m.monitorContextExpanded == "" || m.monitorContextHidden {
		return ""
	}
	g := m.dashboardLayout()
	a := layoutMonitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	rowY := a.topHeight + a.gap - 1
	for i, s := range sessions {
		if s.id == m.monitorContextExpanded {
			mw, cw, _ := monitorSessionColumnWidths(a.width)
			x -= mw + 1
			y -= rowY
			if r, ok := contextActionRect(cw, 0, m.expandedContextAction(cw, heights[i], s)); ok && r.contains(x, y) {
				return s.id
			}
			// If a long action label cannot fit, retain a minimal clickable [i].
			if cw < lipgloss.Width(m.expandedContextAction(cw, heights[i], s))+8 {
				if r, ok := contextActionRect(cw, 0, monitorContextInfo); ok && r.contains(x, y) {
					return s.id
				}
			}
			buttons := m.expandedApprovalButtons(cw, heights[i], s)
			if len(buttons) > 0 {
				_, _, cy := monitorContextBodyLayout(heights[i], buttons[len(buttons)-1].y+1)
				for _, b := range buttons {
					if y == cy+b.y && x >= 2+b.x && x < 2+b.x+lipgloss.Width(b.label) {
						return b.action
					}
				}
			}
			return ""
		}
		rowY += heights[i]
	}
	return ""
}

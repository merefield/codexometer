package ui

import (
	"maps"
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
	for _, s := range m.monitorSessionData {
		if s.id == id && m.monitorSessionVisible(s) {
			mode := m.rowContextMode(id)
			returning := mode == contextFull || m.monitorContextRows[id].returning
			if returning {
				mode--
			} else {
				mode++
			}
			m.setRowContext(id, mode, returning && mode > contextGraph)
			return
		}
	}
}

// With an explicit selection, expose the keyboard action only on that row.
// An empty preview is still a valid destination (and may offer a live prompt).
func (m Model) monitorContextActionVisible(s monitorSession) bool {
	return (m.monitorSelectedID == s.id ||
		m.monitorSelectedID == "" && s.preview.Text != "")
}

func (m *Model) stepBackMonitorContext() {
	id := m.monitorContextTarget()
	if id == "" {
		id = m.monitorSelectedID
	}
	if id == "" {
		return
	}
	mode := max(m.rowContextMode(id)-1, contextGraph)
	m.setRowContext(id, mode, mode > contextGraph)
}

type rowContextState struct {
	mode      int
	returning bool
}

const (
	contextGraph = iota
	contextSplit
	contextWide
	contextFull
)

func (m Model) rowContextMode(id string) int {
	if id != "" && m.monitorContextDetail == id {
		return contextFull
	}
	if state, ok := m.monitorContextRows[id]; ok {
		return state.mode
	}
	if m.monitorContextHidden {
		return contextGraph
	}
	if id != "" && m.monitorContextExpanded == id {
		return contextWide
	}
	return contextSplit
}

func (m Model) contextTargetHidden() bool {
	return m.rowContextMode(m.monitorContextTarget()) == contextGraph
}

func (m *Model) setRowContext(id string, mode int, returning bool) {
	m.monitorContextRows = maps.Clone(m.monitorContextRows)
	if m.monitorContextRows == nil {
		m.monitorContextRows = make(map[string]rowContextState)
	}
	m.monitorContextRows[id] = rowContextState{min(mode, contextWide), returning}
	m.monitorSelectedID = id
	m.monitorContextDetail, m.monitorContextExpanded = "", ""
	if mode == contextFull {
		m.monitorContextDetail = id
	}
	if mode == contextWide {
		m.monitorContextExpanded = id
	}
	m.monitorPrompt = monitorPromptState{}
	m.monitorApprovalConfirm, m.monitorApprovalNotice = "", ""
	m.monitorContextScroll = 0
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
	if s.id != m.monitorContextTarget() {
		return nil
	}
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
	if !m.monitorContextActionVisible(s) {
		return ""
	}
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
	} else if s.id == m.monitorContextTarget() && m.monitorApprovalHasOutcome() {
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
	return frameSizedWithTitleAction(width, max(height-2, 1), "", m.renderContextAction(s.id, m.expandedContextAction(width, height, s), colors), strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) expandedContextAt(x, y int) string {
	g := m.dashboardLayout()
	a := layoutMonitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	rowY := a.topHeight + a.gap - 1
	for i, s := range sessions {
		if m.rowContextMode(s.id) == contextWide && y >= rowY && y < rowY+heights[i] {
			mw, cw, _ := monitorSessionColumnWidths(a.width)
			x -= mw + 1
			y -= rowY
			if r, ok := contextActionRect(cw, 0, m.expandedContextAction(cw, heights[i], s)); m.monitorContextActionVisible(s) && ok && r.contains(x, y) {
				return s.id
			}
			// If a long action label cannot fit, retain a minimal clickable [i].
			if m.monitorContextActionVisible(s) && cw < lipgloss.Width(m.expandedContextAction(cw, heights[i], s))+8 {
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

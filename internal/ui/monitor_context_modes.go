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

func (m *Model) changeMonitorContext(id string, delta int) {
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
			next := min(max(mode+delta, contextGraph), contextFull)
			if next != mode || m.monitorSelectedID != id {
				m.setRowContext(id, next)
			}
			return
		}
	}
}

func (m *Model) stepBackMonitorContext() {
	m.changeMonitorContext("", -1)
}

type rowContextState struct {
	mode int
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

func (m *Model) setRowContext(id string, mode int) {
	m.monitorContextRows = maps.Clone(m.monitorContextRows)
	if m.monitorContextRows == nil {
		m.monitorContextRows = make(map[string]rowContextState)
	}
	m.monitorContextRows[id] = rowContextState{mode: min(max(mode, contextGraph), contextWide)}
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
	header := s.preview.ThreadID + " // " + s.preview.Source
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
	return frameSizedWithTitleAction(width, max(height-2, 1), contextTitle(s.preview), m.renderMonitorNavigation(width, s.id, false, colors), strings.Join(lines, "\n"), colors.primary, colors)
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

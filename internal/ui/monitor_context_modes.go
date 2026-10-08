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
		if s.preview.Text == "" && s.preview.CurrentTask == "" && s.preview.LatestGuidance == "" {
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
	mode   int
	review string // empty: automatic; context or profile: explicitly selected pill
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
	m.clearQuotaConfirmation()
	m.monitorContextRows = maps.Clone(m.monitorContextRows)
	if m.monitorContextRows == nil {
		m.monitorContextRows = make(map[string]rowContextState)
	}
	m.monitorContextRows[id] = rowContextState{mode: min(max(mode, contextGraph), contextWide), review: m.monitorContextRows[id].review}
	m.monitorSelectedID = id
	m.monitorContextDetail, m.monitorContextExpanded = "", ""
	if mode == contextFull {
		m.monitorContextDetail = id
	}
	if mode == contextWide {
		m.monitorContextExpanded = id
	}
	if m.monitorPrompt.session != id {
		m.stashMonitorDraft()
		m.monitorPrompt = monitorPromptState{}
	} else {
		// Changing presentation is not changing the prompt's owner. Keep the
		// draft/capability, but release keyboard focus for navigation.
		m.monitorPrompt.input.Blur()
	}
	m.monitorApprovalConfirm, m.monitorApprovalNotice = "", ""
	m.monitorContextScroll = 0
	m.restoreMonitorDraft()
}

func expandedContextLines(width int, s monitorSession) []string {
	if s.preview.FileChanges != "" {
		lines := strings.Split(ansi.Hardwrap(codex.SanitizeSessionContext(s.preview.Text), max(width-4, 1), true), "\n")
		for _, line := range fileApprovalDocument(s.preview.FileChanges, max(width-4, 1)) {
			lines = append(lines, line.text)
		}
		return lines
	}
	if s.preview.Text == "" && s.preview.CurrentTask == "" && s.preview.LatestGuidance == "" {
		return []string{i18n.Text("NO CONTEXT")}
	}
	header := terminalLabel(s.preview.ThreadID) + " // " + terminalLabel(s.preview.Source)
	text := header + "\n" + codex.SanitizeSessionContext(s.preview.Text)
	if s.preview.CurrentTask != "" || s.preview.LatestGuidance != "" {
		text = header
		if s.preview.CurrentTask != "" {
			text += "\n" + ansi.Truncate(contextTaskTitle(s.preview)+" // "+strings.Join(strings.Fields(codex.SanitizeSessionContext(s.preview.CurrentTask)), " "), max(width-4, 1), "…")
		}
		if s.preview.LatestGuidance != "" {
			text += "\n" + ansi.Truncate(i18n.Text("LATEST GUIDANCE")+" // "+strings.Join(strings.Fields(codex.SanitizeSessionContext(s.preview.LatestGuidance)), " "), max(width-4, 1), "…")
		}
		text += "\n\n" + codex.SanitizeSessionContext(s.preview.Text)
	}
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
	if m.hasSessionProfile(s) {
		return m.renderSessionProfile(width, height, s, colors)
	}
	lines := expandedContextLines(width, s)
	if notice := m.quota.notices[s.id]; notice != "" {
		lines = append(lines, strings.Split(ansi.Hardwrap(codex.SanitizeSessionContext(notice), max(width-4, 1), true), "\n")...)
	}
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
	if n == 0 && s.id == m.monitorContextTarget() {
		if rows := m.monitorPromptRows(width, height); rows > 0 {
			n = rows
			controls = m.renderMonitorPrompt(width, height, colors)
		}
	}
	if dots := m.sessionActivityDots(s); n == 0 && dots != "" && height >= 5 && width >= 7 {
		n = 1
		controls = colors.label().Render(dots)
	}
	if s.id == m.monitorContextTarget() {
		if queueRows := m.monitorQueueRows(width, height, n); queueRows > 0 {
			queue := m.renderMonitorQueue(width, queueRows, colors)
			if controls != "" {
				controls = queue + "\n" + controls
			} else {
				controls = queue
			}
			n += queueRows
		}
	}
	textRows, gap, _ := monitorContextBodyLayout(height, n)
	if s.preview.Kind == codex.SessionContextReply && !s.preview.Streaming && len(lines) > textRows {
		// Keep the reply readable in short rows. Full detail retains the task.
		withoutTask := s
		withoutTask.preview.CurrentTask, withoutTask.preview.LatestGuidance = "", ""
		lines = expandedContextLines(width, withoutTask)
	}
	if hasWorkingCommand(s.preview) {
		lines = workingContextLines(s.preview, max(width-4, 1), max(textRows, 1), false)
	}
	if len(lines) > textRows {
		lines = lines[:textRows]
		if textRows > 0 {
			lines[textRows-1] = ansi.Truncate(lines[textRows-1], max(width-5, 0), "") + "…"
		}
	}
	for i := range lines {
		lines[i] = colors.label().Render(lines[i])
	}
	if s.preview.FileChanges != "" {
		offset := len(strings.Split(ansi.Hardwrap(codex.SanitizeSessionContext(s.preview.Text), max(width-4, 1), true), "\n"))
		document := fileApprovalDocument(s.preview.FileChanges, max(width-4, 1))
		for i, line := range document {
			if i+offset < len(lines) {
				if i+offset == len(lines)-1 && len(document)+offset > len(lines) {
					line.text = ansi.Truncate(line.text, max(width-5, 0), "") + "…"
				}
				lines[i+offset] = line.render(colors)
			}
		}
	}
	if n > 0 {
		for len(lines) < textRows+gap {
			lines = append(lines, "")
		}
		lines = append(lines, controls)
	}
	action := m.renderMonitorNavigationButtons(m.expandedContextNavigation(width, height, s), colors)
	return frameSizedWithActions(width, max(height-2, 1), contextTitle(s.preview), action, m.renderMonitorCopy(width, s.id, colors), strings.Join(lines, "\n"), colors.primary, colors)
}

// Keep the queue's geometry aligned with the controls already rendered in a row.
func (m Model) expandedControlRows(width, height int, s monitorSession) int {
	if buttons := m.expandedApprovalButtons(width, height, s); len(buttons) > 0 {
		return buttons[len(buttons)-1].y + 1
	}
	if s.id == m.monitorContextTarget() {
		if m.monitorApprovalHasOutcome() {
			return 1
		}
		if rows := m.monitorPromptRows(width, height); rows > 0 {
			return rows
		}
	}
	if m.sessionActivityDots(s) != "" && height >= 5 && width >= 7 {
		return 1
	}
	return 0
}

// Keep an explicit route to the complete request when inline decisions cannot
// be safely offered. The warning never grants approval; it only opens detail.
func (m Model) expandedContextNavigation(width, height int, s monitorSession) []monitorNavigationButton {
	buttons := m.monitorNavigationButtons(width, s.id, false)
	if m.hasSessionProfile(s) || s.preview.Kind != codex.SessionContextApproval || len(m.expandedApprovalButtons(width, height, s)) > 0 ||
		(s.id == m.monitorContextTarget() && m.monitorApprovalHasOutcome()) {
		return buttons
	}
	// The provider consumes a live request before the next context snapshot
	// arrives. A stale preview must not turn vanished controls into a warning.
	// Tokenless local/unsupported requests still need the diagnostic route.
	if p, ok := m.fetcher.(codex.SessionApprovalClient); ok && s.preview.ApprovalToken != "" && !p.SessionApprovalPending(s.preview.ApprovalToken) {
		return buttons
	}
	labels := []string{i18n.Text("APPROVAL — OPEN DETAIL →"), i18n.Text("APPROVAL →"), "!→"}
	end := width - 2
	if len(buttons) > 0 {
		end = buttons[0].rect.x - 1
	}
	for _, label := range labels {
		x := end - lipgloss.Width(label)
		if x >= 6 {
			warning := monitorNavigationButton{label: label, action: "detail:" + s.id, enabled: true, rect: monitorRect{x: x, y: 0, width: lipgloss.Width(label), height: 1}}
			return append([]monitorNavigationButton{warning}, buttons...)
		}
	}
	// On the smallest boxes prioritise the warning; arrow keys and half-clicks
	// remain available even when the visible arrow pair must be omitted.
	if width >= 10 {
		return []monitorNavigationButton{{label: "!→", action: "detail:" + s.id, enabled: true, rect: monitorRect{x: width - 4, y: 0, width: 2, height: 1}}}
	}
	return buttons
}

func (m Model) expandedContextAt(x, y int) string {
	g := m.monitorDashboardLayout()
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	rowY := a.topHeight + a.gap - 1
	for i, s := range sessions {
		if m.rowContextMode(s.id) == contextWide && y >= rowY && y < rowY+heights[i] {
			mw, cw, _ := monitorSessionColumnWidths(a.width)
			x -= mw + 1
			y -= rowY
			if s.id == m.monitorContextTarget() && m.monitorPromptOffer().Token != "" {
				if rows := m.monitorPromptRows(cw, heights[i]); rows > 0 {
					_, _, cy := monitorContextBodyLayout(heights[i], rows)
					if y >= cy+m.monitorPromptHeaderRows() && y < cy+rows-1 && x >= 2 && x < cw-2 {
						return "prompt"
					}
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

package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
)

type monitorStatusFilter int

const (
	monitorFilterAll monitorStatusFilter = iota
	monitorFilterWorking
	monitorFilterApproval
	monitorFilterInput
	monitorFilterCheck
	monitorFilterCount
)

func monitorSessionStatus(s monitorSession) monitorStatusFilter {
	switch s.attention {
	case codex.SessionAttentionApproval:
		return monitorFilterApproval
	case codex.SessionAttentionInput:
		return monitorFilterInput
	case codex.SessionAttentionCheck:
		return monitorFilterCheck
	case codex.SessionAttentionNone:
		if s.working {
			return monitorFilterWorking
		}
	}
	return monitorFilterAll
}

func (m Model) monitorSessionInList(s monitorSession) bool {
	return m.monitorSessionVisible(s) && (m.monitorFilter == monitorFilterAll || monitorSessionStatus(s) == m.monitorFilter)
}

func (m Model) monitorListSessionCount() int {
	n := 0
	for _, s := range m.monitorSessionData {
		if m.monitorSessionInList(s) {
			n++
		}
	}
	return n
}

// Layout is shared by text rendering and hit testing, including wrapped rows.
func monitorSummaryColumns(width, rows int) (columns, cell int) {
	columns = 6
	if width < 78 && rows >= 4 {
		columns = 3
	}
	if width < 44 && rows >= 6 {
		columns = 2
	}
	return columns, max((width-(columns-1))/columns, 1)
}

func monitorFilterAction(filter monitorStatusFilter) string {
	return fmt.Sprintf("session-filter:%d", filter)
}

func (m Model) monitorFilterButtons(width, height int) []monitorNavigationButton {
	inner, rows := max(width-4, 1), max(height-3, 1)
	columns, cell := monitorSummaryColumns(inner, rows)
	var buttons []monitorNavigationButton
	for i := 1; i < 6; i++ { // Tokens is a measurement, not a session category.
		x, y := 2+(i%columns)*(cell+1), 1+(i/columns)*2
		if x+cell > width-2 || y >= height-1 {
			continue
		}
		buttons = append(buttons, monitorNavigationButton{
			action: monitorFilterAction(monitorStatusFilter(i - 1)), enabled: true,
			rect: monitorRect{x: x, y: y, width: cell, height: min(2, height-1-y)},
		})
	}
	return buttons
}

func (m Model) monitorFilterAt(x, y int) string {
	if m.meterView != viewMonitor || m.loading && len(m.snapshot.Meters()) == 0 ||
		m.monitorCommands.open || m.monitorQueue.open || m.scheduleUI.open {
		return ""
	}
	g := m.dashboardLayout()
	width, height := 0, 0
	if m.monitorContextDetail != "" && !m.contextTargetHidden() {
		width = g.contentWidth
		height, _, _ = m.monitorDetailHeader(g.contentWidth, g.meterHeight)
	} else {
		a := m.monitorArea(g.contentWidth, g.meterHeight)
		width, height = a.readoutWidth, a.topHeight
	}
	return monitorNavigationButtonsHit(m.monitorFilterButtons(width, height), x-2, y-g.meterY)
}

func (m Model) monitorFilterTitleStyle(column int, colors palette) lipgloss.Style {
	style := colors.dimmed()
	if column == 0 {
		return style
	}
	filter := monitorStatusFilter(column - 1)
	if m.monitorFilter == filter {
		style = style.Foreground(colors.primary).Bold(true).Underline(true)
		if m.monitorSummaryFocused {
			style = style.Foreground(colors.background).Background(colors.primary)
		}
	}
	if m.monitorContextHover == monitorFilterAction(filter) {
		style = style.Foreground(colors.background).Background(colors.accent)
	}
	return style
}

func (m *Model) focusMonitorSummary() {
	m.stashMonitorDraft()
	m.monitorPrompt = monitorPromptState{}
	m.monitorSelectedID, m.monitorContextExpanded = "", ""
	m.monitorApprovalConfirm, m.monitorApprovalNotice = "", ""
	m.clearQuotaConfirmation()
	m.clearMonitorCloseAllConfirmation()
	m.monitorSummaryFocused = true
}

func (m *Model) setMonitorFilter(filter monitorStatusFilter) {
	m.focusMonitorSummary()
	m.monitorContextDetail = ""
	m.monitorContextScroll, m.monitorScroll = 0, 0
	m.monitorFilter = filter
}

// Arrow navigation belongs to filters only while their strip owns focus.
// Composer/menu handlers run first and retain their normal editing keys.
func (m *Model) monitorFilterKey(key string) bool {
	if !m.monitorSummaryFocused || m.monitorContextDetail != "" {
		return false
	}
	switch key {
	case "left", "right":
		delta := 1
		if key == "left" {
			delta = -1
		}
		m.setMonitorFilter((m.monitorFilter + monitorStatusFilter(delta) + monitorFilterCount) % monitorFilterCount)
	case "down", "enter":
		m.selectMonitorSession(1)
	case "up":
	case "esc":
		m.setMonitorFilter(monitorFilterAll)
	case "home":
		m.setMonitorFilter(monitorFilterAll)
	case "end":
		m.setMonitorFilter(monitorFilterCheck)
	default:
		return false
	}
	return true
}

// Telemetry may move a selected row out of its filter. Retain full-detail
// reviews until the user leaves them; otherwise release stale control targets.
func (m *Model) reconcileMonitorFilterSelection() {
	if m.monitorContextDetail != "" || m.monitorSelectedID == "" {
		return
	}
	for _, s := range m.monitorSessionData {
		if s.id == m.monitorSelectedID && m.monitorSessionInList(s) {
			return
		}
	}
	m.focusMonitorSummary()
}

func (m *Model) activateMonitorFilter(action string) bool {
	for filter := monitorFilterAll; filter < monitorFilterCount; filter++ {
		if strings.EqualFold(action, monitorFilterAction(filter)) {
			m.setMonitorFilter(filter)
			return true
		}
	}
	return false
}

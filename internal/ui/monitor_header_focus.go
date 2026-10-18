package ui

import (
	"strconv"
	"strings"
)

func (m *Model) clearMonitorHeaderFocus() {
	m.monitorSummaryFocused, m.monitorAttentionFocused = false, false
}

func (m Model) visibleMonitorHeader() (summary bool, buttons []monitorNavigationButton) {
	g := m.dashboardLayout()
	if m.monitorContextDetail != "" && !m.contextTargetHidden() {
		height, _, pills := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
		return height > 0, pills
	}
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	return len(m.monitorFilterButtons(a.readoutWidth, a.topHeight)) > 0, a.attention
}

func (m *Model) releaseMonitorHeaderControls() {
	m.stashMonitorDraft()
	m.monitorPrompt.input.Blur()
	m.monitorApprovalConfirm = ""
	m.monitorApprovalNumberReleased = false
	m.clearQuotaConfirmation()
	m.clearMonitorCloseAllConfirmation()
}

func (m *Model) focusMonitorAttention(restore bool) bool {
	_, buttons := m.visibleMonitorHeader()
	action := m.selectedAttentionAction()
	if restore {
		action = m.monitorAttentionCursor
	}
	// Only a new overview entry or explicit page navigation chooses the first
	// pill. Restoring an identity must never retarget a vanished review/pager.
	if action == "" && len(buttons) > 0 {
		action = buttons[0].action
	}
	m.monitorAttentionCursor = action
	for _, b := range buttons {
		if b.action == action {
			m.releaseMonitorHeaderControls()
			m.monitorSummaryFocused, m.monitorAttentionFocused = false, true
			return true
		}
	}
	return false
}

// Unlike choosing a filter, moving focus must not replace the detail being read.
func (m *Model) focusMonitorHeaderSummary() bool {
	summary, _ := m.visibleMonitorHeader()
	if !summary {
		return false
	}
	m.releaseMonitorHeaderControls()
	m.monitorSummaryFocused, m.monitorAttentionFocused = true, false
	return true
}

func (m *Model) returnFromMonitorHeader() {
	m.clearMonitorHeaderFocus()
	if m.monitorContextDetail == "" {
		m.selectMonitorSession(0)
	}
}

// Global footer controls still work, but approval/copy shortcuts belong to the
// detail, never to a merely highlighted navigation pill or summary category.
func (m *Model) monitorHeaderBlocksAction(key string) bool {
	switch key {
	case "q", "ctrl+c", "tab", "shift+tab":
		m.clearMonitorHeaderFocus()
		return false
	case "t", "r", "h", "z", "o":
		return false
	default:
		return true
	}
}

func (m *Model) monitorAttentionKey(key string) bool {
	switch key {
	case "q", "ctrl+c", "tab", "shift+tab", "t", "r", "h", "z", "o":
		return m.monitorHeaderBlocksAction(key)
	}
	_, buttons := m.visibleMonitorHeader()
	index := -1
	for i, b := range buttons {
		if b.action == m.monitorAttentionCursor {
			index = i
			break
		}
	}
	if index < 0 {
		// Disappearance, paging or resizing must not retarget this key to
		// another request. A subsequent navigation key can select a new pill.
		m.clearMonitorHeaderFocus()
		m.focusMonitorHeaderSummary()
		return true
	}
	switch key {
	case "left", "right", "home", "end":
		target := index
		switch key {
		case "left":
			target--
		case "right":
			target++
		case "home":
			target = 0
		case "end":
			target = len(buttons) - 1
		}
		m.monitorAttentionCursor = buttons[min(max(target, 0), len(buttons)-1)].action
	case "up":
		m.focusMonitorHeaderSummary()
	case "down", "esc":
		m.returnFromMonitorHeader()
	case "enter":
		action := m.monitorAttentionCursor
		m.clearMonitorHeaderFocus()
		if page, ok := strings.CutPrefix(action, "attention-next:"); ok {
			m.monitorAttentionPage, _ = strconv.Atoi(page)
			m.monitorAttentionCursor = ""
			m.focusMonitorAttention(true)
		} else {
			m.openMonitorAttention(action)
		}
	case "pgup", "pgdown":
	default:
		return m.monitorHeaderBlocksAction(key)
	}
	return true
}

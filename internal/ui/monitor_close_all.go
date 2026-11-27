package ui

import (
	"maps"
	"slices"
	"time"

	"github.com/merefield/codexometer/internal/confirmation"
)

func (m Model) monitorDismissCandidate(s monitorSession, checksOnly bool) bool {
	if checksOnly {
		if monitorSessionStatus(s) != monitorFilterCheck {
			return false
		}
		if _, pending := m.quotaSessionCandidate(s); pending {
			return false
		}
	}
	return m.monitorSessionInList(s)
}

func (m Model) monitorClearCheckEnabled() bool {
	return m.monitorState == monitorRunning && slices.ContainsFunc(m.monitorSessionData, func(s monitorSession) bool {
		return m.monitorDismissCandidate(s, true)
	})
}

func (m Model) visibleMonitorSessionIDs(checksOnly bool) []string {
	var ids []string
	for _, s := range m.monitorSessionData {
		if m.monitorDismissCandidate(s, checksOnly) {
			ids = append(ids, s.id)
		}
	}
	return ids
}

func (m Model) monitorCloseAllArmed() bool {
	return m.monitorBulkDismissArmed(false)
}

func (m Model) monitorBulkDismissArmed(checksOnly bool) bool {
	return len(m.monitorCloseAllConfirm) > 0 && m.monitorCheckOnly == checksOnly &&
		time.Now().Before(m.monitorCloseAllUntil) &&
		slices.Equal(m.monitorCloseAllConfirm, m.visibleMonitorSessionIDs(checksOnly))
}

func (m *Model) clearMonitorCloseAllConfirmation() {
	m.monitorCloseAllConfirm = nil
	m.monitorCheckOnly = false
	m.monitorCloseAllUntil = time.Time{}
}

func (m *Model) requestMonitorCloseAll() {
	m.requestMonitorBulkDismiss(false)
}

func (m *Model) requestMonitorBulkDismiss(checksOnly bool) {
	if !m.confirmations.Required(confirmation.Narrow) || m.monitorBulkDismissArmed(checksOnly) {
		if checksOnly {
			m.dismissMonitorChecks()
		} else {
			m.dismissAllMonitorSessions()
		}
		return
	}
	m.monitorPrompt.input.Blur()
	m.monitorCloseAllConfirm = m.visibleMonitorSessionIDs(checksOnly)
	m.monitorCheckOnly = checksOnly
	m.monitorCloseAllUntil = time.Now().Add(5 * time.Second)
}

func (m *Model) dismissMonitorChecks() {
	m.clearMonitorCloseAllConfirmation()
	m.monitorDismissed = maps.Clone(m.monitorDismissed)
	for _, s := range m.monitorSessionData {
		if !m.monitorDismissCandidate(s, true) {
			continue
		}
		m.recordMonitorDismissal(s)
		if m.monitorDismissHover == s.id {
			m.monitorDismissHover = ""
		}
		if m.monitorDismissFlash == s.id {
			m.monitorDismissFlash = ""
			m.monitorDismissSeq++
		}
	}
	m.reconcileMonitorFilterSelection()
	m.monitorSessions = m.visibleMonitorSessionCount()
	m.scrollMonitorRows(0)
}

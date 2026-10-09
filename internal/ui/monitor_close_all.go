package ui

import (
	"slices"
	"time"
)

func (m Model) visibleMonitorSessionIDs() []string {
	var ids []string
	for _, s := range m.monitorSessionData {
		if m.monitorSessionVisible(s) {
			ids = append(ids, s.id)
		}
	}
	return ids
}

func (m Model) monitorCloseAllArmed() bool {
	return len(m.monitorCloseAllConfirm) > 0 && time.Now().Before(m.monitorCloseAllUntil) &&
		slices.Equal(m.monitorCloseAllConfirm, m.visibleMonitorSessionIDs())
}

func (m *Model) clearMonitorCloseAllConfirmation() {
	m.monitorCloseAllConfirm = nil
	m.monitorCloseAllUntil = time.Time{}
}

func (m *Model) requestMonitorCloseAll() {
	if m.monitorCloseAllArmed() {
		m.dismissAllMonitorSessions()
		return
	}
	m.monitorPrompt.input.Blur()
	m.monitorCloseAllConfirm = m.visibleMonitorSessionIDs()
	m.monitorCloseAllUntil = time.Now().Add(5 * time.Second)
}

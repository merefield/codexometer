package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type monitorApprovalResult struct {
	sessionID string
	err       error
}

func (m Model) monitorApprovalToken() string {
	if m.monitorContextHidden || m.monitorApprovalBusy {
		return ""
	}
	s, ok := m.contextDetailSession()
	p, supported := m.fetcher.(codex.SessionApprovalClient)
	if !ok || !supported || s.preview.Kind != codex.SessionContextApproval || !p.SessionApprovalPending(s.preview.ApprovalToken) {
		return ""
	}
	return s.preview.ApprovalToken
}

func (m Model) monitorApprovalLabels() (string, string) {
	a := i18n.Text("[ APPROVE ONCE ]")
	if token := m.monitorApprovalToken(); token != "" && m.monitorApprovalConfirm == token {
		a = i18n.Text("[ CONFIRM APPROVAL ]")
	}
	return a, i18n.Text("[ DECLINE ]")
}

func (m Model) monitorApprovalControls(width, height int) bool {
	_, b := m.monitorApprovalLabels()
	return height >= 6 && width-4 >= monitorApprovalSlotWidth()+lipgloss.Width(b)+2 && m.monitorApprovalToken() != ""
}

func monitorApprovalSlotWidth() int {
	return max(lipgloss.Width(i18n.Text("[ APPROVE ONCE ]")), lipgloss.Width(i18n.Text("[ CONFIRM APPROVAL ]")))
}

func (m Model) monitorApprovalAction(action string) (Model, tea.Cmd, bool) {
	token := m.monitorApprovalToken()
	if token == "" {
		m.monitorApprovalConfirm = ""
		return m, nil, true
	}
	if action == "approve" && m.monitorApprovalConfirm != token {
		m.monitorApprovalConfirm = token
		return m, nil, true
	}
	decision := "decline"
	if action == "approve" {
		decision = "accept"
	}
	m.monitorApprovalConfirm = ""
	m.monitorApprovalBusy = true
	m.monitorApprovalNotice = i18n.Text("Sending decision…")
	p := m.fetcher.(codex.SessionApprovalClient)
	id := m.monitorContextDetail
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return monitorApprovalResult{id, p.RespondSessionApproval(ctx, token, decision)}
	}, true
}

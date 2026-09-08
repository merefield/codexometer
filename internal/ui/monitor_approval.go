package ui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type monitorApprovalResult struct {
	sessionID string
	token     string
	err       error
}

func (m Model) monitorApprovalHasOutcome() bool {
	s, ok := m.contextDetailSession()
	return ok && s.preview.Kind == codex.SessionContextApproval && s.preview.ApprovalToken != "" && s.preview.ApprovalToken == m.monitorApprovalNoticeToken && (m.monitorApprovalBusy || m.monitorApprovalNotice != "")
}

func (m Model) monitorApprovalBlockReason(c codex.SessionContext) string {
	if m.monitorApprovalBusy {
		return i18n.Text("Sending decision…")
	}
	if c.Kind == codex.SessionContextQuestion {
		return i18n.Text("Questions must be answered in Codex.")
	}
	if c.Source == "LOCAL" {
		return i18n.Text("Local observation cannot answer live approvals.")
	}
	switch c.ApprovalBlocked {
	case "approval-kind":
		return i18n.Text("This approval action must be handled in Codex.")
	case "network":
		return i18n.Text("Network approval is not supported here.")
	case "permissions":
		return i18n.Text("Additional permissions require approval in Codex.")
	case "file-change":
		return i18n.Text("File-change approval is not supported here.")
	case "command-format":
		return i18n.Text("Unsupported command format.")
	case "missing-command":
		return i18n.Text("Command details are missing.")
	case "missing-directory":
		return i18n.Text("Working directory is missing.")
	case "missing-identity":
		return i18n.Text("Turn or item identity is missing.")
	case "decisions":
		return i18n.Text("No supported approval choices were offered.")
	case "truncated":
		return i18n.Text("Request details exceed the display limit.")
	case "sanitised":
		return i18n.Text("Request text was altered for safe display.")
	}
	if c.ApprovalToken == "" {
		return i18n.Text("No actionable live approval was received.")
	}
	if m.monitorApprovalToken() == "" {
		return i18n.Text("Request resolved, expired, or disconnected.")
	}
	return i18n.Text("Enlarge the terminal to show approval controls.")
}

func (m Model) monitorApprovalToken() string {
	if m.contextTargetHidden() || m.monitorApprovalBusy {
		return ""
	}
	s, ok := m.contextDetailSession()
	p, supported := m.fetcher.(codex.SessionApprovalClient)
	if !ok || !supported || s.preview.Kind != codex.SessionContextApproval || !p.SessionApprovalPending(s.preview.ApprovalToken) {
		return ""
	}
	return s.preview.ApprovalToken
}

func approvalOptionLabel(kind string, confirm bool) string {
	switch kind {
	case "accept":
		if confirm {
			return i18n.Text("[ CONFIRM APPROVAL ]")
		}
		return i18n.Text("[ APPROVE ONCE ]")
	case "acceptForSession":
		if confirm {
			return i18n.Text("[ CONFIRM SESSION GRANT ]")
		}
		return i18n.Text("[ ALLOW FOR SESSION ]")
	case "acceptWithExecpolicyAmendment":
		if confirm {
			return i18n.Text("[ CONFIRM PERSISTENT RULE ]")
		}
		return i18n.Text("[ ALWAYS ALLOW PREFIX ]")
	case "decline":
		return i18n.Text("[ DECLINE ]")
	case "cancel":
		return i18n.Text("[ REJECT & STOP TURN ]")
	}
	return ""
}

type monitorApprovalButton struct {
	action, label string
	x, y, slot    int
}

func approvalShortcutLabel(kind string, confirm bool, index int) string {
	label := approvalOptionLabel(kind, confirm)
	if label == "" {
		return ""
	}
	shortcut := strconv.Itoa(index + 1)
	if confirm {
		shortcut = "C"
	}
	return "[ (" + shortcut + ") " + strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(label, "["), "]")) + " ]"
}

// Rendering and hit testing share this layout. Slots reserve confirmation
// widths so neighbouring decisions never move underneath a user's pointer.
func (m Model) monitorApprovalButtons(width, height int) []monitorApprovalButton {
	token := m.monitorApprovalToken()
	if token == "" {
		return nil
	}
	s, ok := m.contextDetailSession()
	if !ok {
		return nil
	}
	var buttons []monitorApprovalButton
	x, y := 0, 0
	for i, option := range s.preview.ApprovalOptions {
		if option.Kind == "" {
			continue
		}
		label := approvalShortcutLabel(option.Kind, false, i)
		slot := max(lipgloss.Width(label), lipgloss.Width(approvalShortcutLabel(option.Kind, true, i)))
		if slot > width-4 || label == "" {
			return nil
		}
		if x+slot > width-4 {
			x = 0
			y++
		}
		if y+1 > height-5 {
			return nil
		}
		action := "decision:" + strconv.Itoa(i)
		if m.monitorApprovalConfirm == token+"/"+action {
			label = approvalShortcutLabel(option.Kind, true, i)
		}
		buttons = append(buttons, monitorApprovalButton{action, label, x, y, slot})
		x += slot + 2
	}
	return buttons
}

// Shortcuts are enabled only for the buttons actually visible on the sole
// expanded/detail target, never for compact rows or clipped inline controls.
func (m Model) visibleMonitorApprovalButtons() []monitorApprovalButton {
	if m.meterView != viewMonitor || m.contextTargetHidden() || m.monitorPrompt.input.Focused() {
		return nil
	}
	g := m.dashboardLayout()
	if m.monitorContextDetail != "" {
		return m.monitorApprovalButtons(g.contentWidth, g.meterHeight)
	}
	if m.monitorContextExpanded == "" || m.monitorSelectedID != "" && m.monitorSelectedID != m.monitorContextExpanded {
		return nil
	}
	a := layoutMonitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	for i, s := range sessions {
		if s.id == m.monitorContextExpanded {
			_, cw, _ := monitorSessionColumnWidths(a.width)
			return m.expandedApprovalButtons(cw, heights[i], s)
		}
	}
	return nil
}

func (m Model) updateMonitorApprovalKey(key string) (Model, tea.Cmd, bool) {
	if key != "c" && (len(key) != 1 || key[0] < '1' || key[0] > '8') {
		return m, nil, false
	}
	buttons := m.visibleMonitorApprovalButtons()
	token := m.monitorApprovalToken()
	for _, b := range buttons {
		if key == "c" {
			if m.monitorApprovalConfirm == token+"/"+b.action {
				return m.monitorApprovalAction(b.action)
			}
		} else if b.action == "decision:"+strconv.Itoa(int(key[0]-'1')) {
			// Repeating a number must never turn selection into a grant. Only C
			// or the explicitly relabelled confirmation button can confirm it.
			m.monitorApprovalConfirm = ""
			return m.monitorApprovalAction(b.action)
		}
	}
	return m, nil, true
}

func (m Model) monitorApprovalControls(width, height int) bool {
	return len(m.monitorApprovalButtons(width, height)) > 0
}

func (m Model) monitorApprovalControlRows(width, height int) int {
	b := m.monitorApprovalButtons(width, height)
	if len(b) == 0 {
		return 0
	}
	return b[len(b)-1].y + 1
}

func (m Model) renderMonitorApprovalControls(width, height int, colors palette) string {
	buttons := m.monitorApprovalButtons(width, height)
	if len(buttons) == 0 {
		return ""
	}
	rows := make([]string, buttons[len(buttons)-1].y+1)
	for _, b := range buttons {
		rows[b.y] += strings.Repeat(" ", max(b.x-lipgloss.Width(rows[b.y]), 0)) + m.renderContextAction(b.action, b.label, colors)
	}
	return strings.Join(rows, "\n")
}

func (m Model) monitorApprovalAction(action string) (Model, tea.Cmd, bool) {
	token := m.monitorApprovalToken()
	if token == "" {
		m.monitorApprovalConfirm = ""
		return m, nil, true
	}
	index, err := strconv.Atoi(strings.TrimPrefix(action, "decision:"))
	s, ok := m.contextDetailSession()
	if err != nil || !strings.HasPrefix(action, "decision:") || !ok || index < 0 || index >= len(s.preview.ApprovalOptions) {
		return m, nil, true
	}
	option := s.preview.ApprovalOptions[index]
	if option.Kind == "" {
		return m, nil, true
	}
	if option.GrantsPermission() && m.monitorApprovalConfirm != token+"/"+action {
		m.monitorApprovalConfirm = token + "/" + action
		return m, nil, true
	}
	decision := option.Value
	m.monitorApprovalConfirm = ""
	m.monitorApprovalBusy = true
	m.monitorApprovalNotice = i18n.Text("Sending decision…")
	m.monitorDetailSent = detailSentState{}
	m.monitorApprovalNoticeToken = token
	p := m.fetcher.(codex.SessionApprovalClient)
	id := m.monitorContextTarget()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return monitorApprovalResult{id, token, p.RespondSessionApproval(ctx, token, decision)}
	}, true
}

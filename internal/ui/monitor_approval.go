package ui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
	"github.com/merefield/codexometer/internal/i18n"
)

type monitorApprovalResult struct {
	sessionID string
	token     string
	err       error
}

const monitorApprovalConfirmDuration = 5 * time.Second

func (m Model) approvalConfirmationActive(token, action string) bool {
	return token != "" && m.monitorApprovalConfirm == token+"/"+action && time.Now().Before(m.monitorApprovalConfirmUntil)
}

func (m Model) monitorApprovalHasOutcome() bool {
	s, ok := m.contextDetailSession()
	return ok && !m.hasSessionProfile(s) && s.preview.Kind == codex.SessionContextApproval && s.preview.ApprovalToken != "" && s.preview.ApprovalToken == m.monitorApprovalNoticeToken && (m.monitorApprovalBusy || m.monitorApprovalNotice != "")
}

var approvalBlockMessages = map[string]string{
	"mcp":               "Unsupported MCP request. Review it in Codex.",
	"approval-kind":     "This approval action must be handled in Codex.",
	"network":           "Network approval is not supported here.",
	"permissions":       "Additional permissions require approval in Codex.",
	"file-change":       "Complete file changes unavailable. Review in Codex.",
	"command-format":    "Unsupported command format.",
	"missing-command":   "Command details are missing.",
	"missing-directory": "Working directory is missing.",
	"missing-identity":  "Turn or item identity is missing.",
	"decisions":         "No supported approval choices were offered.",
	"truncated":         "Request details exceed the display limit.",
	"sanitised":         "Request text was altered for safe display.",
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
	if message := approvalBlockMessages[c.ApprovalBlocked]; message != "" {
		return i18n.Text(message)
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
	if !ok || !supported || m.hasSessionProfile(s) || s.preview.Kind != codex.SessionContextApproval || !p.SessionApprovalPending(s.preview.ApprovalToken) {
		return ""
	}
	return s.preview.ApprovalToken
}

var approvalOptionLabels = map[string][2]string{
	"accept":                        {"[ APPROVE ONCE ]", "[ CONFIRM APPROVAL ]"},
	"acceptForSession":              {"[ ALLOW FOR SESSION ]", "[ CONFIRM SESSION GRANT ]"},
	"acceptWithExecpolicyAmendment": {"[ ALWAYS ALLOW PREFIX ]", "[ CONFIRM PERSISTENT RULE ]"},
	"allowNetworkHost":              {"[ ALWAYS ALLOW HOST ]", "[ CONFIRM HOST ALLOW ]"},
	"denyNetworkHost":               {"[ ALWAYS BLOCK HOST ]", "[ CONFIRM HOST BLOCK ]"},
	"acceptMcpAlways":               {"[ ALWAYS ALLOW TOOL ]", "[ CONFIRM TOOL RULE ]"},
	"cancelMcpTool":                 {"[ CANCEL TOOL CALL ]", "[ CANCEL TOOL CALL ]"},
	"decline":                       {"[ DECLINE ]", "[ DECLINE ]"},
	"cancel":                        {"[ REJECT & STOP TURN ]", "[ REJECT & STOP TURN ]"},
}

func approvalOptionLabel(kind string, confirm bool) string {
	labels, known := approvalOptionLabels[kind]
	if !known {
		return ""
	}
	index := 0
	if confirm {
		index = 1
	}
	return i18n.Text(labels[index])
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
	if confirm && (kind == "decline" || kind == "cancel" || kind == "cancelMcpTool") {
		return i18n.Text("[ C CONFIRM ]") + " " + label
	}
	shortcut := strconv.Itoa(index + 1)
	if confirm {
		shortcut = "C"
	}
	return "[ (" + shortcut + ") " + strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(label, "["), "]")) + " ]"
}

func (m Model) approvalButtonLabel(kind string, confirm bool, index int) string {
	label := approvalShortcutLabel(kind, confirm, index)
	if confirm && kind == "accept" && m.keyboardEventTypes {
		label = strings.Replace(label, "(C)", "("+strconv.Itoa(index+1)+")", 1)
	}
	if m.confirmations.Required(confirmation.Approval(kind)) {
		width := max(lipgloss.Width(approvalShortcutLabel(kind, false, index)), lipgloss.Width(approvalShortcutLabel(kind, true, index)))
		if padding := width - lipgloss.Width(label); padding > 0 {
			// Keep reserved confirmation space inside the painted brackets,
			// so visible gaps and neighbouring button positions stay constant.
			label = strings.TrimSuffix(label, " ]") + strings.Repeat(" ", padding) + " ]"
		}
	}
	return label
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
		label := m.approvalButtonLabel(option.Kind, false, i)
		slot := lipgloss.Width(label)
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
		if m.approvalConfirmationActive(token, action) {
			label = m.approvalButtonLabel(option.Kind, true, i)
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
	g := m.monitorDashboardLayout()
	if m.monitorContextDetail != "" {
		return m.monitorApprovalButtons(g.contentWidth, g.meterHeight)
	}
	if m.monitorContextExpanded == "" || m.monitorSelectedID != "" && m.monitorSelectedID != m.monitorContextExpanded {
		return nil
	}
	a := m.monitorArea(g.contentWidth, g.meterHeight)
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
			if m.approvalConfirmationActive(token, b.action) {
				return m.monitorApprovalAction(b.action)
			}
		} else if b.action == "decision:"+strconv.Itoa(int(key[0]-'1')) {
			s, ok := m.contextDetailSession()
			index := int(key[0] - '1')
			if !ok || s.preview.ApprovalOptions[index].Kind != "accept" || !m.keyboardEventTypes || !m.monitorApprovalNumberReleased {
				m.monitorApprovalConfirm = ""
			}
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
	return renderMonitorButtonRows(m.monitorApprovalButtons(width, height), colors, m.renderMonitorApprovalAction)
}

func renderMonitorButtonRows(buttons []monitorApprovalButton, colors palette, render func(string, string, palette) string) string {
	if len(buttons) == 0 {
		return ""
	}
	rows := make([]string, buttons[len(buttons)-1].y+1)
	for _, b := range buttons {
		rows[b.y] += strings.Repeat(" ", max(b.x-lipgloss.Width(rows[b.y]), 0)) + render(b.action, b.label, colors)
	}
	return strings.Join(rows, "\n")
}

// monitorButtonAt uses frame-local coordinates and the same positioned labels
// as rendering. Reserved confirmation-slot padding is deliberately not active.
func monitorButtonAt(buttons []monitorApprovalButton, height, x, y int) string {
	if len(buttons) == 0 {
		return ""
	}
	_, _, controlY := monitorContextBodyLayout(height, buttons[len(buttons)-1].y+1)
	for _, b := range buttons {
		if y == controlY+b.y && x >= 2+b.x && x < 2+b.x+lipgloss.Width(b.label) {
			return b.action
		}
	}
	return ""
}

// Decision controls keep a solid fill even before hover, so they remain
// discoverable independently of the terminal profile's default background.
func (m Model) renderMonitorApprovalAction(action, label string, colors palette) string {
	background := colors.primary
	if m.approvalConfirmationActive(m.monitorApprovalToken(), action) {
		background = colors.warning
	}
	if m.monitorContextHover == action {
		background = colors.accent
	}
	return colors.label().Foreground(colors.background).Background(background).Render(label)
}

func (m Model) monitorApprovalAction(action string) (Model, tea.Cmd, bool) {
	m.showLiveMonitorHistory()
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
	if m.confirmations.Required(confirmation.Approval(option.Kind)) && !m.approvalConfirmationActive(token, action) {
		m.monitorApprovalConfirm = token + "/" + action
		m.monitorApprovalConfirmUntil = time.Now().Add(monitorApprovalConfirmDuration)
		m.monitorApprovalNumberReleased = false
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

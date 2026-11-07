package ui

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
	"github.com/merefield/codexometer/internal/i18n"
)

// A strict review holds no runnable closure. Confirmation re-enters the normal
// action path, which rechecks the target, exact payload and live capability.
type monitorActionReview struct {
	action, target, title, text string
	signature                   [32]byte
	until                       time.Time
	scroll                      int
	approved                    bool
}

func (m *Model) reviewMonitorAction(action, title, text string, payload any) bool {
	if !m.confirmations.Required(confirmation.Narrow) {
		return false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	signature := sha256.Sum256(encoded)
	target := m.monitorContextTarget()
	if r := m.actionReview; r != nil && r.approved && r.action == action && r.target == target && r.signature == signature && time.Now().Before(r.until) {
		m.actionReview = nil
		return false
	}
	m.actionReview = &monitorActionReview{action: action, target: target, title: title, text: text, signature: signature, until: time.Now().Add(30 * time.Second)}
	return true
}

func (m Model) actionReviewButtons() []monitorNavigationButton {
	g := m.monitorDashboardLayout()
	_, _, y := monitorContextBodyLayout(g.meterHeight, 1)
	labels := []string{i18n.Text("[ C CONFIRM ]"), i18n.Text("[ ESC CANCEL ]")}
	x := 2
	var buttons []monitorNavigationButton
	for i, label := range labels {
		width := lipgloss.Width(label)
		if x+width > g.contentWidth-2 || y >= g.meterHeight-1 {
			return nil
		}
		buttons = append(buttons, monitorNavigationButton{label: label, action: []string{"confirm", "cancel"}[i], enabled: true, rect: monitorRect{x: x, y: y, width: width, height: 1}})
		x += width + 2
	}
	return buttons
}

func (m Model) renderActionReview(width, height int, colors palette) string {
	r := m.actionReview
	rows, gap, _ := monitorContextBodyLayout(height, 1)
	lines := sessionWebLiteralTextLines(codex.SanitizeApprovalText(r.target+"\n\n"+r.text), max(width-4, 1))
	start := min(r.scroll, max(len(lines)-rows, 0))
	body := append([]string(nil), lines[start:min(start+rows, len(lines))]...)
	for len(body) < rows+gap {
		body = append(body, "")
	}
	var controls []string
	for _, button := range m.actionReviewButtons() {
		controls = append(controls, colors.label().Render(button.label))
	}
	body = append(body, strings.Join(controls, "  "))
	return frameSizedWithActions(width, max(height-2, 1), r.title, "", "", strings.Join(body, "\n"), colors.primary, colors)
}

func (m Model) updateActionReview(msg tea.Msg) (Model, tea.Cmd, bool) {
	if m.actionReview == nil {
		return m, nil, false
	}
	if m.meterView != viewMonitor || m.actionReview.target != m.monitorContextTarget() || !time.Now().Before(m.actionReview.until) {
		m.actionReview = nil
		switch msg.(type) {
		case tea.KeyPressMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.PasteMsg:
			return m, nil, true
		}
		return m, nil, false
	}
	// Copy before changing an immutable Model snapshot's review state.
	review := *m.actionReview
	m.actionReview = &review
	action := ""
	switch event := msg.(type) {
	case tea.KeyPressMsg:
		if event.IsRepeat {
			return m, nil, true
		}
		switch event.String() {
		case "c":
			action = "confirm"
		case "esc":
			action = "cancel"
		case "up", "pgup":
			review.scroll = max(review.scroll-3, 0)
		case "down", "pgdown":
			review.scroll++
		}
	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft {
			return m, nil, true
		}
		g := m.monitorDashboardLayout()
		action = monitorNavigationButtonsHit(m.actionReviewButtons(), event.X-2, event.Y-g.meterY)
	case tea.MouseWheelMsg:
		if event.Button == tea.MouseWheelUp {
			review.scroll = max(review.scroll-3, 0)
		} else {
			review.scroll += 3
		}
	case tea.MouseMotionMsg, tea.PasteMsg:
		return m, nil, true
	default:
		return m, nil, false // Telemetry and action responses keep flowing.
	}
	if action == "cancel" {
		m.actionReview = nil
		return m, nil, true
	}
	if action != "confirm" || len(m.actionReviewButtons()) == 0 {
		return m, nil, true
	}
	if m.err != nil || m.monitorError != "" {
		m.actionReview = nil
		return m, nil, true
	}
	review.approved = true
	var next Model
	var cmd tea.Cmd
	switch review.action {
	case "reply":
		next, cmd, _ = m.submitMonitorPrompt()
	case "steer", "queue", "interrupt":
		next, cmd, _ = m.submitMonitorTurn(review.action)
	case "queue-edit":
		next, cmd, _ = m.changeQueueItem()
	case "profile-skip":
		next, cmd, _ = m.profileAction("profile-skip:"+review.target, false)
	case "trigger-delete":
		cmd = m.triggerAction("ctrl+d")
		next = m
	default:
		m.actionReview = nil
		return m, nil, true
	}
	if next.actionReview != nil && next.actionReview.approved {
		next.actionReview = nil
	}
	return next, cmd, true
}

// Ordinary sends and edits are narrow actions. Permission grants, scheduling,
// queue deletion and other guarded workflows retain their own review screens.
func (m *Model) reviewPromptAction(action string, payload any, text string) bool {
	title := i18n.Text("REPLY")
	if action != "reply" {
		title += " // " + strings.ToUpper(action)
	}
	if m.monitorPrompt.input.secret {
		text = "••••"
	}
	return m.reviewMonitorAction(action, title, text, payload)
}

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
	action, target, destination, title, text string
	signature                                [32]byte
	until                                    time.Time
	scroll                                   int
	approved                                 bool
}

func (m *Model) reviewMonitorAction(action, destination, title, text string, payload any) bool {
	if !m.confirmations.Required(confirmation.Narrow) {
		return false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	signature := sha256.Sum256(encoded)
	target := m.monitorContextTarget()
	if r := m.actionReview; r != nil && r.approved && r.action == action && r.target == target && r.destination == destination && r.signature == signature && time.Now().Before(r.until) {
		m.actionReview = nil
		return false
	}
	m.actionReview = &monitorActionReview{action: action, target: target, destination: destination, title: title, text: text, signature: signature, until: time.Now().Add(30 * time.Second)}
	return true
}

// Controls and the document share one layout, including translated wrapping.
func layoutActionReview(width, height int) (buttons []monitorNavigationButton, rows, gap int) {
	labels := []string{i18n.Text("[ C CONFIRM ]"), i18n.Text("[ ESC CANCEL ]")}
	x, y := 2, 0
	for i, label := range labels {
		buttonWidth := lipgloss.Width(label)
		if buttonWidth > width-4 {
			return nil, max(height-2, 1), 0
		}
		if x+buttonWidth > width-2 {
			x, y = 2, y+1
		}
		buttons = append(buttons, monitorNavigationButton{label: label, action: []string{"confirm", "cancel"}[i], enabled: true, rect: monitorRect{x: x, y: y, width: buttonWidth, height: 1}})
		x += buttonWidth + 2
	}
	rows, gap, top := monitorContextBodyLayout(height, y+1)
	if rows < 1 {
		return nil, max(height-2, 1), 0
	}
	for i := range buttons {
		buttons[i].rect.y += top
	}
	return buttons, rows, gap
}

func (m Model) actionReviewButtons() []monitorNavigationButton {
	g := m.monitorDashboardLayout()
	buttons, _, _ := layoutActionReview(g.contentWidth, g.meterHeight)
	return buttons
}

func (m Model) renderActionReview(width, height int, colors palette) string {
	r := m.actionReview
	buttons, rows, gap := layoutActionReview(width, height)
	lines := sessionWebLiteralTextLines(codex.SanitizeApprovalText(r.destination+"\n\n"+r.text), max(width-4, 1))
	start := min(r.scroll, max(len(lines)-rows, 0))
	body := append([]string(nil), lines[start:min(start+rows, len(lines))]...)
	for len(body) < rows+gap {
		body = append(body, "")
	}
	if len(buttons) > 0 {
		top := buttons[0].rect.y
		controls := make([]string, buttons[len(buttons)-1].rect.y-top+1)
		for _, button := range buttons {
			row := button.rect.y - top
			controls[row] += strings.Repeat(" ", max(button.rect.x-2-lipgloss.Width(controls[row]), 0)) + colors.label().Render(button.label)
		}
		body = append(body, controls...)
	}
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
		next, cmd, _ = m.profileAction("profile-skip:"+review.destination, false)
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
func (m *Model) reviewPromptAction(action, destination string, payload any, text string) bool {
	title := i18n.Text("REPLY")
	if action != "reply" {
		title += " // " + strings.ToUpper(action)
	}
	if m.monitorPrompt.input.secret {
		text = "••••"
	}
	return m.reviewMonitorAction(action, destination, title, text, payload)
}

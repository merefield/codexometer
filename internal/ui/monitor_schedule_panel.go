package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
)

func (m Model) pendingTrigger() (schedule.Job, bool) {
	if m.monitorContextDetail == "" || m.scheduleUI.queue == nil {
		return schedule.Job{}, false
	}
	for _, j := range m.scheduleUI.queue.List(m.monitorContextDetail) {
		if j.Status != "sent" {
			return j, true
		}
	}
	return schedule.Job{}, false
}

func (m Model) triggerReady(j schedule.Job) bool {
	if !j.MatchesAccount(m.snapshot.AccountFingerprint) {
		return false
	}
	o := m.monitorPromptOffer()
	return j.Status == "pending" && m.err == nil && m.monitorError == "" && m.quota.busySession == "" && schedule.QuotaReady(m.snapshot, time.Now()) && schedule.Covers(j, m.snapshot) && o.Token != "" && o.ThreadID == j.Session && len(o.Questions) == 0
}

func (m Model) triggerSummary(j schedule.Job) string {
	return m.triggerSummaryAt(j, time.Now())
}

func (m Model) triggerSummaryAt(j schedule.Job, now time.Time) string {
	text := "After quota recovery"
	if j.Trigger == "at" {
		text = "Not before " + j.At.Local().Format("02 Jan 2006 15:04 MST -07:00")
		remaining := j.At.Sub(now)
		if remaining > 0 {
			text += " // Due in " + remaining.Round(time.Second).String()
			if j.Status == "pending" {
				return text
			}
		} else if j.Status == "pending" {
			text += " // Time reached"
		}
	}
	switch {
	case j.Status != "pending":
		return text + " // " + j.Status
	case m.err != nil || !schedule.Fresh(m.snapshot, now):
		return text + " // waiting for fresh quota"
	case !j.MatchesAccount(m.snapshot.AccountFingerprint):
		return text + " // waiting for the original account"
	case !schedule.Covers(j, m.snapshot):
		return text + " // waiting for observed quota windows"
	case !schedule.QuotaReady(m.snapshot, now):
		return text + " // waiting for quota"
	case !m.triggerReady(j):
		if i := m.monitorSessionIndex(j.Session); i >= 0 && m.monitorSessionData[i].working {
			return text + " // waiting for session to finish"
		}
		return text + " // waiting for an eligible idle session"
	}
	return text + " // ready; awaiting dispatch"
}

type triggerButton struct {
	text, key string
	x, y      int
	enabled   bool
}

func (m Model) triggerButtons(width int) []triggerButton {
	j, ok := m.pendingTrigger()
	if !ok {
		return nil
	}
	buttons := []triggerButton{{text: "[ Ctrl+N: SEND NOW ]", key: "ctrl+n", enabled: m.triggerReady(j)}, {text: "[ Ctrl+S: EDIT ]", key: "ctrl+s", enabled: j.Status == "pending"}, {text: "[ Ctrl+D: DELETE ]", key: "ctrl+d", enabled: j.Status != "sending"}}
	if m.scheduleUI.confirmID == j.ID && time.Now().Before(m.scheduleUI.confirmUntil) {
		buttons = []triggerButton{{text: "[ Ctrl+Y: CONFIRM SEND ]", key: "ctrl+y", enabled: m.triggerReady(j)}, {text: "[ Esc: CANCEL ]", key: "esc", enabled: true}}
	}
	x, y := 0, 0
	for i := range buttons {
		w := ansi.StringWidth(buttons[i].text)
		if x > 0 && x+w > max(width-4, 1) {
			x = 0
			y++
		}
		buttons[i].x = x
		buttons[i].y = y
		x += w + 2
	}
	return buttons
}
func (m Model) schedulePanelLines(width int) []string {
	buttons := m.triggerButtons(width)
	if len(buttons) == 0 {
		return nil
	}
	c := paletteFor(m.theme)
	lines := make([]string, buttons[len(buttons)-1].y+1)
	for _, b := range buttons {
		style := c.label().Foreground(c.primary)
		if !b.enabled {
			style = c.dimmed()
		}
		if lines[b.y] != "" {
			lines[b.y] += "  "
		}
		lines[b.y] += style.Render(b.text)
	}
	if m.scheduleUI.confirmID != "" && time.Now().Before(m.scheduleUI.confirmUntil) {
		lines = append(lines, "Send this saved prompt now? This starts Codex work.")
	}
	if m.scheduleUI.notice != "" {
		lines = append(lines, m.scheduleUI.notice)
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(width-4, 1), "…")
	}
	return lines
}

func (m *Model) triggerAction(key string) tea.Cmd {
	j, ok := m.pendingTrigger()
	if !ok {
		return nil
	}
	p := &m.scheduleUI
	switch key {
	case "ctrl+s":
		return m.openSchedule()
	case "esc":
		p.confirmID = ""
	case "ctrl+d":
		if err := p.queue.Cancel(j.Session, j.ID); err != nil {
			p.notice = err.Error()
		} else {
			p.confirmID = ""
			p.notice = ""
		}
	case "ctrl+n":
		if m.triggerReady(j) {
			p.confirmID = j.ID
			p.confirmToken = m.monitorPromptOffer().Token
			p.confirmUntil = time.Now().Add(20 * time.Second)
			p.notice = ""
		}
	case "ctrl+y":
		if p.confirmID != j.ID || !time.Now().Before(p.confirmUntil) || !m.triggerReady(j) || p.confirmToken != m.monitorPromptOffer().Token {
			p.confirmID = ""
			return nil
		}
		client, ok := m.fetcher.(codex.SessionPromptClient)
		if !ok {
			return nil
		}
		token, account, q := p.confirmToken, m.snapshot.AccountFingerprint, p.queue
		p.confirmID = ""
		p.polling = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			err := q.DispatchNow(ctx, j.ID, account, time.Now(), func(ctx context.Context, j schedule.Job) error {
				return client.SendSessionPrompt(ctx, token, []string{j.Text})
			})
			return scheduleDone{err: err}
		}
	}
	return nil
}

func (m Model) schedulePanelKey(msg tea.Msg) (Model, tea.Cmd, bool) {
	if m.monitorContextDetail == "" || !m.hasSchedule(m.monitorContextDetail) {
		return m, nil, false
	}
	g := m.monitorDashboardLayout()
	if m.layoutDetailControls(g.contentWidth, g.meterHeight).kind != "schedule" {
		return m, nil, false
	}
	key := ""
	if k, ok := msg.(tea.KeyPressMsg); ok {
		key = k.String()
	}
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		_, _, y := monitorContextBodyLayout(g.meterHeight, len(m.schedulePanelLines(g.contentWidth)))
		for _, b := range m.triggerButtons(g.contentWidth) {
			if click.Y == g.meterY+y+b.y && click.X >= 4+b.x && click.X < 4+b.x+ansi.StringWidth(b.text) {
				if b.enabled {
					key = b.key
				}
				cmd := m.triggerAction(key)
				return m, cmd, true
			}
		}
	}
	if strings.Contains("|ctrl+n|ctrl+d|ctrl+y|", "|"+key+"|") || (key == "esc" && m.scheduleUI.confirmID != "") {
		cmd := m.triggerAction(key)
		return m, cmd, true
	}
	return m, nil, false
}

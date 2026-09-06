package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type monitorPromptState struct {
	session  string
	offer    codex.SessionPromptOffer
	input    textinput.Model
	answers  [3]string
	question int
	choice   int
	busy     bool
	notice   string
}

type monitorPromptResult struct {
	session, token string
	err            error
}

func (m Model) monitorPromptOffer() codex.SessionPromptOffer {
	if m.meterView != viewMonitor || m.monitorContextDetail == "" || m.monitorContextHidden {
		return codex.SessionPromptOffer{}
	}
	c, ok := m.fetcher.(codex.SessionPromptClient)
	if !ok {
		return codex.SessionPromptOffer{}
	}
	s, ok := m.contextDetailSession()
	if !ok {
		return codex.SessionPromptOffer{}
	}
	if s.preview.Kind == codex.SessionContextApproval {
		return codex.SessionPromptOffer{}
	}
	thread := s.id
	// A question belongs to its exact child/root source. Ordinary follow-ups
	// belong to the displayed root CLI session, not a child's last reply.
	if s.preview.Kind == codex.SessionContextQuestion && s.preview.ThreadID != "" {
		thread = s.preview.ThreadID
	}
	o := c.SessionPrompt(thread)
	if len(o.Questions) > 0 && (s.preview.Kind != codex.SessionContextQuestion || s.preview.InputToken != o.Token) {
		return codex.SessionPromptOffer{}
	}
	if s.preview.Kind == codex.SessionContextQuestion && len(o.Questions) == 0 {
		return codex.SessionPromptOffer{}
	}
	return o
}

func (m Model) monitorPromptRows(width, height int) int {
	if width < 24 || height < 8 || m.monitorContextDetail == "" || m.monitorContextHidden {
		return 0
	}
	if s, ok := m.contextDetailSession(); ok && s.preview.Kind == codex.SessionContextApproval {
		return 0
	}
	if m.monitorPromptOffer().Token != "" || m.monitorPrompt.session == m.monitorContextDetail && (m.monitorPrompt.busy || m.monitorPrompt.notice != "") {
		return 3
	}
	return 0
}

func (m *Model) focusMonitorPrompt() tea.Cmd {
	o := m.monitorPromptOffer()
	if o.Token == "" || m.monitorPrompt.busy {
		return nil
	}
	if m.monitorPrompt.offer.Token != o.Token {
		m.monitorPrompt = monitorPromptState{session: m.monitorContextDetail, offer: o, input: textinput.New(), choice: -1}
		m.monitorPrompt.input.CharLimit = 4096
	}
	m.monitorPrompt.notice = ""
	return m.monitorPrompt.input.Focus()
}

func (m Model) renderMonitorPrompt(width int, colors palette) string {
	p := m.monitorPrompt
	o := m.monitorPromptOffer()
	header := i18n.Text("FOLLOW-UP") + " // " + o.ThreadID
	if len(o.Questions) > 0 {
		n := 0
		if p.offer.Token == o.Token {
			n = p.question
		}
		n = min(n, len(o.Questions)-1)
		header = fmt.Sprintf("%s %d/%d // %s", i18n.Text("REPLY"), n+1, len(o.Questions), o.Questions[n].Text)
	}
	line := i18n.Text("[ Click here or press Enter to write ]")
	hint := i18n.Text("Enter: send / next answer • Esc: leave editor • ↑/↓: choices")
	if p.offer.Token == o.Token && p.input.Focused() && !p.busy {
		input := p.input
		input.SetWidth(max(width-6, 1))
		input.EchoMode = textinput.EchoNormal
		if len(o.Questions) > 0 && o.Questions[p.question].Secret {
			input.EchoMode = textinput.EchoPassword
		}
		styles := input.Styles()
		styles.Focused.Text = colors.label()
		styles.Focused.Prompt = colors.label().Foreground(colors.primary)
		styles.Focused.Placeholder = colors.label()
		styles.Cursor.Color = colors.primary
		input.SetStyles(styles)
		line = input.View()
	}
	if p.busy {
		line = i18n.Text("Sending…")
	}
	if p.notice != "" {
		hint = p.notice
	}
	return colors.label().Render(ansi.Truncate(header, max(width-4, 1), "…")) + "\n" + ansi.Truncate(line, max(width-4, 1), "") + "\n" + colors.label().Render(ansi.Truncate(hint, max(width-4, 1), "…"))
}

func (m Model) updateMonitorPrompt(msg tea.Msg) (Model, tea.Cmd, bool) {
	p := &m.monitorPrompt
	if result, ok := msg.(monitorPromptResult); ok {
		if p.session == result.session && p.offer.Token == result.token {
			p.busy = false
			p.input.Reset()
			p.input.Blur()
			p.answers = [3]string{}
			p.notice = i18n.Text("Text sent; check Codex for the outcome.")
			if result.err != nil {
				p.notice = i18n.Text("Send unconfirmed; check Codex before retrying.")
			}
			p.offer = codex.SessionPromptOffer{}
		}
		return m, nil, true
	}
	wasFocused := p.input.Focused()
	g := m.dashboardLayout()
	if wasFocused && m.monitorPromptRows(g.contentWidth, g.meterHeight) == 0 {
		p.input.Blur()
	}
	if p.session != "" && (m.meterView != viewMonitor || m.monitorContextDetail != p.session || m.monitorContextHidden) {
		*p = monitorPromptState{}
	} else if p.offer.Token != "" && !p.busy && m.monitorPromptOffer().Token != p.offer.Token {
		*p = monitorPromptState{session: p.session, notice: i18n.Text("Prompt changed; review the session before replying.")}
	}
	if wasFocused && !p.input.Focused() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg:
			return m, nil, true
		}
	}
	if !p.input.Focused() || p.busy {
		return m, nil, false
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "ctrl+c":
			p.input.Blur()
			return m, nil, true
		case "enter":
			return m.submitMonitorPrompt()
		case "up", "down":
			if len(p.offer.Questions) > 0 {
				options := p.offer.Questions[p.question].Options
				if len(options) > 0 {
					if key.String() == "down" {
						p.choice = (p.choice + 1) % len(options)
					} else {
						p.choice = (p.choice - 1 + len(options)) % len(options)
					}
					p.input.SetValue(options[p.choice])
					p.input.CursorEnd()
				}
			}
			return m, nil, true
		}
	}
	// Only editor messages are consumed. Telemetry and mouse updates must still
	// reach the dashboard while composing. Paste is never interpreted as Enter.
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg:
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return m, cmd, true
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if cmd != nil || before != p.input.Value() {
		return m, cmd, true
	}
	return m, nil, false
}

func (m Model) submitMonitorPrompt() (Model, tea.Cmd, bool) {
	p := &m.monitorPrompt
	if p.busy || p.offer.Token == "" || m.monitorPromptOffer().Token != p.offer.Token {
		return m, nil, true
	}
	value := strings.TrimSpace(p.input.Value())
	if value == "" {
		return m, nil, true
	}
	if len(p.offer.Questions) > 0 {
		q := p.offer.Questions[p.question]
		if !q.FreeText {
			found := false
			for _, o := range q.Options {
				if value == o {
					found = true
				}
			}
			if !found {
				p.notice = i18n.Text("Choose an offered answer with ↑/↓.")
				return m, nil, true
			}
		}
	}
	p.answers[p.question] = value
	if p.question+1 < len(p.offer.Questions) {
		p.question++
		p.choice = -1
		p.input.Reset()
		p.notice = ""
		return m, nil, true
	}
	client, ok := m.fetcher.(codex.SessionPromptClient)
	if !ok {
		return m, nil, true
	}
	answers := append([]string(nil), p.answers[:p.question+1]...)
	token, session := p.offer.Token, p.session
	p.busy = true
	p.input.Blur()
	p.notice = ""
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		return monitorPromptResult{session: session, token: token, err: client.SendSessionPrompt(ctx, token, answers)}
	}, true
}

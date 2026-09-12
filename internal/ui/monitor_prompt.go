package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type monitorPromptState struct {
	session  string
	offer    codex.SessionPromptOffer
	input    monitorEditor
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
	if m.meterView != viewMonitor || m.monitorContextTarget() == "" || m.contextTargetHidden() ||
		(m.monitorContextDetail == "" && (m.monitorSelectedID != m.monitorContextTarget() || m.rowContextMode(m.monitorSelectedID) != contextWide)) {
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
	// Structured questions retain the full-detail interface.
	if m.monitorContextDetail == "" && len(o.Questions) > 0 {
		return codex.SessionPromptOffer{}
	}
	if len(o.Questions) > 0 && (s.preview.Kind != codex.SessionContextQuestion || s.preview.InputToken != o.Token) {
		return codex.SessionPromptOffer{}
	}
	if s.preview.Kind == codex.SessionContextQuestion && len(o.Questions) == 0 {
		return codex.SessionPromptOffer{}
	}
	return o
}

func (m Model) monitorPromptRows(width, height int) int {
	if width < 24 || height < 8 || m.monitorContextTarget() == "" || m.contextTargetHidden() {
		return 0
	}
	if s, ok := m.contextDetailSession(); ok && s.preview.Kind == codex.SessionContextApproval {
		return 0
	}
	if m.monitorContextDetail == "" && m.monitorSelectedID != m.monitorContextTarget() {
		return 0
	}
	if m.monitorPromptOffer().Token != "" || m.monitorPrompt.session == m.monitorContextTarget() && (m.monitorPrompt.busy || m.monitorPrompt.notice != "" && !sentNotice(m.monitorPrompt.notice)) {
		p := m.monitorPrompt
		rows := 3
		if p.input.Focused() && !p.busy {
			p.input.configure(width, m.monitorPromptEditorHeight(width, height))
			rows = p.input.Height() + 2
		}
		if m.monitorContextDetail == "" {
			s, ok := m.contextDetailSession()
			textRows, _, _ := monitorContextBodyLayout(height, rows)
			if !ok || len(expandedContextLines(width, s)) > textRows {
				return 0
			}
		}
		return rows
	}
	return 0
}

// Inline drafts grow only into spare space, then scroll inside the editor;
// they never displace the source context. Full detail keeps its usual viewport.
func (m Model) monitorPromptEditorHeight(width, height int) int {
	if m.monitorContextDetail == "" {
		if s, ok := m.contextDetailSession(); ok {
			return min(height, max(8, height+3-len(expandedContextLines(width, s))))
		}
	}
	return height
}

func (m Model) monitorPromptSize() (int, int) {
	g := m.dashboardLayout()
	if m.monitorContextDetail != "" {
		return g.contentWidth, g.meterHeight
	}
	a := layoutMonitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	for i, s := range sessions {
		if s.id == m.monitorContextTarget() && m.rowContextMode(s.id) == contextWide {
			_, width, _ := monitorSessionColumnWidths(a.width)
			return width, heights[i]
		}
	}
	return 0, 0
}

func (m *Model) focusMonitorPrompt() tea.Cmd {
	o := m.monitorPromptOffer()
	w, h := m.monitorPromptSize()
	if o.Token == "" || m.monitorPrompt.busy || m.monitorPromptRows(w, h) == 0 {
		return nil
	}
	if m.monitorPrompt.offer.Token != o.Token {
		m.monitorPrompt = monitorPromptState{session: m.monitorContextTarget(), offer: o, input: newMonitorEditor(), choice: -1}
	}
	m.monitorPrompt.input.setSecret(len(o.Questions) > 0 && o.Questions[m.monitorPrompt.question].Secret)
	m.monitorPrompt.input.configure(w, m.monitorPromptEditorHeight(w, h))
	m.monitorPrompt.notice = ""
	return m.monitorPrompt.input.Focus()
}

func (m Model) renderMonitorPrompt(width, height int, colors palette) string {
	p := m.monitorPrompt
	o := m.monitorPromptOffer()
	header := i18n.Text("FOLLOW-UP") + " // " + terminalLabel(o.ThreadID)
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
		input.configure(width, m.monitorPromptEditorHeight(width, height))
		line = input.View(colors)
	}
	if p.busy {
		line = i18n.Text("Sending…")
	}
	if p.notice != "" {
		if !sentNotice(p.notice) {
			hint = p.notice
		} else if feedback := m.detailFeedback(); feedback != "" {
			hint = feedback
		}
	}
	inputLines := strings.Split(line, "\n")
	for i := range inputLines {
		inputLines[i] = ansi.Truncate(inputLines[i], max(width-4, 1), "")
	}
	return colors.label().Render(ansi.Truncate(header, max(width-4, 1), "…")) + "\n" + strings.Join(inputLines, "\n") + "\n" + colors.label().Render(ansi.Truncate(hint, max(width-4, 1), "…"))
}

func (m Model) updateMonitorPrompt(msg tea.Msg) (Model, tea.Cmd, bool) {
	p := &m.monitorPrompt
	if result, ok := msg.(monitorPromptResult); ok {
		if p.session == result.session && p.offer.Token == result.token {
			p.busy = false
			p.input.Reset()
			p.input.Blur()
			p.answers = [3]string{}
			p.notice = i18n.Text("Text sent ...")
			if result.err == nil {
				m.recordDetailSent(p.notice)
			}
			if result.err != nil {
				p.notice = i18n.Text("Send unconfirmed; check Codex before retrying.")
			}
			p.offer = codex.SessionPromptOffer{}
		}
		return m, nil, true
	}
	wasFocused := p.input.Focused()
	layout := m
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		layout.width, layout.height = size.Width, size.Height
	}
	w, h := layout.monitorPromptSize()
	p.input.configure(w, layout.monitorPromptEditorHeight(w, h))
	if wasFocused && layout.monitorPromptRows(w, h) == 0 {
		p.input.Blur()
	}
	if p.session != "" && (m.meterView != viewMonitor || m.monitorContextTarget() != p.session || m.contextTargetHidden() || m.monitorContextDetail == "" && m.monitorSelectedID != p.session) {
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
					return m, nil, true
				}
			}
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
		p.input.setSecret(p.offer.Questions[p.question].Secret)
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
	m.monitorDetailSent = detailSentState{}
	p.input.Blur()
	p.notice = ""
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		return monitorPromptResult{session: session, token: token, err: client.SendSessionPrompt(ctx, token, answers)}
	}, true
}

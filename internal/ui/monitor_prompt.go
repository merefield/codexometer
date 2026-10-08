package ui

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type monitorPromptState struct {
	session     string
	offer       codex.SessionPromptOffer
	input       monitorEditor
	answers     [3]string
	question    int
	choice      int
	busy        bool
	notice      string
	noticeUntil time.Time
}

// Retain only unsent ordinary text, never secret/question answers or in-flight
// submissions. Stored drafts have no reusable server capability or focus.
func (m *Model) stashMonitorDraft() {
	p := m.monitorPrompt
	if p.session == "" || p.busy || len(p.offer.Questions) != 0 || p.input.secret {
		return
	}
	m.monitorDrafts = maps.Clone(m.monitorDrafts)
	if text := p.input.Value(); text != "" {
		if m.monitorDrafts == nil {
			m.monitorDrafts = make(map[string]string)
		}
		m.monitorDrafts[p.session] = text
	} else {
		delete(m.monitorDrafts, p.session)
	}
}

func (m *Model) restoreMonitorDraft() {
	if m.monitorPrompt.session != "" || m.meterView != viewMonitor || m.contextTargetHidden() {
		return
	}
	id := m.monitorContextTarget()
	text := m.monitorDrafts[id]
	if text == "" {
		return
	}
	o := m.monitorPromptOffer()
	if o.Token == "" || len(o.Questions) != 0 {
		return
	}
	m.monitorPrompt = monitorPromptState{session: id, offer: o, input: newMonitorEditor(), choice: -1}
	m.monitorPrompt.input.SetValue(text)
	m.monitorDrafts = maps.Clone(m.monitorDrafts)
	delete(m.monitorDrafts, id)
}

type monitorPromptResult struct {
	session, token string
	err            error
}

type monitorTurnResult struct {
	session, token, action string
	text                   string
	err                    error
	steer                  monitorSteerReceipt
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
	if _, pending := m.sessionProfile(s); pending {
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
	if o.Token == "" {
		if active, ok := m.fetcher.(codex.SessionTurnClient); ok {
			o = active.SessionTurn(thread)
		}
	}
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
	if s, ok := m.contextDetailSession(); ok {
		if _, pending := m.sessionProfile(s); pending {
			return 0
		}
	}
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
		headerRows := m.monitorPromptHeaderRows()
		rows := 2 + headerRows
		if (p.input.Focused() || p.input.Value() != "") && !p.busy {
			p.input.configure(width, m.monitorPromptEditorHeight(width, height))
			rows = p.input.Height() + 1 + headerRows
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

func (m Model) monitorPromptHeaderRows() int {
	if len(m.monitorPromptOffer().Questions) > 0 {
		return 1
	}
	if m.monitorComposerActivity() != "" {
		return 1
	}
	return 0
}

func (m Model) monitorComposerActivity() string {
	if m.monitorPromptOffer().TurnID != "" {
		if s, ok := m.contextDetailSession(); ok {
			return m.sessionActivityDots(s)
		}
	}
	return ""
}

// Inline drafts grow only into spare space, then scroll inside the editor;
// they never displace the source context. Full detail keeps its usual viewport.
func (m Model) monitorPromptEditorHeight(width, height int) int {
	extra := 1 - m.monitorPromptHeaderRows()
	if m.monitorContextDetail == "" {
		if s, ok := m.contextDetailSession(); ok {
			return min(height+extra, max(8, height+3+extra-len(expandedContextLines(width, s))))
		}
	}
	return height + extra
}

func (m Model) monitorPromptSize() (int, int) {
	g := m.monitorDashboardLayout()
	if m.monitorContextDetail != "" {
		return g.contentWidth, g.meterHeight
	}
	a := m.monitorArea(g.contentWidth, g.meterHeight)
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
	m.restoreMonitorDraft()
	o := m.monitorPromptOffer()
	w, h := m.monitorPromptSize()
	if o.Token == "" || m.monitorPrompt.busy || m.monitorPromptRows(w, h) == 0 {
		return nil
	}
	if m.monitorPrompt.offer.Token != o.Token {
		if m.monitorPrompt.offer.Token != "" && m.monitorPrompt.session == m.monitorContextTarget() && len(o.Questions) == 0 && len(m.monitorPrompt.offer.Questions) == 0 {
			m.monitorPrompt.offer = o
		} else {
			m.monitorPrompt = monitorPromptState{session: m.monitorContextTarget(), offer: o, input: newMonitorEditor(), choice: -1}
			if len(o.Questions) == 0 {
				m.monitorPrompt.input.SetValue(m.monitorDrafts[m.monitorPrompt.session])
				m.monitorDrafts = maps.Clone(m.monitorDrafts)
				delete(m.monitorDrafts, m.monitorPrompt.session)
			}
		}
	}
	m.monitorPrompt.input.setSecret(len(o.Questions) > 0 && o.Questions[m.monitorPrompt.question].Secret)
	m.monitorPrompt.input.configure(w, m.monitorPromptEditorHeight(w, h))
	m.monitorPrompt.notice = ""
	return tea.Batch(m.monitorPrompt.input.Focus(), m.syncMonitorSuggestions())
}

func (m Model) renderMonitorPrompt(width, height int, colors palette) string {
	p := m.monitorPrompt
	o := m.monitorPromptOffer()
	header := m.monitorComposerActivity()
	scheduling := m.monitorContextDetail != "" && o.Token != "" && len(o.Questions) == 0 && o.TurnID == ""
	if len(o.Questions) > 0 {
		identity := shortSessionID(o.ThreadID)
		if s, ok := m.contextDetailSession(); ok {
			identity = monitorSessionIdentity(s)
		}
		n := 0
		if p.offer.Token == o.Token {
			n = p.question
		}
		n = min(n, len(o.Questions)-1)
		header = fmt.Sprintf("%s %d/%d // %s // %s", i18n.Text("REPLY"), n+1, len(o.Questions), identity, o.Questions[n].Text)
	}
	line := colors.dimmed().Background(monitorComposerBackground(colors)).
		Width(max(width-4, 1)).Render(ansi.Truncate(i18n.Text("[ Click here or press Enter to write ]"), max(width-4, 1), ""))
	hint := i18n.Text("Enter: send / next answer • Esc: leave editor • ↑/↓: choices")
	hintStyle := colors.dimmed()
	if o.TurnID != "" {
		hint = "Enter: steer • Tab: queue next turn • Esc: interrupt"
	}
	if p.offer.Token == o.Token && (p.input.Focused() || p.input.Value() != "") && !p.busy {
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
			hintStyle = colors.label()
		} else if time.Now().Before(p.noticeUntil) || m.monitorDetailSent.session == m.monitorContextTarget() && time.Now().Before(m.monitorDetailSent.visibleUntil) {
			hint = p.notice
			hintStyle = colors.label()
		}
	}
	inputLines := strings.Split(line, "\n")
	for i := range inputLines {
		inputLines[i] = ansi.Truncate(inputLines[i], max(width-4, 1), "")
	}
	if popup, _ := m.monitorSuggestionPopup(); popup.height > 0 {
		hint = "↑/↓: select • Tab: complete • Enter: options • Esc: dismiss"
		hintStyle = colors.dimmed()
	}
	footer := hintStyle.Render(ansi.Truncate(hint, max(width-4, 1), "…"))
	if scheduling {
		footer = m.renderScheduleToggleHint(width, hint, false, hintStyle, colors)
	}
	body := strings.Join(inputLines, "\n") + "\n" + footer
	if header != "" {
		body = colors.label().Render(ansi.Truncate(header, max(width-4, 1), "…")) + "\n" + body
	}
	return body
}

func (m Model) updateMonitorPrompt(msg tea.Msg) (Model, tea.Cmd, bool) {
	p := &m.monitorPrompt
	if result, ok := msg.(monitorTurnResult); ok {
		if result.err == nil && result.action == "steer" {
			m.recordMonitorSteer(result.steer)
		}
		if p.session == result.session && p.offer.Token == result.token {
			p.busy = false
			if result.err != nil {
				p.notice = "Action unconfirmed or unsupported; check Codex before retrying. Draft retained."
			} else if result.action == "interrupt" {
				p.notice = "Interrupt requested. Draft retained."
			} else {
				p.input.Reset()
				p.notice = i18n.Text("Text sent ...")
				p.noticeUntil = time.Now().Add(3 * time.Second)
				m.recordDetailSent(p.notice)
				if result.action == "queue" {
					p.notice = "Queued in Codex for the next turn."
					cmd := m.recordQueuedSubmission(result.session, result.text)
					return m, cmd, true
				}
			}
		}
		return m, nil, true
	}
	if result, ok := msg.(monitorPromptResult); ok {
		if p.session == result.session && p.offer.Token == result.token {
			p.busy = false
			p.input.Reset()
			p.input.Blur()
			p.answers = [3]string{}
			p.notice = i18n.Text("Text sent ...")
			p.noticeUntil = time.Now().Add(3 * time.Second)
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
	if !p.input.ready && p.session == "" {
		m.restoreMonitorDraft()
		if !p.input.ready {
			return m, nil, false
		}
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
	// A newly visible approval may replace the focused composer. Let an
	// explicit grant shortcut select it, but never reinterpret that first key
	// as confirmation, a decline, or an action on a different session.
	if wasFocused && !p.input.Focused() && !p.busy && !p.input.secret && len(p.offer.Questions) == 0 && p.session == m.monitorContextTarget() {
		if key, ok := msg.(tea.KeyPressMsg); ok && !key.IsRepeat {
			name := key.String()
			if len(name) == 1 && name[0] >= '1' && name[0] <= '8' {
				index := int(name[0] - '1')
				if s, ok := m.contextDetailSession(); ok && s.preview.ApprovalOptions[index].GrantsPermission() {
					for _, b := range m.visibleMonitorApprovalButtons() {
						if b.action == fmt.Sprintf("decision:%d", index) {
							m.stashMonitorDraft()
							*p = monitorPromptState{session: p.session}
							m.monitorApprovalConfirm = ""
							m.monitorApprovalNumberReleased = false
							return m.updateMonitorApprovalKey(name)
						}
					}
				}
			}
		}
	}
	if p.session != "" && (m.meterView != viewMonitor || m.monitorContextTarget() != p.session || m.contextTargetHidden() || m.monitorContextDetail == "" && m.monitorSelectedID != p.session) {
		m.stashMonitorDraft()
		*p = monitorPromptState{}
		m.restoreMonitorDraft()
	} else if p.offer.Token != "" && !p.busy && m.monitorPromptOffer().Token != p.offer.Token {
		next := m.monitorPromptOffer()
		if len(p.offer.Questions) == 0 && len(next.Questions) == 0 && (p.offer.TurnID != "" || next.TurnID != "") {
			// Keep ordinary drafts across working/idle transitions, but never
			// reinterpret the key that arrived during a capability change.
			if next.Token != "" {
				p.offer = next
			} else {
				// Retire the obsolete working capability, not just its focus.
				// Otherwise every later approval key re-enters this transition
				// and is swallowed forever after an intervening telemetry tick.
				m.stashMonitorDraft()
				*p = monitorPromptState{session: p.session}
				m.monitorApprovalConfirm = ""
				m.monitorApprovalNumberReleased = false
			}
			if _, key := msg.(tea.KeyPressMsg); key {
				return m, nil, true
			}
		} else {
			m.stashMonitorDraft()
			*p = monitorPromptState{session: p.session, notice: i18n.Text("Prompt changed; review the session before replying.")}
		}
	}
	if wasFocused && !p.input.Focused() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg:
			return m, nil, true
		}
	}
	if p.busy && p.input.Focused() {
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
			if key.String() == "esc" && p.offer.TurnID != "" {
				return m.submitMonitorTurn("interrupt")
			}
			p.input.Blur()
			return m, nil, true
		case "enter":
			if len(p.offer.Questions) == 0 && codex.IsSessionCommand(p.input.Value()) {
				return m.openMonitorCommands(strings.TrimSpace(p.input.Value()))
			}
			if p.offer.TurnID != "" {
				return m.submitMonitorTurn("steer")
			}
			return m.submitMonitorPrompt()
		case "tab":
			if p.offer.TurnID != "" {
				if codex.IsSessionCommand(p.input.Value()) {
					return m.openMonitorCommands(strings.TrimSpace(p.input.Value()))
				}
				return m.submitMonitorTurn("queue")
			}
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
		m.monitorSuggestions.selected = 0
		return m, tea.Batch(cmd, m.syncMonitorSuggestions()), true
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if cmd != nil || before != p.input.Value() {
		return m, cmd, true
	}
	return m, nil, false
}

func (m Model) submitMonitorTurn(action string) (Model, tea.Cmd, bool) {
	p := &m.monitorPrompt
	o := m.monitorPromptOffer()
	c, ok := m.fetcher.(codex.SessionTurnClient)
	text := strings.TrimSpace(p.input.Value())
	if !ok || p.busy || o.Token == "" || o.Token != p.offer.Token || o.TurnID == "" || action != "interrupt" && text == "" {
		return m, nil, true
	}
	p.busy, p.notice = true, ""
	session := p.session
	steer := monitorSteerReceipt{session: session, thread: o.ThreadID, turn: o.TurnID, text: codex.SanitizeSessionContext(text), sentAt: time.Now()}
	if s, ok := m.contextDetailSession(); ok {
		steer.baselineID = s.preview.LatestGuidanceID
		steer.baselineText = s.preview.LatestGuidance
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		return monitorTurnResult{session: session, token: o.Token, action: action, text: text, steer: steer, err: c.SendSessionTurn(ctx, o, action, text)}
	}, true
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

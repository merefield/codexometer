package ui

import (
	"context"
	"errors"
	"maps"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type newSessionDraft struct {
	prompt     monitorEditor
	directory  *textinput.Model
	scope      string
	generation uint64
	pendingID  string
	notice     string
	sending    bool
	uncertain  bool
}

type newSessionResult struct {
	generation uint64
	id         string
	err        error
}

type newSessionDeadline uint64

func (d newSessionDraft) open() bool { return d.directory != nil }

func (m Model) canStartNewSession() bool {
	_, ok := m.fetcher.(codex.SessionStartClient)
	return ok && m.finder.scope != ""
}

func (m *Model) openNewSession() tea.Cmd {
	if !m.canStartNewSession() || m.newSession.open() {
		return nil
	}
	m.clearMonitorCloseAllConfirmation()
	m.stashMonitorDraft()
	m.monitorPrompt.input.Blur()
	m.monitorContextDetail = ""
	m.finder.clearHistory()
	folder := textinput.New()
	folder.CharLimit = 1024
	folder.Prompt = ""
	cwd, _ := os.Getwd()
	folder.SetValue(cwd)
	m.newSession = newSessionDraft{prompt: newMonitorEditor(), directory: &folder, scope: m.finder.scope, generation: m.newSession.generation + 1}
	return m.newSession.prompt.Focus()
}

func (m *Model) closeNewSession() {
	m.newSession = newSessionDraft{generation: m.newSession.generation + 1}
}

func (m *Model) startDraftSession() tea.Cmd {
	d := &m.newSession
	if !d.open() || d.sending || d.pendingID != "" || d.uncertain {
		return nil
	}
	if !m.canStartNewSession() || d.scope != m.finder.scope {
		d.notice = "Codex connection changed. Close this draft and open a new one."
		return nil
	}
	value := d.prompt.Value()
	switch {
	case strings.EqualFold(strings.TrimSpace(value), "/resume"):
		return m.openResumePicker()
	case strings.TrimSpace(value) == "" || codex.IsSessionCommand(value):
		d.notice = "Type a first message, or use /resume to open a saved session."
		return nil
	}
	cwd, scope, generation := d.directory.Value(), d.scope, d.generation
	if m.reviewMonitorAction("new-session", cwd, "START NEW SESSION", value, []string{scope, cwd, value}) {
		return nil
	}
	d.sending, d.notice = true, "Starting session…"
	starter := m.fetcher.(codex.SessionStartClient)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		id, err := starter.StartSession(ctx, scope, cwd, value)
		return newSessionResult{generation, id, err}
	}
}

// Match render and input coordinates. The composer gets any remaining rows.
func newSessionControls(width, height int) []monitorNavigationButton {
	if height < 5 || width < 25 {
		return nil
	}
	buttons := []monitorNavigationButton{
		{label: "[ Send ]", action: "send", enabled: true},
		{label: "[ Saved sessions ]", action: "saved", enabled: true},
		{label: "[ Esc Close ]", action: "close", enabled: true},
	}
	if width < 47 {
		buttons[1].label, buttons[2].label = "[ /resume ]", "[ Esc ]"
	}
	if width < 34 {
		buttons = buttons[:2]
	}
	layoutNavigationButtons(buttons, 2, 3)
	return buttons
}

func (m Model) renderNewSession(width, height int, colors palette) string {
	d := m.newSession
	folder := *d.directory
	folder.SetWidth(max(width-5, 1))
	rows := []string{colors.label().Render("WORKING DIRECTORY"), folder.View(), ""}
	for _, button := range newSessionControls(width, height) {
		style := colors.label()
		if m.monitorContextHover == "new:"+button.action {
			style = style.Reverse(true)
		}
		rows[2] += style.Render(button.label) + " "
	}
	notice := d.notice
	if notice == "" {
		notice = "Enter sends · Tab changes field · Shift+Enter adds a line"
	}
	rows = append(rows, colors.label().Foreground(colors.dim).Render(ansi.Truncate(notice, max(width-4, 1), "…")),
		colors.label().Render("FIRST MESSAGE"))
	d.prompt.configure(width, height)
	rows = append(rows, d.prompt.View(colors))
	return frameSized(width, max(height-2, 1), "NEW SESSION", strings.Join(rows, "\n"), colors.primary, colors)
}

func (m Model) updateNewSession(message tea.Msg) (Model, tea.Cmd, bool) {
	d := &m.newSession
	switch event := message.(type) {
	case newSessionResult:
		if !d.open() || event.generation != d.generation {
			return m, nil, true
		}
		d.sending = false
		if event.err != nil {
			d.notice = event.err.Error()
			d.uncertain = errors.Is(event.err, codex.ErrSessionStartUncertain)
			return m, nil, true
		}
		d.pendingID, d.notice = event.id, "Opening the new session…"
		generation := d.generation
		deadline := tea.Tick(15*time.Second, func(time.Time) tea.Msg { return newSessionDeadline(generation) })
		if !m.monitorFetchActive {
			next, cmd := m.beginMonitorFetch(monitorFetchSample)
			return next, tea.Batch(cmd, deadline), true
		}
		return m, deadline, true
	case newSessionDeadline:
		if d.open() && uint64(event) == d.generation && d.pendingID != "" {
			d.pendingID, d.uncertain = "", true
			d.notice = "New session did not appear in the live list. Check Codex."
		}
		return m, nil, true
	}
	if m.meterView != viewMonitor || !d.open() || m.finder.include {
		return m, nil, false
	}
	g := m.dashboardLayout()
	if key, ok := message.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m, nil, false
		case "esc":
			if !d.sending {
				m.closeNewSession()
			}
			return m, nil, true
		case "tab", "shift+tab":
			cmd := m.focusNewSessionFolder(!d.directory.Focused())
			return m, cmd, true
		case "enter":
			if !key.IsRepeat {
				if d.directory.Focused() {
					cmd := m.focusNewSessionFolder(false)
					return m, cmd, true
				}
				cmd := m.startDraftSession()
				return m, cmd, true
			}
			return m, nil, true
		case "shift+enter", "alt+enter":
			if !d.directory.Focused() && !d.sending && d.pendingID == "" {
				d.prompt.editArea().InsertString("\n")
			}
			return m, nil, true
		}
	}
	switch event := message.(type) {
	case tea.MouseMotionMsg:
		action := monitorNavigationButtonsHit(newSessionControls(g.contentWidth, g.meterHeight), event.X-2, event.Y-g.meterY)
		m.monitorContextHover = "new:" + action
		return m, nil, true
	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft || event.Y < g.meterY || event.Y >= g.meterY+g.meterHeight {
			return m, nil, false
		}
		switch monitorNavigationButtonsHit(newSessionControls(g.contentWidth, g.meterHeight), event.X-2, event.Y-g.meterY) {
		case "send":
			cmd := m.startDraftSession()
			return m, cmd, true
		case "saved":
			if !d.sending && d.pendingID == "" && d.scope == m.finder.scope {
				cmd := m.openResumePicker()
				return m, cmd, true
			}
		case "close":
			if !d.sending {
				m.closeNewSession()
			}
		default:
			if local := event.Y - g.meterY; local == 2 || local >= 6 {
				cmd := m.focusNewSessionFolder(local == 2)
				return m, cmd, true
			}
		}
		return m, nil, true
	case tea.KeyPressMsg, tea.PasteMsg:
		if d.sending || d.pendingID != "" {
			return m, nil, true
		}
	}
	if d.directory.Focused() {
		folder, cmd := d.directory.Update(message)
		d.directory = &folder
		return m, cmd, newSessionEditMessage(message)
	}
	d.prompt.configure(g.contentWidth, g.meterHeight)
	var cmd tea.Cmd
	d.prompt, cmd = d.prompt.Update(message)
	return m, cmd, newSessionEditMessage(message)
}

func newSessionEditMessage(message tea.Msg) bool {
	switch message.(type) {
	case tea.KeyPressMsg, tea.PasteMsg:
		return true
	default:
		return false
	}
}

func (m *Model) focusNewSessionFolder(folder bool) tea.Cmd {
	d := &m.newSession
	input := *d.directory
	d.directory = &input
	if folder {
		d.prompt.Blur()
		return d.directory.Focus()
	}
	d.directory.Blur()
	return d.prompt.Focus()
}

func (m *Model) openLiveSessionDetail(id string) {
	m.finder.clearHistory()
	if m.newSession.open() {
		m.closeNewSession()
	}
	m.monitorDismissed = maps.Clone(m.monitorDismissed)
	delete(m.monitorDismissed, id)
	m.setRowContext(id, contextFull)
}

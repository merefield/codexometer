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
	"github.com/merefield/codexometer/internal/i18n"
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
	rejected   bool
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
		d.notice = i18n.Text("Codex connection changed. Close this draft and open a new one.")
		return nil
	}
	value := d.prompt.Value()
	switch {
	case strings.EqualFold(strings.TrimSpace(value), "/resume"):
		return m.openResumePicker()
	case strings.TrimSpace(value) == "" || codex.IsSessionCommand(value):
		d.notice = i18n.Text("Type a first message, or use /resume to open a saved session.")
		return nil
	}
	cwd, scope, generation := d.directory.Value(), d.scope, d.generation
	if m.reviewMonitorAction("new-session", cwd, i18n.Text("START NEW SESSION"), value, []string{scope, cwd, value}) {
		return nil
	}
	d.sending, d.notice = true, i18n.Text("Starting session…")
	starter := m.fetcher.(codex.SessionStartClient)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), codex.SessionStartTimeout)
		defer cancel()
		id, err := starter.StartSession(ctx, scope, cwd, value)
		return newSessionResult{generation, id, err}
	}
}

func newSessionControls(width int) []monitorNavigationButton {
	buttons := []monitorNavigationButton{
		{label: i18n.Text("[ Send ]"), action: "send", enabled: true},
		{label: i18n.Text("[ RESUME ]"), action: "saved", enabled: true},
		{label: i18n.Text("[ Esc Close ]"), action: "close", enabled: true},
	}
	available := width - 4
	firstTwo := ansi.StringWidth(buttons[0].label) + 1 + ansi.StringWidth(buttons[1].label)
	if firstTwo > available {
		buttons[1].label = "[/resume]"
		firstTwo = ansi.StringWidth(buttons[0].label) + 1 + ansi.StringWidth(buttons[1].label)
	}
	if firstTwo > available {
		return buttons[:1] // /resume is still available from the composer.
	}
	if width < 47 || firstTwo+1+ansi.StringWidth(buttons[2].label) > available {
		buttons[2].label = "[ Esc ]"
	}
	if width < 34 || firstTwo+1+ansi.StringWidth(buttons[2].label) > available {
		buttons = buttons[:2]
	}
	return buttons
}

type newSessionGeometry struct {
	folder, prompt, notice, promptRows int
	labels                             bool
	buttons                            []monitorNavigationButton
}

// A single layout controls both rendering and clicks. The editor always has
// a row; short panes drop chrome before they grow past the footer.
func layoutNewSession(width, height int, folderFocused, hasNotice bool) newSessionGeometry {
	g := newSessionGeometry{folder: -1, prompt: -1, notice: -1, promptRows: 1}
	if height <= 3 {
		row := min(max(height-1, 0), 1)
		if folderFocused {
			g.folder = row
		} else {
			g.prompt = row
		}
		return g
	}
	g.labels = height >= 8
	g.folder = 1
	if g.labels {
		g.folder++
	}
	row := g.folder + 1
	if width >= 25 && height >= 6 && (!hasNotice || height >= 7) {
		g.buttons = newSessionControls(width)
		layoutNavigationButtons(g.buttons, 2, row)
		row++
	}
	if (g.labels || hasNotice) && row < height-2 {
		g.notice = row
		row++
	}
	if g.labels {
		row++
	}
	g.prompt = row
	g.promptRows = max(height-row-1, 1)
	return g
}

func (m Model) renderNewSession(width, height int, colors palette) string {
	d := m.newSession
	g := layoutNewSession(width, height, d.directory.Focused(), d.notice != "")
	innerWidth := max(width-4, 1)
	label := colors.label()
	title := i18n.Text("NEW SESSION")
	directoryLabel, promptLabel := i18n.Text("WORKING DIRECTORY"), i18n.Text("FIRST MESSAGE")
	if height <= 3 {
		if d.directory.Focused() {
			title += " // " + directoryLabel
		} else {
			title += " // " + promptLabel
		}
	}
	if d.notice != "" && g.notice < 0 {
		title += " // " + d.notice
	}
	rows := make([]string, height)
	if g.labels {
		rows[1], rows[g.prompt-1] = label.Render(directoryLabel), label.Render(promptLabel)
	}
	if g.folder >= 0 {
		folder := *d.directory
		if !g.labels && height > 3 {
			folder.Prompt = i18n.Text("DIR") + " "
		}
		folder.SetWidth(max(innerWidth-ansi.StringWidth(folder.Prompt)-1, 1))
		rows[g.folder] = folder.View()
	}
	for _, button := range g.buttons {
		style := colors.label()
		if m.monitorContextHover == "new:"+button.action {
			style = style.Reverse(true)
		}
		rows[button.rect.y] += style.Render(button.label) + " "
	}
	if g.notice >= 0 {
		notice := d.notice
		if notice == "" {
			notice = i18n.Text("Enter sends · Tab changes field · Shift+Enter adds a line")
		}
		rows[g.notice] = label.Foreground(colors.dim).Render(notice)
	}
	if g.prompt >= 0 {
		d.prompt.configureViewport(width, g.promptRows)
		copy(rows[g.prompt:g.prompt+g.promptRows], strings.Split(d.prompt.View(colors), "\n"))
	}
	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], innerWidth, "")
	}
	if height <= 2 {
		if height == 2 {
			rows[0] = renderFrameTitle(width, title, colors.primary, colors)
		}
		return strings.Join(rows, "\n")
	}
	return frameSized(width, height-2, title, strings.Join(rows[1:height-1], "\n"), colors.primary, colors)
}

// Only these fixed sentinel messages are catalogue keys. Never translate or
// display a raw protocol/diagnostic error. UI locale tests cover every key.
var newSessionKnownErrors = [...]error{
	codex.ErrSessionStartUnavailable,
	codex.ErrSessionStartDirectory,
	codex.ErrSessionStartTrust,
	codex.ErrSessionStartPrompt,
	codex.ErrSessionStartCommand,
	codex.ErrSessionStartRejected,
	codex.ErrSessionFirstPrompt,
	codex.ErrSessionStartUncertain,
}

func newSessionErrorText(err error) string {
	for _, known := range newSessionKnownErrors {
		if errors.Is(err, known) {
			return i18n.Text(known.Error())
		}
	}
	return i18n.Text("Could not start the new session.")
}

func (m Model) updateNewSession(message tea.Msg) (Model, tea.Cmd, bool) {
	d := &m.newSession
	switch event := message.(type) {
	case newSessionResult:
		if !d.open() || event.generation != d.generation {
			return m, nil, true
		}
		d.sending = false
		d.rejected = errors.Is(event.err, codex.ErrSessionFirstPrompt) && event.id != ""
		if event.err != nil && !d.rejected {
			d.notice = newSessionErrorText(event.err)
			d.uncertain = errors.Is(event.err, codex.ErrSessionStartUncertain)
			return m, nil, true
		}
		d.pendingID, d.notice = event.id, i18n.Text("Opening the new session…")
		if d.rejected {
			d.notice = newSessionErrorText(event.err)
		}
		generation := d.generation
		deadline := tea.Tick(15*time.Second, func(time.Time) tea.Msg { return newSessionDeadline(generation) })
		if !m.monitorFetchActive {
			next, cmd := m.beginMonitorFetch(monitorFetchSample)
			return next, tea.Batch(cmd, deadline), true
		}
		return m, deadline, true
	case newSessionDeadline:
		if d.open() && uint64(event) == d.generation && d.pendingID != "" {
			if !d.rejected {
				d.notice = i18n.Text("First message sent; still waiting for the session to appear.")
			}
		}
		return m, nil, true
	}
	if m.meterView != viewMonitor || !d.open() || m.finder.include {
		return m, nil, false
	}
	g := m.dashboardLayout()
	draftGeometry := layoutNewSession(g.contentWidth, g.meterHeight, d.directory.Focused(), d.notice != "")
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
		action := monitorNavigationButtonsHit(draftGeometry.buttons, event.X-2, event.Y-g.meterY)
		if action != "" {
			m.monitorContextHover = "new:" + action
			return m, nil, true
		}
		if strings.HasPrefix(m.monitorContextHover, "new:") {
			m.monitorContextHover = ""
		}
		return m, nil, false
	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft || event.Y < g.meterY || event.Y >= g.meterY+g.meterHeight {
			return m, nil, false
		}
		switch monitorNavigationButtonsHit(draftGeometry.buttons, event.X-2, event.Y-g.meterY) {
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
			local := event.Y - g.meterY
			if local == draftGeometry.folder || draftGeometry.prompt >= 0 && local >= draftGeometry.prompt && local < draftGeometry.prompt+draftGeometry.promptRows {
				cmd := m.focusNewSessionFolder(local == draftGeometry.folder)
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
	d.prompt.configureViewport(g.contentWidth, draftGeometry.promptRows)
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
	rejected := m.newSession.rejected && m.newSession.pendingID == id
	if m.newSession.open() {
		if rejected {
			if m.monitorDrafts == nil {
				m.monitorDrafts = make(map[string]string)
			} else {
				m.monitorDrafts = maps.Clone(m.monitorDrafts)
			}
			m.monitorDrafts[id] = m.newSession.prompt.Value()
		}
		m.closeNewSession()
	}
	m.monitorDismissed = maps.Clone(m.monitorDismissed)
	delete(m.monitorDismissed, id)
	m.setRowContext(id, contextFull)
	if rejected {
		m.monitorPrompt.notice = newSessionErrorText(codex.ErrSessionFirstPrompt)
		m.monitorPrompt.noticeUntil = time.Now().Add(15 * time.Second)
	}
}

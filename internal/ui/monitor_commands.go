package ui

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/statusline"
)

type monitorCommandsState struct {
	open, busy, detail      bool
	session                 string
	request                 uint64
	menu                    codex.SessionCommandMenu
	selected, scroll, hover int
	notice                  string
	until                   time.Time
	footerHover             string
	input                   textinput.Model
}
type monitorCommandsResult struct {
	session string
	path    string
	request uint64
	menu    codex.SessionCommandMenu
	applied bool
	err     error
}

func (m *Model) loadMonitorCommands(path string) tea.Cmd {
	p := &m.monitorCommands
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	if strings.TrimPrefix(strings.TrimSpace(path), "/") == "statusline" {
		p.busy = false
		p.detail = false
		p.notice = ""
		p.selected = 0
		p.scroll = 0
		p.request++
		p.menu = m.statusLineMenu()
		return nil
	}
	c, ok := m.fetcher.(codex.SessionCommandsClient)
	if !ok {
		if path == "" || path == "help" {
			p.menu = localCommandsMenu()
			p.busy = false
			p.notice = ""
			return nil
		}
		p.notice = "Shared app-server commands unavailable."
		return nil
	}
	p.input.Blur()
	p.busy = true
	p.detail = false
	p.notice = ""
	p.selected = 0
	p.scroll = 0
	p.hover = -1
	p.request++
	id, seq := p.session, p.request
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		menu, err := c.SessionCommands(ctx, id, path)
		return monitorCommandsResult{session: id, path: path, request: seq, menu: menu, err: err}
	}
}

func (m Model) openMonitorCommands(path string) (Model, tea.Cmd, bool) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	if _, ok := m.fetcher.(codex.SessionCommandsClient); !ok && path != "statusline" && path != "" && path != "help" {
		m.monitorPrompt.notice = "Slash commands require the shared app-server."
		return m, nil, true
	}
	id := m.monitorContextTarget()
	if id == "" {
		return m, nil, true
	}
	m.monitorPrompt.input.Blur()
	m.monitorContextDetail = id
	m.monitorCommands = monitorCommandsState{open: true, session: id, request: m.monitorCommands.request, hover: -1}
	cmd := m.loadMonitorCommands(path)
	return m, cmd, true
}

func (m *Model) closeMonitorCommands() {
	m.monitorCommands.input.Blur()
	m.monitorCommands.open = false
	m.monitorCommands.request++
	m.monitorCommands.busy = false
}

func (m Model) commandChoice() (codex.SessionCommandChoice, bool) {
	p := m.monitorCommands
	if p.selected < 0 || p.selected >= len(p.menu.Choices) {
		return codex.SessionCommandChoice{}, false
	}
	return p.menu.Choices[p.selected], true
}

func (m Model) chooseMonitorCommand() (Model, tea.Cmd, bool) {
	o, ok := m.commandChoice()
	if !ok || m.monitorCommands.busy {
		return m, nil, true
	}
	if o.Next != "" {
		cmd := m.loadMonitorCommands(o.Next)
		return m, cmd, true
	}
	if m.monitorCommands.menu.Multiple {
		p := &m.monitorCommands
		p.menu.Choices = append([]codex.SessionCommandChoice(nil), p.menu.Choices...)
		p.menu.Choices[p.selected].Selected = !o.Selected
		return m, nil, true
	}
	m.monitorCommands.detail = true
	m.monitorCommands.scroll = 0
	m.monitorCommands.until = time.Now().Add(30 * time.Second)
	return m, nil, true
}

func (m Model) confirmMonitorCommand() (Model, tea.Cmd, bool) {
	p := &m.monitorCommands
	if p.open && p.menu.Multiple && p.menu.Path == "statusline" {
		m.monitorStatusLine = statusline.Normalize(statusLineSelection(p.menu))
		if codex.IsSessionCommand(m.monitorPrompt.input.Value()) {
			m.monitorPrompt.input.Reset()
		}
		m.persistPreferences()
		m.closeMonitorCommands()
		return m, nil, true
	}
	o, ok := m.commandChoice()
	if !p.detail || !ok || !o.Action || p.busy || p.notice != "" {
		return m, nil, true
	}
	if time.Now().After(p.until) {
		p.notice = "Confirmation expired; reopen the option."
		return m, nil, true
	}
	c, ok := m.fetcher.(codex.SessionCommandsClient)
	if !ok {
		return m, nil, true
	}
	p.busy = true
	p.request++
	id, seq, path, rev := p.session, p.request, p.menu.Path, p.menu.Revision
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		err := c.ExecuteSessionCommand(ctx, id, path, rev, o.ID)
		return monitorCommandsResult{session: id, path: path, request: seq, applied: true, err: err}
	}, true
}

type commandDisplayRow struct {
	text   string
	choice int
	action string
}

func (m Model) monitorCommandRows(width, height int) []commandDisplayRow {
	p := m.monitorCommands
	w := max(width-4, 1)
	capacity := max(height-5, 1)
	if p.menu.Multiple {
		capacity = max(height-6, 1)
	}
	var rows []commandDisplayRow
	if p.busy {
		return []commandDisplayRow{{text: "Loading / sending…", choice: -1}}
	}
	if p.notice != "" {
		for _, line := range strings.Split(ansi.Hardwrap(p.notice, w, true), "\n") {
			rows = append(rows, commandDisplayRow{text: line, choice: -1})
		}
		return rows
	}
	if p.menu.Input {
		rows = append(rows, commandDisplayRow{text: "SESSION NAME", choice: -1})
		p.input.SetWidth(max(w-2, 1))
		rows = append(rows, commandDisplayRow{text: p.input.View(), choice: -1})
		for _, line := range strings.Split(ansi.Hardwrap(p.menu.Help, w, true), "\n") {
			if len(rows) >= capacity {
				break
			}
			rows = append(rows, commandDisplayRow{text: line, choice: -1})
		}
		return rows
	}
	if p.detail {
		o, ok := m.commandChoice()
		if !ok {
			return nil
		}
		text := o.Label + "\n\n" + p.menu.Help + "\n\n" + o.Help
		if o.Action {
			text += "\n\nTARGET // " + p.session
			if strings.HasPrefix(p.menu.Path, "rename/") {
				text += "\nOnly this session's saved name changes. Confirm after reviewing the new name."
			} else {
				text += "\nChanges apply to subsequent turns of this session. They are not global defaults. Confirm only after reviewing the option. Automatic quota thresholds may later supersede model settings."
			}
		}
		lines := strings.Split(ansi.Hardwrap(text, w, true), "\n")
		start := min(p.scroll, max(len(lines)-capacity, 0))
		for _, line := range lines[start:min(start+capacity, len(lines))] {
			rows = append(rows, commandDisplayRow{text: line, choice: -1})
		}
		return rows
	}
	start := max(p.selected-capacity+1, 0)
	for i := start; i < min(start+capacity, len(p.menu.Choices)); i++ {
		o := p.menu.Choices[i]
		label := o.Label
		if p.menu.Multiple {
			mark := "[ ] "
			if o.Selected {
				mark = "[x] "
			}
			label = mark + label + " // " + o.Help
		} else if o.Next != "" {
			label += " →"
		} else if !o.Action {
			label += " // HELP"
		}
		rows = append(rows, commandDisplayRow{text: ansi.Truncate(label, w, "…"), choice: i, action: "choose"})
	}
	if len(rows) == 0 {
		rows = append(rows, commandDisplayRow{text: "No available options. Use Codex for unsupported commands.", choice: -1})
	}
	if p.menu.Multiple {
		preview := statusline.Text(statusLineSelection(p.menu), m.monitorStatusValues())
		if preview == "" {
			preview = "—"
		}
		rows = append(rows, commandDisplayRow{text: ansi.Truncate("Preview // "+preview, w, "…"), choice: -1})
	}
	return rows
}

func (m Model) commandFooter(width int) []commandDisplayRow {
	p := m.monitorCommands
	var buttons []commandDisplayRow
	if p.menu.Input && !p.busy {
		buttons = append(buttons, commandDisplayRow{text: "[ ENTER REVIEW ]", action: "review"})
	} else if p.menu.Multiple {
		buttons = append(buttons, commandDisplayRow{text: "[ C APPLY ]", action: "confirm"})
	} else if o, ok := m.commandChoice(); ok && p.detail && o.Action && !p.busy && p.notice == "" {
		buttons = append(buttons, commandDisplayRow{text: "[ C CONFIRM ]", action: "confirm"})
	}
	buttons = append(buttons, commandDisplayRow{text: "[ ← BACK ]", action: "back"}, commandDisplayRow{text: "[ ESC CLOSE ]", action: "close"})
	used := 0
	for i, b := range buttons {
		if used+ansi.StringWidth(b.text) > max(width-4, 0) {
			return buttons[:i]
		}
		used += ansi.StringWidth(b.text) + 2
	}
	return buttons
}

func (m Model) renderMonitorCommands(width, height int, colors palette) string {
	p := m.monitorCommands
	rows := m.monitorCommandRows(width, height)
	body := make([]string, max(height-2, 1))
	for i, r := range rows {
		if i >= len(body)-2 {
			break
		}
		style := colors.label()
		if r.action != "" && (r.choice == p.selected || r.choice == p.hover) {
			style = style.Foreground(colors.primary).Bold(true)
		}
		body[i] = style.Render(ansi.Truncate(r.text, max(width-4, 1), ""))
	}
	var footer []string
	for _, b := range m.commandFooter(width) {
		style := colors.label()
		if p.footerHover == b.action {
			style = style.Foreground(colors.primary).Bold(true).Reverse(true)
		}
		footer = append(footer, style.Render(b.text))
	}
	if len(body) > 1 {
		hint := "↑/↓ select or scroll • Enter open • / root"
		if p.menu.Input {
			hint = "Enter review • Escape cancel"
		} else if p.menu.Multiple {
			hint = "↑/↓ select • Space toggle • ←/→ order • C apply"
		}
		body[len(body)-2] = colors.dimmed().Render(ansi.Truncate(hint, max(width-4, 1), ""))
		body[len(body)-1] = strings.Join(footer, "  ")
	}
	identity := shortSessionID(p.session)
	if s, ok := m.contextDetailSession(); ok {
		identity = monitorSessionIdentity(s)
	}
	title := fmt.Sprintf("%s // %s", p.menu.Title, identity)
	return frameSizedWithActions(width, max(height-2, 1), title, "", "", strings.Join(body, "\n"), colors.primary, colors)
}

func (m Model) updateMonitorCommands(msg tea.Msg) (Model, tea.Cmd, bool) {
	p := &m.monitorCommands
	if r, ok := msg.(monitorCommandsResult); ok {
		if p.open && p.session == r.session && p.request == r.request {
			if m.meterView != viewMonitor || m.monitorContextTarget() != r.session {
				m.closeMonitorCommands()
				return m, nil, true
			}
			p.busy = false
			p.notice = ""
			if r.err != nil {
				if !r.applied && (r.path == "" || r.path == "help") {
					p.menu = localCommandsMenu()
				} else {
					p.notice = "Command unavailable or unconfirmed. Check Codex before retrying."
				}
			} else if r.applied {
				// Applying settings invalidates both the option revision and
				// the root suggestion catalogue. Reopen only from a fresh read.
				m.monitorCommands = monitorCommandsState{request: p.request + 1}
				m.monitorSuggestions = monitorSuggestionState{request: m.monitorSuggestions.request + 1}
				if codex.IsSessionCommand(m.monitorPrompt.input.Value()) {
					m.monitorPrompt.input.Reset()
				}
				if offer := m.monitorPromptOffer(); offer.Token != "" && len(offer.Questions) == 0 && len(m.monitorPrompt.offer.Questions) == 0 {
					m.monitorPrompt.offer = offer
					m.monitorPrompt.session = r.session
				}
				cmd := m.focusMonitorPrompt()
				m.monitorPrompt.session = r.session
				m.monitorPrompt.notice = "Change requested. Codex will apply it to subsequent turns."
				if strings.HasPrefix(r.path, "rename/") {
					m.monitorPrompt.notice = "Session rename requested. The name updates on the next refresh."
				}
				return m, cmd, true
			} else {
				p.menu = r.menu
				if p.menu.Input {
					p.input = textinput.New()
					p.input.CharLimit = 512
					p.input.SetWidth(max(m.monitorDashboardLayout().contentWidth-6, 1))
					p.input.SetValue(p.menu.Value)
					cmd := p.input.Focus()
					return m, cmd, true
				}
				if strings.HasPrefix(p.menu.Path, "rename/") && len(p.menu.Choices) == 1 {
					p.detail = true
					p.until = time.Now().Add(30 * time.Second)
				}
				if p.menu.Path == "" || p.menu.Path == "help" {
					p.menu.Choices = append(p.menu.Choices, localStatusLineChoice())
				}
			}
		}
		return m, nil, true
	}
	if !p.open {
		if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "/" && m.meterView == viewMonitor && m.monitorContextDetail != "" && !m.monitorPrompt.input.Focused() && !m.scheduleUI.open && !m.monitorQueue.open {
			return m.openMonitorCommands("")
		}
		return m, nil, false
	}
	if m.meterView != viewMonitor || m.monitorContextTarget() != p.session {
		m.closeMonitorCommands()
		return m, nil, false
	}
	back := func() tea.Cmd {
		if p.detail {
			p.detail = false
			p.scroll = 0
			p.notice = ""
			return nil
		}
		path := p.menu.Path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			path = path[:i]
		} else {
			path = ""
		}
		return m.loadMonitorCommands(path)
	}
	if p.menu.Input && !p.busy {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "esc":
				m.closeMonitorCommands()
				return m, nil, true
			case "enter":
				return m.reviewMonitorCommandInput()
			case "ctrl+c":
				m.closeMonitorCommands()
				return m, nil, false
			}
		}
		if _, mouse := msg.(tea.MouseMsg); !mouse {
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			return m, cmd, true
		}
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if p.menu.Multiple {
			switch key.String() {
			case "space":
				return m.chooseMonitorCommand()
			case "left", "right":
				d := 1
				if key.String() == "left" {
					d = -1
				}
				target := p.selected + d
				if target >= 0 && target < len(p.menu.Choices) {
					p.menu.Choices = append([]codex.SessionCommandChoice(nil), p.menu.Choices...)
					p.menu.Choices[p.selected], p.menu.Choices[target] = p.menu.Choices[target], p.menu.Choices[p.selected]
					p.selected = target
				}
				return m, nil, true
			}
		}
		switch key.String() {
		case "esc":
			m.closeMonitorCommands()
		case "q", "ctrl+c", "tab", "shift+tab":
			m.closeMonitorCommands()
			return m, nil, false
		case "t", "r":
			return m, nil, false
		case "left", "backspace":
			return m, back(), true
		case "/":
			cmd := m.loadMonitorCommands("")
			return m, cmd, true
		case "enter", "right":
			if !p.detail {
				return m.chooseMonitorCommand()
			}
		case "c", "C":
			return m.confirmMonitorCommand()
		case "up", "down", "pgup", "pgdown":
			d := 1
			if key.String() == "up" || key.String() == "pgup" {
				d = -1
			}
			if strings.HasPrefix(key.String(), "pg") {
				d *= 10
			}
			if p.detail {
				p.scroll = max(0, p.scroll+d)
			} else {
				p.selected = min(max(0, p.selected+d), max(len(p.menu.Choices)-1, 0))
			}
		}
		return m, nil, true
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		g := m.monitorDashboardLayout()
		v := mouse.Mouse()
		x, y := v.X-4, v.Y-g.meterY-1
		if x < 0 || x >= g.contentWidth-4 || y < 0 || y >= g.meterHeight-2 {
			return m, nil, false
		}
		if v.Button == tea.MouseWheelUp || v.Button == tea.MouseWheelDown {
			d := 3
			if v.Button == tea.MouseWheelUp {
				d = -3
			}
			if p.detail {
				p.scroll = max(0, p.scroll+d)
			} else {
				p.selected = min(max(0, p.selected+d), max(len(p.menu.Choices)-1, 0))
			}
			return m, nil, true
		}
		rows := m.monitorCommandRows(g.contentWidth, g.meterHeight)
		p.hover = -1
		p.footerHover = ""
		_, click := msg.(tea.MouseClickMsg)
		if y < len(rows) && rows[y].action == "choose" {
			p.hover = rows[y].choice
			if click && v.Button == tea.MouseLeft {
				p.selected = p.hover
				return m.chooseMonitorCommand()
			}
		}
		if y == g.meterHeight-3 {
			for _, b := range m.commandFooter(g.contentWidth) {
				w := ansi.StringWidth(b.text)
				if x >= 0 && x < w {
					p.footerHover = b.action
					if click && v.Button == tea.MouseLeft {
						switch b.action {
						case "review":
							return m.reviewMonitorCommandInput()
						case "confirm":
							return m.confirmMonitorCommand()
						case "back":
							return m, back(), true
						case "close":
							m.closeMonitorCommands()
						}
					}
					break
				}
				x -= w + 2
			}
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) reviewMonitorCommandInput() (Model, tea.Cmd, bool) {
	p := &m.monitorCommands
	if !p.menu.Input || p.menu.Path != "rename" || p.busy {
		return m, nil, true
	}
	name := strings.TrimSpace(p.input.Value())
	if name == "" {
		return m, nil, true
	}
	cmd := m.loadMonitorCommands("rename/" + url.PathEscape(name))
	return m, cmd, true
}

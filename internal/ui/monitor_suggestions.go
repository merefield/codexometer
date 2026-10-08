package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type monitorSuggestionState struct {
	session, dismissed string
	request            uint64
	active, busy       bool
	selected           int
	menu               codex.SessionCommandMenu
	err                bool
}
type monitorSuggestionResult struct {
	session string
	request uint64
	menu    codex.SessionCommandMenu
	err     error
}

func (m Model) suggestionsActive() bool {
	p, s := m.monitorPrompt, m.monitorSuggestions
	return m.meterView == viewMonitor && !m.monitorCommands.open && !m.scheduleUI.open && !m.monitorQueue.open &&
		p.session == m.monitorContextTarget() && p.input.Focused() && !p.busy && len(p.offer.Questions) == 0 &&
		codex.IsSessionCommand(p.input.Value()) && p.input.Value() != s.dismissed
}

// Fetch once when a slash draft becomes active; subsequent keystrokes filter
// locally. Selection and RPC capabilities never cross session boundaries.
func (m *Model) syncMonitorSuggestions() tea.Cmd {
	s := &m.monitorSuggestions
	if !m.suggestionsActive() {
		s.active = false
		return nil
	}
	if s.active && s.session == m.monitorPrompt.session {
		return nil
	}
	c, ok := m.fetcher.(codex.SessionCommandsClient)
	if !ok {
		return nil
	}
	*s = monitorSuggestionState{active: true, busy: true, session: m.monitorPrompt.session, request: s.request + 1}
	id, seq := s.session, s.request
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		menu, err := c.SessionCommands(ctx, id, "")
		return monitorSuggestionResult{session: id, request: seq, menu: menu, err: err}
	}
}

func (m Model) suggestionChoices() []codex.SessionCommandChoice {
	s := m.monitorSuggestions
	if !m.suggestionsActive() || s.session != m.monitorPrompt.session {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(m.monitorPrompt.input.Value()))
	var choices []codex.SessionCommandChoice
	for _, o := range s.menu.Choices {
		if strings.HasPrefix(strings.ToLower(o.Label), query) {
			choices = append(choices, o)
		}
	}
	return choices
}

func (m Model) suggestionRows(height int) int {
	if !m.suggestionsActive() || m.monitorSuggestions.session != m.monitorPrompt.session {
		return 0
	}
	return min(max(len(m.suggestionChoices()), 1), 4, max(height-9, 0))
}

func (m Model) renderMonitorSuggestions(width, height int, colors palette) []string {
	count := m.suggestionRows(height)
	if count == 0 {
		return nil
	}
	choices := m.suggestionChoices()
	if len(choices) == 0 {
		text := "No matching slash commands."
		if m.monitorSuggestions.busy {
			text = "Loading slash commands…"
		} else if m.monitorSuggestions.err {
			text = "Command catalogue unavailable. Use Codex."
		}
		return []string{colors.dimmed().Render(ansi.Truncate(text, max(width-4, 1), "…"))}
	}
	selected := min(m.monitorSuggestions.selected, len(choices)-1)
	start := max(selected-count+1, 0)
	var lines []string
	for i := start; i < min(start+count, len(choices)); i++ {
		o := choices[i]
		label := o.Label + " // " + strings.Join(strings.Fields(o.Help), " ")
		style := colors.label()
		if i == selected {
			style = style.Foreground(colors.background).Background(colors.primary).Bold(true)
		}
		lines = append(lines, style.Render(ansi.Truncate(label, max(width-4, 1), "…")))
	}
	return lines
}

func (m Model) updateMonitorSuggestions(msg tea.Msg) (Model, tea.Cmd, bool) {
	s := &m.monitorSuggestions
	if r, ok := msg.(monitorSuggestionResult); ok {
		if s.active && s.session == r.session && s.request == r.request {
			s.busy = false
			s.menu = r.menu
			s.err = r.err != nil
		}
		return m, nil, true
	}
	if !m.suggestionsActive() {
		s.active = false
		return m, nil, false
	}
	w, h := m.monitorPromptSize()
	if m.monitorPromptRows(w, h) == 0 {
		return m, nil, false
	}
	choices := m.suggestionChoices()
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			s.dismissed = m.monitorPrompt.input.Value()
			s.active = false
			return m, nil, true
		case "up", "down":
			d := 1
			if key.String() == "up" {
				d = -1
			}
			s.selected = min(max(s.selected+d, 0), max(len(choices)-1, 0))
			return m, nil, true
		case "tab", "enter":
			if s.busy {
				return m, nil, true
			}
			if len(choices) == 0 {
				return m, nil, false
			}
			o := choices[min(s.selected, len(choices)-1)]
			if key.String() == "tab" {
				m.monitorPrompt.input.SetValue(o.Label)
				m.monitorPrompt.input.CursorEnd()
				s.selected = 0
				return m, nil, true
			}
			return m.openMonitorCommands(o.Next)
		}
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		x, y := mouse.Mouse().X, mouse.Mouse().Y
		if i := m.monitorSuggestionAt(x, y); i >= 0 && i < len(choices) {
			s.selected = i
			if _, click := msg.(tea.MouseClickMsg); click && mouse.Mouse().Button == tea.MouseLeft {
				return m.openMonitorCommands(choices[i].Next)
			}
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m Model) monitorSuggestionAt(x, y int) int {
	w, h := m.monitorPromptSize()
	count := m.suggestionRows(h)
	if count == 0 {
		return -1
	}
	g := m.monitorDashboardLayout()
	x -= 4
	y -= g.meterY
	if m.monitorContextDetail == "" {
		a := m.monitorArea(g.contentWidth, g.meterHeight)
		sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
		rowY := a.topHeight + a.gap - 1
		found := false
		for i, s := range sessions {
			if s.id == m.monitorContextTarget() {
				mw, _, _ := monitorSessionColumnWidths(a.width)
				x -= mw + 1
				y -= rowY
				found = true
				break
			}
			rowY += heights[i]
		}
		if !found {
			return -1
		}
	}
	rows := m.monitorPromptRows(w, h)
	if rows == 0 {
		return -1
	}
	_, _, cy := monitorContextBodyLayout(h, rows)
	y -= cy
	if x < 0 || x >= w-4 || y < 0 || y >= count {
		return -1
	}
	choices := m.suggestionChoices()
	start := max(min(m.monitorSuggestions.selected, max(len(choices)-1, 0))-count+1, 0)
	return start + y
}

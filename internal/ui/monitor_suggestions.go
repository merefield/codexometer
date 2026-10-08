package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
		*s = monitorSuggestionState{active: true, session: m.monitorPrompt.session, request: s.request + 1, menu: localCommandsMenu()}
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

func (m Model) withMonitorSuggestions(view string, colors palette) string {
	popup, start := m.monitorSuggestionPopup()
	if popup.height == 0 {
		return view
	}
	choices := m.suggestionChoices()
	var lines []string
	if len(choices) == 0 {
		text := "No matching slash commands."
		if m.monitorSuggestions.busy {
			text = "Loading slash commands…"
		} else if m.monitorSuggestions.err {
			text = "Command catalogue unavailable. Use Codex."
		}
		lines = append(lines, colors.dimmed().Render(ansi.Truncate(text, popup.width-4, "…")))
	} else {
		selected := min(m.monitorSuggestions.selected, len(choices)-1)
		for i := start; i < min(start+popup.height-2, len(choices)); i++ {
			o := choices[i]
			label := o.Label + " // " + strings.Join(strings.Fields(o.Help), " ")
			style := colors.label().Background(colors.background).Width(popup.width - 2)
			if i == selected {
				style = style.Foreground(colors.background).Background(colors.primary).Bold(true)
			}
			lines = append(lines, style.Render(" "+ansi.Truncate(label, popup.width-4, "…")+" "))
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colors.primary).
		Background(colors.background).Width(popup.width - 2).Render(strings.Join(lines, "\n"))
	return lipgloss.NewCompositor(lipgloss.NewLayer(view), lipgloss.NewLayer(box).X(popup.x).Y(popup.y).Z(1)).Render()
}

func (m Model) updateMonitorSuggestions(msg tea.Msg) (Model, tea.Cmd, bool) {
	s := &m.monitorSuggestions
	if r, ok := msg.(monitorSuggestionResult); ok {
		if s.active && s.session == r.session && s.request == r.request {
			s.busy = false
			s.menu = r.menu
			s.menu.Choices = append(s.menu.Choices, localStatusLineChoice())
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
		if popup, _ := m.monitorSuggestionPopup(); popup.contains(x, y) {
			switch mouse.Mouse().Button {
			case tea.MouseWheelUp, tea.MouseWheelDown:
				d := 3
				if mouse.Mouse().Button == tea.MouseWheelUp {
					d = -3
				}
				s.selected = min(max(s.selected+d, 0), max(len(choices)-1, 0))
				return m, nil, true
			}
		}
		if i := m.monitorSuggestionAt(x, y); i >= 0 && i < len(choices) {
			s.selected = i
			if _, click := msg.(tea.MouseClickMsg); click && mouse.Mouse().Button == tea.MouseLeft {
				return m.openMonitorCommands(choices[i].Next)
			}
			return m, nil, true
		}
		// Popup borders and empty-state rows must not activate controls below.
		if popup, _ := m.monitorSuggestionPopup(); popup.contains(x, y) {
			return m, nil, true
		}
	}
	return m, nil, false
}

// One shared rectangle anchors rendering and hit testing immediately above
// the composer, without reserving rows or moving any existing controls.
func (m Model) monitorSuggestionPopup() (popup monitorRect, start int) {
	if !m.suggestionsActive() || m.monitorSuggestions.session != m.monitorPrompt.session {
		return
	}
	w, h := m.monitorPromptSize()
	rows := m.monitorPromptRows(w, h)
	if rows == 0 || w < 12 {
		return
	}
	g := m.monitorDashboardLayout()
	x, y := 4, g.meterY
	if m.monitorContextDetail == "" {
		a := m.monitorArea(g.contentWidth, g.meterHeight)
		sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
		rowY := a.topHeight + a.gap - 1
		found := false
		for i, s := range sessions {
			if s.id == m.monitorContextTarget() {
				mw, _, _ := monitorSessionColumnWidths(a.width)
				x += mw + 1
				y += rowY
				found = true
				break
			}
			rowY += heights[i]
		}
		if !found {
			return
		}
	}
	_, _, cy := monitorContextBodyLayout(h, rows)
	choices := m.suggestionChoices()
	count := min(max(len(choices), 1), max(cy-3, 0))
	if count == 0 {
		return
	}
	width := min(w-4, 44)
	for _, o := range choices {
		width = min(w-4, max(width, ansi.StringWidth(o.Label+" // "+strings.Join(strings.Fields(o.Help), " "))+4))
	}
	start = max(min(m.monitorSuggestions.selected, max(len(choices)-1, 0))-count+1, 0)
	return monitorRect{x: x, y: y + cy - count - 2, width: width, height: count + 2}, start
}

func (m Model) monitorSuggestionAt(x, y int) int {
	popup, start := m.monitorSuggestionPopup()
	if popup.height == 0 || x <= popup.x || x >= popup.x+popup.width-1 || y <= popup.y || y >= popup.y+popup.height-1 || len(m.suggestionChoices()) == 0 {
		return -1
	}
	return start + y - popup.y - 1
}

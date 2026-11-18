package ui

import (
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

func (m Model) inlineCommandPrefix(path string) string {
	if path == "goal/set" {
		return "/goal "
	}
	if strings.HasPrefix(path, "tier/") {
		if m.monitorCommands.menu.Path == path && m.monitorCommands.menu.Picker {
			return m.monitorCommands.menu.Title + " "
		}
		for _, menu := range []codex.SessionCommandMenu{m.monitorCommands.menu, m.monitorSuggestions.menu} {
			for _, choice := range menu.Choices {
				if choice.Next == path && strings.HasPrefix(choice.Label, "/") {
					return choice.Label + " "
				}
			}
		}
		return "/" // Keep internal paths hidden while fetching the public alias.
	}
	prefix := "/" + strings.ReplaceAll(path, "/", " ")
	if path != "" {
		prefix += " "
	}
	return prefix
}

// Submenus keep the slash draft in the real composer. Only the suffix after
// the current command path filters options; no catalogue refetch is needed.
func (m Model) commandQuery() string {
	return strings.TrimSpace(strings.TrimPrefix(m.monitorPrompt.input.Value(), m.monitorCommands.prefix))
}

func (m *Model) openInlineCommandLevel(path, argument, parentDraft string) tea.Cmd {
	// Only these local commands have browsable TUI submenus. Other local
	// paths (including theme/<id>) are actions and require Enter or a click.
	root, _, _ := strings.Cut(path, "/")
	if codex.IsLocalSessionCommand(root) && path != "theme" && path != "statusline" {
		return nil
	}
	p := &m.monitorCommands
	draft := m.inlineCommandPrefix(path) + argument
	if p.open {
		if len(p.history) >= 16 {
			return nil
		}
		m.monitorPrompt.input.SetValue(parentDraft)
		p.preserveDraft = &draft
		return m.navigateMonitorCommands(path)
	}
	m.monitorPrompt.input.SetValue(parentDraft)
	updated, cmd, _ := m.openMonitorCommands(path, draft)
	*m = updated
	return cmd
}

// Typing a separator after an exact advertised branch is equivalent to
// entering it from the list. Opening a menu never commits an action.
func (m *Model) advanceInlineCommand() tea.Cmd {
	p := &m.monitorCommands
	if !p.inline || p.busy || p.detail || p.menu.Input || p.menu.Multiple || commandInputReview(p.menu.Path) {
		return nil
	}
	// commandQuery trims the trailing separator; retain it for navigation.
	raw := strings.TrimPrefix(m.monitorPrompt.input.Value(), p.prefix)
	for _, choice := range p.menu.Choices {
		if choice.Next == "" {
			continue
		}
		names := choice.ParameterNames()
		exact := false
		for _, name := range names {
			exact = exact || strings.EqualFold(raw, name)
		}
		if exact {
			continue // A complete multiword label is not its short token plus an argument.
		}
		for _, name := range names {
			if len(raw) > len(name) && strings.EqualFold(raw[:len(name)], name) {
				suffix := raw[len(name):]
				argument := strings.TrimLeftFunc(suffix, unicode.IsSpace)
				if argument != suffix {
					return m.openInlineCommandLevel(choice.Next, argument, p.prefix+raw[:len(name)])
				}
			}
		}
	}
	return nil
}

func (m Model) commandChoiceIndices() []int {
	p := m.monitorCommands
	query := ""
	if p.inline && !p.menu.Input && !p.menu.Multiple && !commandInputReview(p.menu.Path) {
		query = strings.ToLower(m.commandQuery())
		if p.menu.Path == "permissions" {
			// Compound flags are validated in the existing typed review, not
			// silently dropped by selecting just the profile. Keep its match.
			if name, _, separated := codex.SplitSessionCommand(query); separated {
				query = name
			}
		}
	}
	var indices []int
	for i, choice := range p.menu.Choices {
		if choice.MatchesParameter(query) {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 && p.inline && p.menu.Path == "goal" {
		// /goal also accepts a free-form objective. Surface the advertised
		// create/replace branch rather than claiming valid text is invalid.
		for i, choice := range p.menu.Choices {
			if choice.Next == "goal/set" {
				return []int{i}
			}
		}
	}
	return indices
}

func commandInputReview(path string) bool {
	_, _, ok := codex.SessionCommandInputValue(path)
	return ok
}

func (m *Model) restoreCommandDraft() {
	if p := m.monitorCommands; p.inline && p.restore != nil {
		m.monitorPrompt.input.SetValue(p.restore.draft)
		m.monitorPrompt.input.CursorEnd()
	}
}

func (m *Model) moveInlineCommandSelection(delta int) {
	p := &m.monitorCommands
	indices := m.commandChoiceIndices()
	if len(indices) == 0 {
		p.selected = -1
		return
	}
	position := 0
	for i, index := range indices {
		if index == p.selected {
			position = i
		}
	}
	selected := indices[min(max(position+delta, 0), len(indices)-1)]
	if selected != p.selected {
		p.detail, p.reviewedChoice = false, ""
	}
	p.selected = selected
}

func (m Model) updateInlineCommands(msg tea.Msg) (Model, tea.Cmd, bool) {
	p := &m.monitorCommands
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			cmd := m.escapeMonitorCommands()
			return m, cmd, true
		case "alt+left":
			cmd := m.backMonitorCommands()
			return m, cmd, true
		case "ctrl+r":
			if !p.busy {
				p.restore = &monitorCommandLevel{selected: p.selected, input: p.input.Value(), draft: m.monitorPrompt.input.Value()}
				cmd := m.loadMonitorCommands(p.menu.Path, true)
				return m, cmd, true
			}
			return m, nil, true
		case "ctrl+c":
			m.closeMonitorCommands()
			return m, nil, false
		case "up", "down", "pgup", "pgdown":
			d := 1
			if key.String() == "up" || key.String() == "pgup" {
				d = -1
			}
			if strings.HasPrefix(key.String(), "pg") {
				d *= 10
			}
			m.moveInlineCommandSelection(d)
			return m, nil, true
		case "enter":
			if p.busy || p.notice != "" {
				return m, nil, true
			}
			if p.menu.Path == "permissions" && !p.detail && m.commandQuery() != "" {
				_, matching := m.commandChoice()
				if strings.Contains(m.commandQuery(), "--") || !matching {
					return m.openMonitorCommands("permissions " + m.commandQuery())
				}
			}
			if p.menu.Path == "goal" && !p.detail && m.commandQuery() != "" {
				if choice, ok := m.commandChoice(); ok {
					for _, name := range choice.ParameterNames() {
						if strings.EqualFold(m.commandQuery(), name) {
							return m.chooseMonitorCommand()
						}
					}
				}
				return m.openMonitorCommands("goal " + m.commandQuery())
			}
			if p.menu.Input {
				return m.reviewMonitorCommandInput()
			}
			if p.menu.Multiple && p.menu.Path == "statusline" {
				return m.confirmMonitorCommand()
			}
			if p.detail && time.Now().Before(p.until) {
				return m, nil, true // Enter never bypasses explicit confirmation.
			}
			return m.chooseMonitorCommand()
		case "tab":
			if choice, ok := m.commandChoice(); ok && !p.menu.Input && !p.menu.Multiple && !p.busy && !p.detail {
				m.monitorPrompt.input.SetValue(p.prefix + strings.TrimPrefix(choice.Label, "/"))
				m.monitorPrompt.input.CursorEnd()
			}
			return m, nil, true
		case "left", "backspace":
			if !p.menu.Multiple && !p.menu.Input && m.commandQuery() == "" && m.monitorCommandsCanBack() {
				cmd := m.backMonitorCommands()
				return m, cmd, true
			}
		case "c", "C":
			if !p.menu.Input && (p.detail || p.reviewedChoice != "" || p.menu.Multiple && m.commandQuery() == "") {
				return m.confirmMonitorCommand()
			}
		case "space", "right":
			if p.menu.Multiple {
				return m, nil, false // Existing checkbox/reorder handling below.
			}
		}
		if p.menu.Multiple && key.String() == "left" {
			return m, nil, false
		}
		if p.menu.Multiple {
			return m, nil, true // Checkbox lists are deliberately unfiltered.
		}
	}
	// Use the same editor (including cursor, paste and wrapping) at every level.
	// Keys such as q/t/r/f are text here, never global dashboard shortcuts.
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg:
		if p.menu.Multiple {
			return m, nil, true
		}
		before := m.monitorPrompt.input.Value()
		var cmd tea.Cmd
		m.monitorPrompt.input, cmd = m.monitorPrompt.input.Update(msg)
		if before != m.monitorPrompt.input.Value() {
			p.detail, p.reviewedChoice = false, ""
			if !strings.HasPrefix(m.monitorPrompt.input.Value(), p.prefix) {
				m.closeMonitorCommands()
				return m, tea.Batch(cmd, m.syncMonitorSuggestions()), true
			}
			if commandInputReview(p.menu.Path) {
				// Editing a reviewed name/path must obtain a new capability;
				// never confirm the previous value behind an edited draft.
				parent, _, _ := codex.SessionCommandInputValue(p.menu.Path)
				if n := len(p.history); n > 0 && p.history[n-1].path == parent {
					p.history = append([]monitorCommandLevel(nil), p.history[:n-1]...)
				}
				p.restore = &monitorCommandLevel{input: m.commandQuery(), draft: m.monitorPrompt.input.Value()}
				read := m.loadMonitorCommands(parent)
				return m, tea.Batch(cmd, read), true
			}
			if p.menu.Input {
				query := m.commandQuery()
				p.input.SetValue(query)
				if p.input.Value() != query {
					// Mirror the server's input limit/single-line normalization;
					// never review a silently truncated version of visible text.
					m.monitorPrompt.input.SetValue(p.prefix + p.input.Value())
					m.monitorPrompt.input.CursorEnd()
				}
			} else {
				indices := m.commandChoiceIndices()
				p.selected = -1
				if len(indices) > 0 {
					p.selected = indices[0]
				}
			}
			next := m.advanceInlineCommand()
			return m, tea.Batch(cmd, next), true
		}
		return m, cmd, true
	}
	return m, nil, false
}

package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

func (m Model) withLocalCommands(menu codex.SessionCommandMenu) codex.SessionCommandMenu {
	choices := make([]codex.SessionCommandChoice, 0, len(menu.Choices)+6)
	for _, choice := range menu.Choices {
		if !codex.IsLocalSessionCommand(choice.Label) {
			choices = append(choices, choice)
		}
	}
	choices = append(choices, codex.LocalSessionCommands()...)
	for i := range choices {
		if choices[i].Next == "exit" || choices[i].Next == "quit" {
			choices[i].Help = "Exit this Codexometer terminal interface. Codex sessions continue running."
		}
	}
	menu.Choices = choices
	return menu
}

func (m Model) localCommandsMenu() codex.SessionCommandMenu {
	return m.withLocalCommands(codex.SessionCommandMenu{Title: "/ COMMANDS", Help: "Local Codexometer commands. Shared Codex commands are available when the app-server is connected."})
}

func (m Model) localMonitorMenu(path string) (codex.SessionCommandMenu, bool) {
	switch path {
	case "statusline":
		return m.statusLineMenu(), true
	case "theme":
		menu := codex.SessionCommandMenu{Title: "/theme", Path: "theme", Help: "Choose a Codexometer display theme. Enter applies it and saves your local display preferences."}
		for theme := themeID(0); theme < themeCount; theme++ {
			menu.Choices = append(menu.Choices, codex.SessionCommandChoice{ID: "theme-" + strconv.Itoa(int(theme)), Label: paletteFor(theme).name, Next: "theme/" + strconv.Itoa(int(theme)), Selected: theme == m.theme})
		}
		return menu, true
	case "copy":
		choice := codex.SessionCommandChoice{ID: "copy-reply", Label: "Copy observed text", Next: "copy"}
		if m.monitorCopyText(m.monitorContextTarget()) == "" {
			choice.Unavailable = "No reply or activity text is available to copy for this session."
		}
		return codex.SessionCommandMenu{Title: "/copy", Path: "copy", Help: "Uses the same observed text and clipboard action as the session Copy button.", Choices: []codex.SessionCommandChoice{choice}}, true
	}
	return codex.SessionCommandMenu{}, false
}

// Consume a slash command, while preserving an ordinary unsent draft.
func (m *Model) finishLocalMonitorCommand() tea.Cmd {
	m.closeMonitorCommands()
	if m.monitorPrompt.session == m.monitorContextTarget() && len(m.monitorPrompt.offer.Questions) == 0 && codex.IsSessionCommand(m.monitorPrompt.input.Value()) {
		m.monitorPrompt.input.Reset()
	}
	m.monitorSuggestions = monitorSuggestionState{request: m.monitorSuggestions.request + 1}
	if m.monitorPrompt.input.ready && m.monitorPrompt.session == m.monitorContextTarget() {
		return m.monitorPrompt.input.Focus()
	}
	return nil
}

func (m Model) runLocalMonitorCommand(path string) (Model, tea.Cmd, bool) {
	switch path {
	case "usage":
		m.finishLocalMonitorCommand()
		updated, cmd := m.pressViewTab(viewUsage)
		return updated.(Model), cmd, true
	case "copy":
		id := m.monitorContextTarget()
		if m.monitorCopyText(id) == "" {
			return m, nil, false
		}
		focus := m.finishLocalMonitorCommand()
		m, cmd := m.activateMonitorCopy(id)
		return m, tea.Batch(cmd, focus), true
	case "exit", "quit":
		m.finishLocalMonitorCommand()
		updated, cmd := m.pressFooterButton(footerButtonQuit)
		return updated.(Model), cmd, true
	}
	if value, ok := strings.CutPrefix(path, "theme/"); ok {
		theme, err := strconv.Atoi(value)
		if err != nil || theme < 0 || theme >= int(themeCount) {
			return m, nil, false
		}
		m.theme = themeID(theme)
		m.prepareBenchmarkDetailTranscript()
		m.persistPreferences()
		cmd := m.finishLocalMonitorCommand()
		return m, cmd, true
	}
	return m, nil, false
}

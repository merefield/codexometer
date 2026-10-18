package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type inlineCommandClient struct {
	*suggestionClient
}

func (f *inlineCommandClient) SessionCommands(ctx context.Context, id, path string) (codex.SessionCommandMenu, error) {
	switch path {
	case "model":
		f.reads++
		return codex.SessionCommandMenu{Path: path, Choices: []codex.SessionCommandChoice{
			{ID: "a", Label: "Alpha", Help: "First model", Next: "model/alpha"},
			{ID: "b", Label: "Beta", Help: "Second model", Next: "model/beta"},
		}}, nil
	case "model/beta":
		f.reads++
		return codex.SessionCommandMenu{Path: path, Revision: "beta", Choices: []codex.SessionCommandChoice{
			{ID: "medium", Label: "Medium", Help: "Medium reasoning", Action: true},
			{ID: "high", Label: "High", Help: "High reasoning", Action: true},
		}}, nil
	}
	return f.suggestionClient.SessionCommands(ctx, id, path)
}

func inlineCommandModel(t *testing.T) (Model, *inlineCommandClient) {
	t.Helper()
	m, base := suggestionTestModel(t)
	f := &inlineCommandClient{suggestionClient: base}
	m.fetcher = f
	m.monitorPrompt.input.SetValue("/mo")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no submenu request")
	}
	m = next.(Model)
	next, _ = m.Update(cmd())
	return next.(Model), f
}

func TestMonitorInlineCommandsFilterAndDrillFromComposer(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		m, f := inlineCommandModel(t)
		m.height = 65
		m.monitorSessionData = m.monitorSessionData[:1]
		m.setRowContext("root-one", mode)
		m.monitorPrompt.input.Focus()
		area := m.monitorComposerPopupArea()
		if !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "/model " {
			t.Fatal("submenu left the command line")
		}
		before := m.monitorCommandPopup()
		reads := f.reads
		next, _ := m.Update(key('b'))
		m = next.(Model)
		rows := m.monitorCommandRows(80, 12)
		if len(rows) != 1 || rows[0].choice != 1 || m.monitorPrompt.input.Value() != "/model b" || f.reads != reads || m.monitorCommandPopup().height >= before.height {
			t.Fatal("submenu did not filter and shrink locally from composer typing")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = next.(Model)
		if m.monitorPrompt.input.Value() != "/model Beta" || f.calls != 0 {
			t.Fatal("Tab did not complete the submenu choice without applying it")
		}
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)
		if cmd == nil || m.monitorPrompt.input.Value() != "/model beta " || !m.monitorPrompt.input.Focused() {
			t.Fatal("drilling another level moved out of composer")
		}
		next, _ = m.Update(cmd())
		m = next.(Model)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = next.(Model)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)
		if !m.monitorCommands.detail || f.calls != 0 || m.commandChoiceIndices()[m.monitorCommands.selected] != 1 {
			t.Fatal("Enter did not stage the highlighted reasoning choice")
		}
		out := ansi.Strip(m.render())
		if !strings.Contains(out, "Medium") || !strings.Contains(out, "High") || !strings.Contains(out, i18n.Text("[ C CONFIRM ]")) || area != m.monitorComposerPopupArea() {
			t.Fatal("review replaced the expanding list or moved the composer")
		}
		next, cmd = m.Update(key('c'))
		if cmd == nil || f.calls != 0 {
			t.Fatal("missing explicit confirmation")
		}
		m = next.(Model)
		next, _ = m.Update(cmd())
		m = next.(Model)
		if f.calls != 1 || m.monitorCommands.open || m.monitorPrompt.input.Value() != "" || !m.monitorPrompt.input.Focused() {
			t.Fatal("successful confirmation did not return to a clean composer")
		}
	}
}

func TestMonitorInlineCommandsBackAndNoMatch(t *testing.T) {
	m, f := inlineCommandModel(t)
	for _, ch := range "qtrfc" {
		next, _ := m.Update(key(ch))
		m = next.(Model)
	}
	if m.monitorPrompt.input.Value() != "/model qtrfc" || !m.monitorPrompt.input.Focused() || len(m.commandChoiceIndices()) != 0 || f.calls != 0 {
		t.Fatal("typing ran a shortcut or retained hidden choices")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || f.calls != 0 || m.monitorCommands.detail {
		t.Fatal("no-match Enter acted on a hidden option")
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("Alt+Left did not return to the parent catalogue")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if !m.monitorCommands.open || !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "/mo" || len(m.commandChoiceIndices()) != 1 {
		t.Fatal("Back did not restore the parent command-line query")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.monitorCommands.open || !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "/mo" {
		t.Fatal("Escape at root did not dismiss the expansion and preserve draft")
	}
}

func TestMonitorInlineCheckboxesWholeListSpaceToggleEnterApply(t *testing.T) {
	m, _ := suggestionTestModel(t)
	store := &memoryPreferenceStore{}
	m.preferenceStore = store
	m.monitorPrompt.input.SetValue("/statusline")
	m, _, _ = m.openMonitorCommands("statusline")
	if !m.monitorCommands.inline || !m.monitorPrompt.input.Focused() || !m.monitorCommands.menu.Multiple {
		t.Fatal("checkbox menu did not retain the composer expansion")
	}
	count := len(m.monitorCommands.menu.Choices)
	was := m.monitorCommands.menu.Choices[0].Selected
	next, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = next.(Model)
	if m.monitorCommands.menu.Choices[0].Selected == was {
		t.Fatal("Space did not toggle selected checkbox")
	}
	next, _ = m.Update(key('q'))
	m = next.(Model)
	if len(m.commandChoiceIndices()) != count || m.monitorPrompt.input.Value() != "/statusline " {
		t.Fatal("checkbox list filtered or typed into the composer")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	if m.monitorCommands.selected != 1 {
		t.Fatal("Down did not navigate checkboxes")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.monitorCommands.open || len(store.saves) != 1 || m.monitorPrompt.input.Value() != "" || !m.monitorPrompt.input.Focused() {
		t.Fatal("Enter did not apply checkbox preferences and return to composer")
	}
}

func TestMonitorSlashShortcutStartsInComposer(t *testing.T) {
	m, _ := promptTestModel()
	next, cmd := m.Update(key('/'))
	m = next.(Model)
	if cmd == nil || m.monitorCommands.open || !m.monitorSuggestions.active || !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "/" {
		t.Fatal("slash shortcut did not start the original composer IntelliSense")
	}
}

func TestMonitorSlashShortcutPreservesUnfocusedOrdinaryDraft(t *testing.T) {
	m, _ := promptTestModel()
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("keep this unsent draft")
	m.monitorPrompt.input.Blur()
	next, _ := m.Update(key('/'))
	m = next.(Model)
	if m.monitorPrompt.input.Value() != "keep this unsent draft" || !m.monitorCommands.open || m.monitorCommands.inline {
		t.Fatal("slash shortcut overwrote an ordinary draft")
	}
}

func TestMonitorInlineCheckboxClickSurfaces(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		m, _ := suggestionTestModel(t)
		m.height = 65
		m.monitorSessionData = m.monitorSessionData[:1]
		m.setRowContext("root-one", mode)
		m.monitorPrompt.input.Focus()
		m.monitorPrompt.input.SetValue("/statusline")
		m, _, _ = m.openMonitorCommands("statusline")
		store := &memoryPreferenceStore{}
		m.preferenceStore = store
		label := "[x] " + m.monitorCommands.menu.Choices[0].Label
		x, y := renderedTextStart(t, m, label)
		next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = next.(Model)
		if m.monitorCommands.menu.Choices[0].Selected {
			t.Fatal("rendered checkbox click missed its option")
		}
		x, y = renderedTextStart(t, m, i18n.Text("[ ENTER APPLY ]"))
		next, _ = m.Update(tea.MouseMotionMsg{X: x, Y: y})
		m = next.(Model)
		if m.monitorCommands.footerHover != "confirm" {
			t.Fatal("rendered Apply button did not hover")
		}
		next, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = next.(Model)
		if m.monitorCommands.open || len(store.saves) != 1 {
			t.Fatal("rendered Apply button did not save exactly once")
		}
	}
}

func TestMonitorInlineRenameUsesComposerAndInvalidatesEditedReview(t *testing.T) {
	m, base := suggestionTestModel(t)
	f := &renameTestClient{commandTestClient: base.commandTestClient}
	m.fetcher = f
	m.monitorPrompt.input.SetValue("/rename")
	m, cmd, _ := m.openMonitorCommands("rename")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "/rename Current name" {
		t.Fatal("rename moved input out of composer")
	}
	next, _ = m.Update(key('r'))
	m = next.(Model)
	if m.monitorCommands.input.Value() != "Current namer" {
		t.Fatal("typing did not edit the command argument")
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)
	if !m.monitorCommands.detail || m.monitorPrompt.input.Value() != "/rename Current namer" || !m.monitorPrompt.input.Focused() {
		t.Fatal("review changed the readable command-line argument")
	}
	next, cmd = m.Update(key('t'))
	m = next.(Model)
	if cmd == nil || m.monitorCommands.detail || !m.monitorCommands.busy {
		t.Fatal("editing a reviewed argument did not invalidate the capability")
	}
	applyResult := func(command tea.Cmd) {}
	applyResult = func(command tea.Cmd) {
		switch result := command().(type) {
		case tea.BatchMsg:
			for _, child := range result {
				applyResult(child)
			}
		case monitorCommandsResult:
			next, _ := m.Update(result)
			m = next.(Model)
		}
	}
	applyResult(cmd)
	if !m.monitorCommands.menu.Input || m.monitorCommands.input.Value() != "Current namert" || m.monitorPrompt.input.Value() != "/rename Current namert" || f.calls != 0 {
		t.Fatal("edited argument was lost or the previous review was applied")
	}
}

func TestMonitorInlineCommandEditedFilterInvalidatesConfirmation(t *testing.T) {
	m, _ := inlineCommandModel(t)
	// Use an action list without another RPC to exercise the actual editor.
	m.monitorCommands.menu.Choices = []codex.SessionCommandChoice{
		{ID: "alpha", Label: "Alpha", Action: true},
		{ID: "beta", Label: "Beta", Action: true},
	}
	m, _, _ = m.chooseMonitorCommand()
	next, _ := m.Update(key('b'))
	m = next.(Model)
	if m.monitorCommands.detail || m.monitorCommands.selected != 1 {
		t.Fatal("editing the filter retained a previous review")
	}
	_, cmd, _ := m.confirmMonitorCommand()
	if cmd != nil {
		t.Fatal("edited filter could confirm an unreviewed option")
	}
	m, _, _ = m.chooseMonitorCommand()
	m.height = 8
	_, cmd, _ = m.confirmMonitorCommand()
	if cmd != nil {
		t.Fatal("invisible inline confirmation remained active")
	}
}

package ui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type localCommandSpy struct {
	*commandTestClient
	reads     int
	available bool
}

func (f *localCommandSpy) SessionCommands(context.Context, string, string) (codex.SessionCommandMenu, error) {
	f.reads++
	if f.available {
		return codex.SessionCommandMenu{Title: "/ COMMANDS", Choices: []codex.SessionCommandChoice{{ID: "model", Label: "/model", Next: "model"}}}, nil
	}
	return codex.SessionCommandMenu{}, errors.New("daemon unavailable")
}

func localCommandModel() Model {
	m := completedCopyModel()
	m.setRowContext("root-one", contextFull)
	m.monitorPrompt.session = "root-one"
	m.monitorPrompt.input = newMonitorEditor()
	return m
}

func TestLocalSlashCatalogueWithoutDaemon(t *testing.T) {
	m := localCommandModel()
	m, cmd, _ := m.openMonitorCommands("")
	if cmd != nil {
		t.Fatal("local catalogue requested the daemon")
	}
	for _, path := range []string{"usage", "copy", "theme", "exit", "quit", "statusline"} {
		found := false
		for _, choice := range m.monitorCommands.menu.Choices {
			if choice.Next == path {
				found = true
				if choice.Action {
					t.Fatal("local command became a server action")
				}
			}
		}
		if !found {
			t.Fatal("local command missing", path)
		}
	}
}

func TestLocalSlashUsageConsumesCommandAndPreservesOrdinaryDraft(t *testing.T) {
	for _, draft := range []string{"/usage", "keep ordinary draft"} {
		m := localCommandModel()
		m.monitorPrompt.input.SetValue(draft)
		m, cmd, _ := m.openMonitorCommands("/usage")
		if m.meterView != viewUsage || m.monitorCommands.open || cmd == nil {
			t.Fatal("usage did not use the existing view")
		}
		expected := draft
		if draft == "/usage" {
			expected = ""
		}
		if m.monitorDrafts["root-one"] != expected {
			t.Fatal("wrong retained draft", m.monitorDrafts)
		}
	}
}

func commandCopiesText(cmd tea.Cmd, text string) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if reflect.DeepEqual(msg, tea.SetClipboard(text)()) {
		return true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			if commandCopiesText(child, text) {
				return true
			}
		}
	}
	return false
}

func TestLocalSlashCopyUsesSelectedObservedTextAndNoRPC(t *testing.T) {
	m := localCommandModel()
	_, base := commandTestModel(t)
	spy := &localCommandSpy{commandTestClient: base}
	m.fetcher = spy
	m.monitorPrompt.input.SetValue("/copy")
	want := m.monitorCopyText("root-one")
	m, cmd, _ := m.openMonitorCommands("/copy")
	if !commandCopiesText(cmd, want) || m.monitorCommands.open || m.monitorPrompt.input.Value() != "" || spy.reads != 0 || spy.calls != 0 || m.monitorCopyFlash != "root-one" {
		t.Fatal("copy did not reuse the exact local action")
	}
	m.monitorSessionData[0].preview.Kind = codex.SessionContextApproval
	m, cmd, _ = m.openMonitorCommands("copy")
	if cmd != nil || !strings.Contains(ansi.Strip(m.render()), "No reply or activity text") || strings.Contains(ansi.Strip(m.render()), "ENTER OPEN") {
		t.Fatal("unavailable copy was offered or silently failed")
	}
}

func TestLocalSlashThemeSelectApplyCancelAndPersistence(t *testing.T) {
	m := localCommandModel()
	m.monitorPrompt.input.SetValue("/theme")
	store := &memoryPreferenceStore{}
	m.preferenceStore = store
	original := m.theme
	m, cmd, _ := m.openMonitorCommands("theme")
	if cmd != nil || len(m.monitorCommands.menu.Choices) != int(themeCount) || m.monitorCommands.selected != int(original) || !strings.Contains(ansi.Strip(m.render()), "[ ENTER APPLY ]") {
		t.Fatal("theme picker did not open locally")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	if m.theme != original || len(store.saves) != 0 {
		t.Fatal("highlight changed theme")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.theme != original || m.monitorPrompt.input.Value() != "/theme" {
		t.Fatal("Cancel changed theme or lost slash draft")
	}
	m, _, _ = m.openMonitorCommands("theme")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.theme != original.next() || len(store.saves) != 1 || m.monitorCommands.open || m.monitorPrompt.input.Value() != "" {
		t.Fatal("Enter did not apply and persist local theme")
	}
}

func TestLocalSlashExitAliasesUseLocalQuit(t *testing.T) {
	for _, path := range []string{"exit", "quit"} {
		m := localCommandModel()
		_, base := commandTestModel(t)
		spy := &localCommandSpy{commandTestClient: base}
		m.fetcher = spy
		m, cmd, _ := m.openMonitorCommands(path)
		if cmd == nil || m.flashedButton != footerButtonQuit || spy.reads != 0 || spy.calls != 0 {
			t.Fatal("exit did not use local Quit", path)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("exit did not quit the interface", path)
		}
	}
}

func TestLocalSlashFallbackAndThemeBackUseVisitedCatalogue(t *testing.T) {
	m := localCommandModel()
	_, base := commandTestModel(t)
	spy := &localCommandSpy{commandTestClient: base}
	m.fetcher = spy
	m, cmd, _ := m.openMonitorCommands("")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if spy.reads != 1 || len(m.monitorCommands.menu.Choices) != 6 {
		t.Fatal("failed remote discovery hid local commands")
	}
	for i, choice := range m.monitorCommands.menu.Choices {
		if choice.Next == "theme" {
			m.monitorCommands.selected = i
		}
	}
	m, _, _ = m.chooseMonitorCommand()
	if len(m.monitorCommands.history) != 1 {
		t.Fatal("theme lost its visited parent")
	}
	m.fetcher = nil
	cmd = m.backMonitorCommands()
	if cmd != nil || m.monitorCommands.menu.Path != "" || m.monitorCommands.menu.Choices[m.monitorCommands.selected].Next != "theme" {
		t.Fatal("Back did not restore local catalogue selection")
	}
}

func TestLocalSlashThemeBackRestoresSelectionAfterRemoteRead(t *testing.T) {
	for _, fails := range []bool{false, true} {
		m := localCommandModel()
		_, base := commandTestModel(t)
		m.fetcher = &localCommandSpy{commandTestClient: base, available: !fails}
		m, cmd, _ := m.openMonitorCommands("")
		next, _ := m.Update(cmd())
		m = next.(Model)
		for i, choice := range m.monitorCommands.menu.Choices {
			if choice.Next == "theme" {
				m.monitorCommands.selected = i
			}
		}
		m, _, _ = m.chooseMonitorCommand()
		cmd = m.backMonitorCommands()
		next, _ = m.Update(cmd())
		m = next.(Model)
		if m.monitorCommands.menu.Choices[m.monitorCommands.selected].Next != "theme" || m.monitorCommands.restore != nil {
			t.Fatal("Back lost local selection after remote catalogue read", fails)
		}
	}
}

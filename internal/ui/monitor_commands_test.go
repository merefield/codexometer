package ui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type commandTestClient struct {
	*promptTestClient
	calls int
}

func (f *commandTestClient) SessionCommands(context.Context, string, string) (codex.SessionCommandMenu, error) {
	return codex.SessionCommandMenu{Title: "/ COMMANDS", Path: "model/test", Revision: "r", Choices: []codex.SessionCommandChoice{{ID: "choice", Label: "Medium", Help: "Live effort help", Action: true}}}, nil
}
func (f *commandTestClient) ExecuteSessionCommand(context.Context, string, string, string, string) error {
	f.calls++
	return nil
}

func commandTestModel(t *testing.T) (Model, *commandTestClient) {
	t.Helper()
	m, p := promptTestModel()
	f := &commandTestClient{promptTestClient: p}
	m.fetcher = f
	m.monitorPrompt.input = newMonitorEditor()
	m, cmd, _ := m.openMonitorCommands("")
	if cmd == nil {
		t.Fatal("no catalogue request")
	}
	n, _ := m.Update(cmd())
	return n.(Model), f
}
func TestMonitorCommandsConfirmationAndDraft(t *testing.T) {
	m, f := commandTestModel(t)
	m.monitorPrompt.input.SetValue("keep draft")
	m, _, _ = m.chooseMonitorCommand()
	if !m.monitorCommands.detail || f.calls != 0 {
		t.Fatal("selection should show help, not execute")
	}
	n, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = n.(Model)
	if cmd != nil || f.calls != 0 {
		t.Fatal("Enter bypassed explicit confirmation")
	}
	n, cmd = m.Update(key('c'))
	m = n.(Model)
	if cmd == nil {
		t.Fatal("missing confirmation command")
	}
	msg := cmd()
	n, _ = m.Update(msg)
	m = n.(Model)
	if f.calls != 1 || m.monitorPrompt.input.Value() != "keep draft" {
		t.Fatal("wrong command/draft handling")
	}
	_, again, _ := m.confirmMonitorCommand()
	if again != nil {
		t.Fatal("success notice allowed a second send")
	}
	n, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = n.(Model)
	if m.monitorCommands.open || m.monitorContextDetail != "root-one" {
		t.Fatal("Escape failed to restore detail")
	}
}
func TestMonitorCommandsRenderedClickTargets(t *testing.T) {
	for _, width := range []int{60, 120, 180} {
		m, f := commandTestModel(t)
		m.width = width
		x, y := renderedTextStart(t, m, "Medium")
		n, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = n.(Model)
		if !m.monitorCommands.detail {
			t.Fatalf("width %d choice missed: %s", width, ansi.Strip(m.render()))
		}
		x, y = renderedTextStart(t, m, "[ C CONFIRM ]")
		n, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = n.(Model)
		if cmd == nil {
			t.Fatalf("width %d confirm missed", width)
		}
		cmd()
		if f.calls != 1 {
			t.Fatal("wrong click action")
		}
	}
}
func TestMonitorCommandsIgnoreStaleResultsAndExpiredConfirmation(t *testing.T) {
	m, f := commandTestModel(t)
	m, _, _ = m.chooseMonitorCommand()
	m.monitorCommands.until = time.Now().Add(-time.Second)
	_, cmd, _ := m.confirmMonitorCommand()
	if cmd != nil || f.calls != 0 {
		t.Fatal("expired confirmation sent")
	}
	m.closeMonitorCommands()
	n, _ := m.Update(monitorCommandsResult{session: "root-one", request: m.monitorCommands.request - 1, menu: codex.SessionCommandMenu{Title: "stale"}})
	if n.(Model).monitorCommands.open {
		t.Fatal("late result reopened menu")
	}
}

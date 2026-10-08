package ui

import (
	"context"
	"errors"
	"strconv"
	"strings"
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

type revisionCommandClient struct {
	*commandTestClient
	revision, reads int
	reject          bool
}

func (f *revisionCommandClient) SessionCommands(ctx context.Context, id, path string) (codex.SessionCommandMenu, error) {
	f.reads++
	menu, err := f.commandTestClient.SessionCommands(ctx, id, path)
	menu.Revision = strconv.Itoa(f.revision)
	return menu, err
}

func (f *revisionCommandClient) ExecuteSessionCommand(_ context.Context, _, _, revision, _ string) error {
	f.calls++
	if f.reject || revision != strconv.Itoa(f.revision) {
		return errors.New("stale or rejected command")
	}
	f.revision++
	return nil
}

func TestMonitorCommandsSuccessfulChangesDiscardOldCatalogues(t *testing.T) {
	m, base := commandTestModel(t)
	f := &revisionCommandClient{commandTestClient: base}
	m.fetcher = f
	for attempt := 0; attempt < 2; attempt++ {
		var cmd tea.Cmd
		m, cmd, _ = m.openMonitorCommands("model/test")
		m, _ = approvalEvent(m, cmd())
		if m.monitorCommands.menu.Revision != strconv.Itoa(attempt) {
			t.Fatal("reopened commands did not fetch a fresh revision")
		}
		m.monitorPrompt.input.SetValue("/model")
		m.monitorSuggestions = monitorSuggestionState{active: true, busy: true, session: "root-one", request: 42, menu: m.monitorCommands.menu}
		m, _, _ = m.chooseMonitorCommand()
		m, cmd, _ = m.confirmMonitorCommand()
		if cmd == nil {
			t.Fatal("missing confirmed change")
		}
		m, _ = approvalEvent(m, cmd())
		if m.monitorCommands.open || m.monitorCommands.detail || m.monitorCommands.menu.Revision != "" || len(m.monitorCommands.menu.Choices) > 0 || m.monitorSuggestions.active || len(m.monitorSuggestions.menu.Choices) > 0 {
			t.Fatal("successful change retained old menus")
		}
		if m.monitorPrompt.input.Value() != "" || !m.monitorPrompt.input.Focused() || !strings.Contains(ansi.Strip(m.render()), "Change requested.") {
			t.Fatal("success did not clear and focus composer with a visible notice")
		}
		m, _ = approvalEvent(m, monitorSuggestionResult{session: "root-one", request: 42, menu: codex.SessionCommandMenu{Revision: "stale"}})
		if m.monitorSuggestions.menu.Revision != "" {
			t.Fatal("late suggestions restored an invalidated revision")
		}
		m.monitorPrompt.input.SetValue("/")
		cmd = m.syncMonitorSuggestions()
		if cmd == nil {
			t.Fatal("typing slash reused the invalidated catalogue")
		}
		m, _ = approvalEvent(m, cmd())
		if m.monitorSuggestions.menu.Revision != strconv.Itoa(attempt+1) {
			t.Fatal("suggestions did not fetch the new revision")
		}
	}
	if f.calls != 2 || f.reads != 4 {
		t.Fatalf("expected two independent confirmations and fresh reads: calls=%d reads=%d", f.calls, f.reads)
	}
}

func TestMonitorCommandsFailedChangeRetainsDraftAndError(t *testing.T) {
	m, base := commandTestModel(t)
	f := &revisionCommandClient{commandTestClient: base, reject: true}
	m.fetcher = f
	m.monitorPrompt.input.SetValue("/model")
	m, _, _ = m.chooseMonitorCommand()
	m, cmd, _ := m.confirmMonitorCommand()
	m, _ = approvalEvent(m, cmd())
	if !m.monitorCommands.open || !m.monitorCommands.detail || m.monitorPrompt.input.Value() != "/model" || !strings.Contains(ansi.Strip(m.render()), "Command unavailable or unconfirmed") {
		t.Fatal("failed change lost its draft or error context")
	}
	_, cmd, _ = m.confirmMonitorCommand()
	if cmd != nil || f.calls != 1 {
		t.Fatal("failed change allowed an automatic retry")
	}
}

func TestMonitorCommandsLateSuccessCannotClearAnotherSessionDraft(t *testing.T) {
	m, _ := commandTestModel(t)
	m, _, _ = m.chooseMonitorCommand()
	m, cmd, _ := m.confirmMonitorCommand()
	result := cmd()
	m.monitorContextDetail = "root-two"
	m.monitorPrompt.session = "root-two"
	m.monitorPrompt.input.SetValue("/keep this other draft")
	m, cmd = approvalEvent(m, result)
	if cmd != nil || m.monitorCommands.open || m.monitorPrompt.session != "root-two" || m.monitorPrompt.input.Value() != "/keep this other draft" || m.monitorPrompt.notice != "" {
		t.Fatal("late success modified the newly selected session")
	}
}

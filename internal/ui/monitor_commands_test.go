package ui

import (
	"context"
	"errors"
	"net/url"
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

type renameTestClient struct {
	*commandTestClient
	path string
}

func (f *renameTestClient) SessionCommands(_ context.Context, _ string, path string) (codex.SessionCommandMenu, error) {
	f.path = path
	m := codex.SessionCommandMenu{Title: "/rename", Path: path, Revision: "rename-revision"}
	if path == "rename" {
		m.Input = true
		m.Value = "Current name"
	} else {
		name, _ := url.PathUnescape(strings.TrimPrefix(path, "rename/"))
		m.Choices = []codex.SessionCommandChoice{{ID: "rename-choice", Label: "Rename to " + name, Help: "Only the saved name changes.", Action: true}}
	}
	return m, nil
}
func TestMonitorRenameEditorReviewAndConfirmation(t *testing.T) {
	m, base := commandTestModel(t)
	f := &renameTestClient{commandTestClient: base}
	m.fetcher = f
	m.monitorPrompt.input.SetValue("unsent ordinary draft")
	m, cmd, _ := m.openMonitorCommands("rename")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !m.monitorCommands.menu.Input || !m.monitorCommands.input.Focused() || m.monitorCommands.input.Value() != "Current name" {
		t.Fatal("rename editor missing or not focused")
	}
	m.monitorCommands.input.SetValue("quota / review")
	// Letters used as global shortcuts belong to the name editor.
	next, _ = m.Update(key('r'))
	m = next.(Model)
	if !strings.Contains(m.monitorCommands.input.Value(), "r") || !m.monitorCommands.open {
		t.Fatal("name editor lost input")
	}
	m.monitorCommands.input.SetValue("quota / review")
	m, cmd, _ = m.updateMonitorCommands(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !m.monitorCommands.busy || f.calls != 0 {
		t.Fatal("review missing or rename sent before confirmation")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if f.path != "rename/"+url.PathEscape("quota / review") || !m.monitorCommands.detail || !strings.Contains(ansi.Strip(m.render()), "quota / review") {
		t.Fatal("new name not reviewed", f.path)
	}
	m, cmd, _ = m.updateMonitorCommands(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || f.calls != 0 {
		t.Fatal("Enter bypassed rename confirmation")
	}
	m, cmd, _ = m.confirmMonitorCommand()
	if cmd == nil {
		t.Fatal("missing confirmed rename")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if f.calls != 1 || m.monitorCommands.open || m.monitorPrompt.input.Value() != "unsent ordinary draft" || !strings.Contains(m.monitorPrompt.notice, "rename requested") {
		t.Fatal("rename did not restore draft and notice")
	}
}

func renameEditorTestModel(t *testing.T) Model {
	t.Helper()
	m, base := commandTestModel(t)
	m.fetcher = &renameTestClient{commandTestClient: base}
	m, cmd, _ := m.openMonitorCommands("rename")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !m.monitorCommands.menu.Input || !m.monitorCommands.input.Focused() {
		t.Fatal("rename editor did not open")
	}
	return m
}

func TestMonitorRenameEditorKeepsBackgroundUpdates(t *testing.T) {
	t.Run("poll ticker", func(t *testing.T) {
		m := renameEditorTestModel(t)
		phase := m.phase
		next, cmd := m.Update(secondMsg(time.Now()))
		m = next.(Model)
		if m.phase != phase+1 || cmd == nil || !m.monitorFetchActive {
			t.Fatal("rename editor swallowed the ticker: animation and polling will never resume")
		}
		if !m.monitorCommands.open || m.monitorCommands.input.Value() != "Current name" {
			t.Fatal("background tick changed the rename draft")
		}
	})
	t.Run("telemetry and steer reconciliation", func(t *testing.T) {
		m := renameEditorTestModel(t)
		m.monitorRequest, m.monitorFetchActive = 7, true
		m.recordMonitorSteer(monitorSteerReceipt{session: "root-one", thread: "root-one", turn: "turn", text: "new guidance"})
		next, _ := m.Update(monitorFetchedMsg{kind: monitorFetchSample, sequence: 7, at: time.Now(), usage: codex.LiveUsageSnapshot{
			TotalTokens: 100,
			Sessions: []codex.LiveUsageSession{{ID: "root-one", Name: "Renamed root", TotalTokens: 100, Active: true,
				Attention: codex.SessionAttentionApproval,
				Context:   codex.SessionContext{Kind: codex.SessionContextApproval, ThreadID: "root-one", TurnID: "turn", Text: "new approval", LatestGuidance: "new guidance", LatestGuidanceID: "guidance-2"},
			}},
		}})
		m = next.(Model)
		if m.monitorFetchActive || m.monitorLatest != 100 || m.monitorSessionData[0].name != "Renamed root" || m.monitorSessionData[0].preview.Text != "new approval" || m.monitorSessionData[0].attention != codex.SessionAttentionApproval || len(m.monitorSteers) != 0 {
			t.Fatal("rename editor swallowed telemetry: name, approvals and sent-steer receipts remain stale")
		}
		// The editor can close normally even in the buggy version. Verify that
		// the completed fetch was released so later ticks schedule another read.
		m.closeMonitorCommands()
		phase := m.phase
		next, cmd := m.Update(secondMsg(time.Now().Add(2 * time.Second)))
		m = next.(Model)
		if cmd == nil || m.phase != phase+1 || m.monitorRequest != 8 || !m.monitorFetchActive {
			t.Fatal("polling did not continue after leaving the rename editor")
		}
	})
	t.Run("window resize", func(t *testing.T) {
		m := renameEditorTestModel(t)
		next, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 50})
		m = next.(Model)
		if m.width != 180 || m.height != 50 {
			t.Fatal("rename editor swallowed window size")
		}
	})
}

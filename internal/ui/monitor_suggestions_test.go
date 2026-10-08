package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type suggestionClient struct {
	*commandTestClient
	reads int
}

func (f *suggestionClient) SessionCommands(ctx context.Context, id, path string) (codex.SessionCommandMenu, error) {
	f.reads++
	if path != "" {
		return f.commandTestClient.SessionCommands(ctx, id, path)
	}
	return codex.SessionCommandMenu{Title: "/ COMMANDS", Choices: []codex.SessionCommandChoice{
		{ID: "model", Label: "/model", Help: "Choose model and reasoning", Next: "model"},
		{ID: "mcp", Label: "/mcp", Help: "Browse tool descriptions", Next: "mcp"},
	}}, nil
}
func suggestionTestModel(t *testing.T) (Model, *suggestionClient) {
	t.Helper()
	m, p := promptTestModel()
	f := &suggestionClient{commandTestClient: &commandTestClient{promptTestClient: p}}
	m.fetcher = f
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("/")
	cmd := m.syncMonitorSuggestions()
	if cmd == nil {
		t.Fatal("missing catalogue fetch")
	}
	n, _ := m.Update(cmd())
	return n.(Model), f
}

func TestMonitorSuggestionsFilterCompleteAndOpen(t *testing.T) {
	m, f := suggestionTestModel(t)
	n, _ := m.Update(key('m'))
	m = n.(Model)
	if len(m.suggestionChoices()) != 2 || f.reads != 1 {
		t.Fatal("typing did not filter locally")
	}
	n, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = n.(Model)
	n, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = n.(Model)
	if m.monitorPrompt.input.Value() != "/mcp" || f.calls != 0 {
		t.Fatal("Tab did not complete without sending")
	}
	n, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = n.(Model)
	if !m.monitorCommands.open || cmd == nil || f.calls != 0 || len(f.answers) != 0 {
		t.Fatal("Enter did not open options without sending")
	}
}

func TestMonitorSuggestionsEscapeAndSessionIsolation(t *testing.T) {
	m, _ := suggestionTestModel(t)
	n, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = n.(Model)
	if m.suggestionsActive() || !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "/" {
		t.Fatal("Escape should only dismiss suggestions")
	}
	n, _ = m.Update(key('m'))
	m = n.(Model)
	if !m.suggestionsActive() {
		t.Fatal("editing did not reopen suggestions")
	}
	m.monitorContextDetail = "root-two"
	n, _ = m.Update(monitorSuggestionResult{session: "root-one", request: m.monitorSuggestions.request - 1, menu: codex.SessionCommandMenu{Title: "stale"}})
	m = n.(Model)
	if len(m.suggestionChoices()) != 0 {
		t.Fatal("suggestions crossed session boundary")
	}
}

func TestMonitorSuggestionsRenderedClicks(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		for _, width := range []int{100, 180} {
			m, f := suggestionTestModel(t)
			m.width = width
			m.height = 65
			m.monitorSessionData = m.monitorSessionData[:1]
			m.monitorSessionData[0].preview.Text = "Ready."
			m.setRowContext("root-one", mode)
			m.focusMonitorPrompt()
			if !strings.Contains(ansi.Strip(m.render()), "/mcp // Browse tool") {
				t.Fatalf("mode %d width %d suggestions missing", mode, width)
			}
			x, y := renderedTextStart(t, m, "/mcp // Browse tool")
			if got := m.monitorSuggestionAt(x, y); got != 1 {
				t.Fatalf("mode %d width %d hit %d at %d,%d", mode, width, got, x, y)
			}
			n, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			m = n.(Model)
			if !m.monitorCommands.open || cmd == nil || f.calls != 0 {
				t.Fatal("suggestion click did not open safe menu")
			}
		}
	}
}

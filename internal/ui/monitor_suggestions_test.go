package ui

import (
	"context"
	"image"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
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

func TestMonitorSuggestionPopupShrinksWithoutMovingComposer(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		m, f := suggestionTestModel(t)
		m.width, m.height = 120, 50
		m.monitorSessionData = m.monitorSessionData[:1]
		m.setRowContext("root-one", mode)
		m.focusMonitorPrompt()
		w, h := m.monitorPromptSize()
		composerRows := m.monitorPromptRows(w, h)
		initial, _ := m.monitorSuggestionPopup()
		if initial.height != 5 { // three commands and a two-line border
			t.Fatalf("mode %d: popup height %d", mode, initial.height)
		}
		for _, step := range []struct {
			text  string
			count int
		}{{"/m", 2}, {"/mo", 1}, {"/none", 0}} {
			m.monitorPrompt.input.SetValue(step.text)
			if cmd := m.syncMonitorSuggestions(); cmd != nil || f.reads != 1 {
				t.Fatal("filtering refetched the catalogue")
			}
			popup, _ := m.monitorSuggestionPopup()
			if popup.height != max(step.count, 1)+2 || popup.y+popup.height != initial.y+initial.height || m.monitorPromptRows(w, h) != composerRows {
				t.Fatalf("mode %d query %s: popup did not shrink in place: %+v", mode, step.text, popup)
			}
			view := m.render()
			if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
				t.Fatal("popup overflowed the terminal")
			}
			if m.monitorSuggestionAt(popup.x, popup.y+1) != -1 || m.monitorSuggestionAt(popup.x+1, popup.y) != -1 {
				t.Fatal("popup border became a suggestion target")
			}
			if step.count == 0 && (!strings.Contains(ansi.Strip(view), "No matching") || m.monitorSuggestionAt(popup.x+1, popup.y+1) != -1) {
				t.Fatal("empty state became selectable or disappeared")
			}
		}
	}
}

func TestMonitorSuggestionPopupScrollAndResize(t *testing.T) {
	m, _ := suggestionTestModel(t)
	m.width, m.height = 100, 24
	for i := 0; i < 30; i++ {
		m.monitorSuggestions.menu.Choices = append(m.monitorSuggestions.menu.Choices, codex.SessionCommandChoice{ID: "other", Label: "/other", Help: "Other command"})
	}
	m.monitorSuggestions.selected = len(m.suggestionChoices()) - 1
	for _, size := range [][2]int{{100, 24}, {60, 20}, {180, 60}} {
		m.width, m.height = size[0], size[1]
		popup, start := m.monitorSuggestionPopup()
		if popup.height == 0 || popup.x < 0 || popup.y < 0 || popup.x+popup.width > m.width || popup.y+popup.height > m.height {
			t.Fatalf("popup not bounded at %v: %+v", size, popup)
		}
		last := start + popup.height - 3
		if got := m.monitorSuggestionAt(popup.x+1, popup.y+popup.height-2); got != last || last != m.monitorSuggestions.selected {
			t.Fatalf("scrolled hit target %d want %d", got, m.monitorSuggestions.selected)
		}
		before := m.monitorSuggestions.selected
		n, cmd := m.Update(tea.MouseWheelMsg{X: popup.x + 1, Y: popup.y + 1, Button: tea.MouseWheelUp})
		if cmd != nil || n.(Model).monitorSuggestions.selected != max(before-3, 0) {
			t.Fatal("mouse wheel did not navigate popup without executing a command")
		}
	}
}

func TestMonitorSuggestionHelpIsSubduedAndSingleLine(t *testing.T) {
	m, _ := suggestionTestModel(t)
	m.width, m.height = 80, 30
	m.monitorSuggestions.menu.Choices = []codex.SessionCommandChoice{{ID: "model", Label: "/model", Help: strings.Repeat("Long help\n", 40)}}
	popup, _ := m.monitorSuggestionPopup()
	colors := paletteFor(m.theme)
	view := m.render()
	line := strings.Split(ansi.Strip(view), "\n")[popup.y+1]
	if !strings.Contains(line, "/model // Long help") || !strings.Contains(line, "…") || popup.height != 3 {
		t.Fatal("long help did not stay on one ellipsized row", line)
	}
	cells := uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(view).Draw(&cells, image.Rect(0, 0, m.width, m.height))
	command := cells.CellAt(popup.x+2, popup.y+1)
	help := cells.CellAt(popup.x+2+len("/model // "), popup.y+1)
	if reflect.DeepEqual(command.Style.Bg, help.Style.Bg) {
		t.Fatal("help inherited the command highlight")
	}
	want := uv.NewScreenBuffer(1, 1)
	uv.NewStyledString(colors.dimmed().Render("x")).Draw(&want, image.Rect(0, 0, 1, 1))
	if !reflect.DeepEqual(help.Style.Fg, want.CellAt(0, 0).Style.Fg) {
		t.Fatal("help is not subdued")
	}
}

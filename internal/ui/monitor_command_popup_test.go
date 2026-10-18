package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorCommandPopupReadOnlySession(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		for _, size := range [][2]int{{80, 30}, {120, 40}, {180, 65}} {
			t.Run(fmt.Sprintf("mode%d/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				m := localCommandModel() // No fetcher or live prompt offer.
				m.width, m.height = size[0], size[1]
				m.monitorSessionData = m.monitorSessionData[:1]
				m.setRowContext("root-one", mode)
				w, h := m.monitorPromptSize()
				if m.monitorPromptRows(w, h) != 0 {
					t.Fatal("fixture must not have a composer")
				}
				if mode == contextFull {
					next, _ := m.Update(key('/'))
					m = next.(Model)
				} else {
					// The bare '/' shortcut is detail-only without a composer;
					// also exercise the shared popup geometry in a wide row.
					m, _, _ = m.openMonitorCommands("")
				}
				if !m.monitorCommands.open || m.monitorCommands.inline || m.monitorCommandPopup().height == 0 {
					t.Fatal("read-only slash picker is not visible")
				}
				x, y := renderedTextStart(t, m, "/statusline")
				if !m.monitorCommandPopup().contains(x, y) {
					t.Fatal("read-only choice rendered outside its click surface")
				}
				next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				m = next.(Model)
				if m.monitorCommands.menu.Path != "statusline" {
					t.Fatal("read-only choice click did not open the picker")
				}
				before := m.monitorCommands.menu.Choices[0].Selected
				next, _ = m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
				m = next.(Model)
				if m.monitorCommands.menu.Choices[0].Selected == before {
					t.Fatal("read-only checkbox keyboard navigation did not toggle")
				}
				x, y = renderedTextStart(t, m, "[ C APPLY ]")
				next, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				m = next.(Model)
				if m.monitorCommands.open || m.monitorStatusLine == nil {
					t.Fatal("read-only Apply click did not save and close the picker")
				}
				if m.rowContextMode("root-one") != mode {
					t.Fatal("read-only picker changed the session detail mode")
				}
			})
		}
	}
}

func TestMonitorCommandPopupPreservesContextComposerAndRowMode(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		for _, size := range [][2]int{{80, 30}, {120, 40}, {180, 65}} {
			m, f := suggestionTestModel(t)
			m.width, m.height = size[0], size[1]
			m.monitorSessionData = m.monitorSessionData[:1]
			m.monitorSessionData[0].preview.Text = "CONVERSATION STAYS VISIBLE"
			m.setRowContext("root-one", mode)
			m.focusMonitorPrompt()
			before := m.monitorComposerPopupArea()
			suggestions, _ := m.monitorSuggestionPopup()
			next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(Model)
			if cmd == nil {
				t.Fatal("no submenu fetch")
			}
			popup := m.monitorCommandPopup()
			if popup.height == 0 || popup.y+popup.height != suggestions.y+suggestions.height || before != m.monitorComposerPopupArea() || m.rowContextMode("root-one") != mode {
				t.Fatalf("%v mode %d moved composer or forced full detail: before=%+v after=%+v popup=%+v", size, mode, before, m.monitorComposerPopupArea(), popup)
			}
			next, _ = m.Update(cmd())
			m = next.(Model)
			out := ansi.Strip(m.render())
			if !strings.Contains(out, "CONVERSATION STAYS VISIBLE") || !strings.Contains(out, "Medium") || f.calls != 0 {
				t.Fatalf("submenu replaced context or applied a command: %s", out)
			}
			if lipgloss.Width(out) > m.width || lipgloss.Height(out) > m.height {
				t.Fatalf("popup exceeds viewport at %v mode %d", size, mode)
			}
			m, _, _ = m.chooseMonitorCommand()
			if !m.monitorCommands.detail || m.rowContextMode("root-one") != mode || m.monitorComposerPopupArea() != before {
				t.Fatal("review moved away from its bottom popup")
			}
			m, _, _ = m.updateMonitorCommands(tea.KeyPressMsg{Code: tea.KeyLeft})
			if m.monitorCommands.detail || !m.monitorCommands.open {
				t.Fatal("Back did not return to the options in place")
			}
			cmd = m.backMonitorCommands()
			if cmd == nil {
				t.Fatal("drilling in from suggestions did not retain the root level")
			}
			next, _ = m.Update(cmd())
			m = next.(Model)
			if m.monitorCommands.menu.Path != "" || !m.monitorCommands.open || m.monitorComposerPopupArea() != before {
				t.Fatal("Back did not return to the root list in place")
			}
		}
	}
}

func TestMonitorCommandPopupRenderedSurfaces(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		for _, size := range [][2]int{{80, 30}, {120, 40}, {180, 65}} {
			m, f := commandTestModel(t)
			m.width, m.height = size[0], size[1]
			m.monitorSessionData = m.monitorSessionData[:1]
			m.monitorSessionData[0].preview.Text = "Visible context"
			m.setRowContext("root-one", mode)
			popup := m.monitorCommandPopup()
			if popup.height == 0 {
				t.Fatal("popup missing")
			}
			x, y := renderedTextStart(t, m, "Medium")
			if !popup.contains(x, y) {
				t.Fatal("choice rendered outside the popup")
			}
			next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			m = next.(Model)
			if !m.monitorCommands.detail || f.calls != 0 {
				t.Fatal("choice click failed to review, or applied early")
			}
			x, y = renderedTextStart(t, m, i18n.Text("[ C CONFIRM ]"))
			next, _ = m.Update(tea.MouseMotionMsg{X: x, Y: y})
			m = next.(Model)
			if m.monitorCommands.footerHover != "confirm" {
				t.Fatal("confirm hover missed its rendered surface")
			}
			next, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if cmd == nil || !next.(Model).monitorCommands.busy {
				t.Fatal("confirm click missed its rendered surface")
			}
			cmd()
			if f.calls != 1 {
				t.Fatal("confirmation did not apply exactly once")
			}
		}
	}
}

func TestMonitorCommandPopupComposerClickKeepsExpansion(t *testing.T) {
	m, f := suggestionTestModel(t)
	m.monitorPrompt.input.SetValue("/model")
	m, cmd, _ := m.openMonitorCommands("model")
	next, _ := m.Update(cmd())
	m = next.(Model)
	area := m.monitorComposerPopupArea()
	popup := m.monitorCommandPopup()
	x, y := area.x+1, area.y+area.height
	if popup.contains(x, y) {
		t.Fatal("fixture click must be in the composer, not the popup")
	}
	next, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = next.(Model)
	if !m.monitorCommands.open || !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "/model " || f.calls != 0 {
		t.Fatal("composer click did not keep the expansion and typing focus")
	}
	next, _ = m.Update(key('c'))
	if next.(Model).monitorPrompt.input.Value() != "/model c" || f.calls != 0 {
		t.Fatal("typing into a submenu acted as a hidden command confirmation")
	}
}

func TestMonitorCommandPopupPreventsHiddenConfirmationAndSupportsLocalAlias(t *testing.T) {
	m, f := commandTestModel(t)
	m, _, _ = m.chooseMonitorCommand()
	m.height = 8
	_, cmd, _ := m.confirmMonitorCommand()
	if cmd != nil || f.calls != 0 {
		t.Fatal("shrinking the terminal allowed an invisible confirmation")
	}
	m = localCommandModel()
	m, cmd, _ = m.openMonitorCommands("/commands")
	if cmd != nil || !m.monitorCommands.open || len(m.monitorCommands.menu.Choices) == 0 || m.monitorCommands.menu.Path != "" {
		t.Fatal("/commands did not open the local root menu")
	}
	m.monitorCommands.menu.Choices = []codex.SessionCommandChoice{{ID: "long", Label: "Only command", Help: strings.Repeat("single-line-help ", 40)}}
	rows := m.monitorCommandRows(60, 8)
	if len(rows) != 1 || rows[0].help == "" {
		t.Fatal("list help consumed extra rows")
	}
}

func TestMonitorCommandPopupWidthsFollowDetailMode(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		for _, long := range []bool{false, true} {
			m, _ := suggestionTestModel(t)
			m.height = 65
			m.monitorSessionData = m.monitorSessionData[:1]
			m.setRowContext("root-one", mode)
			m.monitorPrompt.input.Focus()
			help := "Choose model"
			if long {
				help = strings.Repeat("Long help ", 100)
			}
			m.monitorSuggestions.menu.Choices = []codex.SessionCommandChoice{{ID: "model", Label: "/model", Help: help, Next: "model"}}
			// Recalculate width on resize, without changing the selected command.
			for _, width := range []int{240, 120, 80} {
				m.width = width
				area := m.monitorComposerPopupArea()
				popup, _ := m.monitorSuggestionPopup()
				want := min(ansi.StringWidth("/model // "+strings.Join(strings.Fields(help), " "))+4, area.width)
				if popup.width != want || popup.y+popup.height != area.y+area.height {
					t.Fatalf("mode %d width %d: popup %+v, want width %d", mode, width, popup, want)
				}
				if long && !strings.Contains(ansi.Strip(m.render()), "…") {
					t.Fatal("long root help did not ellipsize")
				}
			}
			m, cmd, _ := m.openMonitorCommands("model")
			next, _ := m.Update(cmd())
			m = next.(Model)
			m.monitorCommands.menu.Choices[0].Help = help
			for _, width := range []int{240, 120, 80} {
				m.width = width
				area := m.monitorComposerPopupArea()
				popup := m.monitorCommandPopup()
				if popup.width > area.width || long && popup.width != area.width {
					t.Fatalf("nested mode %d width %d: popup width %d, pane width %d", mode, width, popup.width, area.width)
				}
				box := m.renderMonitorCommands(popup.width, popup.height, paletteFor(m.theme))
				if lipgloss.Width(box) != popup.width || long && !strings.Contains(ansi.Strip(box), "…") {
					t.Fatal("nested menu overflowed or failed to ellipsize its help")
				}
			}
		}
	}
}

func TestMonitorCommandPopupFitsStatuslinePreview(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		for _, inline := range []bool{false, true} {
			m := localCommandModel()
			m.height = 65
			m.monitorSessionData = m.monitorSessionData[:1]
			m.monitorSessionData[0].name = strings.Repeat("session", 24)
			m.setRowContext("root-one", mode)
			m, _, _ = m.openMonitorCommands("statusline")
			m.monitorCommands.inline = inline
			m.monitorCommands.menu.Choices = []codex.SessionCommandChoice{{ID: "thread-name", Label: "Session name", Help: "Name", Selected: true}}
			preview := "Preview // " + m.monitorSessionData[0].name
			for _, width := range []int{360, 240, 120, 80} {
				m.width = width
				area := m.monitorComposerPopupArea()
				popup := m.monitorCommandPopup()
				want := min(ansi.StringWidth(preview)+4, area.width)
				if popup.width < want || popup.width > area.width || inline && popup.width != want {
					t.Fatalf("mode %d inline %t width %d: popup width %d, preview needs %d", mode, inline, width, popup.width, want)
				}
				box := m.renderMonitorCommands(popup.width, popup.height, paletteFor(m.theme))
				if lipgloss.Width(box) != popup.width || lipgloss.Height(box) != popup.height {
					t.Fatal("preview overflowed the popup bounds")
				}
				if ansi.StringWidth(preview)+4 <= area.width && !strings.Contains(ansi.Strip(box), preview) {
					t.Fatal("preview shortened despite sufficient room in the parent pane")
				}
				x, y := renderedTextStart(t, m, i18n.Text(map[bool]string{true: "[ ENTER APPLY ]", false: "[ C APPLY ]"}[inline]))
				next, _ := m.Update(tea.MouseMotionMsg{X: x, Y: y})
				m = next.(Model)
				if m.monitorCommands.footerHover != "confirm" {
					t.Fatal("resized popup moved Apply away from its hover target")
				}
			}
		}
	}
}

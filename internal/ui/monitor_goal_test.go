package ui

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type goalUIClient struct{ *commandTestClient }

func (f *goalUIClient) SessionCommands(_ context.Context, _ string, path string) (codex.SessionCommandMenu, error) {
	if strings.HasPrefix(path, "goal ") {
		path = "goal/set/" + url.PathEscape(strings.TrimPrefix(path, "goal "))
	}
	menu := codex.SessionCommandMenu{Title: "/goal", Path: path, Revision: "goal-r"}
	if path == "goal" {
		menu.Help = "PURSUING GOAL // 200 / 10000 TOKENS\nOBJECTIVE // Fix tests"
		menu.Choices = []codex.SessionCommandChoice{
			{ID: "edit", Label: "Edit objective", Next: "goal/edit"},
			{ID: "budget", Label: "Token budget", Next: "goal/budget"},
			{ID: "replace", Label: "Replace goal", Next: "goal/set"},
		}
	} else if codex.SessionCommandInputPath(path) {
		menu.Input, menu.InputLimit, menu.Value = true, 4096, "Current objective"
	} else if commandInputReview(path) {
		menu.Choices = []codex.SessionCommandChoice{{ID: "goal-change", Label: "Apply goal", Action: true}}
	}
	return menu, nil
}

func TestMonitorGoalOverviewAndCompletedChoiceNavigation(t *testing.T) {
	for _, mode := range []int{contextFull, contextWide} {
		m, base := suggestionTestModel(t)
		m.height = 65
		m.monitorSessionData = m.monitorSessionData[:1]
		m.setRowContext("root-one", mode)
		m.fetcher = &goalUIClient{base.commandTestClient}
		m.focusMonitorPrompt()
		m.monitorPrompt.input.SetValue("/goal")
		m, cmd, _ := m.openMonitorCommands("goal")
		next, _ := m.Update(cmd())
		m = next.(Model)
		rows := m.monitorCommandRows(100, 12)
		if len(rows) < 3 || rows[0].choice != -1 || rows[0].action != "" || rows[1].choice != -1 || rows[2].action != "choose" {
			t.Fatal("overview became an option or displaced the choices", rows)
		}
		for _, height := range []int{4, 5, 6} {
			rows := m.monitorCommandRows(40, height)
			selectable := false
			for _, row := range rows {
				selectable = selectable || row.action == "choose"
			}
			if !selectable || len(rows) > m.commandContentRows(height) {
				t.Fatal("compact summary hid every option or overflowed")
			}
		}
		x, y := renderedTextStart(t, m, "PURSUING GOAL")
		next, cmd = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = next.(Model)
		if cmd != nil || m.monitorCommands.menu.Path != "goal" {
			t.Fatal("clicking the overview selected a command")
		}
		m.monitorCommands.selected = 2
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = next.(Model)
		if m.monitorPrompt.input.Value() != "/goal Replace goal" {
			t.Fatal("Tab failed to complete goal choice", m.monitorPrompt.input.Value(), m.monitorCommands.selected, m.monitorCommands.prefix)
		}
		next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)
		if cmd == nil || m.monitorCommands.menu.Path != "goal/set" {
			t.Fatal("completed menu label was treated as an objective", m.monitorCommands.menu.Path)
		}
		m, cmd, _ = m.openMonitorCommands("goal")
		next, _ = m.Update(cmd())
		m = next.(Model)
		x, y = renderedTextStart(t, m, "Edit objective")
		next, cmd = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if cmd == nil || next.(Model).monitorCommands.menu.Path != "goal/edit" {
			t.Fatal("overview rows displaced the Edit click target")
		}
	}
}

func TestMonitorGoalInputReviewRetainsNestedParent(t *testing.T) {
	for _, path := range []string{"goal/set", "goal/edit", "goal/budget", "goal A new objective"} {
		m, base := suggestionTestModel(t)
		f := &goalUIClient{base.commandTestClient}
		m.fetcher = f
		m.monitorPrompt.input.SetValue("/goal")
		m, cmd, _ := m.openMonitorCommands(path)
		next, _ := m.Update(cmd())
		m = next.(Model)
		parent := path
		if !commandInputReview(m.monitorCommands.menu.Path) {
			if !m.monitorCommands.menu.Input || !m.monitorPrompt.input.Focused() {
				t.Fatal("goal editor left the composer")
			}
			next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(Model)
			next, _ = m.Update(cmd())
			m = next.(Model)
		} else {
			parent = "goal/set"
		}
		if !m.monitorCommands.detail || f.calls != 0 || !strings.HasPrefix(m.monitorPrompt.input.Value(), m.inlineCommandPrefix(parent)) {
			t.Fatal("goal mutation bypassed review or leaked an encoded path")
		}
		next, cmd = m.Update(key('x'))
		m = next.(Model)
		if cmd == nil || m.monitorCommands.detail || m.monitorCommands.menu.Path != parent {
			t.Fatal("editing a goal review did not return to its exact editor parent", path, m.monitorCommands.menu.Path)
		}
	}
}

func TestMonitorGoalEscapeBurstStaysInSessionDetail(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, path := range []string{"goal", "goal/edit", "goal/budget", "goal/edit/New%20objective"} {
			for _, state := range []string{"ready", "loading", "error"} {
				for _, repeat := range []bool{false, true} {
					m, base := suggestionTestModel(t)
					m.fetcher = &goalUIClient{base.commandTestClient}
					m.monitorPrompt.input.SetValue("/goal")
					if !inline {
						m.monitorPrompt.input.Blur()
					}
					m, cmd, _ := m.openMonitorCommands(path)
					if state != "loading" {
						next, _ := m.Update(cmd())
						m = next.(Model)
					}
					if state == "error" {
						m.monitorCommands.notice = "Command unavailable."
					}
					draft := m.monitorPrompt.input.Value()
					for i := range 4 {
						next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, IsRepeat: repeat && i > 0})
						m = next.(Model)
						if m.monitorContextDetail != "root-one" {
							t.Fatalf("Escape burst left detail: path=%s inline=%t state=%s repeat=%t press=%d", path, inline, state, repeat, i+1)
						}
					}
					if state == "loading" {
						next, _ := m.Update(cmd())
						m = next.(Model)
					}
					if base.calls != 0 || m.monitorCommands.open || m.monitorPrompt.input.Value() != draft {
						t.Fatal("Escape submitted/reopened a goal action or lost the draft")
					}
				}
			}
		}
	}
}

func TestMonitorCommandEscapeRestoresOrdinaryNavigation(t *testing.T) {
	for _, resume := range []string{"release", "timeout", "typing", "paste", "click"} {
		m, base := suggestionTestModel(t)
		m.fetcher = &goalUIClient{base.commandTestClient}
		m.monitorPrompt.input.SetValue("/goal")
		m, cmd, _ := m.openMonitorCommands("goal/edit")
		next, _ := m.Update(cmd())
		m = next.(Model)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(Model)
		switch resume {
		case "release":
			next, _ = m.Update(tea.KeyReleaseMsg{Code: tea.KeyEscape})
			m = next.(Model)
		case "timeout":
			m.monitorCommands.escapeUntil = time.Now().Add(-time.Second)
		default:
			var message tea.Msg = key('x')
			if resume == "paste" {
				message = tea.PasteMsg{Content: "new text"}
			} else if resume == "click" {
				message = tea.MouseClickMsg{X: -1, Y: -1, Button: tea.MouseLeft}
			}
			m.consumeMonitorCommandEscape(message)
		}
		m.monitorPrompt.input.Blur()
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(Model)
		if m.monitorContextDetail != "" {
			t.Fatal("ordinary Escape navigation remained blocked after", resume)
		}
	}
}

func TestMonitorGoalEscapeWithoutComposer(t *testing.T) {
	m, base := suggestionTestModel(t)
	m.fetcher = &goalUIClient{base.commandTestClient}
	m.monitorPrompt.input.SetValue("/goal")
	m.monitorPrompt.input.Blur()
	m, cmd, _ := m.openMonitorCommands("goal/edit")
	next, _ := m.Update(cmd())
	m = next.(Model)
	m.monitorPrompt = monitorPromptState{}
	for range 3 {
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(Model)
		if m.monitorContextDetail != "root-one" {
			t.Fatal("Escape dismissed detail when no composer could take focus")
		}
	}
}

func TestMonitorGoalEscapeDoesNotInterruptWorkingTurn(t *testing.T) {
	m, c := turnTestModel()
	m.fetcher = &goalUIClient{&commandTestClient{promptTestClient: c.promptTestClient}}
	m.monitorPrompt.input.SetValue("/goal")
	m, cmd, _ := m.openMonitorCommands("goal/edit")
	next, _ := m.Update(cmd())
	m = next.(Model)
	m.fetcher = c
	for i := range 3 {
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, IsRepeat: i > 0})
		m = next.(Model)
		if m.monitorPrompt.busy || m.monitorContextDetail != "root-one" {
			t.Fatal("dismissing a goal interrupted work or navigated away")
		}
	}
	next, _ = m.Update(tea.KeyReleaseMsg{Code: tea.KeyEscape})
	m = next.(Model)
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("deliberate working-composer Escape lost its interrupt action")
	}
	cmd()
	if c.action != "interrupt" {
		t.Fatal("fresh Escape did not restore the ordinary working-composer action", c.action)
	}
}

func TestMonitorGoalFollowsConfiguredFields(t *testing.T) {
	m := completedCopyModel()
	m.monitorStatusLine = []string{"model-with-reasoning", "used-tokens"}
	s := m.monitorSessionData[0]
	s.modelSettings = codex.SessionModelSettings{Model: "test-model", ReasoningEffort: "high"}
	s.latest = 1200
	s.goal = &codex.SessionGoal{Status: "active", Objective: "A separate objective"}
	out := ansi.Strip(m.renderMonitorSessionMetrics(48, 12, s, "", paletteFor(m.theme)))
	before := -1
	for _, text := range []string{"TURN COMPLETE", "test-model high", "1.2K tokens", "PURSUING GOAL"} {
		at := strings.Index(out, text)
		if at <= before || strings.Count(out, text) != 1 {
			t.Fatalf("goal/state order or duplication for %q:\n%s", text, out)
		}
		before = at
	}
	if strings.Contains(out, s.goal.Objective) {
		t.Fatal("objective cluttered row telemetry")
	}
	for _, height := range []int{3, 4, 5, 8} {
		out := m.renderMonitorSessionMetrics(30, height, s, "", paletteFor(m.theme))
		if lipgloss.Height(out) != height || lipgloss.Width(out) > 30 {
			t.Fatal("goal caused compact telemetry overflow")
		}
	}
}

func TestMonitorGoalFooterKeepsCopyTarget(t *testing.T) {
	for _, width := range []int{24, 60, 120, 180} {
		m := completedCopyModel()
		m.width = width
		m.setRowContext("root-one", contextFull)
		m.monitorSessionData[0].modelSettings = codex.SessionModelSettings{Model: "test-model", ReasoningEffort: "high"}
		before := m.render()
		m.monitorSessionData[0].goal = &codex.SessionGoal{Status: "blocked", Objective: "Find a fix"}
		out := ansi.Strip(m.render())
		if width >= 120 {
			goalAt, copyAt := strings.Index(out, "GOAL STALLED"), strings.Index(out, benchmarkDetailCopyLabel)
			if goalAt < 0 || copyAt < goalAt || strings.Contains(out[goalAt:copyAt], "\n") {
				t.Fatal("goal not on bottom border immediately before Copy", out)
			}
		}
		if strings.Contains(ansi.Strip(before), benchmarkDetailCopyLabel) {
			x, y := renderedTextStart(t, m, benchmarkDetailCopyLabel)
			if m.monitorContextAt(x, y) != "copy:root-one" {
				t.Fatal("goal displaced Copy hit target")
			}
		}
		if lipgloss.Width(out) > width || lipgloss.Height(out) > m.height {
			t.Fatal("goal footer overflowed terminal")
		}
	}
}

func TestMonitorGoalStatusStaleAndSync(t *testing.T) {
	m := completedCopyModel()
	usage := codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "root-one", Active: true, Goal: &codex.SessionGoal{Status: "active", Objective: "Goal"}}}}
	now := time.Now()
	m.startMonitorSessions(usage, now)
	if m.monitorSessionData[0].goal == nil {
		t.Fatal("startup dropped goal")
	}
	usage.Sessions[0].Goal.Status = "paused"
	if m.monitorSessionData[0].goal.Status != "active" {
		t.Fatal("goal aliased source snapshot")
	}
	usage.Sessions = append(usage.Sessions, codex.LiveUsageSession{ID: "new", Active: true, Goal: &codex.SessionGoal{Status: "blocked", Objective: "Other"}})
	m.syncMonitorSessions(usage, now)
	if m.monitorSessionData[0].goal.Status != "paused" || m.monitorSessionData[1].goal.Status != "blocked" {
		t.Fatal("refresh/new session dropped goal")
	}
	m.syncMonitorSessions(codex.LiveUsageSnapshot{}, now)
	if !m.monitorSessionData[0].goal.Stale {
		t.Fatal("missing observation claimed current goal")
	}
	m.monitorError = "disconnected"
	if got := ansi.Strip(m.monitorGoalStatus(m.monitorSessionData[1], paletteFor(m.theme))); got != "GOAL STALE" {
		t.Fatal("disconnected goal not stale:", got)
	}
	m.monitorError = ""
	usage.Sessions[0].Goal = nil
	m.syncMonitorSessions(usage, now)
	if m.monitorSessionData[0].goal != nil {
		t.Fatal("clear retained old goal")
	}
}

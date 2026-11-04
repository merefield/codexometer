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
	if codex.SessionCommandInputPath(path) {
		menu.Input, menu.InputLimit, menu.Value = true, 4096, "Current objective"
	} else if commandInputReview(path) {
		menu.Choices = []codex.SessionCommandChoice{{ID: "goal-change", Label: "Apply goal", Action: true}}
	}
	return menu, nil
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

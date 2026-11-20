package ui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

type agentInspectionProbe struct {
	Fetcher
}

func (agentInspectionProbe) InspectSessionAgents(_ context.Context, root, child string) (codex.AgentInspection, error) {
	if child != "" {
		return codex.AgentInspection{Turns: []codex.SavedTurn{{Reply: root + ":" + child}}}, nil
	}
	return codex.AgentInspection{Agents: []codex.SessionAgent{{ID: "child", Name: "Worker", Status: "WORKING"}}}, nil
}

func TestSessionAgentButtonAndKeyboardDrillDown(t *testing.T) {
	m := contextTestModel()
	m.fetcher = agentInspectionProbe{}
	m.height = 66
	m.monitorSessionData[0].agentCount = 2
	x, y := renderedTextStart(t, m, "[2 AGENTS]")
	var cmd tea.Cmd
	apply := func(msg tea.Msg) {
		next, c := m.Update(msg)
		m, cmd = next.(Model), c
	}
	apply(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd == nil || m.inspector.root != m.monitorSessionData[0].id {
		t.Fatalf("rendered link was not clickable at %d,%d", x, y)
	}
	apply(cmd())
	apply(specialKey(tea.KeyEnter))
	if cmd == nil || m.inspector.child != "child" {
		t.Fatal("keyboard did not open the child transcript")
	}
	apply(cmd())
	if got := m.inspectorLines(100); got[0] != "root-one:child" || m.monitorContextTarget() == "child" {
		t.Fatal("drill-down retargeted the parent composer", got)
	}
}

package ui

import (
	"context"
	"strings"
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

type terminalInspectionProbe struct {
	Fetcher
	reads int
}

func TestAgentTranscriptEmptyStateExplainsUnverifiedOwnership(t *testing.T) {
	m := contextTestModel()
	m.inspector = sessionInspector{
		kind: "subagents", child: "child", page: codex.AgentInspection{UnverifiedTurns: true},
	}
	if got := strings.Join(m.inspectorLines(100), " "); !strings.Contains(got, "Could not verify which completed turns") {
		t.Fatal("unverified transcript presented as empty", got)
	}
	m.inspector.page.UnverifiedTurns = false
	if got := m.inspectorLines(100); got[0] != "NO COMPLETED TURNS" {
		t.Fatal("genuinely empty transcript did not keep its original notice", got)
	}
}

func (p *terminalInspectionProbe) InspectSessionTerminals(_ context.Context, id string) (codex.TerminalInspection, error) {
	p.reads++
	return codex.TerminalInspection{Terminals: []codex.SessionTerminal{{ID: id, Command: "go test", Directory: "/work"}}}, nil
}

func TestSessionTerminalSlashAndRefreshLeaveDetailInPlace(t *testing.T) {
	m := contextTestModel()
	probe := &terminalInspectionProbe{}
	m.fetcher = probe
	m.setRowContext("root-one", contextFull)
	m, cmd, ok := m.runLocalMonitorCommand("ps")
	if !ok || cmd == nil || m.inspector.root != "root-one" {
		t.Fatal("/ps did not open the selected session")
	}
	m, _, _ = m.updateSessionInspector(cmd())
	if lines := m.inspectorLines(80); lines[0] != "root-one // /work" || lines[1] != "go test" {
		t.Fatal(lines)
	}
	x, y := renderedTextStart(t, m, "[R]")
	m, cmd, ok = m.updateSessionInspector(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !ok || cmd == nil || m.inspector.root != "root-one" {
		t.Fatalf("rendered Refresh control did not refresh at %d,%d in %+v (root %q)", x, y, m.monitorDashboardLayout(), m.inspector.root)
	}
	m, _, _ = m.updateSessionInspector(cmd())
	m, _, _ = m.updateSessionInspector(specialKey(tea.KeyEscape))
	if probe.reads != 2 || m.inspector.root != "" || m.monitorContextDetail != "root-one" {
		t.Fatal("process inspection changed the selected detail")
	}
}

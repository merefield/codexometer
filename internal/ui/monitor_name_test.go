package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorSessionNameDisplayAndRename(t *testing.T) {
	m := Model{}
	u := codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "root", Name: "Fix dashboard layout", WorkingDirectory: "/work/dashboard", Active: true}}}
	m.startMonitorSessions(u, time.Now())
	for _, height := range []int{4, 8, 12} {
		out := m.renderMonitorSessionMetrics(64, height, m.monitorSessionData[0], "", paletteFor(themeHacker))
		if !strings.Contains(ansi.Strip(out), "ROOT // Fix dashboard layout") || strings.Count(ansi.Strip(out), "Fix dashboard layout") != 1 || lipgloss.Height(out) > height || lipgloss.Width(out) > 64 {
			t.Fatalf("name missing or oversized: %s", out)
		}
		if height >= 8 && !strings.Contains(ansi.Strip(out), "/work/dashboard") {
			t.Fatal("directory missing from session body")
		}
	}
	u.Sessions[0].Name = "Renamed session"
	m.syncMonitorSessions(u, time.Now())
	if m.monitorSessionData[0].name != "Renamed session" {
		t.Fatal("rename not propagated")
	}
}

func TestNamedSessionPillAndBadgeNavigation(t *testing.T) {
	for _, attention := range []codex.SessionAttention{codex.SessionAttentionComplete, codex.SessionAttentionApproval, codex.SessionAttentionInput} {
		m := Model{meterView: viewMonitor, width: 140, height: 40, snapshot: codex.DemoSnapshot(), monitorState: monitorRunning}
		s := monitorSession{id: "session-ABCDE", name: "Fix dashboard", workingDirectory: "/work/project", displayed: true, active: true, attention: attention}
		m.monitorSessionData = []monitorSession{s}
		buttons, _ := m.monitorAttentionButtons(136, 1)
		if len(buttons) == 0 || !strings.Contains(buttons[0].label, "ABCDE // Fix dashboard") {
			t.Fatalf("pill identity differs: %+v", buttons)
		}
		found := false
		for y := 0; y < m.height && !found; y++ {
			for x := 0; x < m.width; x++ {
				if m.monitorContextAt(x, y) != "badge:"+s.id {
					continue
				}
				next, _, handled := m.updateMonitorContextMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				if !handled || next.monitorContextDetail != s.id || next.monitorContextRows[s.id].review != "context" {
					t.Fatal("badge did not open native session detail")
				}
				found = true
				break
			}
		}
		if !found {
			t.Fatal("badge has no click surface")
		}
	}
}

func TestNamedSessionDirectoryDoesNotDisplaceStatus(t *testing.T) {
	for _, active := range []bool{false, true} {
		s := monitorSession{id: "root", name: "Named session", workingDirectory: "/work/dashboard", active: active}
		want := "IDLE"
		if active {
			want = "ACTIVE"
		}
		m := Model{}
		short := ansi.Strip(m.renderMonitorSessionMetrics(64, 4, s, "", paletteFor(themeHacker)))
		if !strings.Contains(short, want) || !strings.Contains(short, "TOKENS") || strings.Contains(short, s.workingDirectory) {
			t.Fatalf("short row must retain tokens and %s before directory:\n%s", want, short)
		}
		tall := ansi.Strip(m.renderMonitorSessionMetrics(64, 8, s, "", paletteFor(themeHacker)))
		if !strings.Contains(tall, want) || !strings.Contains(tall, s.workingDirectory) {
			t.Fatalf("tall row should show status and directory:\n%s", tall)
		}
	}
}

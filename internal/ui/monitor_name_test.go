package ui

import (
	"strings"
	"testing"
	"time"

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
		if !strings.Contains(ansi.Strip(out), "SESSION // Fix dashboard layout") || strings.Count(ansi.Strip(out), "Fix dashboard layout") != 1 || lipgloss.Height(out) > height || lipgloss.Width(out) > 64 {
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

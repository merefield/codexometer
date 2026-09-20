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

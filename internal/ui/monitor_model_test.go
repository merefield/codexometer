package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorSessionModelMetadataPriority(t *testing.T) {
	m := contextTestModel()
	s := m.monitorSessionData[0]
	s.modelSettings = codex.SessionModelSettings{Model: "gpt-6-astra", ReasoningEffort: "medium", ServiceTier: "priority"}
	s.workingDirectory = "/work/projects"
	out := ansi.Strip(m.renderMonitorSessionMetrics(40, 7, s, "", paletteFor(m.theme)))
	lines := strings.Split(out, "\n")
	if !strings.Contains(out, "gpt-6-astra medium fast") {
		t.Fatal("missing model readout", out)
	}
	for i, line := range lines {
		if strings.Contains(line, "TOKENS") && (i+1 >= len(lines) || !strings.Contains(lines[i+1], "gpt-6-astra medium fast")) {
			t.Fatal("model settings not immediately after tokens", out)
		}
	}
	for _, unwanted := range []string{"DIR //", " // ROOT", "TTFT N/A // PEAK N/A", "LAST OUT N/A // PEAK N/A"} {
		if strings.Contains(out, unwanted) {
			t.Fatal("redundant metadata", out)
		}
	}
	compact := ansi.Strip(m.renderMonitorSessionMetrics(40, 5, s, "ROWS 1-2/3", paletteFor(m.theme)))
	if !strings.Contains(compact, "gpt-6-astra medium fast") {
		t.Fatal("paging replaced high-priority model line", compact)
	}
	s.modelSettings.ServiceTier = ""
	if strings.Contains(formatMonitorSessionModel(s), "fast") {
		t.Fatal("unknown tier claimed fast")
	}
	s.modelSettings = codex.SessionModelSettings{Model: "gpt-6-astra\x1b[31m\n", ReasoningEffort: "medium"}
	if strings.ContainsAny(formatMonitorSessionModel(s), "\x1b\n") {
		t.Fatal("unsafe model text")
	}
}

func TestMonitorSessionSettingsFollowUpdates(t *testing.T) {
	m := contextTestModel()
	now := time.Now()
	settings := codex.SessionModelSettings{Model: "gpt-6-astra", ReasoningEffort: "medium", ServiceTier: "fast"}
	u := codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "root", Active: true, ModelSettings: settings}}}
	m.startMonitorSessions(u, now)
	if m.monitorSessionData[0].modelSettings != settings {
		t.Fatal("initial settings lost")
	}
	u.Sessions[0].ModelSettings = codex.SessionModelSettings{Model: "gpt-5.6-sol"}
	m.syncMonitorSessions(u, now)
	if m.monitorSessionData[0].modelSettings != u.Sessions[0].ModelSettings {
		t.Fatal("stale settings retained")
	}
	u.Sessions[0].ModelSettings = settings
	m.resumeMonitorSessions(u, now, time.Minute)
	if m.monitorSessionData[0].modelSettings != settings {
		t.Fatal("resume lost settings")
	}
}

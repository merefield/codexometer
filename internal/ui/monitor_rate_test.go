package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorAverageRatesUseSharedCadence(t *testing.T) {
	now := time.Now()
	m := contextTestModel()
	m.monitorStartedAt = now.Add(-time.Minute)
	m.monitorBaseline, m.monitorLatest = 0, 600
	for i := range m.monitorSessionData {
		m.monitorSessionData[i].startedAt = m.monitorStartedAt
		m.monitorSessionData[i].baseline = 0
		m.monitorSessionData[i].latest = 200
	}
	m.refreshMonitorRates(now, true)
	m.monitorLatest = 900 // Intermediate telemetry must not refresh displayed rates.
	for i := range m.monitorSessionData {
		m.monitorSessionData[i].latest = 300
	}
	for _, msg := range []tea.Msg{tea.MouseMotionMsg{X: 5, Y: 5}, tea.KeyPressMsg{Code: '?', Text: "?"}, secondMsg(now.Add(4 * time.Second))} {
		n, _ := m.Update(msg)
		m = n.(Model)
		_ = m.render()
		if m.monitorAverageRate != 600 {
			t.Fatal("redraw/intermediate tick changed total average")
		}
		for _, s := range m.monitorSessionData {
			if s.averageRate != 200 {
				t.Fatal("session average changed independently")
			}
		}
	}
	n, _ := m.Update(secondMsg(now.Add(5 * time.Second)))
	m = n.(Model)
	if m.monitorAverageRate != averageTokenRate(900, 65*time.Second) {
		t.Fatal("total did not refresh at five seconds")
	}
	for _, s := range m.monitorSessionData {
		if s.averageRate != averageTokenRate(300, 65*time.Second) {
			t.Fatal("session missed shared refresh")
		}
	}
	m.monitorState, m.monitorStoppedAt = monitorPaused, now.Add(5*time.Second)
	m.refreshMonitorRates(now.Add(time.Minute), true)
	if m.monitorAverageRate != averageTokenRate(900, 65*time.Second) {
		t.Fatal("paused average kept decaying")
	}
}

func TestMonitorAverageRatesRefreshOnControls(t *testing.T) {
	now := time.Now()
	m := contextTestModel()
	m.monitorStartedAt = now.Add(-time.Minute)
	m.monitorBaseline, m.monitorLatest = 0, 600
	m.monitorRateAt = now // Force must bypass the usual five-second gate.
	u := codex.LiveUsageSnapshot{TotalTokens: 600, Sessions: []codex.LiveUsageSession{{ID: "root-one", TotalTokens: 600, Active: true}}}
	n, _, ok := m.applyMonitorFetch(monitorFetchedMsg{kind: monitorFetchPause, usage: u, at: now})
	m = n.(Model)
	if !ok || m.monitorAverageRate != 600 {
		t.Fatal("pause did not immediately refresh average")
	}
	n, _, ok = m.applyMonitorFetch(monitorFetchedMsg{kind: monitorFetchResume, usage: u, at: now.Add(time.Minute)})
	m = n.(Model)
	if !ok || m.monitorRateAt != now.Add(time.Minute) || m.monitorAverageRate != 600 {
		t.Fatal("resume did not immediately rebase average")
	}
	n, _, ok = m.applyMonitorFetch(monitorFetchedMsg{kind: monitorFetchReset, usage: u, at: now.Add(time.Minute + time.Second)})
	m = n.(Model)
	if !ok || m.monitorAverageRate != 0 {
		t.Fatal("reset retained total average")
	}
	for _, s := range m.monitorSessionData {
		if s.averageRate != 0 {
			t.Fatal("reset retained session average")
		}
	}
}

package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func contextTestModel() Model {
	m := Model{width: 120, height: 40, meterView: viewMonitor, snapshot: codex.DemoSnapshot(), monitorState: monitorRunning}
	for _, id := range []string{"root-one", "root-two", "root-three"} {
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{id: id, active: true, displayed: true, preview: codex.SessionContext{Kind: codex.SessionContextReply, Text: "Finished updating the documentation.\n次の手順を選んでください。", ThreadID: id, Source: "LOCAL", At: time.Now()}, samples: []monitorSample{{intervalTokens: 100}}})
	}
	return m
}

func TestMonitorContextResponsiveHitTargets(t *testing.T) {
	for _, size := range []struct{ w, h int }{{40, 16}, {60, 24}, {80, 24}, {100, 30}, {120, 40}, {180, 50}} {
		m := contextTestModel()
		m.width, m.height = size.w, size.h
		for _, kind := range []codex.SessionContextKind{codex.SessionContextReply, codex.SessionContextActivity, codex.SessionContextQuestion} {
			for i := range m.monitorSessionData {
				m.monitorSessionData[i].preview.Kind = kind
			}
			out := m.render()
			if lipgloss.Width(out) > size.w || lipgloss.Height(out) > size.h {
				t.Fatalf("overflow %dx%d: %dx%d", size.w, size.h, lipgloss.Width(out), lipgloss.Height(out))
			}
			lines := strings.Split(ansi.Strip(out), "\n")
			found := 0
			for y, line := range lines {
				for from := 0; from < len(line); {
					n := strings.Index(line[from:], monitorContextInfo)
					if n < 0 {
						break
					}
					n += from
					x := lipgloss.Width(line[:n])
					found++
					for offset := 0; offset < 3; offset++ {
						if id := m.monitorContextAt(x+offset, y); id == "" {
							t.Fatalf("info miss %dx%d at %d,%d layout=%+v\n%s", size.w, size.h, x+offset, y, m.dashboardLayout(), ansi.Strip(out))
						}
					}
					updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
					detail := updated.(Model)
					if detail.monitorContextDetail == "" {
						t.Fatal("click failed")
					}
					modal := detail.render()
					if lipgloss.Width(modal) > size.w || lipgloss.Height(modal) > size.h {
						t.Fatal("detail overflow")
					}
					from = n + 3
				}
			}
			g := m.dashboardLayout()
			a := layoutMonitorArea(g.contentWidth, g.meterHeight)
			visible, _, _ := m.monitorSessionPage(a.graphHeight)
			if found != len(visible) {
				t.Fatalf("expected %d info buttons, got %d at %dx%d", len(visible), found, size.w, size.h)
			}
		}
	}
}

func TestMonitorContextPrivacyAndModalIsolation(t *testing.T) {
	m := contextTestModel()
	m.openMonitorContext("root-one")
	m.monitorSessionData[0].preview.Text = strings.Repeat("private context\n", 100)
	updated, _ := m.Update(key('h'))
	m = updated.(Model)
	if !m.monitorContextHidden || m.monitorContextDetail != "" || strings.Contains(m.render(), "private context") {
		t.Fatal("privacy leak")
	}
	updated, _ = m.Update(key('i'))
	m = updated.(Model)
	if m.monitorContextDetail != "" {
		t.Fatal("opened hidden context")
	}
	updated, _ = m.Update(key('h'))
	m = updated.(Model)
	m.openMonitorContext("root-one")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = updated.(Model)
	if m.monitorContextScroll == 0 {
		t.Fatal("detail did not scroll")
	}
	updated, _ = m.Update(key('s'))
	m = updated.(Model)
	if m.monitorState != monitorRunning {
		t.Fatal("modal activated underlying reset")
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if m.monitorContextDetail != "" || cmd != nil {
		t.Fatal("escape quit instead of closing")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.monitorContextDetail != "root-one" {
		t.Fatal("keyboard open failed")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	if m.meterView == viewMonitor || m.monitorContextDetail != "" {
		t.Fatal("tab navigation left stale modal")
	}
}

func TestMonitorContextPrivacyPersistsWithoutText(t *testing.T) {
	m := contextTestModel()
	store := &memoryPreferenceStore{}
	m.preferenceStore = store
	m.toggleMonitorContext()
	if !store.preferences.HideSessionContext {
		t.Fatal("privacy not saved")
	}
	saved, err := json.Marshal(store.preferences)
	if err != nil || strings.Contains(string(saved), "Finished") || strings.Contains(string(saved), "root-one") {
		t.Fatalf("unexpected stored content: %s", saved)
	}
	next := NewWithPreferences(nil, time.Minute, store)
	if !next.monitorContextHidden {
		t.Fatal("privacy not restored")
	}
}

func TestMonitorContextPrivacyAndDismissRenderedTargets(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 180} {
		m := contextTestModel()
		m.width = width
		for _, hidden := range []bool{false, true} {
			m.monitorContextHidden = hidden
			for y, line := range strings.Split(ansi.Strip(m.render()), "\n") {
				if idx := strings.Index(line, m.monitorPrivacyLabel()); idx >= 0 {
					x := lipgloss.Width(line[:idx])
					for n := 0; n < lipgloss.Width(m.monitorPrivacyLabel()); n++ {
						if m.monitorContextAt(x+n, y) != "privacy" {
							t.Fatal("privacy button miss")
						}
					}
					updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
					if updated.(Model).monitorContextHidden == hidden {
						t.Fatal("privacy click failed")
					}
				}
				if idx := strings.Index(line, monitorDismissLabel); idx >= 0 {
					x := lipgloss.Width(line[:idx])
					if _, ok := m.monitorSessionDismissAt(x, y); !ok {
						t.Fatalf("dismiss miss at width %d", width)
					}
				}
			}
		}
	}
}

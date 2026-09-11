package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestMonitorNavigationRenderedButtons(t *testing.T) {
	for _, size := range [][2]int{{40, 16}, {60, 24}, {80, 30}, {120, 40}, {200, 50}} {
		for _, mode := range []int{contextGraph, contextSplit, contextWide, contextFull} {
			m := contextTestModel()
			m.width, m.height = size[0], size[1]
			m.setRowContext("root-one", mode)
			out := ansi.Strip(m.render())
			if lipgloss.Width(out) > m.width || lipgloss.Height(out) > m.height {
				t.Fatalf("overflow %v mode %d", size, mode)
			}
			found := 0
			for y, line := range strings.Split(out, "\n") {
				for _, label := range []string{"[←]", "[→]", "[↑]", "[↓]"} {
					for from := 0; from < len(line); {
						pos := strings.Index(line[from:], label)
						if pos < 0 {
							break
						}
						pos += from
						x := lipgloss.Width(line[:pos])
						from = pos + len(label)
						found++
						hit := m.monitorContextAt(x, y)
						if hit == "" {
							t.Fatalf("%v mode %d label %s misses at %d,%d", size, mode, label, x, y)
						}
						for dx := 0; dx < 3; dx++ {
							if got := m.monitorContextAt(x+dx, y); got != hit {
								t.Fatalf("partial button %s: %q vs %q", label, got, hit)
							}
						}
						n, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
						next := n.(Model)
						if hit == "navigation-disabled" {
							if next.monitorSelectedID != m.monitorSelectedID || next.rowContextMode("root-one") != mode {
								t.Fatal("disabled button fell through")
							}
						} else if id, ok := strings.CutPrefix(hit, "less:"); ok {
							if next.rowContextMode(id) != m.rowContextMode(id)-1 {
								t.Fatal("left did not decrease detail")
							}
						} else if id, ok := strings.CutPrefix(hit, "more:"); ok {
							if next.rowContextMode(id) != m.rowContextMode(id)+1 {
								t.Fatal("right did not increase detail")
							}
						}
					}
				}
			}
			if size[0] >= 120 && found == 0 {
				t.Fatal("missing navigation buttons")
			}
			if mode == contextFull && (strings.Contains(out, "[↑]") || strings.Contains(out, "[↓]")) {
				t.Fatal("session buttons in full detail")
			}
		}
	}
}

func TestMonitorSessionNavigationBoundaries(t *testing.T) {
	m := contextTestModel()
	m.width = 200
	m.monitorSelectedID = "root-one"
	for _, want := range []string{"root-two", "root-three", "root-three"} {
		g := m.dashboardLayout()
		a := layoutMonitorArea(g.contentWidth, g.meterHeight)
		for _, b := range m.monitorNavigationButtons(a.readoutWidth, "", false) {
			if b.action != "session-down" {
				continue
			}
			n, _ := m.Update(tea.MouseClickMsg{X: 2 + b.rect.x, Y: g.meterY, Button: tea.MouseLeft})
			m = n.(Model)
		}
		if m.monitorSelectedID != want {
			t.Fatalf("selected %q want %q", m.monitorSelectedID, want)
		}
	}
	m.monitorSelectedID = "root-one"
	buttons := m.monitorNavigationButtons(100, "", false)
	if buttons[0].enabled || !buttons[1].enabled {
		t.Fatal("first-session state")
	}
	m.dismissMonitorSession("root-three")
	m.monitorSelectedID = "root-two"
	buttons = m.monitorNavigationButtons(100, "", false)
	if !buttons[0].enabled || buttons[1].enabled {
		t.Fatal("dismissed session counted")
	}
}

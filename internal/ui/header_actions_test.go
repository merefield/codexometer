package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHeaderClickTargets(t *testing.T) {
	for _, width := range []int{20, 40, 60, 67, 68, 80, 120, 200} {
		m := contextTestModel()
		m.width, m.appVersion, m.monitorContextDetail = width, "0.14.0", "root-one"
		g := m.dashboardLayout()
		colors := paletteFor(m.theme)
		lines := strings.Split(ansi.Strip(renderHeader(g.contentWidth, m.phase, m.renderSignalStatus(g.contentWidth, colors), m.renderAccount(colors), m.appVersion, colors)), "\n")
		for y, line := range lines {
			versionIndex := strings.Index(line, "0.14.0")
			for x := -1; x <= g.contentWidth; x++ {
				action := m.headerActionAt(x+2, y+1)
				wantRepo := versionIndex >= 0 && y == len(lines)-1 && x >= lipgloss.Width(line[:versionIndex]) && x < lipgloss.Width(line[:versionIndex])+6
				if (action == "repository") != wantRepo {
					t.Fatalf("width %d at %d,%d: %q", width, x, y, action)
				}
				if action == "" {
					continue
				}
				next, cmd := m.Update(tea.MouseClickMsg{X: x + 2, Y: y + 1, Button: tea.MouseLeft})
				n := next.(Model)
				if action == "home" && (n.meterView != viewBars || n.quotaMeterView != viewBars || n.monitorContextDetail != "") {
					t.Fatal("title did not navigate home")
				}
				if action == "repository" && (cmd == nil || n.meterView != viewMonitor) {
					t.Fatal("version did not return opener command")
				}
			}
		}
		for _, p := range [][2]int{{0, 0}, {2, g.tabsY}, {2, g.meterY}, {width - 1, 1}} {
			if m.headerActionAt(p[0], p[1]) != "" {
				t.Fatalf("ghost header target at %v", p)
			}
		}
		if width >= 80 && m.headerActionAt(2, 1) != "home" {
			t.Fatal("logo not clickable")
		}
	}
}

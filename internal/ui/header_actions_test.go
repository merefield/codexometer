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
				if action == "repository" && (cmd != nil || n.meterView != viewMonitor || !n.versionHovered) {
					t.Fatal("version click should only highlight the terminal hyperlink")
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

func TestHeaderVersionHyperlink(t *testing.T) {
	colors := paletteFor(themeHacker)
	header := renderHeader(120, 0, "", "", "0.14.0", colors)
	plain := linkHeaderVersion(header, "0.14.0", false, colors)
	hover := linkHeaderVersion(header, "0.14.0", true, colors)
	if !strings.Contains(plain, ansi.SetHyperlink(repositoryURL+"/releases/tag/v0.14.0")) || !strings.Contains(plain, ansi.ResetHyperlink()) {
		t.Fatal("missing terminal hyperlink")
	}
	if ansi.Strip(plain) != ansi.Strip(hover) || lipgloss.Width(plain) != lipgloss.Width(hover) {
		t.Fatal("hover changed header geometry")
	}
	if plain == hover {
		t.Fatal("hover did not change styling")
	}
	if strings.Contains(renderHeader(12, 0, "", "", "0.14.0", colors), repositoryURL) {
		t.Fatal("invisible version is linked")
	}
}

func TestHeaderReleaseDestinations(t *testing.T) {
	for _, v := range []string{"0.14.0", "v0.14.0", "0.14.0-1-gec05240-dirty", "0.14.0-dev+abc"} {
		if got := versionHighlightsURL(v); got != repositoryURL+"/releases/tag/v0.14.0" {
			t.Fatal(got)
		}
	}
	if versionHighlightsURL("DEVELOPMENT") != repositoryURL+"/releases" {
		t.Fatal("unknown version invented a release")
	}
	m := contextTestModel()
	m.appVersion = "0.14.0"
	view := m.render()
	if strings.Contains(view, ansi.SetHyperlink(repositoryURL)) || !strings.Contains(view, ansi.SetHyperlink(versionHighlightsURL(m.appVersion))) {
		t.Fatal("only the version should have a header hyperlink")
	}
	next, _ := m.Update(tea.MouseClickMsg{X: 2, Y: 1, Button: tea.MouseLeft})
	if next.(Model).meterView != viewBars {
		t.Fatal("title must still return to Quota Bars")
	}
}

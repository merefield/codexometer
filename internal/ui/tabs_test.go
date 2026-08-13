package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func TestMainTabsChooseResponsiveLabels(t *testing.T) {
	for _, test := range []struct {
		width int
		want  string
	}{
		{width: 100, want: "BENCHMARK"},
		{width: 22, want: "QTA"},
		{width: 9, want: "[M]"},
		{width: 3, want: "M"},
	} {
		t.Run(test.want, func(t *testing.T) {
			tabs, _ := mainTabLayout(test.width, false)
			if len(tabs) != int(mainTabCount) {
				t.Fatalf("width %d displayed %d main tabs, want %d", test.width, len(tabs), mainTabCount)
			}
			var labels strings.Builder
			for _, tab := range tabs {
				labels.WriteString(tab.label)
				if tab.x+tab.width > test.width {
					t.Fatalf("tab %q overflows width %d", tab.label, test.width)
				}
			}
			if !strings.Contains(labels.String(), test.want) {
				t.Fatalf("width %d labels %q do not contain %q", test.width, labels.String(), test.want)
			}
		})
	}
}

func TestQuotaStyleTabsChooseResponsiveLabels(t *testing.T) {
	for _, test := range []struct {
		width int
		want  string
	}{
		{width: 100, want: "CONSUMPTION PACE"},
		{width: 28, want: "PACE"},
		{width: 12, want: "[C]"},
		{width: 4, want: "C"},
	} {
		t.Run(test.want, func(t *testing.T) {
			tabs, _ := quotaStyleTabLayout(test.width)
			if len(tabs) != len(quotaStyleOrder) {
				t.Fatalf("width %d displayed %d quota tabs, want %d", test.width, len(tabs), len(quotaStyleOrder))
			}
			var labels strings.Builder
			for _, tab := range tabs {
				labels.WriteString(tab.label)
				if tab.x+tab.width > test.width {
					t.Fatalf("tab %q overflows width %d", tab.label, test.width)
				}
			}
			if !strings.Contains(labels.String(), test.want) {
				t.Fatalf("width %d labels %q do not contain %q", test.width, labels.String(), test.want)
			}
		})
	}
}

func TestMainTabsSupportHoverClickAndPulse(t *testing.T) {
	model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width, model.height = 100, 30
	tabs, _ := mainTabLayout(model.contentWidth(), false)
	target := tabs[mainTabMonitor]
	mouse := tea.MouseMsg{
		X:      2 + target.x + target.width/2,
		Y:      model.dashboardLayout().tabsY,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || !model.mainTabHovered || model.hoveredMainTab != mainTabMonitor {
		t.Fatalf("monitor hover was not recorded: hovered=%v tab=%d", model.mainTabHovered, model.hoveredMainTab)
	}

	mouse.Action = tea.MouseActionPress
	updated, command = model.Update(mouse)
	model = updated.(Model)
	if command == nil || model.meterStyle != styleMonitor || !model.styleFlashing || model.flashedStyle != styleMonitor {
		t.Fatalf("monitor click did not select and pulse: style=%d flashing=%v", model.meterStyle, model.styleFlashing)
	}
}

func TestQuotaStyleTabsSupportHoverClickAndPulse(t *testing.T) {
	model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width, model.height = 100, 30
	tabs, _ := quotaStyleTabLayout(model.contentWidth())
	target := tabs[1]
	mouse := tea.MouseMsg{
		X:      2 + target.x + target.width/2,
		Y:      model.dashboardLayout().quotaTabsY,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || !model.styleHovered || model.hoveredStyle != styleConsumptionPace {
		t.Fatalf("consumption pace hover was not recorded: hovered=%v style=%d", model.styleHovered, model.hoveredStyle)
	}

	mouse.Action = tea.MouseActionPress
	updated, command = model.Update(mouse)
	model = updated.(Model)
	if command == nil || model.meterStyle != styleConsumptionPace || model.quotaMeterStyle != styleConsumptionPace || !model.styleFlashing || model.flashedStyle != styleConsumptionPace {
		t.Fatalf("consumption pace click did not select, remember, and pulse: style=%d remembered=%d flashing=%v", model.meterStyle, model.quotaMeterStyle, model.styleFlashing)
	}
	updated, _ = model.Update(styleTabFlashExpiredMsg{style: styleConsumptionPace, sequence: model.styleSequence})
	model = updated.(Model)
	if model.styleFlashing {
		t.Fatal("current tab pulse did not expire")
	}
}

func TestEveryRenderedTabCellIsClickableAcrossWidths(t *testing.T) {
	for _, width := range []int{8, 12, 20, 40, 45, 60, 80, 100, 160} {
		model := Model{snapshot: codex.DemoSnapshot(), width: width, height: 24}
		layout := model.dashboardLayout()
		mainTabs, _ := mainTabLayout(layout.contentWidth, false)
		for _, tab := range mainTabs {
			for offset := 0; offset < tab.width; offset++ {
				if got, ok := model.mainTabAt(2+tab.x+offset, layout.tabsY); !ok || got != tab.tab {
					t.Errorf("width %d main tab %q cell %d hit (%d,%v)", width, tab.label, offset, got, ok)
				}
			}
		}
		quotaTabs, _ := quotaStyleTabLayout(layout.contentWidth)
		for _, tab := range quotaTabs {
			for offset := 0; offset < tab.width; offset++ {
				if got, ok := model.quotaStyleTabAt(2+tab.x+offset, layout.quotaTabsY); !ok || got != tab.style {
					t.Errorf("width %d quota tab %q cell %d hit (%d,%v)", width, tab.label, offset, got, ok)
				}
			}
		}
	}
}

func TestQuotaStyleIsRememberedAcrossMainTabNavigation(t *testing.T) {
	model := Model{meterStyle: stylePie}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.meterStyle != styleMonitor {
		t.Fatalf("Tab selected %s, want Monitor", model.meterStyle.name())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.meterStyle != styleBenchmark {
		t.Fatalf("second Tab selected %s, want Benchmark", model.meterStyle.name())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.meterStyle != stylePie {
		t.Fatalf("return to Quota selected %s, want remembered Pie", model.meterStyle.name())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(Model)
	if model.meterStyle != styleBenchmark {
		t.Fatalf("Shift+Tab selected %s, want Benchmark", model.meterStyle.name())
	}
}

func TestVSelectsQuotaViewAndSOnlyStartsMonitor(t *testing.T) {
	model := Model{meterStyle: styleBars}
	for _, want := range []meterStyleID{styleConsumptionPace, stylePie, styleFuel, styleBars} {
		updated, command := model.Update(key('v'))
		model = updated.(Model)
		if command == nil || model.meterStyle != want || model.quotaMeterStyle != want || model.flashedButton != footerButtonView {
			t.Fatalf("V selected view=%d remembered=%d flash=%d, want view=%d", model.meterStyle, model.quotaMeterStyle, model.flashedButton, want)
		}
	}
	sequence := model.flashSequence
	updated, command := model.Update(key('s'))
	model = updated.(Model)
	if command != nil || model.meterStyle != styleBars || model.flashSequence != sequence {
		t.Fatal("S changed the Quota view")
	}

	model = Model{meterStyle: styleMonitor, monitorState: monitorIdle}
	updated, command = model.Update(key('s'))
	model = updated.(Model)
	if command == nil || model.monitorState != monitorStarting || model.flashedButton != footerButtonMonitorGo {
		t.Fatalf("Monitor S did not start: state=%d flash=%d", model.monitorState, model.flashedButton)
	}

	model = Model{meterStyle: styleBenchmark}
	updated, command = model.Update(key('s'))
	model = updated.(Model)
	if command != nil || model.meterStyle != styleBenchmark || model.flashedButton != footerButtonNone {
		t.Fatal("S changed state in Benchmark")
	}
}

func TestQuotaSubTabsOnlyRenderAndHitTestWithinQuota(t *testing.T) {
	colors := paletteFor(themeHacker)
	for _, style := range []meterStyleID{styleBars, styleMonitor, styleBenchmark} {
		model := Model{snapshot: codex.DemoSnapshot(), width: 100, height: 30, meterStyle: style}
		output := ansi.Strip(model.View())
		layout := model.dashboardLayout()
		if style.isQuota() {
			if layout.quotaTabsY < 0 || !strings.Contains(output, "CONSUMPTION PACE") {
				t.Fatalf("Quota did not render its sub-tab rail:\n%s", output)
			}
			continue
		}
		if layout.quotaTabsY != -1 || strings.Contains(output, "CONSUMPTION PACE") {
			t.Fatalf("%s exposed Quota sub-tabs", style.name())
		}
		for y := 0; y < model.height; y++ {
			for x := 0; x < model.width; x++ {
				if _, ok := model.quotaStyleTabAt(x, y); ok {
					t.Fatalf("%s exposed hidden Quota sub-tab hit target at %d,%d", style.name(), x, y)
				}
			}
		}
		if lipgloss.Width(model.renderMainTabs(model.contentWidth(), colors)) != model.contentWidth() {
			t.Fatalf("%s main tab rail did not fill available width", style.name())
		}
	}
}

func TestMonitorMainTabShowsPulsingRecordingDotWhileQuotaIsActive(t *testing.T) {
	model := Model{meterStyle: styleBars, monitorState: monitorRunning}
	colors := paletteFor(themeHacker)
	model.phase = 0
	bright := model.renderMainTabs(100, colors)
	model.phase = 1
	dark := model.renderMainTabs(100, colors)
	if !strings.Contains(ansi.Strip(bright), "●") {
		t.Fatal("recording tab did not retain a pulsing dot away from the Monitor view")
	}
	if recordingDotColor(0, colors) == recordingDotColor(1, colors) {
		t.Fatal("recording tab dot does not alternate between bright and dark red")
	}
	if lipgloss.Width(bright) != 100 || lipgloss.Width(dark) != 100 {
		t.Fatalf("tab rail did not fill its responsive width: bright=%d dark=%d", lipgloss.Width(bright), lipgloss.Width(dark))
	}
}

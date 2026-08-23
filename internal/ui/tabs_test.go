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

func TestMainTabsChooseResponsiveLabels(t *testing.T) {
	for _, test := range []struct {
		width int
		want  string
	}{
		{width: 100, want: "BENCHMARK"},
		{width: 22, want: "QTA"},
		{width: 9, want: "●"},
		{width: 3, want: "●"},
	} {
		t.Run(test.want, func(t *testing.T) {
			tabs, _ := mainTabLayout(test.width, true)
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

func TestQuotaViewTabsChooseResponsiveLabels(t *testing.T) {
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
			tabs, _ := quotaViewTabLayout(test.width)
			if len(tabs) != len(quotaViewOrder) {
				t.Fatalf("width %d displayed %d quota tabs, want %d", test.width, len(tabs), len(quotaViewOrder))
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
			if len(tabs) >= 3 && (tabs[1].view != viewConsumptionPace || !strings.Contains(tabs[1].label, test.want) || tabs[2].view != viewPie) {
				t.Fatalf("width %d tab order/labels do not match views: %#v", test.width, tabs)
			}
		})
	}
}

func TestMainTabsSupportHoverClickAndPulse(t *testing.T) {
	model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width, model.height = 100, 30
	tabs, _ := mainTabLayout(model.contentWidth(), true)
	target := tabs[mainTabMonitor]
	mouse := tea.MouseMotionMsg{
		X:      2 + target.x + target.width/2,
		Y:      model.dashboardLayout().tabsY,
		Button: tea.MouseLeft,
	}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || !model.mainTabHovered || model.hoveredMainTab != mainTabMonitor {
		t.Fatalf("monitor hover was not recorded: hovered=%v tab=%d", model.mainTabHovered, model.hoveredMainTab)
	}

	updated, command = model.Update(tea.MouseClickMsg(mouse))
	model = updated.(Model)
	if command == nil || model.meterView != viewMonitor || !model.viewFlashing || model.flashedView != viewMonitor {
		t.Fatalf("monitor click did not select and pulse: view=%d flashing=%v", model.meterView, model.viewFlashing)
	}
}

func TestQuotaViewTabsSupportHoverClickAndPulse(t *testing.T) {
	model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width, model.height = 100, 30
	tabs, _ := quotaViewTabLayout(model.contentWidth())
	target := tabs[1]
	mouse := tea.MouseMotionMsg{
		X:      2 + target.x + target.width/2,
		Y:      model.dashboardLayout().quotaTabsY,
		Button: tea.MouseLeft,
	}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || !model.viewHovered || model.hoveredView != viewConsumptionPace {
		t.Fatalf("consumption pace hover was not recorded: hovered=%v view=%d", model.viewHovered, model.hoveredView)
	}

	updated, command = model.Update(tea.MouseClickMsg(mouse))
	model = updated.(Model)
	if command == nil || model.meterView != viewConsumptionPace || model.quotaMeterView != viewConsumptionPace || !model.viewFlashing || model.flashedView != viewConsumptionPace {
		t.Fatalf("consumption pace click did not select, remember, and pulse: view=%d remembered=%d flashing=%v", model.meterView, model.quotaMeterView, model.viewFlashing)
	}
	updated, _ = model.Update(viewTabFlashExpiredMsg{view: viewConsumptionPace, sequence: model.viewSequence})
	model = updated.(Model)
	if model.viewFlashing {
		t.Fatal("current tab pulse did not expire")
	}
}

func TestEveryRenderedTabCellIsClickableAcrossWidths(t *testing.T) {
	for _, width := range []int{8, 12, 20, 40, 45, 60, 80, 100, 160} {
		model := Model{snapshot: codex.DemoSnapshot(), width: width, height: 24}
		layout := model.dashboardLayout()
		mainTabs, _ := mainTabLayout(layout.contentWidth, true)
		for _, tab := range mainTabs {
			for offset := 0; offset < tab.width; offset++ {
				if got, ok := model.mainTabAt(2+tab.x+offset, layout.tabsY); !ok || got != tab.tab {
					t.Errorf("width %d main tab %q cell %d hit (%d,%v)", width, tab.label, offset, got, ok)
				}
			}
		}
		quotaTabs, _ := quotaViewTabLayout(layout.contentWidth)
		for _, tab := range quotaTabs {
			for offset := 0; offset < tab.width; offset++ {
				if got, ok := model.quotaViewTabAt(2+tab.x+offset, layout.quotaTabsY); !ok || got != tab.view {
					t.Errorf("width %d quota tab %q cell %d hit (%d,%v)", width, tab.label, offset, got, ok)
				}
			}
		}
	}
}

func TestQuotaStyleIsRememberedAcrossMainTabNavigation(t *testing.T) {
	model := Model{meterView: viewPie}
	updated, _ := model.Update(specialKey(tea.KeyTab))
	model = updated.(Model)
	if model.meterView != viewMonitor {
		t.Fatalf("Tab selected %s, want Monitor", model.meterView.name())
	}
	updated, _ = model.Update(specialKey(tea.KeyTab))
	model = updated.(Model)
	if model.meterView != viewBenchmark {
		t.Fatalf("second Tab selected %s, want Benchmark", model.meterView.name())
	}
	updated, _ = model.Update(specialKey(tea.KeyTab))
	model = updated.(Model)
	if model.meterView != viewPie {
		t.Fatalf("return to Quota selected %s, want remembered Pie", model.meterView.name())
	}
	updated, _ = model.Update(modifiedKey(tea.KeyTab, tea.ModShift))
	model = updated.(Model)
	if model.meterView != viewBenchmark {
		t.Fatalf("Shift+Tab selected %s, want Benchmark", model.meterView.name())
	}
}

func TestVSelectsQuotaViewAndMonitorShortcutsStayScoped(t *testing.T) {
	model := Model{meterView: viewBars}
	for _, want := range []meterViewID{viewConsumptionPace, viewPie, viewFuel, viewBars} {
		updated, command := model.Update(key('v'))
		model = updated.(Model)
		if command == nil || model.meterView != want || model.quotaMeterView != want || model.flashedButton != footerButtonView {
			t.Fatalf("V selected view=%d remembered=%d flash=%d, want view=%d", model.meterView, model.quotaMeterView, model.flashedButton, want)
		}
	}
	sequence := model.flashSequence
	updated, command := model.Update(key('s'))
	model = updated.(Model)
	if command != nil || model.meterView != viewBars || model.flashSequence != sequence {
		t.Fatal("S changed the Quota view")
	}

	model = Model{meterView: viewMonitor, monitorState: monitorRunning}
	updated, command = model.Update(key('s'))
	model = updated.(Model)
	if command == nil || model.monitorState != monitorResetting || model.flashedButton != footerButtonMonitorReset {
		t.Fatalf("Monitor S did not reset: state=%d flash=%d", model.monitorState, model.flashedButton)
	}

	model = Model{meterView: viewMonitor, monitorState: monitorRunning}
	updated, command = model.Update(key('p'))
	model = updated.(Model)
	if command == nil || model.monitorState != monitorPausing || model.flashedButton != footerButtonMonitorPause {
		t.Fatalf("Monitor P did not pause: state=%d flash=%d", model.monitorState, model.flashedButton)
	}

	model = Model{meterView: viewBenchmark}
	updated, command = model.Update(key('s'))
	model = updated.(Model)
	if command != nil || model.meterView != viewBenchmark || model.flashedButton != footerButtonNone {
		t.Fatal("S changed state in Benchmark")
	}
}

func TestQuotaSubTabsOnlyRenderAndHitTestWithinQuota(t *testing.T) {
	colors := paletteFor(themeHacker)
	for _, view := range []meterViewID{viewBars, viewMonitor, viewBenchmark} {
		model := Model{snapshot: codex.DemoSnapshot(), width: 100, height: 30, meterView: view}
		output := ansi.Strip(model.render())
		layout := model.dashboardLayout()
		if view.isQuota() {
			if layout.quotaTabsY < 0 || !strings.Contains(output, "CONSUMPTION PACE") {
				t.Fatalf("Quota did not render its sub-tab rail:\n%s", output)
			}
			continue
		}
		if layout.quotaTabsY != -1 || strings.Contains(output, "CONSUMPTION PACE") {
			t.Fatalf("%s exposed Quota sub-tabs", view.name())
		}
		for y := 0; y < model.height; y++ {
			for x := 0; x < model.width; x++ {
				if _, ok := model.quotaViewTabAt(x, y); ok {
					t.Fatalf("%s exposed hidden Quota sub-tab hit target at %d,%d", view.name(), x, y)
				}
			}
		}
		if lipgloss.Width(model.renderMainTabs(model.contentWidth(), colors)) != model.contentWidth() {
			t.Fatalf("%s main tab rail did not fill available width", view.name())
		}
	}
}

func TestMonitorIndicatorUsesActivityAndAppServerHealth(t *testing.T) {
	model := Model{
		meterView: viewBars, monitorState: monitorRunning,
		monitorAppServerKnown: true, monitorAppServerUp: true, monitorAppServerWorking: true,
	}
	colors := paletteFor(themeHacker)
	model.phase = 0
	bright := model.renderMainTabs(100, colors)
	model.phase = 1
	dark := model.renderMainTabs(100, colors)
	if !strings.Contains(ansi.Strip(bright), "●") {
		t.Fatal("Monitor tab did not retain its status light away from the Monitor view")
	}
	if model.monitorIndicatorColor(colors) != colors.dim {
		t.Fatal("working indicator did not alternate to the theme's dim color")
	}
	model.phase = 0
	if model.monitorIndicatorColor(colors) != colors.accent {
		t.Fatal("working indicator did not alternate to the theme's highlight color")
	}
	model.monitorAppServerWorking = false
	if model.monitorIndicatorColor(colors) != colors.success {
		t.Fatal("idle app server indicator was not steady green")
	}
	model.monitorAppServerUp = false
	if model.monitorIndicatorColor(colors) != colors.danger {
		t.Fatal("unreachable app server indicator was not steady red")
	}
	model.monitorAppServerKnown = false
	if model.monitorIndicatorColor(colors) != colors.dim {
		t.Fatal("unknown app server status was not shown as dim")
	}
	model.monitorState = monitorPaused
	model.monitorAppServerKnown = true
	model.monitorAppServerUp = true
	model.monitorAppServerWorking = true
	if model.monitorIndicatorColor(colors) != colors.dim {
		t.Fatal("paused Monitor presented stale app-server health as current")
	}
	if lipgloss.Width(bright) != 100 || lipgloss.Width(dark) != 100 {
		t.Fatalf("tab rail did not fill its responsive width: bright=%d dark=%d", lipgloss.Width(bright), lipgloss.Width(dark))
	}
}

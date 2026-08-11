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

func TestStyleTabsChooseResponsiveLabels(t *testing.T) {
	for _, test := range []struct {
		width int
		want  string
	}{
		{width: 100, want: "CONSUMPTION PACE"},
		{width: 44, want: "MON"},
		{width: 19, want: "[M]"},
		{width: 9, want: "M"},
	} {
		t.Run(test.want, func(t *testing.T) {
			tabs, _ := styleTabLayout(test.width, false)
			if len(tabs) != int(styleCount) {
				t.Fatalf("width %d displayed %d tabs, want %d", test.width, len(tabs), styleCount)
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

func TestStyleTabsSupportHoverClickAndPulse(t *testing.T) {
	model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width, model.height = 100, 30
	tabs, _ := styleTabLayout(model.contentWidth(), false)
	target := tabs[stylePie]
	mouse := tea.MouseMsg{
		X:      2 + target.x + target.width/2,
		Y:      model.dashboardLayout().tabsY,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	}
	updated, command := model.Update(mouse)
	model = updated.(Model)
	if command != nil || !model.styleHovered || model.hoveredStyle != stylePie {
		t.Fatalf("pie hover was not recorded: hovered=%v style=%d", model.styleHovered, model.hoveredStyle)
	}

	mouse.Action = tea.MouseActionPress
	updated, command = model.Update(mouse)
	model = updated.(Model)
	if command == nil || model.meterStyle != stylePie || !model.styleFlashing || model.flashedStyle != stylePie {
		t.Fatalf("pie click did not select and pulse: style=%d flashing=%v", model.meterStyle, model.styleFlashing)
	}
	updated, _ = model.Update(styleTabFlashExpiredMsg{style: stylePie, sequence: model.styleSequence})
	model = updated.(Model)
	if model.styleFlashing {
		t.Fatal("current tab pulse did not expire")
	}
}

func TestEveryRenderedTabCellIsClickableAcrossWidths(t *testing.T) {
	for _, width := range []int{40, 45, 60, 80, 100, 160} {
		model := Model{snapshot: codex.DemoSnapshot(), width: width, height: 24}
		layout := model.dashboardLayout()
		tabs, _ := styleTabLayout(layout.contentWidth, false)
		for _, tab := range tabs {
			for offset := 0; offset < tab.width; offset++ {
				if got, ok := model.styleTabAt(2+tab.x+offset, layout.tabsY); !ok || got != tab.style {
					t.Errorf("width %d tab %q cell %d hit (%d,%v)", width, tab.label, offset, got, ok)
				}
			}
		}
	}
}

func TestMonitorTabShowsPulsingRecordingDotWhileAnotherTabIsActive(t *testing.T) {
	model := Model{meterStyle: styleBars, monitorState: monitorRunning}
	colors := paletteFor(themeHacker)
	model.phase = 0
	bright := model.renderStyleTabs(100, colors)
	model.phase = 1
	dark := model.renderStyleTabs(100, colors)
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

func TestSOnlyStartsMonitor(t *testing.T) {
	model := Model{meterStyle: styleBars}
	updated, command := model.Update(key('s'))
	model = updated.(Model)
	if command != nil || model.meterStyle != styleBars || model.monitorState != monitorIdle {
		t.Fatal("S changed state outside the Monitor tab")
	}
}

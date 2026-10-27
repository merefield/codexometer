package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestMonitorSessionColumnsCapTelemetryAndPreserveSpace(t *testing.T) {
	for _, tc := range []struct{ width, metrics, detail int }{
		{3, 1, 1}, {80, 26, 53}, {144, 47, 96}, {145, 48, 96},
		{147, 48, 98}, {148, 48, 99}, {240, 48, 191}, {320, 48, 271},
	} {
		metrics, detail, ok := monitorSessionColumnWidths(tc.width)
		if !ok || metrics != tc.metrics || detail != tc.detail || metrics+detail+1 != tc.width {
			t.Fatalf("width %d: metrics/detail = %d/%d, ok %v; want %d/%d", tc.width, metrics, detail, ok, tc.metrics, tc.detail)
		}
	}
	for _, width := range []int{0, 1, 2} {
		metrics, detail, ok := monitorSessionColumnWidths(width)
		if ok || metrics != 0 || detail != width {
			t.Fatalf("narrow fallback changed at %d", width)
		}
	}
}

func TestMonitorTelemetryCapResizeKeepsRenderedTargetsAligned(t *testing.T) {
	m := contextTestModel()
	m.setRowContext("root-one", contextWide)
	for _, width := range []int{120, 320, 148, 80, 240} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 45})
		m = next.(Model)
		label := ansi.Strip(m.renderMonitorSessionDismiss("root-one", paletteFor(m.theme)))
		x, y := renderedTextStart(t, m, label)
		if id, ok := m.monitorSessionDismissAt(x, y); !ok || id != "root-one" {
			t.Fatalf("width %d dismiss target missed: %s, %v", width, id, ok)
		}
		if x+lipgloss.Width(monitorDismissLabel) > 2+48 {
			t.Fatalf("width %d dismiss target exceeds capped box", width)
		}
		for _, delta := range []int{-1, 1} {
			x, y := monitorDetailPoint(m, "root-one", delta)
			want := "less:root-one"
			if delta > 0 {
				want = "more:root-one"
			}
			if got := m.monitorContextAt(x, y); got != want {
				t.Fatalf("width %d detail half click = %s, want %s", width, got, want)
			}
		}
	}
}

func TestMonitorSessionTelemetryRenderedWidthIsCappedInEveryRowMode(t *testing.T) {
	for _, width := range []int{145, 148, 240, 320} {
		for _, mode := range []int{contextGraph, contextSplit, contextWide} {
			m := contextTestModel()
			m.setRowContext("root-one", mode)
			s := m.monitorSessionData[0]
			s.name = strings.Repeat("A long session name 日本語 ", 20)
			s.workingDirectory = "/work/" + strings.Repeat("long-directory/", 20)
			row := ansi.Strip(m.renderMonitorSessionRow(width, 10, s, "", paletteFor(m.theme)))
			if lipgloss.Width(row) != width {
				t.Fatalf("mode %d row width %d, want %d", mode, lipgloss.Width(row), width)
			}
			top := strings.Split(row, "\n")[0]
			end := strings.Index(top, "╮")
			if end < 0 || lipgloss.Width(top[:end])+1 != 48 {
				t.Fatalf("mode %d width %d telemetry border not at 48: %q", mode, width, top)
			}
		}
	}
}

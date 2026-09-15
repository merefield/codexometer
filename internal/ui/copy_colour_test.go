package ui

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
)

func TestCopyColoursAcrossThemes(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		colors := paletteFor(theme)
		m := completedCopyModel()
		if got := m.renderMonitorCopy(80, "root-one", colors); got != colors.label().Foreground(colors.primary).Render(benchmarkDetailCopyLabel) {
			t.Fatalf("theme %v: session copy does not match border", theme)
		}
		want := lipgloss.NewStyle().Foreground(colors.primary).Background(colors.background).Render(benchmarkDetailCopyLabel)
		if got := m.renderBenchmarkDetailControl(benchmarkDetailCopyLabel, footerButtonBenchmarkCopy, colors); got != want {
			t.Fatalf("theme %v: benchmark detail copy does not match border", theme)
		}
		dim := lipgloss.NewStyle().Foreground(colors.dim).Background(colors.background).Render(benchmarkDetailCopyLabel)
		if got := m.renderBenchmarkTableCopyControl(colors); got != dim {
			t.Fatalf("theme %v: unavailable matrix copy is not dim", theme)
		}
		m.benchmarkResults = []codex.BenchmarkResult{{Model: "test"}}
		if got := m.renderBenchmarkTableCopyControl(colors); got != want {
			t.Fatalf("theme %v: available matrix copy does not match border", theme)
		}
	}
}

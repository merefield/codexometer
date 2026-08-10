package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func TestEveryMeterStyleHasDistinctiveOutput(t *testing.T) {
	colors := paletteFor(themeHacker)
	tests := []struct {
		style meterStyleID
		want  string
	}{
		{styleBars, "█"},
		{stylePie, "BRAILLE PIE"},
		{styleConsumptionPace, "PACE DATA UNAVAILABLE"},
		{styleFuel, "RANGE"},
	}
	for _, test := range tests {
		output := renderVisualization(60, 62, test.style, colors.primary, colors)
		if !strings.Contains(output, test.want) {
			t.Errorf("style %s output did not contain %q", test.style.name(), test.want)
		}
	}
}

func TestEveryMeterStyleUsesAllocatedRectangle(t *testing.T) {
	colors := paletteFor(themeHacker)
	for style := styleBars; style < styleCount; style++ {
		output := renderVisualizationSized(50, 12, 62, style, colors.primary, colors)
		if got := lipgloss.Width(output); got > 50 {
			t.Errorf("%s width = %d, exceeds allocation 50", style.name(), got)
		}
		if got := lipgloss.Height(output); got != 12 {
			t.Errorf("%s height = %d, want allocation 12", style.name(), got)
		}
	}
}

func TestEveryStyleIncludesResetCycleGauge(t *testing.T) {
	colors := paletteFor(themeHacker)
	duration := int64(60)
	reset := time.Now().Add(30 * time.Minute).Unix()
	meter := codex.Meter{
		Bucket: "codex",
		Name:   "1 HOUR",
		Window: codex.Window{UsedPercent: 40, WindowDurationMins: &duration, ResetsAt: &reset},
	}
	for style := styleBars; style < styleCount; style++ {
		output := ansi.Strip(renderMeterArea(80, 18, meter, style, colors))
		if !strings.Contains(output, "RESET CYCLE  50%") {
			t.Errorf("%s missing reset-cycle comparison gauge:\n%s", style.name(), output)
		}
		visual := renderVisualizationSized(50, 10, 40, style, colors.primary, colors)
		visualWidth := min(lipgloss.Width(visual), 50)
		gauge := ansi.Strip(renderResetGauge(50, visualWidth, meter.Window, time.Unix(reset, 0).Add(-30*time.Minute), "RESET T-00:30:00", colors.primary, colors))
		lines := strings.Split(gauge, "\n")
		if len(lines) != 2 {
			t.Errorf("%s reset gauge used %d lines, want label and bar: %q", style.name(), len(lines), gauge)
			continue
		}
		if strings.ContainsAny(lines[0], "█░") {
			t.Errorf("%s reset label shares its line with the bar: %q", style.name(), lines[0])
		}
		if got := lipgloss.Width(strings.TrimRight(lines[1], " ")); got != visualWidth {
			t.Errorf("%s reset bar width = %d, want main display width %d", style.name(), got, visualWidth)
		}
	}
}

func TestResetGaugeUsesSameActiveColorAsMeter(t *testing.T) {
	colors := paletteFor(themeHacker)
	duration := int64(60)
	reset := time.Unix(10_000, 0)
	window := codex.Window{WindowDurationMins: &duration, ResetsAt: ptr(reset.Unix())}
	gauge := renderResetGauge(40, 40, window, reset.Add(-30*time.Minute), "RESET T-00:30:00", colors.warning, colors)
	want := lipgloss.NewStyle().Foreground(colors.warning).Render(strings.Repeat("█", 20))
	if !strings.Contains(gauge, want) {
		t.Fatalf("reset gauge did not use meter's active color: %q", gauge)
	}
}

func TestFuelResetGaugeDrainsAndAlignsWithTankCells(t *testing.T) {
	colors := paletteFor(themeHacker)
	duration := int64(60)
	reset := time.Unix(10_000, 0)
	window := codex.Window{WindowDurationMins: &duration, ResetsAt: ptr(reset.Unix())}
	gauge := ansi.Strip(renderReverseResetGauge(50, 44, window, reset.Add(-45*time.Minute), "RESET T-00:45:00", colors.primary, colors))
	lines := strings.Split(gauge, "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "RESET CYCLE  75% LEFT") {
		t.Fatalf("reverse reset gauge did not show remaining cycle time: %q", gauge)
	}
	if leading := lipgloss.Width(lines[1]) - lipgloss.Width(strings.TrimLeft(lines[1], " ")); leading != 3 {
		t.Fatalf("reverse reset gauge begins at column %d, want tank-cell column 3: %q", leading, lines[1])
	}
	if cells := strings.Count(lines[1], "█") + strings.Count(lines[1], "░"); cells != 44 {
		t.Fatalf("reverse reset gauge has %d cells, want tank width 44: %q", cells, lines[1])
	}
	if filled := strings.Count(lines[1], "█"); filled != 33 {
		t.Fatalf("reverse reset gauge filled %d cells, want 75%% of 44 = 33: %q", filled, lines[1])
	}
}

func TestResetProgressUsesWindowStartAndClamps(t *testing.T) {
	duration := int64(60)
	reset := time.Unix(10_000, 0)
	window := codex.Window{WindowDurationMins: &duration, ResetsAt: ptr(reset.Unix())}
	tests := []struct {
		name string
		now  time.Time
		want int
	}{
		{"before start", reset.Add(-2 * time.Hour), 0},
		{"halfway", reset.Add(-30 * time.Minute), 50},
		{"at reset", reset, 100},
		{"after reset", reset.Add(time.Hour), 100},
	}
	for _, test := range tests {
		got, ok := resetProgress(window, test.now)
		if !ok || got != test.want {
			t.Errorf("%s progress = %d, %v; want %d, true", test.name, got, ok, test.want)
		}
	}
	if _, ok := resetProgress(codex.Window{}, reset); ok {
		t.Fatal("missing reset data unexpectedly produced progress")
	}
}

func TestConsumptionPaceComparesElapsedTimeWithUsage(t *testing.T) {
	duration := int64(100)
	reset := time.Unix(20_000, 0)
	now := reset.Add(-50 * time.Minute)
	window := codex.Window{UsedPercent: 25, WindowDurationMins: &duration, ResetsAt: ptr(reset.Unix())}
	if got, ok := consumptionPace(window, now); !ok || got != 25 {
		t.Fatalf("under-pace consumption = %d, %v; want +25, true", got, ok)
	}
	window.UsedPercent = 75
	if got, ok := consumptionPace(window, now); !ok || got != -25 {
		t.Fatalf("over-pace consumption = %d, %v; want -25, true", got, ok)
	}
	if _, ok := consumptionPace(codex.Window{UsedPercent: 25}, now); ok {
		t.Fatal("missing window timing unexpectedly produced a consumption pace")
	}
}

func ptr[T any](value T) *T { return &value }

func TestFuelTankReachesEmptyEndState(t *testing.T) {
	colors := paletteFor(themeUltraviolet)
	output := renderFuelTank(40, 100, colors.danger, colors)
	if !strings.Contains(output, "RANGE   0%") {
		t.Fatalf("fuel end state missing empty range: %q", output)
	}
}

func TestFuelTankIsReverseGaugeWithCorrectEndpoints(t *testing.T) {
	colors := paletteFor(themeHacker)
	output := renderFuelTankSized(20, 4, 25, colors.warning, colors)
	available := lipgloss.NewStyle().Foreground(colors.primary).Render(strings.Repeat("▰", 11))
	consumed := lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("▱", 3))
	if !strings.Contains(output, available+consumed) {
		t.Fatalf("fuel tank did not render remaining capacity as a reverse gauge: %q", output)
	}
	if !strings.Contains(ansi.Strip(output), "E              F") {
		t.Fatalf("fuel tank endpoint labels were not Empty-to-Full: %q", output)
	}
}

func TestConsumptionPaceGaugeUsesSignedHorizontalAxis(t *testing.T) {
	colors := paletteFor(themeHacker)
	positive := ansi.Strip(renderConsumptionPaceSized(41, 4, 25, true, colors))
	negative := ansi.Strip(renderConsumptionPaceSized(41, 4, -25, true, colors))
	for _, want := range []string{"-100", "+100", "HEADROOM +25 POINTS", "CONSUMPTION PACE"} {
		if !strings.Contains(positive, want) {
			t.Fatalf("positive pace gauge missing %q: %q", want, positive)
		}
	}
	if !strings.Contains(negative, "DEFICIT -25 POINTS") {
		t.Fatalf("negative pace gauge missing deficit: %q", negative)
	}
	positiveMarker := runeColumn(lineContainingRune(positive, '◆'), '◆')
	negativeMarker := runeColumn(lineContainingRune(negative, '◆'), '◆')
	if positiveMarker <= 20 || negativeMarker >= 20 {
		t.Fatalf("pace markers did not straddle zero: negative=%d positive=%d", negativeMarker, positiveMarker)
	}
}

func TestRadialGraphicsUseLargeCircularCanvases(t *testing.T) {
	colors := paletteFor(themeUltraviolet)
	plain := ansi.Strip(renderPie(52, 62, colors.primary, colors))
	if width := longestLineWidth(plain); width < 36 {
		t.Errorf("pie is only %d cells wide, want at least 36", width)
	}
	if height := strings.Count(plain, "\n") + 1; height < 11 {
		t.Errorf("pie is only %d rows tall, want at least 11", height)
	}
}

func TestRadialGraphicsScaleToAllocatedRectangle(t *testing.T) {
	colors := paletteFor(themeBlueSteel)
	small := renderVisualizationSized(42, 9, 62, stylePie, colors.primary, colors)
	large := renderVisualizationSized(70, 20, 62, stylePie, colors.primary, colors)
	if got := lipgloss.Width(small); got != 42 {
		t.Errorf("pie small width = %d, want 42", got)
	}
	if got := lipgloss.Height(small); got != 9 {
		t.Errorf("pie small height = %d, want 9", got)
	}
	if got := lipgloss.Width(large); got != 70 {
		t.Errorf("pie large width = %d, want 70", got)
	}
	if got := lipgloss.Height(large); got != 20 {
		t.Errorf("pie large height = %d, want 20", got)
	}
	if brailleCount(ansi.Strip(large)) <= brailleCount(ansi.Strip(small)) {
		t.Error("pie did not add radial detail when enlarged")
	}
}

func brailleCount(output string) int {
	count := 0
	for _, r := range output {
		if r >= 0x2800 && r <= 0x28ff {
			count++
		}
	}
	return count
}

func TestBraillePieHasSmoothPartialEdgesAndEndStates(t *testing.T) {
	colors := paletteFor(themeHacker)
	partial := ansi.Strip(renderPie(52, 62, colors.primary, colors))
	if !strings.ContainsAny(partial, "⡀⢀⠈⠁⠂⠄⠆⠇⣀⣠⣤⣴⣶⣷") {
		t.Fatalf("pie did not use partial Braille edge cells: %q", partial)
	}
	for _, used := range []int{0, 100} {
		output := ansi.Strip(renderPie(52, used, colors.primary, colors))
		if !strings.Contains(output, fmt.Sprintf("%3d%%", used)) {
			t.Fatalf("pie end state %d missing percentage: %q", used, output)
		}
	}
}

func longestLineWidth(output string) int {
	longest := 0
	for _, line := range strings.Split(output, "\n") {
		longest = max(longest, len([]rune(line)))
	}
	return longest
}

func lineContainingRune(output string, needle rune) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.ContainsRune(line, needle) {
			return line
		}
	}
	return ""
}

func runeColumn(line string, needle rune) int {
	for column, r := range []rune(line) {
		if r == needle {
			return column
		}
	}
	return -1
}

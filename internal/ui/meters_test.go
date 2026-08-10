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
		{styleRotary, "ROTARY"},
		{stylePie, "BRAILLE PIE"},
		{styleTachometer, "QUOTA RPM"},
		{styleCrashBar, "🚗"},
		{styleFuel, "RANGE"},
		{styleFuse, "🔥"},
		{stylePac, "╭──╮"},
		{styleBoat, "HULL STATUS"},
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

func ptr[T any](value T) *T { return &value }

func TestMetaphorStylesReachTheirEndStates(t *testing.T) {
	colors := paletteFor(themeUltraviolet)
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"fuel", renderFuelTank(40, 100, colors.danger, colors), "RANGE   0%"},
		{"fuse", renderFuse(40, 100, colors.danger, colors), "DETONATED"},
		{"pellet", renderPelletRun(40, 100, colors.danger, colors), "💥"},
		{"boat", renderSinkingShip(40, 100, colors.danger, colors), "SUNK"},
	}
	for _, test := range tests {
		if !strings.Contains(test.output, test.want) {
			t.Errorf("%s end state missing %q: %q", test.name, test.want, test.output)
		}
	}
}

func TestFuseUsesLargeBomb(t *testing.T) {
	colors := paletteFor(themeRust)
	output := ansi.Strip(renderFuse(50, 62, colors.warning, colors))
	for _, want := range []string{"╭─────╮", "┤  ●  │", "TNT", "╰─────╯"} {
		if !strings.Contains(output, want) {
			t.Fatalf("large bomb missing %q: %q", want, output)
		}
	}
	if lines := strings.Count(output, "\n") + 1; lines != 6 {
		t.Fatalf("large bomb used %d lines, want 6: %q", lines, output)
	}
}

func TestRevMeterLooksLikeCarTachometer(t *testing.T) {
	colors := paletteFor(themeHacker)
	output := ansi.Strip(renderTachometerSized(52, 14, 75, colors.warning, colors))
	for _, want := range []string{"6.0K RPM", "QUOTA RPM", "75%", "REDLINE 80"} {
		if !strings.Contains(output, want) {
			t.Fatalf("tachometer missing %q: %q", want, output)
		}
	}
	if !strings.ContainsAny(output, "⡀⢀⠈⠁⠂⠄⠆⠇⣀⣠⣤⣴⣶⣷⣿") {
		t.Fatalf("tachometer did not use a Braille radial face: %q", output)
	}
}

func TestCrashBarHitsWallAtFullUsage(t *testing.T) {
	colors := paletteFor(themeRust)
	output := ansi.Strip(renderCrashBar(40, 100, colors.danger, colors))
	if !strings.Contains(output, "█▌💥🚗") {
		t.Fatalf("full crash bar did not show impact: %q", output)
	}
}

func TestCrashBarCarTravelsRightToLeft(t *testing.T) {
	colors := paletteFor(themeHacker)
	start := ansi.Strip(renderCrashBar(50, 0, colors.primary, colors))
	progress := ansi.Strip(renderCrashBar(50, 75, colors.warning, colors))
	startRoad := lineContaining(start, "🚗")
	progressRoad := lineContaining(progress, "🚗")
	startCar := strings.Index(startRoad, "🚗")
	progressCar := strings.Index(progressRoad, "🚗")
	if strings.Index(startRoad, "█▌") != 0 || strings.Index(progressRoad, "█▌") != 0 {
		t.Fatalf("wall is not on the left: start=%q progress=%q", start, progress)
	}
	if progressCar < 0 || startCar < 0 || progressCar >= startCar {
		t.Fatalf("car did not move left: start=%d progress=%d", startCar, progressCar)
	}
}

func TestPelletRunUsesLargePacmanAndSpacedPellets(t *testing.T) {
	colors := paletteFor(themeHacker)
	output := ansi.Strip(renderPelletRun(60, 25, colors.primary, colors))
	for _, want := range []string{"╭──╮", "│   <", "╰──╯", "•  •"} {
		if !strings.Contains(output, want) {
			t.Fatalf("large pellet run missing %q: %q", want, output)
		}
	}
	if lines := strings.Count(output, "\n") + 1; lines != 5 {
		t.Fatalf("large Pac-Man used %d lines, want 5: %q", lines, output)
	}
}

func TestRadialGraphicsUseLargeCircularCanvases(t *testing.T) {
	colors := paletteFor(themeUltraviolet)
	tests := []struct {
		name      string
		output    string
		minWidth  int
		minHeight int
	}{
		{"rotary", renderRotary(52, 62, colors.primary, colors), 46, 14},
		{"pie", renderPie(52, 62, colors.primary, colors), 36, 11},
		{"rev meter", renderTachometer(52, 62, colors.primary, colors), 46, 14},
	}
	for _, test := range tests {
		plain := ansi.Strip(test.output)
		if width := longestLineWidth(plain); width < test.minWidth {
			t.Errorf("%s is only %d cells wide, want at least %d", test.name, width, test.minWidth)
		}
		if height := strings.Count(plain, "\n") + 1; height < test.minHeight {
			t.Errorf("%s is only %d rows tall, want at least %d", test.name, height, test.minHeight)
		}
	}
}

func TestRadialGraphicsScaleToAllocatedRectangle(t *testing.T) {
	colors := paletteFor(themeBlueSteel)
	for _, style := range []meterStyleID{styleRotary, stylePie, styleTachometer} {
		small := renderVisualizationSized(42, 9, 62, style, colors.primary, colors)
		large := renderVisualizationSized(70, 20, 62, style, colors.primary, colors)
		if got := lipgloss.Width(small); got != 42 {
			t.Errorf("%s small width = %d, want 42", style.name(), got)
		}
		if got := lipgloss.Height(small); got != 9 {
			t.Errorf("%s small height = %d, want 9", style.name(), got)
		}
		if got := lipgloss.Width(large); got != 70 {
			t.Errorf("%s large width = %d, want 70", style.name(), got)
		}
		if got := lipgloss.Height(large); got != 20 {
			t.Errorf("%s large height = %d, want 20", style.name(), got)
		}
		if brailleCount(ansi.Strip(large)) <= brailleCount(ansi.Strip(small)) {
			t.Errorf("%s did not add radial detail when enlarged", style.name())
		}
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

func lineContaining(output, needle string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

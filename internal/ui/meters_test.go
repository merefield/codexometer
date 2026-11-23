package ui

import (
	"fmt"
	"image"
	imagecolor "image/color"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

var quotaMeterViews = []meterViewID{viewBars, viewPace, viewPie, viewFuel}

func TestEveryMeterViewHasDistinctiveOutput(t *testing.T) {
	colors := paletteFor(themeHacker)
	tests := []struct {
		view meterViewID
		want string
	}{
		{viewBars, "█"},
		{viewPie, "PIE  // 62% USED"},
		{viewPace, "GRAPH DATA UNAVAILABLE"},
		{viewFuel, "RANGE"},
	}
	for _, test := range tests {
		output := renderVisualization(60, 62, test.view, colors.primary, colors)
		if !strings.Contains(output, test.want) {
			t.Errorf("view %s output did not contain %q", test.view.name(), test.want)
		}
	}
}

func TestEveryMeterViewUsesAllocatedRectangle(t *testing.T) {
	colors := paletteFor(themeHacker)
	for _, view := range quotaMeterViews {
		output := renderVisualizationSized(50, 12, 62, view, colors.primary, colors)
		if got := lipgloss.Width(output); got > 50 {
			t.Errorf("%s width = %d, exceeds allocation 50", view.name(), got)
		}
		if got := lipgloss.Height(output); got != 12 {
			t.Errorf("%s height = %d, want allocation 12", view.name(), got)
		}
	}
}

func TestHorizontalViewsIncludeAlignedResetCycleGauge(t *testing.T) {
	colors := paletteFor(themeHacker)
	duration := int64(60)
	reset := time.Now().Add(30 * time.Minute).Unix()
	meter := codex.Meter{
		Bucket: "codex",
		Name:   "1 HOUR",
		Window: codex.Window{UsedPercent: 40, WindowDurationMins: &duration, ResetsAt: &reset},
	}
	for _, view := range []meterViewID{viewBars, viewFuel} {
		output := ansi.Strip(renderMeterArea(80, 18, meter, view, colors))
		if !strings.Contains(output, "RESET CYCLE  50%") {
			t.Errorf("%s missing reset-cycle comparison gauge:\n%s", view.name(), output)
		}
		visual := renderVisualizationSized(50, 10, 40, view, colors.primary, colors)
		visualWidth := min(lipgloss.Width(visual), 50)
		gauge := ansi.Strip(renderResetGauge(50, visualWidth, meter.Window, time.Unix(reset, 0).Add(-30*time.Minute), "RESET T-00:30:00", colors.primary, colors))
		lines := strings.Split(gauge, "\n")
		if len(lines) != 2 {
			t.Errorf("%s reset gauge used %d lines, want label and bar: %q", view.name(), len(lines), gauge)
			continue
		}
		if strings.ContainsAny(lines[0], "█░") {
			t.Errorf("%s reset label shares its line with the bar: %q", view.name(), lines[0])
		}
		if got := lipgloss.Width(strings.TrimRight(lines[1], " ")); got != visualWidth {
			t.Errorf("%s reset bar width = %d, want main display width %d", view.name(), got, visualWidth)
		}
	}
}

func TestResetDeadlineUsesActualLocalWeekday(t *testing.T) {
	reset := time.Date(2026, time.August, 13, 9, 36, 35, 0, time.Local)
	if got := formatResetDeadline(reset); got != "THU 09:36:35" {
		t.Fatalf("reset deadline = %q, want THU 09:36:35", got)
	}
	duration := int64((7 * 24 * time.Hour) / time.Minute)
	meter := codex.Meter{
		Bucket: "codex",
		Name:   "1 WEEK",
		Window: codex.Window{WindowDurationMins: &duration, ResetsAt: ptr(reset.Unix())},
	}
	output := ansi.Strip(renderMeterArea(100, 18, meter, viewBars, paletteFor(themeHacker)))
	if !strings.Contains(output, "//  THU 09:36:35") {
		t.Fatalf("rendered reset deadline did not contain its actual weekday:\n%s", output)
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
		{"round half up", reset.Add(-30*time.Minute - 18*time.Second), 50},
		{"below rounding boundary", reset.Add(-30*time.Minute - 19*time.Second), 49},
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

func TestMonthlyCreditMeterShowsDetailsWithoutInventingCycleProgress(t *testing.T) {
	colors := paletteFor(themeHacker)
	reset := time.Now().Add(14 * 24 * time.Hour).Unix()
	meter := codex.Meter{
		Bucket: "codex", Name: "MONTHLY CREDIT LIMIT", Kind: codex.MeterIndividualLimit,
		Details: "8000 OF 25000 CREDITS USED",
		Window:  codex.Window{UsedPercent: 32, ResetsAt: &reset},
	}
	output := ansi.Strip(renderMeterArea(80, 18, meter, viewBars, colors))
	for _, want := range []string{"MONTHLY CREDIT LIMIT", "USED  32%", "8000 OF 25000 CREDITS USED", "CYCLE START UNAVAILABLE", "RESET T-"} {
		if !strings.Contains(output, want) {
			t.Errorf("monthly meter missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "MONTHLY CREDIT LIMIT LOOP") || strings.Contains(output, "RESET CYCLE   0%") {
		t.Fatalf("monthly meter invented a rolling cycle:\n%s", output)
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

func TestFuelTankPrioritizesFuelAtShortHeights(t *testing.T) {
	colors := paletteFor(themeHacker)
	for _, height := range []int{1, 2, 3} {
		output := ansi.Strip(renderFuelTankSized(30, height, 40, colors.primary, colors))
		if got := lipgloss.Height(output); got != height {
			t.Errorf("height %d rendered %d rows: %q", height, got, output)
		}
		if !strings.Contains(output, "▰") || !strings.Contains(output, "▱") {
			t.Errorf("height %d omitted the fuel tank row: %q", height, output)
		}
	}

	minimum := ansi.Strip(renderFuelTankSized(30, 1, 40, colors.primary, colors))
	for _, secondary := range []string{"RANGE", "E", "F"} {
		if strings.Contains(minimum, secondary) {
			t.Errorf("one-row tank retained secondary label %q: %q", secondary, minimum)
		}
	}
}

func TestShortFuelMeterDropsTelemetryBeforeTank(t *testing.T) {
	colors := paletteFor(themeHacker)
	duration := int64(60)
	reset := time.Now().Add(30 * time.Minute).Unix()
	meter := codex.Meter{
		Bucket: "codex",
		Name:   "1 HOUR",
		Window: codex.Window{UsedPercent: 40, WindowDurationMins: &duration, ResetsAt: &reset},
	}
	output := ansi.Strip(renderMeterArea(60, 3, meter, viewFuel, colors))
	if !strings.Contains(output, "▰") || !strings.Contains(output, "▱") {
		t.Fatalf("minimum-height fuel meter omitted its tank:\n%s", output)
	}
	for _, secondary := range []string{"FREE", "USED", "RANGE", "RESET CYCLE", "RESET T-"} {
		if strings.Contains(output, secondary) {
			t.Errorf("minimum-height fuel meter retained secondary telemetry %q:\n%s", secondary, output)
		}
	}
}

func TestFuelTankStatsFollowReverseGaugeDirection(t *testing.T) {
	colors := paletteFor(themeHacker)
	meter := codex.Meter{
		Bucket: "codex",
		Name:   "1 HOUR",
		Window: codex.Window{UsedPercent: 25},
	}
	rendered := renderMeter(60, meter, viewFuel, colors)
	wantFree := lipgloss.NewStyle().Bold(true).Foreground(colors.primary).Render("FREE  75%")
	wantUsed := colors.dimmed().Render("USED  25%")
	if !strings.Contains(rendered, wantFree) || !strings.Contains(rendered, wantUsed) {
		t.Fatalf("fuel stats do not highlight free and dim used: %q", rendered)
	}
	output := ansi.Strip(rendered)
	stats := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "FREE  75%") {
			stats = line
			break
		}
	}
	freeAt := strings.Index(stats, "FREE  75%")
	usedAt := strings.Index(stats, "USED  25%")
	if freeAt < 0 || usedAt < 0 || freeAt >= usedAt {
		t.Fatalf("fuel stats do not show free on the left and used on the right: %q", stats)
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
	small := renderVisualizationSized(42, 9, 62, viewPie, colors.primary, colors)
	large := renderVisualizationSized(70, 20, 62, viewPie, colors.primary, colors)
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
		if !strings.Contains(output, fmt.Sprintf("PIE  // %d%% USED", used)) {
			t.Fatalf("pie end state %d missing percentage: %q", used, output)
		}
	}
}

func TestPieResetRingUsesItsOwnClockwiseProgress(t *testing.T) {
	colors := paletteFor(themeHacker)
	const width, height = 18, 9 // No side legend; the canvas fills the allocation.
	for _, tc := range []struct {
		elapsed              int
		northEast, southEast imagecolor.Color
	}{
		{0, colors.dim, colors.dim},
		{25, colors.warning, colors.dim},
		{100, colors.warning, colors.warning},
	} {
		output := renderPieSized(width, height, 50, &tc.elapsed, colors.warning, colors)
		cells := uv.NewScreenBuffer(width, height)
		uv.NewStyledString(output).Draw(&cells, image.Rect(0, 0, width, height))
		for _, spot := range []struct {
			x, y int
			want imagecolor.Color
		}{
			{16, 3, tc.northEast}, {16, 5, tc.southEast}, // Outer band.
			{12, 5, colors.warning}, {5, 5, colors.dim}, // Pie remains 50% used.
		} {
			cell := cells.CellAt(spot.x, spot.y)
			if cell == nil || cell.Content == " " || cell.Style.Fg == nil {
				t.Fatalf("cycle %d: missing dot at %d,%d", tc.elapsed, spot.x, spot.y)
			}
			r, g, b, _ := cell.Style.Fg.RGBA()
			wr, wg, wb, _ := spot.want.RGBA()
			if r != wr || g != wg || b != wb {
				t.Errorf("cycle %d: wrong tone at %d,%d", tc.elapsed, spot.x, spot.y)
			}
		}
	}
}

func TestPieCycleHasTextButNoHorizontalBar(t *testing.T) {
	colors := paletteFor(themeHacker)
	duration, reset := int64(60), time.Now().Add(45*time.Minute).Unix()
	meter := codex.Meter{Bucket: "codex", Name: "1 HOUR", Window: codex.Window{
		UsedPercent: 50, WindowDurationMins: &duration, ResetsAt: &reset,
	}}
	for _, tc := range []struct {
		width, height int
		cycle, reset  string
	}{
		{80, 6, "RESET CYCLE  25%", "RESET T-"},
		{80, 22, "RING // 25% ELAPSED", "RESET T-"},
		{34, 16, "RING 25% ELAPSED", "T-"},
	} {
		got := ansi.Strip(renderMeterArea(tc.width, tc.height, meter, viewPie, colors))
		if strings.ContainsAny(got, "█░") || !strings.Contains(got, tc.cycle) || !strings.Contains(got, tc.reset) {
			t.Fatalf("%dx%d: expected cycle label and countdown without horizontal bar:\n%s", tc.width, tc.height, got)
		}
		if tc.width == 80 && tc.height == 22 && (!strings.Contains(got, "PIE  // 50% USED") || strings.Contains(got, "RESET CYCLE")) {
			t.Fatalf("wide Pie should label both shapes without repeating the cycle percentage:\n%s", got)
		}
	}
	meter.Window.WindowDurationMins = nil
	unknown := ansi.Strip(renderMeterArea(80, 18, meter, viewPie, colors))
	if !strings.Contains(unknown, "CYCLE START UNAVAILABLE // RESET T-") || strings.ContainsAny(unknown, "█░") {
		t.Fatalf("missing cycle start should not look like 0%% elapsed:\n%s", unknown)
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

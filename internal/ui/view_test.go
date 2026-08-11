package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func TestViewRendersEveryThemeAndStyleWithinStandardTerminal(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		for style := styleBars; style < styleCount; style++ {
			model := Model{
				snapshot:    codex.DemoSnapshot(),
				width:       120,
				height:      52,
				nextRefresh: time.Now().Add(time.Minute),
				theme:       theme,
				meterStyle:  style,
			}
			output := ansi.Strip(model.View())
			if !strings.Contains(output, paletteFor(theme).name) {
				t.Errorf("theme %d name missing from view", theme)
			}
			if !strings.Contains(output, style.name()) {
				t.Errorf("style %d name missing from view", style)
			}
			if style == styleStopwatch {
				if !strings.Contains(output, "SESSION READOUT") || !strings.Contains(output, "30 SEC LOCAL TOKEN BARS") {
					t.Errorf("stopwatch components missing for theme %d", theme)
				}
			} else if !strings.Contains(output, "5 HOURS LOOP") || !strings.Contains(output, "1 WEEK LOOP") {
				t.Errorf("quota windows missing for theme %d style %d", theme, style)
			}
			if lines := strings.Count(output, "\n") + 1; lines > 52 {
				t.Errorf("theme %d style %d used %d lines, want at most 52", theme, style, lines)
			}
		}
	}
}

func TestViewReflowsAcrossTerminalDimensions(t *testing.T) {
	sizes := []struct{ width, height int }{{80, 24}, {120, 40}, {200, 60}}
	for _, size := range sizes {
		for style := styleBars; style < styleCount; style++ {
			model := Model{
				snapshot:    codex.DemoSnapshot(),
				width:       size.width,
				height:      size.height,
				nextRefresh: time.Now().Add(time.Minute),
				meterStyle:  style,
			}
			output := model.View()
			if got := lipgloss.Width(output); got > size.width {
				t.Errorf("%s at %dx%d rendered width %d", style.name(), size.width, size.height, got)
			}
			if got := lipgloss.Height(output); got > size.height {
				t.Errorf("%s at %dx%d rendered height %d:\n%s", style.name(), size.width, size.height, got, ansi.Strip(output))
			}
		}
	}
}

func TestPieViewPlacesQuotaWindowsSideBySide(t *testing.T) {
	model := Model{
		snapshot:    codex.DemoSnapshot(),
		width:       80,
		height:      24,
		nextRefresh: time.Now().Add(time.Minute),
		meterStyle:  stylePie,
	}
	output := ansi.Strip(model.View())
	foundSharedRow := false
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "5 HOURS LOOP") && strings.Contains(line, "1 WEEK LOOP") {
			foundSharedRow = true
			break
		}
	}
	if !foundSharedRow {
		t.Fatalf("pie gauges were not side by side:\n%s", output)
	}
}

func TestGaugeGridStyleClassification(t *testing.T) {
	for style := styleBars; style < styleStopwatch; style++ {
		if got := usesMeterGrid(style); !got {
			t.Errorf("usesMeterGrid(%s) = false, want true", style.name())
		}
	}
	if usesMeterGrid(styleStopwatch) {
		t.Fatal("stopwatch should use its own full-width layout")
	}
}

func TestPieGridIncludesAdditionalGaugesResponsively(t *testing.T) {
	meters := codex.DemoSnapshot().Meters()
	meters = append(meters, codex.Meter{Bucket: "extra", Name: "1 DAY", Window: codex.Window{UsedPercent: 12}})
	output := ansi.Strip(renderMeterGrid(80, 20, meters, stylePie, paletteFor(themeBlueSteel)))
	if columns := meterGridColumns(80, 20, len(meters), stylePie); columns < 2 {
		t.Fatalf("three radial gauges used %d columns, want at least 2", columns)
	}
	for _, want := range []string{"5 HOURS LOOP", "1 WEEK LOOP", "EXTRA // 1 DAY"} {
		if !strings.Contains(output, want) {
			t.Fatalf("responsive pie grid missing %q:\n%s", want, output)
		}
	}
	if height := lipgloss.Height(output); height != 20 {
		t.Fatalf("responsive pie grid height = %d, want 20:\n%s", height, output)
	}
}

func TestGridRespondsToFourRateLimitsAndTerminalShape(t *testing.T) {
	meters := codex.DemoSnapshot().Meters()
	meters = append(meters,
		codex.Meter{Bucket: "extra", Name: "1 DAY", Window: codex.Window{UsedPercent: 12}},
		codex.Meter{Bucket: "extra", Name: "1 MONTH", Window: codex.Window{UsedPercent: 48}},
	)
	colors := paletteFor(themeUltraviolet)
	for _, style := range []meterStyleID{stylePie} {
		narrowColumns := meterGridColumns(100, 24, len(meters), style)
		wideColumns := meterGridColumns(300, 24, len(meters), style)
		if narrowColumns < 2 {
			t.Errorf("%s used %d narrow columns, want at least 2", style.name(), narrowColumns)
		}
		if wideColumns < narrowColumns {
			t.Errorf("%s shrank from %d to %d columns in a wider terminal", style.name(), narrowColumns, wideColumns)
		}
		output := ansi.Strip(renderMeterGrid(100, 24, meters, style, colors))
		if got := strings.Count(output, "USED"); got != 4 {
			t.Errorf("%s rendered %d of 4 rate limits:\n%s", style.name(), got, output)
		}
		if got := lipgloss.Height(output); got != 24 {
			t.Errorf("%s four-limit grid height = %d, want 24", style.name(), got)
		}
	}
}

func TestHorizontalStylesAlwaysFlowOneMeterPerRow(t *testing.T) {
	meters := codex.DemoSnapshot().Meters()
	for _, style := range []meterStyleID{styleBars, styleConsumptionPace, styleFuel} {
		if columns := meterGridColumns(160, 30, len(meters), style); columns != 1 {
			t.Fatalf("%s used %d columns, want one", style.name(), columns)
		}
		output := ansi.Strip(renderMeterGrid(160, 30, meters, style, paletteFor(themeHacker)))
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "5 HOURS LOOP") && strings.Contains(line, "1 WEEK LOOP") {
				t.Fatalf("%s meters appeared side by side:\n%s", style.name(), output)
			}
		}
	}
}

func TestMeterGridKeepsRowHeightsEqualAndPadsAboveFooter(t *testing.T) {
	meters := codex.DemoSnapshot().Meters()
	output := ansi.Strip(renderMeterGrid(80, 24, meters, styleBars, paletteFor(themeHacker)))
	lines := strings.Split(output, "\n")
	var tops, bottoms []int
	for index, line := range lines {
		if strings.Contains(line, "╭") {
			tops = append(tops, index)
		}
		if strings.Contains(line, "╰") {
			bottoms = append(bottoms, index)
		}
	}
	if len(tops) != 2 || len(bottoms) != 2 {
		t.Fatalf("could not identify two meter cards:\n%s", output)
	}
	firstHeight := bottoms[0] - tops[0] + 1
	secondHeight := bottoms[1] - tops[1] + 1
	if firstHeight != secondHeight {
		t.Fatalf("meter card heights differ: first=%d second=%d\n%s", firstHeight, secondHeight, output)
	}
	if got := lipgloss.Height(output); got != 24 {
		t.Fatalf("grid height = %d, want allocation 24", got)
	}
	if bottoms[1] >= len(lines)-1 {
		t.Fatalf("indivisible spare row was not left below meters:\n%s", output)
	}
}

func TestMeterGridUsesAllocatedHeightWithoutInternalOverflow(t *testing.T) {
	meters := codex.DemoSnapshot().Meters()
	for _, style := range []meterStyleID{stylePie, styleConsumptionPace} {
		output := ansi.Strip(renderMeterGrid(116, 24, meters, style, paletteFor(themeHacker)))
		height := strings.Count(output, "\n") + 1
		if height != 24 {
			t.Fatalf("%s grid is %d rows, want allocated height 24:\n%s", style.name(), height, output)
		}
	}
}

func TestRadialViewsPlaceQuotaWindowsSideBySide(t *testing.T) {
	for _, style := range []meterStyleID{stylePie} {
		model := Model{
			snapshot:    codex.DemoSnapshot(),
			width:       80,
			height:      24,
			nextRefresh: time.Now().Add(time.Minute),
			meterStyle:  style,
		}
		output := ansi.Strip(model.View())
		foundSharedRow := false
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "5 HOURS LOOP") && strings.Contains(line, "1 WEEK LOOP") {
				foundSharedRow = true
				break
			}
		}
		if !foundSharedRow {
			t.Errorf("%s gauges were not side by side:\n%s", style.name(), output)
		}
	}
}

func TestViewCoversBootCompactErrorAndEmptyStates(t *testing.T) {
	boot := ansi.Strip((Model{width: 50, loading: true, theme: themeRust}).View())
	if !strings.Contains(boot, "▰ CODEXOMETER ▰") || !strings.Contains(boot, "ACQUIRING SIGNAL") {
		t.Fatalf("compact boot view missing content: %q", boot)
	}

	stale := Model{
		snapshot:    codex.DemoSnapshot(),
		err:         errors.New("connection lost"),
		width:       80,
		nextRefresh: time.Now().Add(time.Minute),
		theme:       themeBlueSteel,
	}
	staleOutput := ansi.Strip(stale.View())
	if !strings.Contains(staleOutput, "STALE SIGNAL") || !strings.Contains(staleOutput, "connection lost") {
		t.Fatalf("stale view missing error: %q", staleOutput)
	}

	empty := ansi.Strip((Model{width: 80}).View())
	if !strings.Contains(empty, "no quota windows returned") {
		t.Fatalf("empty view missing diagnostic: %q", empty)
	}
}

func TestRenderMeterHandlesAdditionalBucketAndMissingReset(t *testing.T) {
	colors := paletteFor(themeUltraviolet)
	meter := codex.Meter{
		Bucket: "codex_other",
		Name:   "30 MINUTES",
		Window: codex.Window{UsedPercent: 75},
	}
	output := ansi.Strip(renderMeter(70, meter, styleFuel, colors))
	for _, want := range []string{"CODEX OTHER // 30 MINUTES LOOP", "USED  75%", "RESET DATA UNAVAILABLE", "RANGE  25%"} {
		if !strings.Contains(output, want) {
			t.Errorf("rendered meter missing %q: %q", want, output)
		}
	}
}

func TestMeterColorsCoverThresholds(t *testing.T) {
	colors := paletteFor(themeHacker)
	if got := meterColor(69, colors); got != colors.primary {
		t.Fatalf("low usage color = %q", got)
	}
	if got := meterColor(70, colors); got != colors.warning {
		t.Fatalf("warning usage color = %q", got)
	}
	if got := meterColor(90, colors); got != colors.danger {
		t.Fatalf("danger usage color = %q", got)
	}
}

func TestPalettesAndFallback(t *testing.T) {
	wants := []string{"HACKER", "RUST", "BLUE STEEL", "ULTRAVIOLET", "NIGHTSHADE"}
	for theme, want := range wants {
		colors := paletteFor(themeID(theme))
		if colors.name != want || colors.primary == "" || colors.dim == "" || colors.accent == "" || colors.background == "" {
			t.Fatalf("invalid palette %d: %#v", theme, colors)
		}
		if colors.header().GetForeground() != colors.primary || colors.label().GetForeground() != colors.accent {
			t.Fatalf("palette styles do not use palette colors: %#v", colors)
		}
	}
	if got := paletteFor(themeID(999)); got.name != "HACKER" {
		t.Fatalf("invalid theme did not fall back: %#v", got)
	}
}

func TestTimeFormatting(t *testing.T) {
	now := time.Unix(10_000, 0)
	if got := countdownFrom(now, now.Add(-time.Second)); got != "00:00:00" {
		t.Fatalf("past countdown = %q", got)
	}
	if got := countdownFrom(now, now.Add(125*time.Hour)); got != "5D 05:00" {
		t.Fatalf("long countdown = %q", got)
	}
	if got := countdownFrom(now, now.Add(time.Hour+2*time.Minute+3*time.Second)); got != "01:02:03" {
		t.Fatalf("short countdown = %q", got)
	}
	if got := compactDuration(60*time.Second + time.Nanosecond); got != "01:01" {
		t.Fatalf("compactDuration = %q", got)
	}
	if got := compactDuration(-time.Second); got != "00:00" {
		t.Fatalf("negative compactDuration = %q", got)
	}
}

func TestVisualizationClampsAndFallbacks(t *testing.T) {
	colors := paletteFor(themeHacker)
	if output := ansi.Strip(renderVisualization(20, 0, meterStyleID(999), colors.primary, colors)); !strings.Contains(output, "░") {
		t.Fatalf("unknown style did not fall back to bars: %q", output)
	}
	if output := ansi.Strip(renderPie(20, 100, colors.primary, colors)); !strings.Contains(output, "100%") {
		t.Fatalf("full pie missing percentage: %q", output)
	}
}

package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func TestViewRendersEveryThemeAndViewWithinStandardTerminal(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		for view := viewBars; view < viewCount; view++ {
			model := Model{
				snapshot:    codex.DemoSnapshot(),
				width:       120,
				height:      52,
				nextRefresh: time.Now().Add(time.Minute),
				theme:       theme,
				meterView:   view,
			}
			output := ansi.Strip(model.render())
			if !strings.Contains(output, paletteFor(theme).name) {
				t.Errorf("theme %d name missing from view", theme)
			}
			if !strings.Contains(output, view.name()) {
				t.Errorf("view %d name missing from view", view)
			}
			if view == viewMonitor {
				if !strings.Contains(output, "MONITOR READOUT") || !strings.Contains(output, "30 SEC LOCAL TOKEN BARS") {
					t.Errorf("monitor components missing for theme %d", theme)
				}
			} else if view == viewBenchmark {
				if !strings.Contains(output, "ALGORITHM TRIAL") || !strings.Contains(output, "RESULT MATRIX") {
					t.Errorf("benchmark components missing for theme %d", theme)
				}
			} else if !strings.Contains(output, "5 HOURS LOOP") || !strings.Contains(output, "1 WEEK LOOP") {
				t.Errorf("quota windows missing for theme %d view %d", theme, view)
			}
			if lines := strings.Count(output, "\n") + 1; lines > 52 {
				t.Errorf("theme %d view %d used %d lines, want at most 52", theme, view, lines)
			}
		}
	}
}

func TestViewDeclaresTerminalModes(t *testing.T) {
	model := Model{snapshot: codex.DemoSnapshot(), width: 80, height: 24}
	view := model.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeAllMotion {
		t.Fatalf("default view modes = alt:%v mouse:%v", view.AltScreen, view.MouseMode)
	}

	model.SetInline(true)
	view = model.View()
	if view.AltScreen || view.MouseMode != tea.MouseModeAllMotion {
		t.Fatalf("inline view modes = alt:%v mouse:%v", view.AltScreen, view.MouseMode)
	}
}

func TestViewReflowsAcrossTerminalDimensions(t *testing.T) {
	sizes := []struct{ width, height int }{{40, 16}, {40, 24}, {45, 24}, {60, 24}, {80, 24}, {120, 40}, {200, 60}}
	for _, size := range sizes {
		for view := viewBars; view < viewCount; view++ {
			model := Model{
				snapshot:    codex.DemoSnapshot(),
				width:       size.width,
				height:      size.height,
				nextRefresh: time.Now().Add(time.Minute),
				meterView:   view,
			}
			output := model.render()
			if got := lipgloss.Width(output); got > size.width {
				t.Errorf("%s at %dx%d rendered width %d", view.name(), size.width, size.height, got)
			}
			if got := lipgloss.Height(output); got > size.height {
				t.Errorf("%s at %dx%d rendered height %d:\n%s", view.name(), size.width, size.height, got, ansi.Strip(output))
			}
		}
	}
}

func TestQuotaViewsAccommodateFiveHourAdditionalAndMonthlyLimits(t *testing.T) {
	now := time.Now()
	window := func(used int, durationMinutes int64) *codex.Window {
		reset := now.Add(time.Duration(durationMinutes/2) * time.Minute).Unix()
		return &codex.Window{UsedPercent: used, WindowDurationMins: &durationMinutes, ResetsAt: &reset}
	}
	snapshot := codex.Snapshot{RateLimitsByLimitID: map[string]codex.RateLimitSnapshot{
		"codex": {
			Primary:         window(35, 300),
			Secondary:       window(48, 10_080),
			IndividualLimit: &codex.IndividualLimit{Limit: "25000", Used: "8000", RemainingPercent: 68, ResetsAt: now.Add(14 * 24 * time.Hour).Unix()},
		},
		"spark": {
			LimitName: ptr("spark"),
			Primary:   window(20, 60),
			Secondary: window(70, 1_440),
		},
	}}
	if meters := snapshot.Meters(); len(meters) != 5 || meters[0].Name != "5 HOURS" || meters[2].Name != "MONTHLY CREDIT LIMIT" {
		t.Fatalf("restored snapshot produced meters %#v", meters)
	}

	for _, size := range []struct{ width, height int }{{40, 24}, {80, 24}, {120, 40}} {
		for _, view := range quotaMeterViews {
			model := Model{
				snapshot: snapshot, width: size.width, height: size.height,
				nextRefresh: now.Add(time.Minute), meterView: view,
			}
			output := ansi.Strip(model.render())
			if got := lipgloss.Width(output); got > size.width {
				t.Errorf("%s with five limits at %dx%d rendered width %d", view.name(), size.width, size.height, got)
			}
			if got := lipgloss.Height(output); got > size.height {
				t.Errorf("%s with five limits at %dx%d rendered height %d:\n%s", view.name(), size.width, size.height, got, output)
			}
			if size.width >= 80 {
				for _, title := range []string{"5 HOURS LOOP", "1 WEEK LOOP", "MONTHLY CREDIT LIMIT", "SPARK // 1 HOUR LOOP", "SPARK // 1 DAY LOOP"} {
					if !strings.Contains(output, title) {
						t.Errorf("%s at %dx%d omitted %q:\n%s", view.name(), size.width, size.height, title, output)
					}
				}
			}
		}
	}
}

func TestStatusAndFooterKeepOnlyEssentialMetadata(t *testing.T) {
	model := Model{
		snapshot:    codex.DemoSnapshot(),
		nextRefresh: time.Now().Add(time.Minute),
		meterView:   viewBars,
		appVersion:  "1.2.3-dev+abc123.dirty",
	}
	colors := paletteFor(themeHacker)
	account := ansi.Strip(model.renderAccount(colors))
	if account != "ACCOUNT // PLUS" {
		t.Fatalf("account metadata = %q", account)
	}
	header := ansi.Strip(renderHeader(100, 0, model.renderSignalStatus(100, colors), model.renderAccount(colors), model.appVersion, colors))
	headerLines := strings.Split(header, "\n")
	firstLine := headerLines[0]
	status := ansi.Strip(model.renderSignalStatus(100, colors))
	if !strings.HasSuffix(firstLine, status) || !strings.Contains(status, "● ONLINE // 5 HOURS // ") {
		t.Fatalf("online status is not at the top right of the header: %q", firstLine)
	}
	if !strings.HasSuffix(headerLines[len(headerLines)-1], "ACCOUNT // PLUS") {
		t.Fatalf("account is not aligned with the subtitle: %q", headerLines[len(headerLines)-1])
	}
	if !strings.Contains(headerLines[len(headerLines)-1], "VERSION 1.2.3-DEV+ABC123.DIRTY") {
		t.Fatalf("resolved build version is absent from the masthead: %q", headerLines[len(headerLines)-1])
	}
	footer := ansi.Strip(model.renderFooter(100, colors))
	if !strings.Contains(footer, "THEME // HACKER") || strings.Contains(footer, "VIEW") {
		t.Fatalf("footer theme readout = %q", footer)
	}
	footerLines := strings.Split(footer, "\n")
	if !strings.HasSuffix(footerLines[len(footerLines)-1], "THEME // HACKER") {
		t.Fatalf("theme is not at the bottom right of the footer: %q", footerLines[len(footerLines)-1])
	}
}

func TestFooterShowsAvailableAccountCredits(t *testing.T) {
	balance := "17000"
	snapshot := codex.DemoSnapshot()
	snapshot.RateLimits.Credits = &codex.Credits{HasCredits: true, Balance: &balance}
	model := Model{snapshot: snapshot, nextRefresh: time.Now().Add(time.Minute), meterView: viewBars}
	footer := ansi.Strip(model.renderFooter(100, paletteFor(themeHacker)))
	if !strings.Contains(footer, "CREDITS 17000") {
		t.Fatalf("account credit balance missing from footer: %q", footer)
	}
}

func TestPricingFooterCentersProvenanceOnEveryPricedView(t *testing.T) {
	colors := paletteFor(themeHacker)
	model := Model{nextRefresh: time.Now().Add(time.Minute)}
	pricedViews := append([]meterViewID{viewBenchmark}, quotaViewOrder[:]...)
	for _, view := range pricedViews {
		model.meterView = view
		raw := model.renderFooter(100, colors)
		if !strings.Contains(raw, codex.StandardAPIPricingSourceURL) {
			t.Errorf("%s footer does not contain pricing hyperlink: %q", view.name(), raw)
			continue
		}
		firstLine := strings.Split(ansi.Strip(raw), "\n")[0]
		label := "PRICES RETRIEVED " + codex.StandardAPIPricingRetrievedOn + " // OPENAI.COM"
		start := strings.Index(firstLine, label)
		if start < 0 {
			t.Errorf("%s footer does not contain pricing provenance: %q", view.name(), firstLine)
			continue
		}
		center := start + len(label)/2
		if center < 49 || center > 51 {
			t.Errorf("%s pricing provenance center = %d, want terminal center 50: %q", view.name(), center, firstLine)
		}
	}

	for _, view := range []meterViewID{viewMonitor} {
		model.meterView = view
		footer := model.renderFooter(100, colors)
		if strings.Contains(footer, codex.StandardAPIPricingSourceURL) || strings.Contains(ansi.Strip(footer), codex.StandardAPIPricingRetrievedOn) {
			t.Fatalf("pricing provenance leaked into %s footer: %q", view.name(), footer)
		}
	}
}

func TestPricingFooterRemainsResponsiveAcrossPricedViews(t *testing.T) {
	model := Model{nextRefresh: time.Now().Add(time.Minute)}
	pricedViews := append([]meterViewID{viewBenchmark}, quotaViewOrder[:]...)
	for _, view := range pricedViews {
		model.meterView = view
		for _, width := range []int{12, 24, 40, 60, 79, 80, 100} {
			footer := model.renderFooter(width, paletteFor(themeHacker))
			for line, value := range strings.Split(footer, "\n") {
				if got := lipgloss.Width(value); got > width {
					t.Errorf("%s width %d line %d rendered at %d: %q", view.name(), width, line, got, ansi.Strip(value))
				}
			}
			hasPricing := strings.Contains(footer, codex.StandardAPIPricingSourceURL)
			if width < 80 && hasPricing {
				t.Errorf("%s width %d unexpectedly showed pricing provenance", view.name(), width)
			}
			if width >= 80 && !hasPricing {
				t.Errorf("%s width %d omitted responsive pricing provenance", view.name(), width)
			}
		}
	}
}

func TestFrameTitlesAreIntegratedIntoBorderInsteadOfButtonBrackets(t *testing.T) {
	colors := paletteFor(themeHacker)
	output := ansi.Strip(frameSized(36, 3, "SESSION READOUT", "BODY", colors.primary, colors))
	lines := strings.Split(output, "\n")
	if len(lines) != 5 {
		t.Fatalf("frame height = %d, want 5:\n%s", len(lines), output)
	}
	if !strings.HasPrefix(lines[0], "╭─ SESSION READOUT ") || !strings.HasSuffix(lines[0], "─╮") {
		t.Fatalf("title was not integrated into the top border: %q", lines[0])
	}
	if strings.HasSuffix(lines[0], " ╮") {
		t.Fatalf("title frame left a gap before the top-right corner: %q", lines[0])
	}
	if strings.Contains(output, "[ SESSION READOUT ]") {
		t.Fatalf("frame title still looks like a button:\n%s", output)
	}
	for index, line := range lines {
		if width := lipgloss.Width(line); width != 36 {
			t.Errorf("line %d width = %d, want 36: %q", index, width, line)
		}
	}

	narrow := ansi.Strip(frameSized(5, 1, "TITLE TOO LONG", "X", colors.primary, colors))
	for index, line := range strings.Split(narrow, "\n") {
		if width := lipgloss.Width(line); width != 5 {
			t.Errorf("narrow line %d width = %d, want 5: %q", index, width, line)
		}
	}

	const closeAction = "[ (X) CLOSE ]"
	withAction := ansi.Strip(frameSizedWithTitleAction(48, 3, "RUN DETAIL", closeAction, "BODY", colors.primary, colors))
	actionLines := strings.Split(withAction, "\n")
	if !strings.HasSuffix(actionLines[0], closeAction+"─╮") {
		t.Fatalf("title action did not reconnect to the top-right corner: %q", actionLines[0])
	}
	for index, line := range actionLines {
		if width := lipgloss.Width(line); width != 48 {
			t.Errorf("action frame line %d width = %d, want 48: %q", index, width, line)
		}
	}
}

func TestPieViewPlacesQuotaWindowsSideBySide(t *testing.T) {
	model := Model{
		snapshot:    codex.DemoSnapshot(),
		width:       80,
		height:      24,
		nextRefresh: time.Now().Add(time.Minute),
		meterView:   viewPie,
	}
	output := ansi.Strip(model.render())
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

func TestGaugeGridViewClassification(t *testing.T) {
	for _, view := range []meterViewID{viewBars, viewPie, viewConsumptionPace, viewFuel} {
		if got := usesMeterGrid(view); !got {
			t.Errorf("usesMeterGrid(%s) = false, want true", view.name())
		}
	}
	if usesMeterGrid(viewMonitor) {
		t.Fatal("monitor should use its own full-width layout")
	}
	if usesMeterGrid(viewBenchmark) {
		t.Fatal("benchmark should use its own full-width layout")
	}
}

func TestPieGridIncludesAdditionalGaugesResponsively(t *testing.T) {
	meters := codex.DemoSnapshot().Meters()
	meters = append(meters, codex.Meter{Bucket: "extra", Name: "1 DAY", Window: codex.Window{UsedPercent: 12}})
	output := ansi.Strip(renderMeterGrid(80, 20, meters, viewPie, paletteFor(themeBlueSteel)))
	if columns := meterGridColumns(80, 20, len(meters), viewPie); columns < 2 {
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
	for _, view := range []meterViewID{viewPie} {
		narrowColumns := meterGridColumns(100, 24, len(meters), view)
		wideColumns := meterGridColumns(300, 24, len(meters), view)
		if narrowColumns < 2 {
			t.Errorf("%s used %d narrow columns, want at least 2", view.name(), narrowColumns)
		}
		if wideColumns < narrowColumns {
			t.Errorf("%s shrank from %d to %d columns in a wider terminal", view.name(), narrowColumns, wideColumns)
		}
		output := ansi.Strip(renderMeterGrid(100, 24, meters, view, colors))
		if got := strings.Count(output, "USED"); got != 4 {
			t.Errorf("%s rendered %d of 4 rate limits:\n%s", view.name(), got, output)
		}
		if got := lipgloss.Height(output); got != 24 {
			t.Errorf("%s four-limit grid height = %d, want 24", view.name(), got)
		}
	}
}

func TestHorizontalStylesAlwaysFlowOneMeterPerRow(t *testing.T) {
	meters := codex.DemoSnapshot().Meters()
	for _, view := range []meterViewID{viewBars, viewConsumptionPace, viewFuel} {
		if columns := meterGridColumns(160, 30, len(meters), view); columns != 1 {
			t.Fatalf("%s used %d columns, want one", view.name(), columns)
		}
		output := ansi.Strip(renderMeterGrid(160, 30, meters, view, paletteFor(themeHacker)))
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "5 HOURS LOOP") && strings.Contains(line, "1 WEEK LOOP") {
				t.Fatalf("%s meters appeared side by side:\n%s", view.name(), output)
			}
		}
	}
}

func TestMeterGridKeepsRowHeightsEqualAndPadsAboveFooter(t *testing.T) {
	meters := codex.DemoSnapshot().Meters()
	output := ansi.Strip(renderMeterGrid(80, 24, meters, viewBars, paletteFor(themeHacker)))
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
	for _, view := range []meterViewID{viewPie, viewConsumptionPace} {
		output := ansi.Strip(renderMeterGrid(116, 24, meters, view, paletteFor(themeHacker)))
		height := strings.Count(output, "\n") + 1
		if height != 24 {
			t.Fatalf("%s grid is %d rows, want allocated height 24:\n%s", view.name(), height, output)
		}
	}
}

func TestRadialViewsPlaceQuotaWindowsSideBySide(t *testing.T) {
	for _, view := range []meterViewID{viewPie} {
		model := Model{
			snapshot:    codex.DemoSnapshot(),
			width:       80,
			height:      24,
			nextRefresh: time.Now().Add(time.Minute),
			meterView:   view,
		}
		output := ansi.Strip(model.render())
		foundSharedRow := false
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "5 HOURS LOOP") && strings.Contains(line, "1 WEEK LOOP") {
				foundSharedRow = true
				break
			}
		}
		if !foundSharedRow {
			t.Errorf("%s gauges were not side by side:\n%s", view.name(), output)
		}
	}
}

func TestViewCoversBootCompactErrorAndEmptyStates(t *testing.T) {
	boot := ansi.Strip((Model{width: 50, loading: true, theme: themeRust}).render())
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
	staleOutput := ansi.Strip(stale.render())
	if !strings.Contains(staleOutput, "STALE SIGNAL") || !strings.Contains(staleOutput, "connection lost") {
		t.Fatalf("stale view missing error: %q", staleOutput)
	}

	empty := ansi.Strip((Model{width: 80}).render())
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
	output := ansi.Strip(renderMeter(70, meter, viewFuel, colors))
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
		if colors.name != want || colors.primary == nil || colors.dim == nil || colors.accent == nil || colors.background == nil {
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
	if output := ansi.Strip(renderVisualization(20, 0, meterViewID(999), colors.primary, colors)); !strings.Contains(output, "░") {
		t.Fatalf("unknown view did not fall back to bars: %q", output)
	}
	if output := ansi.Strip(renderPie(20, 100, colors.primary, colors)); !strings.Contains(output, "100%") {
		t.Fatalf("full pie missing percentage: %q", output)
	}
}

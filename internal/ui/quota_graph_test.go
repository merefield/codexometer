package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/quotagraph"
)

func graphWindow(now time.Time, used int) codex.Window {
	duration, reset := int64(100), now.Add(50*time.Minute).Unix()
	return codex.Window{UsedPercent: used, WindowDurationMins: &duration, ResetsAt: &reset}
}

func TestQuotaPlotsFitResponsiveRectangles(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, width := range []int{1, 9, 17, 18, 30, 80, 140} {
		for _, height := range []int{1, 2, 5, 6, 12, 30} {
			for _, used := range []int{0, 25, 75, 100} {
				out := renderQuotaPlot(width, height, graphWindow(now, used), now, quotaPlotOptions{}, paletteFor(themeHacker))
				if lipgloss.Width(out) > width || lipgloss.Height(out) != height {
					t.Fatalf("%dx%d used=%d rendered %dx%d", width, height, used, lipgloss.Width(out), lipgloss.Height(out))
				}
				if width >= 18 && height >= 6 && !strings.Contains(out, "●") {
					t.Fatal("current dot missing")
				}
			}
		}
	}
}

func TestQuotaPlotAxesAndProjection(t *testing.T) {
	now := time.Unix(1800000000, 0)
	w := graphWindow(now, 25)
	pace := ansi.Strip(renderQuotaPlot(60, 15, w, now, quotaPlotOptions{}, paletteFor(themeHacker)))
	for _, want := range []string{"100%", "TIME ELAPSED", "25% USED // 50.0% TIME", "50.0% AT RESET"} {
		if !strings.Contains(pace, want) {
			t.Fatalf("Pace missing %q:\n%s", want, pace)
		}
	}
	for _, removed := range []string{"+100", "-100", "PP FROM SAFETY", "PP AT RESET", "DISTANCE FROM SAFETY"} {
		if strings.Contains(pace, removed) {
			t.Fatalf("removed graph transform still present: %q", removed)
		}
	}
	if strings.ContainsAny(pace, "↗↘▸") {
		t.Fatal("terminal graph still contains trend arrows")
	}
	off := ansi.Strip(renderQuotaPlot(60, 15, w, now, quotaPlotOptions{mode: quotagraph.Off}, paletteFor(themeHacker)))
	if strings.Join(strings.Split(pace, "\n")[1:12], "\n") == strings.Join(strings.Split(off, "\n")[1:12], "\n") {
		t.Fatal("removing arrows also removed the trend line")
	}
	if strings.Contains(renderQuotaPlot(60, 15, w, now, quotaPlotOptions{mode: quotagraph.Off}, paletteFor(themeHacker)), "AT RESET") {
		t.Fatal("Off still rendered projection")
	}
}

func TestQuotaUnavailableTrendIsMuted(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for theme := themeHacker; theme < themeCount; theme++ {
		colors := paletteFor(theme)
		for _, options := range []quotaPlotOptions{{mode: quotagraph.HalfHour}, {mode: quotagraph.Hour}, {mode: quotagraph.Day}} {
			out := renderQuotaPlot(60, 15, graphWindow(now, 25), now, options, colors)
			if got, want := strings.Split(out, "\n")[14], colors.dimmed().Render("TREND UNAVAILABLE"); got != want {
				t.Fatalf("theme=%v mode=%v unavailable trend is not muted: %q", theme, options.mode, got)
			}
		}
		for _, test := range []struct {
			used      int
			ink       string
			projected float64
		}{
			{25, "#45DB79", 50},
			{50, "#45DB79", 100},
			{75, "#9D2537", 150},
		} {
			out := renderQuotaPlot(60, 15, graphWindow(now, test.used), now, quotaPlotOptions{}, colors)
			text := fmt.Sprintf("TREND // %.1f%% AT RESET", test.projected)
			want := lipgloss.NewStyle().Foreground(lipgloss.Color(test.ink)).Render(text)
			if got := strings.Split(out, "\n")[14]; got != want {
				t.Fatalf("valid projection lost its endpoint colour: %q", got)
			}
		}
	}
}

func TestQuotaLinesUseDenseBrailleWithoutDashGaps(t *testing.T) {
	now := time.Unix(1800000000, 0)
	w := graphWindow(now, 75)
	const width, height = 60, 15
	out := ansi.Strip(renderQuotaPlot(width, height, w, now, quotaPlotOptions{hideTrace: true}, paletteFor(themeHacker)))
	if strings.ContainsAny(out, "╭╮╰╯↗↘▸") {
		t.Fatal("dense Braille graph contains box-drawing elbows or arrows")
	}
	trend, ok := quotagraph.Project(quotagraph.WindowStart, w, nil, now)
	if !ok {
		t.Fatal("missing test projection")
	}
	segment, ok := trend.Segment()
	if !ok {
		t.Fatal("missing test segment")
	}
	canvas := quotaCanvas{width: width - 6, height: height - 4, cells: make([]quotaPlotCell, (width-6)*(height-4))}
	guideEnd := 100.0
	canvas.line(0, 0, 100, guideEnd, nil, 1, 0)
	canvas.line(segment.X1, segment.Y1, segment.X2, segment.Y2, nil, 3, 0)
	elapsed, _ := quotagraph.Elapsed(w, now)
	y := float64(w.UsedPercent)
	canvas.mark(elapsed, y, '●', nil, 4)
	lines := strings.Split(out, "\n")
	for row := 0; row < canvas.height; row++ {
		runes := []rune(lines[row+1])
		for col := 0; col < canvas.width; col++ {
			cell := canvas.cells[row*canvas.width+col]
			if cell.priority > 0 && cell.priority < 4 && runes[col+5] != rune(0x2800+cell.mask) {
				t.Fatalf("line priority=%d has a gap at %d,%d", cell.priority, col, row)
			}
		}
	}
}

func TestQuotaCanvasPreservesDotAndDirection(t *testing.T) {
	c := quotaCanvas{width: 20, height: 10, cells: make([]quotaPlotCell, 200)}
	_, above := c.position(50, 75)
	_, below := c.position(50, 25)
	if above >= below {
		t.Fatal("greater consumption must appear above safety")
	}
	c.mark(50, 50, '●', nil, 4)
	c.line(0, 0, 100, 100, nil, 3, 0)
	px, py := c.position(50, 50)
	if c.cells[(py/4)*c.width+px/2].symbol != '●' {
		t.Fatal("trend overwrote the current dot")
	}
	if quotaFieldColor(1) == quotaFieldColor(-1) {
		t.Fatal("danger and safety backgrounds match")
	}
}

func TestQuotaGraphButtonsTrackRenderedSurfaces(t *testing.T) {
	for _, view := range []meterViewID{viewPace} {
		for _, width := range []int{24, 40, 80, 120} {
			m := Model{snapshot: codex.DemoSnapshot(), meterView: view, width: width, height: 30}
			layout := m.dashboardLayout()
			lines := strings.Split(ansi.Strip(m.render()), "\n")
			y, x := layout.quotaTabsY+1, 2
			buttons := m.quotaGraphButtons(layout.contentWidth)
			for _, b := range buttons {
				if !strings.Contains(lines[y], b.label) {
					t.Fatalf("button %q missing from hit-test row %d", b.label, y)
				}
				for col := x; col < x+lipgloss.Width(b.label); col++ {
					if m.footerButtonAt(col, y) != b.id {
						t.Fatalf("button %q missed at %d,%d", b.label, col, y)
					}
				}
				if m.footerButtonAt(x, y+1) != footerButtonNone {
					t.Fatal("graph body is a control surface")
				}
				x += lipgloss.Width(b.label)
				if m.footerButtonAt(x, y) != footerButtonNone {
					t.Fatal("button padding is clickable")
				}
				x++
			}
			mouse := tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft}
			updated, _ := m.Update(mouse)
			m = updated.(Model)
			if m.hoveredButton != footerButtonQuotaTrend {
				t.Fatal("trend hover missing")
			}
			updated, cmd := m.Update(tea.MouseClickMsg(mouse))
			m = updated.(Model)
			if cmd == nil || m.flashedButton != footerButtonQuotaTrend || m.quotaGraphMode != quotagraph.Off {
				t.Fatal("trend did not skip unavailable observation intervals or flash")
			}
			updated, _ = m.Update(key('h'))
			if !updated.(Model).quotaGraphHideTrace {
				t.Fatal("H did not toggle trace")
			}
		}
	}
	for _, view := range []meterViewID{viewBars, viewPie, viewFuel, viewUsage, viewBenchmark} {
		m := Model{meterView: view}
		updated, _ := m.Update(key('g'))
		if updated.(Model).quotaGraphMode != quotagraph.WindowStart {
			t.Fatal("G leaked into another view")
		}
		updated, _ = m.Update(key('h'))
		if updated.(Model).quotaGraphHideTrace {
			t.Fatal("H leaked into another view")
		}
	}
}

func TestQuotaGraphRefreshCollectionIsIndependentOfSelectedTab(t *testing.T) {
	now := time.Unix(1800000000, 0)
	w := graphWindow(now, 20)
	q := codex.Snapshot{AccountFingerprint: "A", RateLimits: codex.RateLimitSnapshot{Primary: &w}}
	m := Model{meterView: viewMonitor}
	updated, _ := m.Update(fetchedMsg{snapshot: q, at: now})
	m = updated.(Model)
	updated, _ = m.Update(fetchedMsg{snapshot: q, at: now.Add(time.Minute), err: errors.New("offline")})
	m = updated.(Model)
	updated, _ = m.Update(fetchedMsg{snapshot: q, at: now.Add(2 * time.Minute)})
	m = updated.(Model)
	points := m.quotaPlotOptions()[0].points
	if len(points) != 2 || !points[1].Break {
		t.Fatal("quota observations were not retained while viewing Sessions")
	}
	m.resetRevision++
	updated, _ = m.Update(fetchedMsg{snapshot: q, at: now.Add(3 * time.Minute)})
	if len(updated.(Model).quotaPlotOptions()[0].points) != 2 {
		t.Fatal("pre-reset result changed the trace")
	}
}

func TestQuotaGraphPreferencesRestoreFormerZoneAsPace(t *testing.T) {
	for saved, want := range map[string]meterViewID{"consumption-pace": viewPace, "pace": viewPace, "zone": viewPace} {
		store := &memoryPreferenceStore{}
		m := Model{preferenceStore: store}
		m.applyPreferences(Preferences{QuotaView: saved})
		if m.meterView != want || m.quotaMeterView != want || quotaViewPreferenceNames[want] != "pace" {
			t.Fatalf("saved %q restored %v", saved, m.meterView)
		}
		m.persistPreferences()
		if store.preferences.QuotaView != "pace" {
			t.Fatalf("saved alias %q was not canonicalised", saved)
		}
	}
}

func TestQuotaGraphsUseMultipleColumnsOnlyWhenReadable(t *testing.T) {
	for _, view := range []meterViewID{viewPace} {
		if meterGridColumns(36, 24, 4, view) != 1 || meterGridColumns(80, 24, 4, view) != 2 || meterGridColumns(160, 30, 4, view) != 4 {
			t.Fatal("graph grid does not adapt to available width and window count")
		}
	}
}

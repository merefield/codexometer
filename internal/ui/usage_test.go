package ui

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestHistoryPeriods(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	data := codex.AccountUsage{DailyUsageBuckets: []codex.AccountUsageDay{
		{StartDate: "2026-08-29", Tokens: 10},
		{StartDate: "2026-08-30", Tokens: 20},
		{StartDate: "2026-08-30", Tokens: 5},
		{StartDate: "2026-09-05", Tokens: 30},
		{StartDate: "2026-09-06", Tokens: 999},
		{StartDate: "2026-09-01", Tokens: -999},
		{StartDate: "2020-01-01", Tokens: 999},
		{StartDate: "invalid", Tokens: 999},
	}}
	daily := historyPoints(data, now, 0)
	weekly := historyPoints(data, now, 1)
	cumulative := historyPoints(data, now, 2)
	if len(daily) != 364 || len(weekly) != 52 || daily[357].tokens != 25 || daily[363].tokens != 30 || weekly[50].tokens != 10 || weekly[51].tokens != 55 || cumulative[51].tokens != 65 {
		t.Fatalf("incorrect grouping: daily tail=%v weekly tail=%v cumulative=%v", daily[356:], weekly[50:], cumulative[51])
	}
	data.DailyUsageBuckets = []codex.AccountUsageDay{{StartDate: "2026-09-05", Tokens: math.MaxInt64}, {StartDate: "2026-09-05", Tokens: 1}}
	if got := historyPoints(data, now, 2)[51].tokens; got != math.MaxInt64 {
		t.Fatalf("overflow: %d", got)
	}
	// The same instant in a different timezone must preserve UTC bucketing.
	if got := historyPoints(data, now.In(time.FixedZone("west", -18*3600)), 0); len(got) != 364 {
		t.Fatal("dates depend on local timezone")
	}
}

type historyStub struct {
	stubFetcher
	data codex.AccountUsage
	err  error
}

func (s historyStub) FetchAccountUsage(context.Context) (codex.AccountUsage, error) {
	return s.data, s.err
}

func TestHistoryRequestLifecycle(t *testing.T) {
	data := codex.AccountUsage{AccountFingerprint: "a", FetchedAt: time.Now(), DailyUsageBuckets: []codex.AccountUsageDay{}}
	m := New(historyStub{data: data}, time.Minute)
	m.meterView = viewUsage
	m.snapshot.AccountFingerprint = "a"
	cmd := m.requestHistory()
	if cmd == nil || !m.history.loading || m.requestHistory() != nil {
		t.Fatal("request not marked busy or duplicated")
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if m.history.loading || m.history.data.FetchedAt.IsZero() {
		t.Fatal("request did not complete")
	}
	updated, _ = m.Update(accountHistoryMsg{sequence: m.history.sequence - 1, err: errors.New("late")})
	if updated.(Model).history.err != nil {
		t.Fatal("stale response accepted")
	}
	updated, _ = m.Update(accountHistoryMsg{sequence: m.history.sequence, err: errors.New("offline")})
	m = updated.(Model)
	if !strings.Contains(ansi.Strip(m.renderHistory(100, 20, paletteFor(themeHacker))), "STALE") {
		t.Fatal("stale data not labelled")
	}
	data.AccountFingerprint = "b"
	updated, _ = m.Update(accountHistoryMsg{sequence: m.history.sequence, data: data})
	m = updated.(Model)
	if !m.history.data.FetchedAt.IsZero() || m.history.err == nil {
		t.Fatal("cross-account data displayed")
	}
}

func TestHistoryRefreshAndControls(t *testing.T) {
	m := New(historyStub{}, time.Minute)
	m.meterView = viewUsage
	m.width, m.height = 80, 24
	updated, cmd := m.activateFooterButton(footerButtonRefresh)
	if cmd == nil || !updated.history.loading {
		t.Fatal("refresh didn't retain busy state")
	}
	for _, test := range []struct {
		key  rune
		mode int
	}{{'w', 1}, {'c', 2}, {'d', 0}} {
		u, _ := m.Update(key(test.key))
		m = u.(Model)
		if m.history.mode != test.mode {
			t.Fatal("period key failed")
		}
	}
	m.width = 40 // Only narrow terminals now require history paging.
	m.activateHistory(3)
	if m.history.offset == 0 {
		t.Fatal("older history not selected")
	}
	m.activateHistory(4)
	if m.history.offset != 0 {
		t.Fatal("newer history not selected")
	}
	for i := 0; i < 100; i++ {
		m.activateHistory(3)
	}
	if m.history.offset >= 364 {
		t.Fatal("pagination exceeded history")
	}
	m.activateHistory(1)
	if m.history.offset != 0 {
		t.Fatal("period change didn't reset page")
	}
}

func TestHistoryRenderAndHitboxes(t *testing.T) {
	for _, width := range []int{9, 20, 40, 60, 80, 120, 200} {
		for _, height := range []int{12, 16, 24, 40} {
			m := Model{meterView: viewUsage, width: width, height: height, history: accountHistoryState{data: codex.AccountUsage{FetchedAt: time.Now(), DailyUsageBuckets: []codex.AccountUsageDay{{StartDate: time.Now().UTC().Format("2006-01-02"), Tokens: 12345}}}}}
			layout := m.dashboardLayout()
			for mode := 0; mode < 3; mode++ {
				m.history.mode = mode
				output := m.renderHistory(layout.contentWidth, layout.meterHeight, paletteFor(themeHacker))
				if lipgloss.Width(output) > layout.contentWidth || lipgloss.Height(output) != layout.meterHeight {
					t.Fatalf("history overflow %dx%d mode %d", width, height, mode)
				}
			}
			for _, button := range historyButtons(layout.contentWidth) {
				for x := button.x; x < button.x+lipgloss.Width(button.label); x++ {
					action, ok := m.historyButtonAt(x+2, layout.meterY)
					if !ok || action != button.action {
						t.Fatalf("unclickable button %q at %d width %d", button.label, x, width)
					}
				}
				updated, _ := m.Update(tea.MouseClickMsg{X: button.x + 2, Y: layout.meterY, Button: tea.MouseLeft})
				if button.action < 3 && updated.(Model).history.mode != button.action {
					t.Fatal("click didn't select period")
				}
			}
			if _, ok := m.historyButtonAt(2, layout.meterY+1); ok {
				t.Fatal("summary is clickable")
			}
		}
	}
}

func TestHistoryMissingVersusEmpty(t *testing.T) {
	m := Model{meterView: viewUsage}
	if !strings.Contains(m.renderHistory(120, 20, paletteFor(themeHacker)), "history unavailable") {
		t.Fatal("missing history represented as zero")
	}
	m.history.data.DailyUsageBuckets = []codex.AccountUsageDay{}
	output := ansi.Strip(m.renderHistory(120, 20, paletteFor(themeHacker)))
	if strings.Contains(output, "history unavailable") || !strings.Contains(output, "PEAK DAY 0") {
		t.Fatal("empty history not graphed as zero")
	}
}

func TestDailyActivityCalendar(t *testing.T) {
	m := Model{history: accountHistoryState{data: codex.AccountUsage{DailyUsageBuckets: []codex.AccountUsageDay{{StartDate: time.Now().UTC().Format("2006-01-02"), Tokens: 100}}}}}
	for _, size := range []struct{ w, h int }{{40, 11}, {80, 15}, {160, 30}, {240, 60}} {
		lines := m.renderHistoryCalendar(size.w, size.h, paletteFor(themeHacker))
		text := ansi.Strip(strings.Join(lines, "\n"))
		if len(lines) > size.h || lipgloss.Width(text) > size.w {
			t.Fatalf("calendar overflow %dx%d", size.w, size.h)
		}
		for _, weekday := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
			if !strings.Contains(text, weekday) {
				t.Fatalf("missing weekday %s", weekday)
			}
		}
		if !strings.Contains(text, "LESS") || !strings.Contains(text, "MORE") {
			t.Fatal("missing legend")
		}
	}
	for i, want := range []int{0, 1, 1, 2, 3, 4} {
		n := []int64{0, 1, 25, 50, 75, 100}[i]
		if got := historyHeatLevel(n, 100); got != want {
			t.Fatalf("heat level %d: %d want %d", n, got, want)
		}
	}
	if got := m.renderHistoryCalendar(80, 10, paletteFor(themeHacker)); len(got) != 1 || !strings.Contains(got[0], "Enlarge") {
		t.Fatal("short calendar silently lost weekdays")
	}
}

func TestHistoryViewsPrioritiseFullYear(t *testing.T) {
	for _, height := range []int{11, 20, 40, 80} {
		for width := 56; width <= 300; width++ {
			calendar := historyCalendarGeometry(width, height)
			if calendar.columns != 52 {
				t.Fatalf("calendar at %dx%d shows %d weeks", width, height, calendar.columns)
			}
			if got := 4 + 52*calendar.cellWidth + 51*calendar.gap; got > width {
				t.Fatalf("calendar width %d exceeds %d", got, width)
			}
			if got := 7*calendar.cellHeight + 4; got > height {
				t.Fatalf("calendar height %d exceeds %d", got, height)
			}
		}
	}
	for width := 60; width <= 300; width++ {
		bars := historyBarGeometry(width)
		if bars.columns != 52 || 8+52*bars.cellWidth+51*bars.gap > width {
			t.Fatalf("bars do not fit full year at %d: %+v", width, bars)
		}
	}
	m := Model{width: 80, height: 24, meterView: viewUsage, history: accountHistoryState{data: codex.AccountUsage{DailyUsageBuckets: []codex.AccountUsageDay{}}}}
	points := historyPoints(m.history.data, time.Now(), 0)
	first := points[0].date.Format("02 Jan 2006")
	for mode := 0; mode < 3; mode++ {
		m.history.mode = mode
		m.history.offset = 25 // A previous narrow viewport must not hide older weeks.
		layout := m.dashboardLayout()
		output := ansi.Strip(m.renderHistory(layout.contentWidth, layout.meterHeight, paletteFor(themeHacker)))
		if !strings.Contains(output, first) {
			t.Fatalf("mode %d omits oldest week:\n%s", mode, output)
		}
		m.activateHistory(3)
		if m.history.offset != 0 {
			t.Fatalf("mode %d pages although full year fits", mode)
		}
	}
}

package ui

import (
	"errors"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestUsageReportValueColours(t *testing.T) {
	for _, theme := range []themeID{themeHacker, themeRust, themeBlueSteel, themeUltraviolet, themeNightshade} {
		p := paletteFor(theme)
		for _, test := range []struct {
			known   bool
			percent float64
			want    color.Color
		}{
			{false, 100, p.dim}, {true, 0, p.primary}, {true, 79.99, p.primary},
			{true, 80, p.warning}, {true, 99.99, p.warning}, {true, 100, p.danger}, {true, 125, p.danger},
		} {
			got := usageReportValueStyle(p, test.known, test.percent).GetForeground()
			if got != test.want {
				t.Fatalf("theme %v known %v percent %v: wrong colour", theme, test.known, test.percent)
			}
		}
	}
}

func TestUsageControlsKeepViewNavigationAndHideInapplicableOptions(t *testing.T) {
	for _, width := range []int{40, 80, 160} {
		for _, mode := range []int{0, 1, 2, 7, 8} {
			m := New(historyStub{}, time.Minute)
			m.meterView, m.width, m.height, m.history.mode = viewUsage, width, 24, mode
			layout := m.dashboardLayout()
			seen := map[int]bool{}
			for _, button := range historyButtons(layout.contentWidth, mode) {
				seen[button.action] = true
				action, ok := m.historyButtonAt(2+button.x, layout.meterY+button.row)
				if !ok || action != button.action {
					t.Fatalf("width %d mode %d: incorrect hitbox for action %d", width, mode, button.action)
				}
				if button.action < 3 || button.action == 7 || button.action == 8 {
					next, _ := m.Update(tea.MouseClickMsg{X: 2 + button.x, Y: layout.meterY + button.row, Button: tea.MouseLeft})
					if next.(Model).history.mode != button.action {
						t.Fatalf("width %d mode %d: cannot navigate to view %d", width, mode, button.action)
					}
				}
			}
			for action, want := range map[int]bool{0: true, 1: true, 2: true, 7: true, 8: true, 9: mode >= 7, 5: mode < 7, 6: mode < 7, 3: true, 4: true} {
				if seen[action] != want {
					t.Fatalf("width %d mode %d: action %d visible=%v, want %v", width, mode, action, seen[action], want)
				}
			}
		}
	}
}

func TestUsageViewCycleAndReturn(t *testing.T) {
	m := New(historyStub{}, time.Minute)
	m.meterView, m.width, m.height = viewUsage, 120, 30
	for _, mode := range []int{0, 1, 2, 7, 8} {
		m.history.mode = mode
		for _, shortcut := range "dwcbp" {
			next, _ := m.Update(key(shortcut))
			if next.(Model).history.mode != mode {
				t.Fatalf("legacy shortcut %c changed Usage view", shortcut)
			}
		}
	}
	m.history.mode = 0
	for _, want := range []int{1, 2, 7, 8, 0} {
		next, _ := m.Update(key('v'))
		m = next.(Model)
		if m.history.mode != want {
			t.Fatalf("V selected %d, want %d", m.history.mode, want)
		}
	}
	next, _ := m.Update(footerMouseMessage(t, m, footerButtonView, true))
	m = next.(Model)
	if m.history.mode != 1 {
		t.Fatal("View footer did not cycle Usage")
	}
	next, _ = m.Update(specialKey(tea.KeyTab))
	m = next.(Model)
	if m.currentMainTab() != mainTabBenchmark {
		t.Fatal("Tab changed a subview")
	}
	next, _ = m.Update(modifiedKey(tea.KeyTab, tea.ModShift))
	m = next.(Model)
	if m.meterView != viewUsage || m.history.mode != 1 {
		t.Fatal("Usage selection not retained")
	}
}

func TestUsageRefreshOnlyInFooter(t *testing.T) {
	m := New(historyStub{}, time.Minute)
	m.meterView, m.width, m.height = viewUsage, 120, 30
	for _, mode := range []int{0, 1, 2, 7, 8} {
		m.history.mode = mode
		for _, err := range []error{nil, errors.New("offline")} {
			m.history.err = err
			body := ansi.Strip(m.renderHistory(116, 20, paletteFor(themeHacker)))
			if strings.Contains(body, "R REFRESH") || strings.Contains(body, "R RETRY") {
				t.Fatal("duplicate refresh hint in Usage")
			}
		}
	}
	if _, ok := footerButtonByID(m, footerButtonRefresh); !ok {
		t.Fatal("refresh footer missing")
	}
	next, cmd := m.Update(key('r'))
	if cmd == nil || !next.(Model).history.loading {
		t.Fatal("R no longer refreshes Usage")
	}
}

func TestUsageGroupControlOnSeparateRow(t *testing.T) {
	m := New(historyStub{}, time.Minute)
	m.meterView, m.width, m.height, m.history.mode = viewUsage, 120, 30, 7
	for group, name := range reportDimensions {
		m.history.group = group
		text := ansi.Strip(m.renderHistory(116, 20, paletteFor(themeHacker)))
		if strings.Count(text, "GROUP") != 1 || !strings.Contains(text, "GROUP: "+strings.ToUpper(name)) {
			t.Fatalf("missing or duplicated group control for %s", name)
		}
		for _, b := range historyButtons(116, 7, group) {
			if b.action == 9 {
				if b.row != 1 {
					t.Fatal("Group rendered as a view tab")
				}
				for x := b.x; x < b.x+len(b.label); x++ {
					if action, ok := m.historyButtonAt(x+2, m.dashboardLayout().meterY+1); !ok || action != 9 {
						t.Fatal("Group hitbox mismatch")
					}
				}
			}
		}
	}
}

func TestUsageReportsNavigationAndResponsiveRendering(t *testing.T) {
	m := New(historyStub{}, time.Minute)
	m.meterView = viewUsage
	used := 12500.0
	m.history.data.Reports = &codex.UsageReports{DailyStatus: "OPENAI", PlanStatus: "OPENAI", Daily: &codex.DailyUsageReport{Units: "RELATIVE USAGE", Days: []codex.UsageBreakdownDay{
		{Date: "2026-10-01", Total: 3, Groups: map[string]map[string]float64{"model": {"older-model": 3}}},
		{Date: "2026-10-02", Total: 4, Groups: map[string]map[string]float64{"model": {"gpt-6.1-sol": 4}}},
	}}, Plan: &codex.PlanUsageReport{Approximate: true, Periods: []codex.PlanUsagePeriod{{WindowMinutes: 10080, UsedBasisPoints: &used, StartsAt: "2026-09-28T00:00:00Z", EndsAt: "2026-10-05T00:00:00Z"}, {WindowMinutes: 300}}}}
	for _, mode := range []int{7, 8} {
		m.activateHistory(mode)
		if m.history.mode != mode {
			t.Fatal("mode not selected")
		}
		for _, size := range [][2]int{{20, 5}, {40, 16}, {80, 24}, {160, 60}} {
			m.width, m.height = size[0], size[1]
			rendered := m.renderHistory(size[0], size[1], paletteFor(themeHacker))
			if len(strings.Split(rendered, "\n")) != size[1] {
				t.Fatal("height overflow")
			}
			for _, line := range strings.Split(rendered, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("width overflow")
				}
			}
		}
	}
	text := ansi.Strip(m.renderHistory(120, 24, paletteFor(themeHacker)))
	for _, want := range []string{"APPROXIMATE", "PARTIAL COVERAGE", "125.00%", "PARTIAL ACCOUNTING"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s", want)
		}
	}
	m.activateHistory(3)
	if text := ansi.Strip(m.renderHistory(120, 24, paletteFor(themeHacker))); !strings.Contains(text, "USED UNKNOWN") {
		t.Fatal("unknown quota shown as zero")
	}
	m.activateHistory(7)
	m.activateHistory(9) // model
	if text := ansi.Strip(m.renderHistory(120, 24, paletteFor(themeHacker))); !strings.Contains(text, "gpt-6.1-sol") {
		t.Fatal("missing model")
	}
	m.activateHistory(3)
	if text := ansi.Strip(m.renderHistory(120, 24, paletteFor(themeHacker))); !strings.Contains(text, "older-model") {
		t.Fatal("older day unavailable")
	}
	for _, width := range []int{40, 80, 160} {
		m.width, m.height = width, 24
		layout := m.dashboardLayout()
		for _, button := range historyButtons(layout.contentWidth, m.history.mode, m.history.group) {
			a, ok := m.historyButtonAt(2+button.x, layout.meterY+button.row)
			if !ok || a != button.action {
				t.Fatal("button hit surface mismatch")
			}
		}
	}
	m.activateHistory(8)
	m.history.err = errors.New("offline")
	if !strings.Contains(ansi.Strip(m.renderHistory(120, 24, paletteFor(themeHacker))), "STALE // refresh failed") {
		t.Fatal("in-memory fallback missing stale warning")
	}
	m.history.err = nil
	m.history.group = 1
	m.history.data.Reports.Plan.Periods[0].Breakdowns = []codex.PlanUsageBreakdown{{Dimension: "model", Rows: []codex.PlanUsageValue{{Key: "credit correction", BasisPoints: -25}}}}
	if rendered := ansi.Strip(m.renderHistory(120, 24, paletteFor(themeHacker))); !strings.Contains(rendered, "-0.25") {
		t.Fatal("signed amount hidden")
	}
}

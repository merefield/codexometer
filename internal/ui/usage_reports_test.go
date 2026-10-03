package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestUsageReportsNavigationAndResponsiveRendering(t *testing.T) {
	m := New(historyStub{}, time.Minute)
	m.meterView = viewUsage
	used := 12500.0
	m.history.data.Reports = &codex.UsageReports{DailyStatus: "OPENAI", PlanStatus: "OPENAI", Daily: &codex.DailyUsageReport{Units: "RELATIVE USAGE", Days: []codex.UsageBreakdownDay{
		{Date: "2026-10-01", Total: 3, Groups: map[string]map[string]float64{"model": {"older-model": 3}}},
		{Date: "2026-10-02", Total: 4, Groups: map[string]map[string]float64{"model": {"gpt-6.1-sol": 4}}},
	}}, Plan: &codex.PlanUsageReport{Approximate: true, Periods: []codex.PlanUsagePeriod{{WindowMinutes: 10080, UsedBasisPoints: &used, StartsAt: "2026-09-28T00:00:00Z", EndsAt: "2026-10-05T00:00:00Z"}, {WindowMinutes: 300}}}}
	for _, view := range []struct {
		key  string
		mode int
	}{{"b", 7}, {"p", 8}} {
		a, ok := historyKey(view.key)
		if !ok {
			t.Fatal("missing shortcut")
		}
		m.activateHistory(a)
		if m.history.mode != view.mode {
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
		for _, button := range historyButtons(layout.contentWidth, m.history.mode) {
			a, ok := m.historyButtonAt(2+button.x, layout.meterY)
			if !ok || a != button.action {
				t.Fatal("button hit surface mismatch")
			}
		}
	}
	m.activateHistory(8)
	m.history.group = 1
	m.history.data.Reports.Plan.Periods[0].Breakdowns = []codex.PlanUsageBreakdown{{Dimension: "model", Rows: []codex.PlanUsageValue{{Key: "credit correction", BasisPoints: -25}}}}
	if rendered := ansi.Strip(m.renderHistory(120, 24, paletteFor(themeHacker))); !strings.Contains(rendered, "-0.25") {
		t.Fatal("signed amount hidden")
	}
}

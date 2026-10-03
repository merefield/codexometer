package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestThresholdsRenderActiveAndNextPolicySteps(t *testing.T) {
	snapshot := codex.DemoSnapshot()
	snapshot.AccountFingerprint = "account"
	snapshot.RateLimits.Secondary.UsedPercent = 65
	m := Model{snapshot: snapshot, quotaSteps: []codex.QuotaStep{
		{Threshold: 80, Model: "gpt-small", Effort: "low", Mode: "auto"},
		{Threshold: 50, Model: "gpt-medium", Effort: "medium", ServiceTier: "fast"},
	}}
	output := ansi.Strip(m.renderThresholds(100, 16, paletteFor(themeHacker)))
	for _, want := range []string{"THRESHOLDS", "50%", "ACTIVE", "gpt-medium / medium / fast", "80%", "AUTO", "NEXT // 15 PP"} {
		if !strings.Contains(output, want) {
			t.Fatalf("threshold view missing %q:\n%s", want, output)
		}
	}
}

func TestThresholdNavigationHitboxesAndScroll(t *testing.T) {
	for _, width := range []int{20, 40, 80, 120} {
		m := Model{width: width, height: 16, snapshot: codex.DemoSnapshot(), quotaSteps: []codex.QuotaStep{{Threshold: 80, Model: "small", Effort: "low"}}}
		g := m.dashboardLayout()
		tabs, _ := quotaViewTabLayout(g.contentWidth, true)
		for _, tab := range tabs {
			for x := tab.x; x < tab.x+tab.width; x++ {
				if got, ok := m.quotaViewTabAt(x+2, g.quotaTabsY); !ok || got != tab.view {
					t.Fatalf("width %d: tab %d hitbox mismatch at %d", width, tab.view, x)
				}
			}
		}
		m.meterView = viewThresholds
		for i := 1; i < 20; i++ {
			m.quotaSteps = append(m.quotaSteps, codex.QuotaStep{Threshold: i, Model: "model", Effort: "medium"})
		}
		next, _ := m.Update(specialKey(tea.KeyPgDown))
		if next.(Model).thresholdScroll == 0 {
			t.Fatal("Page Down did not scroll overflowing thresholds")
		}
	}
}

func TestThresholdsRenderCompactAndStayWithinBounds(t *testing.T) {
	m := Model{quotaSteps: []codex.QuotaStep{{Threshold: 80, Model: "gpt-very-long-model-name", Effort: "medium"}}}
	output := m.renderThresholds(36, 8, paletteFor(themeHacker))
	if lipgloss.Width(output) > 36 || strings.Count(output, "\n")+1 > 8 {
		t.Fatalf("compact threshold view exceeded 36x8:\n%s", ansi.Strip(output))
	}
}

func TestThresholdSpeedDisplay(t *testing.T) {
	for _, tc := range []struct{ tier, want string }{
		{"default", "standard"}, {"", "speed unchanged"}, {"fast", "fast"}, {"priority", "priority"},
	} {
		m := Model{quotaSteps: []codex.QuotaStep{{Threshold: 80, Model: "model", Effort: "low", ServiceTier: tc.tier}}}
		output := ansi.Strip(m.renderThresholds(120, 16, paletteFor(themeHacker)))
		if !strings.Contains(output, "model / low / "+tc.want) {
			t.Errorf("tier %q: missing displayed speed %q in %s", tc.tier, tc.want, output)
		}
		if m.quotaSteps[0].ServiceTier != tc.tier {
			t.Fatal("presentation changed configured tier")
		}
	}
}

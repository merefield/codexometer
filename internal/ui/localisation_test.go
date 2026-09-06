package ui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

// Each subprocess exercises startup locale selection, including labels stored
// in package-level button definitions, without mutating global language state.
func TestLocalisedScreens(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"en-GB", "nl", "de", "fr", "it", "es", "ru", "ja", "zh-Hans"} {
		t.Run(code, func(t *testing.T) {
			cmd := exec.Command(exe, "-test.run=^TestLocalisedScreensHelper$")
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, i18n.EnvironmentVariable+"=") && !strings.HasPrefix(env, "CODEXOMETER_LOCALE_HELPER=") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			cmd.Env = append(cmd.Env, i18n.EnvironmentVariable+"="+code, "CODEXOMETER_LOCALE_HELPER=1")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}

func TestLocalisedScreensHelper(t *testing.T) {
	if os.Getenv("CODEXOMETER_LOCALE_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	if i18n.Code() != os.Getenv(i18n.EnvironmentVariable) {
		t.Fatal("locale not applied at startup")
	}
	t.Run("benchmark_click_surfaces", TestBenchmarkRenderedClickSurfacesMatchHitTestingAcrossSizes)
	t.Run("monitor_click_surfaces", TestMonitorButtonBoxesMatchEnabledHitSurfacesAcrossSizes)
	t.Run("monitor_context_surfaces", TestMonitorContextResponsiveHitTargets)
	t.Run("monitor_privacy_surfaces", TestMonitorContextPrivacyAndDismissRenderedTargets)
	t.Run("monitor_approval_surfaces", TestMonitorApprovalRenderedTargets)
	t.Run("tab_click_surfaces", TestEveryRenderedTabCellIsClickableAcrossWidths)
	t.Run("reset_click_surfaces", TestQuotaResetRenderedHitSurfaces)
	t.Run("quota_estimator", TestQuotaAPIEstimatorLearnsRangeAndCurrentSpend)
	t.Run("canonical_restart_reasons", TestShortQuotaAPIRestartReasonsStayCanonical)
	t.Run("quota_estimate_text", func(t *testing.T) {
		now := time.Now().Truncate(time.Second)
		reset := now.Add(4 * time.Hour).Unix()
		m := New(nil, time.Minute)
		m.snapshot = apiEqSnapshot(10, reset)
		m.observeQuotaAPIEq(m.snapshot, codex.LiveUsageSnapshot{}, now)
		m.snapshot = apiEqSnapshot(15, reset)
		m.observeQuotaAPIEq(m.snapshot, codex.LiveUsageSnapshot{APIEqUSD: 1, APIEqPricedCalls: 1}, now.Add(time.Minute))
		for _, width := range []int{40, 52, 80} {
			line := m.quotaAPILine(m.snapshot.Meters()[0], width)
			if !utf8.ValidString(line) || strings.Contains(line, "%!") {
				t.Errorf("invalid estimate at width %d: %q", width, line)
			}
		}
		if i18n.Code() == "fr" {
			line := m.quotaAPILine(m.snapshot.Meters()[0], 80)
			if !strings.HasPrefix(line, "API-EQ OBSERVÉ // DÉPENSE ") || !strings.Contains(line, "FAIBLE") || strings.Contains(line, "SPEND") {
				t.Errorf("partially translated French estimate: %q", line)
			}
		}
		// A second and third clean observation exercise medium confidence too.
		for step := 2; step <= 3; step++ {
			m.snapshot = apiEqSnapshot(10+5*step, reset)
			m.observeQuotaAPIEq(m.snapshot, codex.LiveUsageSnapshot{APIEqUSD: float64(step), APIEqPricedCalls: int64(step)}, now.Add(time.Duration(step)*time.Minute))
		}
		estimate, ok := m.quotaAPIEstimate(m.snapshot, m.snapshot.Meters()[0])
		if !ok || estimate.confidence != "MED" {
			t.Fatalf("expected canonical medium confidence, got %+v", estimate)
		}
		line := m.quotaAPILine(m.snapshot.Meters()[0], 80)
		if !strings.Contains(line, i18n.Text("MED")) || (i18n.Code() != "en-GB" && strings.Contains(line, "// SPEND ")) {
			t.Errorf("partially translated medium-confidence estimate: %q", line)
		}
	})
	for _, state := range []monitorState{monitorIdle, monitorRunning, monitorPaused} {
		for _, width := range []int{12, 24, 40, 80} {
			m := Model{monitorState: state}
			graph := m.renderMonitorGraphSamples(width, 8, nil, i18n.Text("TOKEN BARS"), paletteFor(themeHacker))
			if lipgloss.Width(graph) > width || lipgloss.Height(graph) > 8 {
				t.Errorf("empty monitor graph exceeds %dx8: %dx%d", width, lipgloss.Width(graph), lipgloss.Height(graph))
			}
		}
	}
	for _, size := range []struct{ w, h int }{{40, 16}, {60, 24}, {80, 24}, {120, 40}, {200, 60}} {
		for view := viewBars; view < viewCount; view++ {
			m := Model{snapshot: codex.DemoSnapshot(), width: size.w, height: size.h, meterView: view, nextRefresh: time.Now().Add(time.Minute)}
			m.history.data = codex.AccountUsage{DailyUsageBuckets: []codex.AccountUsageDay{}}
			output := m.render()
			if !utf8.ValidString(output) || strings.Contains(output, "%!") {
				t.Fatalf("%s invalid text", view.name())
			}
			if lipgloss.Width(output) > size.w || lipgloss.Height(output) > size.h {
				t.Errorf("%s at %dx%d rendered %dx%d:\n%s", view.name(), size.w, size.h, lipgloss.Width(output), lipgloss.Height(output), ansi.Strip(output))
			}
			layout := m.dashboardLayout()
			tabs, _ := mainTabLayout(layout.contentWidth, true)
			for _, tab := range tabs {
				for x := 0; x < tab.width; x++ {
					if got, ok := m.mainTabAt(2+tab.x+x, layout.tabsY); !ok || got != tab.tab {
						t.Errorf("tab %q has a misplaced hitbox", tab.label)
					}
				}
			}
			buttons, sep := footerButtonLayoutWithTheme(layout.contentWidth, paletteFor(m.theme).name, m.meterView.isQuota())
			x := 2
			for _, b := range buttons {
				for col := 0; col < lipgloss.Width(b.label); col++ {
					if got := m.footerButtonAt(x+col, layout.footerY+1); got != b.id {
						t.Errorf("footer %q hitbox off at %d", b.label, col)
					}
				}
				x += lipgloss.Width(b.label) + len(sep)
			}
			if view == viewUsage {
				for _, b := range historyButtons(layout.contentWidth) {
					for col := 0; col < lipgloss.Width(b.label); col++ {
						if action, ok := m.historyButtonAt(2+b.x+col, layout.meterY); !ok || action != b.action {
							t.Errorf("Usage %q hitbox off", b.label)
						}
					}
				}
			}
		}
	}
}

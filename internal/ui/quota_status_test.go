package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func TestWindowQuotaHealthUsesBurnPaceAgainstResetProgress(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		used       int
		elapsedPct int
		want       quotaHealth
	}{
		{name: "clear with ample quota", used: 20, elapsedPct: 50, want: quotaHealthClear},
		{name: "clear when low quota matches late reset", used: 90, elapsedPct: 95, want: quotaHealthClear},
		{name: "watch when low quota is burning too quickly", used: 85, elapsedPct: 80, want: quotaHealthWatch},
		{name: "near when projected exhaustion is much earlier than reset", used: 40, elapsedPct: 10, want: quotaHealthNear},
		{name: "near when less than five percent remains at excess pace", used: 96, elapsedPct: 90, want: quotaHealthNear},
		{name: "exhausted", used: 100, elapsedPct: 99, want: quotaHealthExhausted},
	} {
		t.Run(test.name, func(t *testing.T) {
			window := quotaHealthTestWindow(now, test.used, test.elapsedPct)
			if got := windowQuotaHealth(window, now); got != test.want {
				t.Fatalf("health = %s, want %s", got.label(), test.want.label())
			}
		})
	}
}

func TestWindowQuotaHealthFallsBackToRemainingQuotaWithoutTiming(t *testing.T) {
	for _, test := range []struct {
		used int
		want quotaHealth
	}{
		{used: 79, want: quotaHealthClear},
		{used: 80, want: quotaHealthWatch},
		{used: 95, want: quotaHealthNear},
		{used: 100, want: quotaHealthExhausted},
	} {
		if got := windowQuotaHealth(codex.Window{UsedPercent: test.used}, time.Now()); got != test.want {
			t.Errorf("used %d health = %s, want %s", test.used, got.label(), test.want.label())
		}
	}
}

func TestSnapshotQuotaHealthUsesWorstWindowAndExplicitLimitSignal(t *testing.T) {
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	clearWindow := quotaHealthTestWindow(now, 20, 50)
	watchWindow := quotaHealthTestWindow(now, 85, 80)
	snapshot := codex.Snapshot{RateLimits: codex.RateLimitSnapshot{Primary: &clearWindow, Secondary: &watchWindow}}
	if got := snapshotQuotaHealth(snapshot, now); got != quotaHealthWatch {
		t.Fatalf("worst-window health = %s, want %s", got.label(), quotaHealthWatch.label())
	}

	reached := "primary"
	snapshot.RateLimits.RateLimitReachedType = &reached
	if got := snapshotQuotaHealth(snapshot, now); got != quotaHealthExhausted {
		t.Fatalf("explicit reached health = %s, want %s", got.label(), quotaHealthExhausted.label())
	}

	if got := snapshotQuotaHealth(codex.Snapshot{}, now); got != quotaHealthUnknown {
		t.Fatalf("empty snapshot health = %s, want %s", got.label(), quotaHealthUnknown.label())
	}
}

func TestQuotaHealthUsesSemanticSignalColors(t *testing.T) {
	colors := paletteFor(themeNightshade)
	for _, test := range []struct {
		health quotaHealth
		want   lipgloss.Color
	}{
		{quotaHealthClear, lipgloss.Color("#57FF8A")},
		{quotaHealthWatch, lipgloss.Color("#67B7FF")},
		{quotaHealthNear, lipgloss.Color("#FFB454")},
		{quotaHealthExhausted, lipgloss.Color("#FF5F6D")},
	} {
		if got := test.health.color(colors); got != test.want {
			t.Errorf("%s color = %q, want %q", test.health.label(), got, test.want)
		}
	}
}

func TestQuotaSignalLabelsRemainResponsive(t *testing.T) {
	now := time.Now()
	window := quotaHealthTestWindow(now, 85, 80)
	model := Model{snapshot: codex.Snapshot{RateLimits: codex.RateLimitSnapshot{Primary: &window}}}
	colors := paletteFor(themeUltraviolet)
	for _, test := range []struct {
		width int
		want  string
	}{
		{width: 80, want: "● ONLINE // QUOTA WATCH"},
		{width: 40, want: "● ON // WATCH"},
		{width: 20, want: "● WATCH"},
	} {
		got := ansi.Strip(model.renderSignalStatus(test.width, colors))
		if got != test.want {
			t.Errorf("width %d status = %q, want %q", test.width, got, test.want)
		}
		if strings.Contains(got, "EXHAUSTED") {
			t.Errorf("width %d rendered the wrong health label: %q", test.width, got)
		}
	}
}

func quotaHealthTestWindow(now time.Time, used, elapsedPercent int) codex.Window {
	durationMinutes := int64(100)
	remainingMinutes := int64(100 - elapsedPercent)
	reset := now.Add(time.Duration(remainingMinutes) * time.Minute).Unix()
	return codex.Window{UsedPercent: used, WindowDurationMins: &durationMinutes, ResetsAt: &reset}
}

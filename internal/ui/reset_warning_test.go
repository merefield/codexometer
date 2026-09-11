package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestResetWarningClickSurfaces(t *testing.T) {
	for _, width := range []int{20, 24, 28, 40, 60, 80, 120, 180} {
		for _, state := range []string{"ready", "confirm", "retry", "busy"} {
			m, f := resetModel()
			m.width, m.height = width, 60
			m.snapshot.RateLimits.Primary.UsedPercent = 1
			m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("first", 51*time.Hour)}
			switch state {
			case "confirm":
				m.resetConfirmUntil = time.Now().Add(10 * time.Second)
			case "retry":
				m.resetKey = "uncertain-attempt"
			case "busy":
				m.resetBusy = true
			}
			g := m.dashboardLayout()
			c := m.resetControlsLayout(g.contentWidth)
			if c.warning == "" {
				t.Fatalf("%d %s: missing warning", width, state)
			}
			rows := strings.Split(ansi.Strip(m.renderMainTabs(g.contentWidth, paletteFor(m.theme))), "\n")
			if len(rows) != c.extraRows+1 {
				t.Fatal("row allocation mismatch")
			}
			for _, row := range rows {
				if lipgloss.Width(row) > g.contentWidth {
					t.Fatalf("%d %s overflow: %q", width, state, row)
				}
			}
			for _, target := range []struct {
				text    string
				x, y    int
				warning bool
			}{{c.warning, c.warningX, c.warningY, true}, {c.button, c.buttonX, c.buttonY, false}} {
				index := strings.Index(rows[target.y], target.text)
				if index < 0 || lipgloss.Width(rows[target.y][:index]) != target.x {
					t.Fatalf("%d %s: render disagrees with hit geometry", width, state)
				}
				for offset := range lipgloss.Width(target.text) {
					x, y := 2+target.x+offset, g.tabsY+target.y
					if m.resetWarningAt(x, y) != target.warning || m.resetAt(x, y) == target.warning {
						t.Fatal("overlapping or missing target")
					}
					if _, hit := m.mainTabAt(x, y); hit {
						t.Fatal("overlaps main tab")
					}
					if _, hit := m.quotaViewTabAt(x, y); hit {
						t.Fatal("overlaps quota view")
					}
					if target.warning {
						u, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
						n := u.(Model)
						if n.meterView != viewResets || !n.resetConfirmUntil.IsZero() || len(f.keys) != 0 || n.resetKey != m.resetKey || n.resetBusy != m.resetBusy {
							t.Fatal("warning did more than navigate/cancel confirmation")
						}
					}
				}
			}
			if g.quotaTabsY != g.tabsY+c.extraRows+1 {
				t.Fatal("quota tabs not moved with warning")
			}
		}
	}
}

func TestResetWarningVisibility(t *testing.T) {
	m, _ := resetModel()
	for _, age := range []time.Duration{-time.Hour, 73 * time.Hour} {
		m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("first", age)}
		if m.resetControlsLayout(100).warning != "" {
			t.Fatal("warned outside expiry interval")
		}
	}
	m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("first", 30*time.Minute)}
	if !strings.Contains(m.resetControlsLayout(100).warning, "<1H") {
		t.Fatal("sub-hour expiry not shown")
	}
	m.snapshot.FetchedAt = time.Now().Add(-3 * time.Minute)
	if m.resetControlsLayout(100).warning != "" {
		t.Fatal("warned from stale data")
	}
	m.snapshot.FetchedAt = time.Now()
	m.meterView = viewMonitor
	if m.resetControlsLayout(100).warning != "" {
		t.Fatal("warning leaked outside quota")
	}
}

func TestResetWarningHoursOverride(t *testing.T) {
	m, _ := resetModel()
	m.snapshot.RateLimits.Primary.UsedPercent = 1
	m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("first", 96*time.Hour)}
	if m.resetWarningHours != 72 || m.resetExpiringSoon() || m.resetLabel() != "" {
		t.Fatal("wrong default")
	}
	m.SetResetWarningHours(168)
	if !m.resetExpiringSoon() || m.resetControlsLayout(100).warning == "" || m.resetLabel() == "" {
		t.Fatal("larger lead time ignored")
	}
	if !strings.Contains(strings.Join(m.resetDetailLines(100, paletteFor(m.theme)), "\n"), "168 hours") {
		t.Fatal("detail warning kept fixed lead time")
	}
	m.SetResetWarningHours(0)
	if m.resetExpiringSoon() || m.resetLabel() != "" || m.resetControlsLayout(100).warning != "" {
		t.Fatal("zero did not disable warning")
	}
	m.snapshot.RateLimits.Primary.UsedPercent = 80
	if m.resetLabel() == "" {
		t.Fatal("disabled expiry warning suppressed consumption threshold")
	}
	m.SetResetWarningHours(-1)
	if m.resetWarningHours != 0 {
		t.Fatal("negative duration")
	}
}

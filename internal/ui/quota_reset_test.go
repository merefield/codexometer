package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type resetFake struct {
	stubFetcher
	keys []string
	err  error
}

func (f *resetFake) ConsumeReset(_ context.Context, key, account string) (string, error) {
	f.keys = append(f.keys, key)
	return "reset", f.err
}
func resetModel() (Model, *resetFake) {
	f := &resetFake{}
	m := New(f, time.Minute)
	m.width, m.height, m.loading = 100, 40, false
	m.snapshot = codex.Snapshot{AccountFingerprint: "account", FetchedAt: time.Now(), RateLimitResetCredits: &codex.ResetCredits{AvailableCount: 2}, RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 80}}}
	f.snapshot = m.snapshot
	return m, f
}

func TestQuotaResetConfirmationAndRetry(t *testing.T) {
	m, f := resetModel()
	u, cmd := m.pressQuotaReset()
	m = u.(Model)
	if cmd != nil || len(f.keys) != 0 || m.resetConfirmUntil.IsZero() {
		t.Fatal("first press submitted reset")
	}
	u, cmd = m.pressQuotaReset()
	m = u.(Model)
	if cmd == nil || !m.resetBusy {
		t.Fatal("confirmation did not submit")
	}
	if _, duplicate := m.pressQuotaReset(); duplicate != nil {
		t.Fatal("duplicate submission")
	}
	f.err = errors.New("connection lost")
	u, _ = m.Update(cmd())
	m = u.(Model)
	first := f.keys[0]
	u, _ = m.pressQuotaReset()
	m = u.(Model)
	u, cmd = m.pressQuotaReset()
	m = u.(Model)
	f.err = nil
	u, refresh := m.Update(cmd())
	m = u.(Model)
	if len(f.keys) != 2 || f.keys[1] != first || m.resetKey != "" || refresh == nil {
		t.Fatal("retry lost idempotency or refresh")
	}
}

func TestQuotaResetConsumptionThresholdAndOverride(t *testing.T) {
	for _, used := range []int{0, 79, 80, 100} {
		m, _ := resetModel()
		m.snapshot.RateLimits.Primary.UsedPercent = used
		if visible := m.resetLabel() != ""; visible != (used >= 80) {
			t.Fatalf("used %d visibility %v", used, visible)
		}
		m.SetAlwaysShowReset(true)
		if m.resetLabel() == "" {
			t.Fatalf("override hidden at %d", used)
		}
		m.snapshot.RateLimitResetCredits.AvailableCount = 0
		if m.resetLabel() != "" {
			t.Fatal("override invented available credit")
		}
	}
	m, _ := resetModel()
	m.snapshot.RateLimits.Primary.UsedPercent = 10
	m.snapshot.RateLimits.Secondary = &codex.Window{UsedPercent: 80}
	if m.resetLabel() == "" {
		t.Fatal("secondary window threshold ignored")
	}
	m.snapshot.RateLimitsByLimitID = map[string]codex.RateLimitSnapshot{
		"one": {Primary: &codex.Window{UsedPercent: 20}},
		"two": {Primary: &codex.Window{UsedPercent: 80}},
	}
	if m.resetLabel() == "" {
		t.Fatal("additional quota bucket threshold ignored")
	}
}

func TestQuotaResetCancelAndAccountChange(t *testing.T) {
	m, _ := resetModel()
	u, _ := m.pressQuotaReset()
	m = u.(Model)
	u, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = u.(Model)
	if cmd != nil || !m.resetConfirmUntil.IsZero() {
		t.Fatal("escape did not cancel")
	}
	u, _ = m.pressQuotaReset()
	m = u.(Model)
	m.snapshot.AccountFingerprint = "other"
	if _, cmd := m.pressQuotaReset(); cmd != nil {
		t.Fatal("account change submitted reset")
	}
	m, _ = resetModel()
	u, _ = m.pressQuotaReset()
	m = u.(Model)
	u, _ = m.pressMainTab(mainTabMonitor)
	m = u.(Model)
	if !m.resetConfirmUntil.IsZero() || m.resetLabel() != "" {
		t.Fatal("tab change retained confirmation")
	}
}

func TestQuotaResetVisibilityAndStaleFetch(t *testing.T) {
	m, _ := resetModel()
	m.snapshot.RateLimitResetCredits.AvailableCount = 0
	if m.resetLabel() != "" {
		t.Fatal("zero credits visible")
	}
	m.snapshot.RateLimitResetCredits.AvailableCount = 2
	m.snapshot.FetchedAt = time.Now().Add(-time.Hour)
	if m.resetLabel() != "" {
		t.Fatal("stale credits visible")
	}
	m, _ = resetModel()
	m.resetRevision = 1
	u, _ := m.Update(fetchedMsg{snapshot: codex.Snapshot{}})
	if u.(Model).snapshot.AccountFingerprint != "account" {
		t.Fatal("old fetch replaced current quota")
	}
}

func TestQuotaResetRenderedHitSurfaces(t *testing.T) {
	for _, width := range []int{24, 36, 60, 100, 180} {
		for _, state := range []string{"ready", "confirm", "busy", "retry"} {
			m, _ := resetModel()
			m.width = width
			switch state {
			case "confirm":
				m.resetConfirmUntil = time.Now().Add(time.Second)
			case "busy":
				m.resetBusy = true
			case "retry":
				m.resetKey = "attempt"
			}
			g := m.dashboardLayout()
			lines := strings.Split(ansi.Strip(m.renderMainTabs(g.contentWidth, paletteFor(m.theme))), "\n")
			for row, line := range lines {
				if lipgloss.Width(line) > g.contentWidth {
					t.Fatalf("overflow %d %s: %q", width, state, line)
				}
				index := strings.Index(line, m.resetLabel())
				if index < 0 {
					continue
				}
				x := lipgloss.Width(line[:index]) + 2
				for offset := 0; offset < lipgloss.Width(m.resetLabel()); offset++ {
					if !m.resetAt(x+offset, g.tabsY+row) {
						t.Fatalf("miss %d %s at %d", width, state, offset)
					}
					if _, overlap := m.mainTabAt(x+offset, g.tabsY+row); overlap {
						t.Fatal("tab overlap")
					}
				}
				if m.resetAt(x-1, g.tabsY+row) || m.resetAt(2+g.contentWidth, g.tabsY+row) {
					t.Fatal("hit outside button")
				}
			}
		}
	}
}

func TestQuotaResetClicksAndSurroundingControls(t *testing.T) {
	for _, width := range []int{28, 60, 100, 180} {
		m, f := resetModel()
		m.width = width
		for press := 0; press < 2; press++ {
			lines := strings.Split(ansi.Strip(m.render()), "\n")
			found := false
			for y, line := range lines {
				index := strings.Index(line, m.resetLabel())
				if index < 0 {
					continue
				}
				found = true
				x := lipgloss.Width(line[:index])
				u, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				m = u.(Model)
				if press == 0 && cmd != nil {
					t.Fatal("first click redeemed")
				}
				if press == 1 {
					if cmd == nil {
						t.Fatal("second click missed")
					}
					cmd()
				}
				break
			}
			if !found {
				t.Fatalf("missing rendered button width %d", width)
			}
			g := m.dashboardLayout()
			for y, line := range strings.Split(ansi.Strip(m.render()), "\n") {
				if index := strings.Index(line, "[ (T)HEME ]"); index >= 0 {
					x := lipgloss.Width(line[:index])
					if y != g.footerY+1 || m.footerButtonAt(x, y) != footerButtonTheme {
						t.Fatal("reset notice displaced footer clicks")
					}
				}
			}
		}
		if len(f.keys) != 1 {
			t.Fatal("wrong number of redemptions")
		}
	}
}

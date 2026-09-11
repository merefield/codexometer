package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
)

type specificResetFake struct {
	resetFake
	ids []string
}

func (f *specificResetFake) ConsumeResetCredit(ctx context.Context, key, account, id string) (string, error) {
	f.ids = append(f.ids, id)
	return f.ConsumeReset(ctx, key, account)
}

func credit(id string, after time.Duration) codex.ResetCredit {
	expiry := time.Now().Add(after).Unix()
	return codex.ResetCredit{ID: id, Status: "available", ResetType: "codexRateLimits", ExpiresAt: &expiry, Title: id}
}

func TestResetCreditOrderingAndWarning(t *testing.T) {
	m, _ := resetModel()
	m.snapshot.RateLimits.Primary.UsedPercent = 1
	m.snapshot.RateLimitResetCredits.AvailableCount = 8
	noExpiry := credit("forever", time.Hour)
	noExpiry.ExpiresAt = nil
	redeemed := credit("redeemed", time.Minute)
	redeemed.Status = "redeemed"
	unknown := credit("unknown", time.Minute)
	unknown.ResetType = "unknown"
	m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{noExpiry, credit("later", 80*time.Hour), credit("first", time.Hour), credit("expired", -time.Hour), redeemed, unknown}
	got := m.availableResetCredits()
	if len(got) != 3 || got[0].ID != "first" || got[2].ID != "forever" {
		t.Fatalf("order: %#v", got)
	}
	if !m.resetExpiringSoon() || m.resetControlsLayout(100).warning == "" || m.resetLabel() != "[ RESET // 8 ]" {
		t.Fatal("expiry did not bypass threshold")
	}
	m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("later", 73*time.Hour)}
	if m.resetLabel() != "" {
		t.Fatal("warned too early")
	}
	m.meterView = viewResets
	if m.resetLabel() == "" {
		t.Fatal("resets view inaccessible below threshold")
	}
	m.snapshot.FetchedAt = time.Now().Add(-3 * time.Minute)
	if m.resetLabel() != "" {
		t.Fatal("stale source allowed redemption")
	}
}

func TestResetSelectionBoundToConfirmationAndRetry(t *testing.T) {
	m, _ := resetModel()
	f := &specificResetFake{}
	m.fetcher = f
	m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("later", 24*time.Hour), credit("first", time.Hour)}
	u, cmd := m.pressQuotaReset()
	m = u.(Model)
	if cmd != nil || m.meterView != viewResets || m.resetCreditID != "first" {
		t.Fatal("did not open and arm earliest credit")
	}
	// A newer snapshot must not silently replace the already-confirmed choice.
	m.snapshot.RateLimitResetCredits.Credits = append(m.snapshot.RateLimitResetCredits.Credits, credit("new-first", time.Minute))
	m.snapshot.RateLimitResetCredits.AvailableCount = 3
	u, cmd = m.pressQuotaReset()
	m = u.(Model)
	if cmd == nil {
		t.Fatal("confirmation did not submit")
	}
	_ = cmd()
	if len(f.ids) != 1 || f.ids[0] != "first" {
		t.Fatalf("selected %v", f.ids)
	}
	key := m.resetKey
	m.resetBusy = false // Simulate an uncertain response: preserve the attempt.
	m.snapshot.RateLimitResetCredits.Credits = nil
	u, _ = m.pressQuotaReset()
	m = u.(Model)
	u, cmd = m.pressQuotaReset()
	m = u.(Model)
	if cmd == nil {
		t.Fatal("retry unavailable")
	}
	_ = cmd()
	if m.resetKey != key || len(f.ids) != 2 || f.ids[1] != "first" {
		t.Fatal("retry changed credit or key")
	}
}

func TestResetDisappearingCreditRequiresNewConfirmation(t *testing.T) {
	m, _ := resetModel()
	m.fetcher = &specificResetFake{}
	m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("first", time.Hour)}
	u, _ := m.pressQuotaReset()
	m = u.(Model)
	m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("replacement", time.Hour)}
	u, cmd := m.pressQuotaReset()
	m = u.(Model)
	if cmd != nil || m.resetKey != "" || !strings.Contains(m.resetNotice, "no longer available") {
		t.Fatal("silently substituted credit")
	}
}

func TestResetConfirmationRejectsStaleData(t *testing.T) {
	m, _ := resetModel()
	u, _ := m.pressQuotaReset()
	m = u.(Model)
	m.snapshot.FetchedAt = time.Now().Add(-3 * time.Minute)
	u, cmd := m.pressQuotaReset()
	m = u.(Model)
	if cmd != nil || m.resetKey != "" || !strings.Contains(m.resetNotice, "not fresh") {
		t.Fatal("submitted with stale data")
	}
}

func TestResetDetailsMissingAndPartial(t *testing.T) {
	m, _ := resetModel()
	colors := paletteFor(m.theme)
	if !strings.Contains(strings.Join(m.resetDetailLines(100, colors), "\n"), "Expiry information unavailable") {
		t.Fatal("missing details presented as no resets")
	}
	m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("one", 24*time.Hour)}
	lines := strings.Join(m.resetDetailLines(100, colors), "\n")
	if !strings.Contains(lines, "Showing 1 of 2") || !strings.Contains(lines, "Expires") {
		t.Fatal("missing partial disclosure")
	}
}

func TestResetSafetyMessages(t *testing.T) {
	for _, mode := range []string{"missing", "partial", "complete", "non-expiring", "none"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := resetModel()
			m.fetcher = &specificResetFake{}
			if mode != "missing" {
				m.snapshot.RateLimitResetCredits.Credits = []codex.ResetCredit{credit("known", time.Hour)}
			}
			if mode == "complete" || mode == "non-expiring" {
				m.snapshot.RateLimitResetCredits.AvailableCount = 1
			}
			if mode == "non-expiring" {
				m.snapshot.RateLimitResetCredits.Credits[0].ExpiresAt = nil
			}
			if mode == "none" {
				m.snapshot.RateLimitResetCredits.AvailableCount = 0
			}
			notice := m.resetExpiryDataNotice()
			if mode == "missing" && !strings.Contains(notice, "No warning does not mean no expiry") {
				t.Fatal("missing data implies safety")
			}
			if mode == "partial" && !strings.Contains(notice, "Other credits may expire sooner") {
				t.Fatal("partial inventory implies global earliest expiry")
			}
			if mode != "missing" && mode != "partial" && notice != "" {
				t.Fatal("invented missing data")
			}
			body := strings.Join(m.resetDetailLines(500, paletteFor(m.theme)), "\n")
			if notice != "" && !strings.Contains(body, notice) {
				t.Fatal("inventory omitted disclosure")
			}
			if mode == "non-expiring" && !strings.Contains(body, "Does not expire") {
				t.Fatal("non-expiring credit confused with missing information")
			}
			if mode == "none" {
				return
			}
			u, cmd := m.pressQuotaReset()
			m = u.(Model)
			if cmd != nil || !strings.Contains(m.resetNotice, "Unused allowance does not carry over or stack") || !strings.Contains(m.resetNotice, "weekly reset schedule") {
				t.Fatal("confirmation omitted trade-off")
			}
			if notice != "" && !strings.Contains(m.resetNotice, notice) {
				t.Fatal("confirmation omitted disclosure")
			}
		})
	}
}

func TestResetsResponsiveGeometryAndScroll(t *testing.T) {
	for _, width := range []int{28, 40, 80, 140} {
		m, _ := resetModel()
		m.width, m.height, m.meterView = width, 24, viewResets
		m.snapshot.RateLimitResetCredits.AvailableCount = 12
		for range 12 {
			m.snapshot.RateLimitResetCredits.Credits = append(m.snapshot.RateLimitResetCredits.Credits, credit("Reset", time.Hour))
		}
		g := m.dashboardLayout()
		rendered := m.renderResets(g.contentWidth, g.meterHeight, paletteFor(m.theme))
		if lipgloss.Width(rendered) > g.contentWidth || lipgloss.Height(rendered) != g.meterHeight {
			t.Fatalf("%d: wrong dimensions", width)
		}
		row := g.tabsY + m.resetControlsLayout(g.contentWidth).buttonY
		label := m.resetLabel()
		for x := 2 + g.contentWidth - lipgloss.Width(label); x < 2+g.contentWidth; x++ {
			if !m.resetAt(x, row) {
				t.Fatalf("%d missed reset cell %d,%d", width, x, row)
			}
		}
		u, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = u.(Model)
		if m.resetScroll == 0 {
			t.Fatal("page down did not scroll")
		}
	}
}

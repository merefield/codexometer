package ui

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type resetConsumer interface {
	ConsumeReset(context.Context, string, string) (string, error)
}

// SetResetThreshold sets the consumed percentage required to offer a reset.
// Zero bypasses consumption checks, but still requires an available credit.
func (m *Model) SetResetThreshold(percent int) { m.resetThreshold = min(max(percent, 0), 100) }

func (m Model) renderResetButton(label string, colors palette) string {
	style := colors.label()
	if m.resetHovered && !m.resetBusy {
		style = style.Foreground(colors.background).Background(colors.primary)
	}
	return style.Render(label)
}

type quotaResetResult struct {
	outcome string
	err     error
}

func (m Model) renderResetNotice(width int) string {
	c := paletteFor(m.theme)
	return frame(width, "QUOTA RESET", c.label().Render(ansi.Hardwrap(m.resetNotice, max(width-4, 1), true)), c.primary, c)
}

func (m Model) resetNoticeHeight(width int) int {
	if !m.meterView.isQuota() || m.resetNotice == "" {
		return 0
	}
	return lipgloss.Height(m.renderResetNotice(width))
}

func (m Model) resetLabel() string {
	if !m.meterView.isQuota() {
		return ""
	}
	if m.resetBusy {
		return "[ RESETTING… ]"
	}
	if !m.resetConfirmUntil.IsZero() {
		return "[ CONFIRM RESET ]"
	}
	if m.resetKey != "" {
		return "[ RETRY RESET ]"
	}
	_, supported := m.fetcher.(resetConsumer)
	if !supported || m.loading || m.err != nil || m.snapshot.AccountFingerprint == "" ||
		m.snapshot.RateLimitResetCredits == nil || m.snapshot.RateLimitResetCredits.AvailableCount <= 0 ||
		m.snapshot.FetchedAt.IsZero() || time.Since(m.snapshot.FetchedAt) > 2*m.refreshEvery {
		return ""
	}
	if m.resetThreshold > 0 {
		eligible := false
		for _, meter := range m.snapshot.Meters() {
			if meter.Window.UsedPercent >= m.resetThreshold {
				eligible = true
				break
			}
		}
		if !eligible {
			return ""
		}
	}
	return fmt.Sprintf("[ RESET // %d ]", m.snapshot.RateLimitResetCredits.AvailableCount)
}

// Reserve the same horizontal span for rendering and hit testing.
func (m Model) resetLayout(width int) (int, string) {
	label := m.resetLabel()
	if label == "" || width < lipgloss.Width(label)+12 {
		return width, ""
	}
	return width - lipgloss.Width(label) - 1, label
}

func (m Model) resetAt(x, y int) bool {
	g := m.dashboardLayout()
	w, label := m.resetLayout(g.contentWidth)
	if m.resetOwnRow(g.contentWidth) {
		label = m.resetLabel()
		return y == g.tabsY+1 && x >= 2+max(g.contentWidth-lipgloss.Width(label), 0) && x < 2+g.contentWidth
	}
	return label != "" && !(m.loading && len(m.snapshot.Meters()) == 0) && y == g.tabsY && x >= 2+w+1 && x < 2+g.contentWidth
}

func (m Model) resetOwnRow(width int) bool {
	label := m.resetLabel()
	return label != "" && width < lipgloss.Width(label)+12
}

func (m Model) pressQuotaReset() (tea.Model, tea.Cmd) {
	if m.resetBusy || m.resetLabel() == "" {
		return m, nil
	}
	if m.resetConfirmUntil.IsZero() || time.Now().After(m.resetConfirmUntil) {
		if m.resetKey == "" {
			m.resetAccount = m.snapshot.AccountFingerprint
		}
		m.resetConfirmUntil = time.Now().Add(10 * time.Second)
		m.resetNotice = "Use one reset? Refreshes eligible quota and changes the weekly reset schedule. Click CONFIRM; Esc cancels."
		return m, nil
	}
	m.resetConfirmUntil = time.Time{}
	if m.resetAccount != m.snapshot.AccountFingerprint {
		m.resetNotice = "Account changed; reset cancelled."
		return m, nil
	}
	if m.resetKey == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			m.resetNotice = err.Error()
			return m, nil
		}
		id[6] = (id[6] & 15) | 64
		id[8] = (id[8] & 63) | 128
		m.resetKey = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	}
	m.resetBusy = true
	m.resetRevision++
	m.resetNotice = "Resetting quota…"
	consumer := m.fetcher.(resetConsumer)
	key, account := m.resetKey, m.resetAccount
	return m, func() tea.Msg {
		outcome, err := consumer.ConsumeReset(context.Background(), key, account)
		return quotaResetResult{outcome, err}
	}
}

package ui

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type resetConsumer interface {
	ConsumeReset(context.Context, string, string) (string, error)
}

type resetCreditConsumer interface {
	ConsumeResetCredit(context.Context, string, string, string) (string, error)
}

// MaxResetWarningHours prevents overflow when converting CLI hours to duration.
const MaxResetWarningHours = int((1<<63 - 1) / int64(time.Hour))

func (m *Model) SetResetWarningHours(hours int) {
	m.resetWarningHours = min(max(hours, 0), MaxResetWarningHours)
}

func (m Model) resetExpiryWarning() time.Duration {
	return time.Duration(m.resetWarningHours) * time.Hour
}

// Details may be absent or capped. Never infer expiry from the quota window.
func (m Model) availableResetCredits() []codex.ResetCredit {
	summary := m.snapshot.RateLimitResetCredits
	if summary == nil || summary.AvailableCount <= 0 {
		return nil
	}
	credits := make([]codex.ResetCredit, 0, len(summary.Credits))
	for _, credit := range summary.Credits {
		if credit.ID != "" && credit.Status == "available" && credit.ResetType == "codexRateLimits" && (credit.ExpiresAt == nil || *credit.ExpiresAt > time.Now().Unix()) {
			credits = append(credits, credit)
		}
	}
	slices.SortStableFunc(credits, func(a, b codex.ResetCredit) int {
		if a.HasKnownExpiry() != b.HasKnownExpiry() {
			if a.HasKnownExpiry() {
				return -1
			}
			return 1
		}
		if a.ExpiresAt == nil && b.ExpiresAt == nil {
			return strings.Compare(a.ID, b.ID)
		}
		if a.ExpiresAt == nil {
			return 1
		}
		if b.ExpiresAt == nil {
			return -1
		}
		if *a.ExpiresAt < *b.ExpiresAt {
			return -1
		}
		if *a.ExpiresAt > *b.ExpiresAt {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return credits[:min(len(credits), summary.AvailableCount)]
}

func (m Model) resetExpiringSoon() bool {
	credits := m.availableResetCredits()
	return m.resetWarningHours > 0 && len(credits) > 0 && credits[0].ExpiresAt != nil && time.Until(time.Unix(*credits[0].ExpiresAt, 0)) < m.resetExpiryWarning()
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
	return frame(width, i18n.Text("QUOTA RESET"), c.label().Render(ansi.Hardwrap(m.resetNotice, max(width-4, 1), true)), c.primary, c)
}

func (m Model) resetNoticeHeight(width int) int {
	if !m.meterView.isQuota() || m.resetNotice == "" {
		return 0
	}
	return lipgloss.Height(m.renderResetNotice(width))
}

func (m Model) resetLabel() string {
	width := m.width
	if width == 0 {
		width = 80
	}
	return ansi.Truncate(m.resetFullLabel(), max(width-4, 1), "")
}

func (m Model) resetFullLabel() string {
	if !m.meterView.isQuota() {
		return ""
	}
	if m.resetBusy {
		return i18n.Text("[ RESETTING… ]")
	}
	if !m.resetConfirmUntil.IsZero() {
		return i18n.Text("[ CONFIRM RESET ]")
	}
	if m.resetKey != "" {
		return i18n.Text("[ RETRY RESET ]")
	}
	_, supported := m.fetcher.(resetConsumer)
	if !supported || m.loading || m.err != nil || m.snapshot.AccountFingerprint == "" ||
		m.snapshot.RateLimitResetCredits == nil || m.snapshot.RateLimitResetCredits.AvailableCount <= 0 ||
		m.snapshot.FetchedAt.IsZero() || time.Since(m.snapshot.FetchedAt) > 2*m.refreshEvery {
		return ""
	}
	if m.resetThreshold > 0 && m.meterView != viewResets && !m.resetExpiringSoon() {
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
	return i18n.Format("[ RESET // %d ]", m.snapshot.RateLimitResetCredits.AvailableCount)
}

type resetControls struct {
	button, warning                      string
	buttonX, buttonY, warningX, warningY int
	tabsWidth, extraRows                 int
}

// One geometry source for rendering, tab allocation and both click surfaces.
func (m Model) resetControlsLayout(width int) resetControls {
	c := resetControls{tabsWidth: width, button: m.resetLabel()}
	if c.button == "" {
		return c
	}
	c.buttonX = max(width-lipgloss.Width(c.button), 0)
	credits := m.availableResetCredits()
	if m.resetWarningHours > 0 && len(credits) > 0 && credits[0].ExpiresAt != nil && time.Until(time.Unix(*credits[0].ExpiresAt, 0)) < m.resetExpiryWarning() && !m.loading && m.err == nil && !m.snapshot.FetchedAt.IsZero() && time.Since(m.snapshot.FetchedAt) <= 2*m.refreshEvery {
		hours := max(int(time.Until(time.Unix(*credits[0].ExpiresAt, 0)).Hours()), 0)
		remaining := fmt.Sprintf("%dH", hours)
		if hours >= 24 {
			remaining = fmt.Sprintf("%dD %dH", hours/24, hours%24)
		} else if hours == 0 {
			remaining = "<1H"
		}
		c.warning = i18n.Format("⚠ RESET EXPIRES IN %s", remaining)
		if lipgloss.Width(c.warning)+1+lipgloss.Width(c.button)+12 > width {
			if hours >= 24 {
				remaining = fmt.Sprintf("%dD", hours/24)
			}
			c.warning = i18n.Format("⚠ EXPIRES %s", remaining)
		}
	}
	total := lipgloss.Width(c.button)
	if c.warning != "" {
		total += lipgloss.Width(c.warning) + 1
	}
	if total+12 <= width {
		c.tabsWidth = width - total - 1
	} else {
		c.extraRows, c.buttonY, c.warningY = 1, 1, 1
		if total > width && c.warning != "" {
			c.extraRows, c.buttonY = 2, 2
			c.warning = ansi.Truncate(c.warning, width, "")
		}
	}
	c.warningX = max(c.buttonX-lipgloss.Width(c.warning)-1, 0)
	if c.warningY != c.buttonY {
		c.warningX = max(width-lipgloss.Width(c.warning), 0)
	}
	return c
}

func (m Model) resetLayout(width int) (int, string) {
	c := m.resetControlsLayout(width)
	if c.extraRows > 0 {
		return width, ""
	}
	return c.tabsWidth, c.button
}

func (m Model) resetAt(x, y int) bool {
	g := m.dashboardLayout()
	c := m.resetControlsLayout(g.contentWidth)
	return c.button != "" && !(m.loading && len(m.snapshot.Meters()) == 0) && y == g.tabsY+c.buttonY && x >= 2+c.buttonX && x < 2+c.buttonX+lipgloss.Width(c.button)
}

func (m Model) resetWarningAt(x, y int) bool {
	g := m.dashboardLayout()
	c := m.resetControlsLayout(g.contentWidth)
	return c.warning != "" && !(m.loading && len(m.snapshot.Meters()) == 0) && y == g.tabsY+c.warningY && x >= 2+c.warningX && x < 2+c.warningX+lipgloss.Width(c.warning)
}

func (m Model) renderResetControls(width int, tabs string, colors palette) string {
	c := m.resetControlsLayout(width)
	if c.button == "" {
		return tabs
	}
	rows := []string{tabs}
	for range c.extraRows {
		rows = append(rows, "")
	}
	if c.warning != "" {
		style := colors.label().Foreground(colors.warning)
		if m.resetWarningHovered {
			style = style.Underline(true)
		}
		rows[c.warningY] += strings.Repeat(" ", max(c.warningX-lipgloss.Width(rows[c.warningY]), 0)) + style.Render(c.warning)
	}
	rows[c.buttonY] += strings.Repeat(" ", max(c.buttonX-lipgloss.Width(rows[c.buttonY]), 0)) + m.renderResetButton(c.button, colors)
	return strings.Join(rows, "\n")
}

func (m Model) resetOwnRow(width int) bool {
	return m.resetControlsLayout(width).extraRows > 0
}

func (m Model) pressQuotaReset() (tea.Model, tea.Cmd) {
	if m.resetBusy || m.resetLabel() == "" {
		return m, nil
	}
	consumer, supported := m.fetcher.(resetConsumer)
	if !supported {
		m.resetConfirmUntil = time.Time{}
		m.resetNotice = "Reset unavailable: this quota source does not support redemption. No reset submitted."
		return m, nil
	}
	if m.resetConfirmUntil.IsZero() || time.Now().After(m.resetConfirmUntil) {
		if m.resetKey == "" {
			m.resetAccount = m.snapshot.AccountFingerprint
			m.resetCreditID = ""
			if credits := m.availableResetCredits(); len(credits) > 0 && credits[0].HasKnownExpiry() {
				if _, ok := m.fetcher.(resetCreditConsumer); ok {
					m.resetCreditID = credits[0].ID
				}
			}
		}
		m.meterView, m.quotaMeterView = viewResets, viewResets
		m.resetScroll = 0
		m.persistPreferences()
		m.resetConfirmUntil = time.Now().Add(10 * time.Second)
		m.resetNotice = i18n.Text("Use one reset? Refreshes eligible quota and changes the weekly reset schedule. Click CONFIRM; Esc cancels.")
		m.resetNotice += " " + i18n.Text("Unused allowance does not carry over or stack.")
		if notice := m.resetExpiryDataNotice(); notice != "" {
			m.resetNotice += " " + notice
		}
		if m.resetCreditID == "" {
			m.resetNotice += " " + i18n.Text("Expiry order unavailable; backend chooses the credit.")
		} else {
			for _, credit := range m.availableResetCredits() {
				if credit.ID == m.resetCreditID {
					m.resetNotice += " Selected: " + resetCreditTitle(credit) + " — " + resetCreditExpiry(credit) + "."
					break
				}
			}
		}
		return m, nil
	}
	m.resetConfirmUntil = time.Time{}
	if m.resetAccount != m.snapshot.AccountFingerprint {
		m.resetNotice = "Account changed; reset cancelled."
		return m, nil
	}
	if m.resetKey == "" {
		if m.loading || m.err != nil || m.snapshot.FetchedAt.IsZero() || time.Since(m.snapshot.FetchedAt) > 2*m.refreshEvery {
			m.resetNotice = "Quota data is not fresh; refresh and confirm again. No reset submitted."
			return m, nil
		}
		if m.snapshot.RateLimitResetCredits == nil || m.snapshot.RateLimitResetCredits.AvailableCount <= 0 {
			m.resetNotice = "No reset credits available; no reset submitted."
			return m, nil
		}
		if m.resetCreditID != "" {
			found := false
			for _, credit := range m.availableResetCredits() {
				if credit.ID == m.resetCreditID && credit.HasKnownExpiry() {
					found = true
					break
				}
			}
			if !found {
				m.resetNotice = "Selected reset is no longer available; review and confirm again."
				return m, nil
			}
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			m.resetNotice = "Could not generate a reset request ID; no reset submitted: " + err.Error()
			return m, nil
		}
		id[6] = (id[6] & 15) | 64
		id[8] = (id[8] & 63) | 128
		m.resetKey = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	}
	m.resetBusy = true
	m.resetRevision++
	m.resetNotice = "Resetting quota…"
	key, account, creditID := m.resetKey, m.resetAccount, m.resetCreditID
	return m, func() tea.Msg {
		if creditID != "" {
			if specific, ok := consumer.(resetCreditConsumer); ok {
				outcome, err := specific.ConsumeResetCredit(context.Background(), key, account, creditID)
				return quotaResetResult{outcome, err}
			}
			return quotaResetResult{err: fmt.Errorf("selected-credit redemption is unavailable; no reset submitted")}
		}
		outcome, err := consumer.ConsumeReset(context.Background(), key, account)
		return quotaResetResult{outcome, err}
	}
}

func resetCreditTitle(c codex.ResetCredit) string {
	if strings.TrimSpace(c.Title) != "" {
		return codex.SanitizeSessionContext(c.Title)
	}
	return "Full reset"
}

func resetCreditExpiry(c codex.ResetCredit) string {
	if !c.HasKnownExpiry() {
		return i18n.Text("Expiry information unavailable.")
	}
	if c.ExpiresAt == nil {
		return "Does not expire"
	}
	return "Expires " + time.Unix(*c.ExpiresAt, 0).Local().Format("02 Jan 2006 15:04 MST")
}

func (m Model) resetExpiryDataNotice() string {
	summary := m.snapshot.RateLimitResetCredits
	if summary != nil && summary.AvailableCount <= 0 {
		return ""
	}
	known := 0
	for _, credit := range m.availableResetCredits() {
		if credit.HasKnownExpiry() {
			known++
		}
	}
	if known == 0 {
		return i18n.Text("Expiry information unavailable. No warning does not mean no expiry.")
	}
	if known < summary.AvailableCount {
		return i18n.Text("Expiry information incomplete. Other credits may expire sooner.")
	}
	return ""
}

func (m Model) resetDetailLines(width int, colors palette) (result []string) {
	defer func() { result = strings.Split(ansi.Hardwrap(strings.Join(result, "\n"), max(width, 1), true), "\n") }()
	summary := m.snapshot.RateLimitResetCredits
	if summary == nil {
		return []string{"Reset information unavailable.", m.resetExpiryDataNotice()}
	}
	lines := []string{fmt.Sprintf("AVAILABLE // %d", summary.AvailableCount)}
	credits := m.availableResetCredits()
	if notice := m.resetExpiryDataNotice(); notice != "" {
		lines = append(lines, colors.label().Foreground(colors.warning).Render(notice))
	}
	if m.resetExpiringSoon() {
		lines = append(lines, colors.label().Foreground(colors.warning).Render(i18n.Format("EXPIRING SOON // within %d hours", m.resetWarningHours)))
	}
	if summary.AvailableCount == 0 {
		return append(lines, "No resets available.")
	}
	if len(credits) == 0 {
		return append(lines, i18n.Text("Expiry order unavailable; backend chooses the credit."))
	}
	lines = append(lines, fmt.Sprintf("Showing %d of %d available resets. Earliest known expiry first.", len(credits), summary.AvailableCount))
	for index, credit := range credits {
		title := fmt.Sprintf("%d // %s", index+1, resetCreditTitle(credit))
		if credit.ID == m.resetCreditID && (!m.resetConfirmUntil.IsZero() || m.resetKey != "") {
			title += " // SELECTED"
		} else if index == 0 && credit.HasKnownExpiry() {
			title += " // NEXT"
		}
		lines = append(lines, "", colors.label().Render(title), resetCreditExpiry(credit))
		if credit.GrantedAt > 0 {
			lines = append(lines, "Granted "+time.Unix(credit.GrantedAt, 0).Local().Format("02 Jan 2006 15:04 MST"))
		}
		if credit.Description != "" {
			lines = append(lines, codex.SanitizeSessionContext(credit.Description))
		}
	}
	return lines
}

func (m Model) renderResets(width, height int, colors palette) string {
	lines := m.resetDetailLines(max(width-4, 1), colors)
	rows := max(height-2, 1)
	start := min(m.resetScroll, max(len(lines)-rows, 0))
	title := i18n.Text("RESETS")
	if len(lines) > rows {
		title += " // ↑↓ PgUp/PgDn"
	}
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(frameSized(width, max(height-2, 1), title, strings.Join(lines[start:min(start+rows, len(lines))], "\n"), colors.primary, colors))
}

package ui

import (
	"fmt"
	imagecolor "image/color"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = !m.inline
	view.MouseMode = tea.MouseModeAllMotion
	return view
}

func (m Model) render() string {
	colors := paletteFor(m.theme)
	layout := m.dashboardLayout()
	contentWidth := layout.contentWidth

	account := ""
	if !m.loading || len(m.snapshot.Meters()) > 0 {
		account = m.renderAccount(colors)
	}
	header := renderHeader(contentWidth, m.phase, m.renderSignalStatus(contentWidth, colors), account, m.appVersion, colors)
	parts := []string{header}
	if m.meterView != viewUsage && m.loading && len(m.snapshot.Meters()) == 0 {
		parts = append(parts, renderBoot(contentWidth, m.phase, colors))
	} else {
		if layout.headerSpacer {
			parts = append(parts, strings.Repeat(" ", contentWidth))
		}
		parts = append(parts, m.renderMainTabs(contentWidth, colors))
		if m.meterView.isQuota() {
			parts = append(parts, m.renderQuotaViewTabs(contentWidth, colors))
		}
		if m.err != nil && m.meterView != viewUsage {
			errorView := renderError(contentWidth, m.err, colors)
			parts = append(parts, errorView)
		}
		if m.meterView.isQuota() && m.resetNotice != "" {
			parts = append(parts, m.renderResetNotice(contentWidth))
		}
		meters := m.snapshot.Meters()
		if m.meterView.isQuota() {
			meters = m.quotaMetersWithInsights(contentWidth)
		}
		if len(meters) == 0 && m.meterView != viewUsage {
			emptyView := renderError(contentWidth, fmt.Errorf("no quota windows returned"), colors)
			parts = append(parts, emptyView)
		}
		footer := m.renderFooter(contentWidth, colors)
		if m.meterView == viewUsage {
			parts = append(parts, m.renderHistory(contentWidth, layout.meterHeight, colors))
		} else if m.meterView == viewMonitor {
			parts = append(parts, m.renderMonitorArea(contentWidth, layout.meterHeight, colors).view)
		} else if m.meterView == viewBenchmark {
			parts = append(parts, m.renderBenchmarkArea(contentWidth, layout.meterHeight, colors))
		} else if len(meters) > 1 {
			parts = append(parts, renderMeterGrid(contentWidth, layout.meterHeight, meters, m.meterView, colors))
		} else if len(meters) == 1 {
			parts = append(parts, renderMeterArea(contentWidth, layout.meterHeight, meters[0], m.meterView, colors))
		}
		parts = append(parts, footer)
	}

	panel := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.NewStyle().Margin(1, 2).Render(panel)
}

func renderHeader(width, phase int, signal, account, appVersion string, colors palette) string {
	displayedVersion := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(appVersion, "v")))
	if displayedVersion == "" {
		displayedVersion = "DEVELOPMENT"
	}
	if width < 64 {
		title := colors.header().Render("▰ CODEXOMETER ▰")
		subtitle := colors.dimmed().Render(ansi.Truncate("QUOTA TELEMETRY // VERSION "+displayedVersion, width, ""))
		return joinRight(title, signal, width) + "\n" + joinRight(subtitle, account, width)
	}
	logo := []string{
		"█▀▀ █▀█ █▀▄ █▀▀ ▀▄▀ █▀█ █▀▄▀█ █▀▀ ▀█▀ █▀▀ █▀█",
		"█▄▄ █▄█ █▄▀ ██▄ █ █ █▄█ █ ▀ █ ██▄  █  ██▄ █▀▄",
	}
	beacon := []string{"◉", "◎", "◌", "◎"}[phase%4]
	subtitle := colors.dimmed().Render(fmt.Sprintf("%s QUOTA TELEMETRY CONSOLE · VERSION %s", beacon, displayedVersion))
	return joinRight(colors.header().Render(logo[0]), signal, width) + "\n" +
		colors.header().Render(logo[1]) + "\n" +
		joinRight(subtitle, account, width)
}

func renderBoot(width, phase int, colors palette) string {
	trackWidth := max(width-18, 8)
	position := phase % trackWidth
	track := strings.Repeat("·", position) + "◆" + strings.Repeat("·", trackWidth-position-1)
	return frame(width, "ACQUIRING SIGNAL", colors.label().Render(track)+"\n"+colors.dimmed().Render("HANDSHAKE WITH CODEX APP-SERVER IN PROGRESS"), colors.primary, colors)
}

func (m Model) renderAccount(colors palette) string {
	plan := "UNKNOWN PLAN"
	if m.snapshot.RateLimits.PlanType != nil {
		plan = codex.DisplayName(*m.snapshot.RateLimits.PlanType)
	}
	return colors.dimmed().Render("ACCOUNT // " + plan)
}

func (m Model) renderSignalStatus(width int, colors palette) string {
	if m.loading {
		return renderColoredSignal("SCANNING", colors.accent)
	}
	if m.err != nil {
		return renderColoredSignal("STALE SIGNAL", colors.warning)
	}
	signal := snapshotQuotaSignal(m.snapshot, time.Now())
	semantic := lipgloss.NewStyle().Bold(true).Foreground(signal.health.color(colors))
	online := lipgloss.NewStyle().Bold(true).Foreground(colors.primary)
	if width < 24 {
		return semantic.Render(ansi.Truncate("● "+signal.compactLabel(), width, ""))
	}
	if width < 48 {
		label := ansi.Truncate(signal.compactLabel(), max(width-8, 1), "")
		return semantic.Render("●") + online.Render(" ON // ") + semantic.Render(label)
	}
	label := ansi.Truncate(signal.label(), max(width-12, 1), "")
	return semantic.Render("●") + online.Render(" ONLINE // ") + semantic.Render(label)
}

func renderColoredSignal(label string, color imagecolor.Color) string {
	return lipgloss.NewStyle().Bold(true).Foreground(color).Render("● " + label)
}

func joinRight(left, right string, width int) string {
	if right == "" {
		return left
	}
	if lipgloss.Width(right) >= width {
		return ansi.Truncate(right, width, "")
	}
	availableLeft := max(width-lipgloss.Width(right)-1, 0)
	if lipgloss.Width(left) > availableLeft {
		left = ansi.Truncate(left, availableLeft, "")
	}
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) renderFooter(width int, colors palette) string {
	remaining := time.Until(m.nextRefresh)
	if remaining < 0 {
		remaining = 0
	}
	left := fmt.Sprintf("AUTO-SCAN %s", compactDuration(remaining))
	if m.snapshot.RateLimitResetCredits != nil && m.snapshot.RateLimitResetCredits.AvailableCount > 0 {
		left += fmt.Sprintf("  //  RESET TOKENS %d", m.snapshot.RateLimitResetCredits.AvailableCount)
	}
	if credits, ok := m.snapshot.CreditStatus(); ok {
		switch {
		case credits.Unlimited:
			left += "  //  CREDITS UNLIMITED"
		case credits.Balance != nil && strings.TrimSpace(*credits.Balance) != "":
			left += "  //  CREDITS " + strings.TrimSpace(*credits.Balance)
		case credits.HasCredits:
			left += "  //  CREDITS AVAILABLE"
		}
	}
	status := colors.dimmed().Render(ansi.Truncate(left, width, ""))
	if m.meterView == viewBenchmark || m.meterView.isQuota() {
		status = renderPricingFooter(status, width, colors)
	}
	buttons, separator := footerButtonLayoutWithTheme(width, colors.name, m.meterView.isQuota())
	controls := make([]string, 0, len(buttons))
	for _, button := range buttons {
		controls = append(controls, footerButtonAppearance(
			colors,
			button.id == m.hoveredButton,
			button.id == m.flashedButton,
		).Render(button.label))
	}
	theme := colors.dimmed().Render(fmt.Sprintf("THEME // %s", colors.name))
	controlRow := joinRight(strings.Join(controls, separator), theme, width)
	return status + "\n" + controlRow
}

func renderPricingFooter(left string, width int, colors palette) string {
	const minimumWidth = 80
	if width < minimumWidth {
		return left
	}
	label := "PRICES RETRIEVED " + codex.StandardAPIPricingRetrievedOn + " // OPENAI.COM"
	center := pricingHyperlink(label, colors)
	start := (width - lipgloss.Width(center)) / 2
	if lipgloss.Width(left)+2 > start {
		return left
	}
	return left + strings.Repeat(" ", start-lipgloss.Width(left)) + center
}

func pricingHyperlink(label string, colors palette) string {
	styled := colors.dimmed().Underline(true).Render(label)
	return ansi.SetHyperlink(codex.StandardAPIPricingSourceURL) + styled + ansi.ResetHyperlink()
}

func footerButtonAppearance(colors palette, hovered, flashed bool) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(colors.dim).Background(colors.background)
	if hovered {
		style = style.Bold(true).Foreground(colors.primary)
	}
	if flashed {
		style = style.Bold(true).Foreground(colors.background).Background(colors.primary)
	}
	return style
}

func renderError(width int, err error, colors palette) string {
	message := err.Error()
	if len(message) > width-4 {
		message = message[:max(width-7, 0)] + "..."
	}
	return frame(width, "SIGNAL FAULT", lipgloss.NewStyle().Foreground(colors.danger).Render(message), colors.danger, colors)
}

func frame(width int, title, body string, color imagecolor.Color, colors palette) string {
	return frameSized(width, 0, title, body, color, colors)
}

func frameSized(width, height int, title, body string, color imagecolor.Color, colors palette) string {
	return frameSizedWithTitleAction(width, height, title, "", body, color, colors)
}

func frameSizedWithTitleAction(width, height int, title, action, body string, color imagecolor.Color, colors palette) string {
	return frameSizedWithActions(width, height, title, action, "", body, color, colors)
}

func frameSizedWithActions(width, height int, title, titleAction, footerAction, body string, color imagecolor.Color, colors palette) string {
	width = max(width, 1)
	style := lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(color).
		Foreground(colors.primary).
		Background(colors.background)
	if height > 0 {
		// Lip Gloss v2 includes the bottom border in Height. The title is
		// rendered separately, so preserve the existing body allocation by
		// reserving one extra row for that border.
		style = style.Height(height + 1)
	}
	renderedBody := style.Render(body)
	if footerAction != "" {
		lines := strings.Split(renderedBody, "\n")
		lines[len(lines)-1] = renderFrameFooterWithAction(width, footerAction, color, colors)
		renderedBody = strings.Join(lines, "\n")
	}
	return renderFrameTitleWithAction(width, title, titleAction, color, colors) + "\n" + renderedBody
}

func renderFrameTitle(width int, title string, color imagecolor.Color, colors palette) string {
	return renderFrameTitleWithAction(width, title, "", color, colors)
}

func renderFrameTitleWithAction(width int, title, action string, color imagecolor.Color, colors palette) string {
	width = max(width, 1)
	border := lipgloss.RoundedBorder()
	borderStyle := lipgloss.NewStyle().Foreground(color).Background(colors.background)
	if width == 1 {
		return borderStyle.Render(border.Top)
	}
	if width == 2 {
		return borderStyle.Render(border.TopLeft + border.TopRight)
	}

	actionWidth := lipgloss.Width(action)
	if actionWidth > 0 && width < actionWidth+8 {
		action = ""
		actionWidth = 0
	}
	titleWidth := max(width-6, 0)
	if actionWidth > 0 {
		titleWidth = max(width-actionWidth-6, 0)
	}
	title = strings.TrimSpace(ansi.Truncate(title, titleWidth, ""))
	if title == "" {
		return borderStyle.Render(border.TopLeft + strings.Repeat(border.Top, width-2) + border.TopRight)
	}
	prefix := border.TopLeft + border.Top + " "
	// Join the horizontal rule directly into the corner. Optional title actions
	// retain their leading padding, but must not leave a visual hole after them.
	suffix := borderStyle.Render(border.Top + border.TopRight)
	if actionWidth > 0 {
		suffix = borderStyle.Render(" ") + action + borderStyle.Render(border.Top+border.TopRight)
	}
	middleWidth := max(width-lipgloss.Width(prefix)-lipgloss.Width(title)-lipgloss.Width(suffix), 0)
	middle := ""
	if middleWidth > 0 {
		middle = " " + strings.Repeat(border.Top, middleWidth-1)
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(color).Background(colors.background)
	return borderStyle.Render(prefix) + titleStyle.Render(title) + borderStyle.Render(middle) + suffix
}

func renderFrameFooterWithAction(width int, action string, color imagecolor.Color, colors palette) string {
	width = max(width, 1)
	border := lipgloss.RoundedBorder()
	borderStyle := lipgloss.NewStyle().Foreground(color).Background(colors.background)
	if width == 1 {
		return borderStyle.Render(border.Bottom)
	}
	if width == 2 {
		return borderStyle.Render(border.BottomLeft + border.BottomRight)
	}
	actionWidth := lipgloss.Width(action)
	if actionWidth == 0 || width < actionWidth+4 {
		return borderStyle.Render(border.BottomLeft + strings.Repeat(border.Bottom, width-2) + border.BottomRight)
	}
	middleWidth := width - actionWidth - 4
	return borderStyle.Render(border.BottomLeft+strings.Repeat(border.Bottom, middleWidth)+" ") + action + borderStyle.Render(" "+border.BottomRight)
}

func countdown(at time.Time) string {
	return countdownFrom(time.Now(), at)
}

func countdownFrom(now, at time.Time) string {
	d := at.Sub(now)
	if d <= 0 {
		return "00:00:00"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	if hours > 99 {
		return fmt.Sprintf("%dD %02d:%02d", hours/24, hours%24, minutes)
	}
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func compactDuration(d time.Duration) string {
	seconds := int(math.Ceil(d.Seconds()))
	return fmt.Sprintf("%02d:%02d", max(seconds, 0)/60, max(seconds, 0)%60)
}

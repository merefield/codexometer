package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

func (m Model) View() string {
	colors := paletteFor(m.theme)
	layout := m.dashboardLayout()
	contentWidth := layout.contentWidth

	account := ""
	if !m.loading || len(m.snapshot.Meters()) > 0 {
		account = m.renderAccount(colors)
	}
	header := renderHeader(contentWidth, m.phase, m.renderSignalStatus(colors), account, colors)
	parts := []string{header}
	if m.loading && len(m.snapshot.Meters()) == 0 {
		parts = append(parts, renderBoot(contentWidth, m.phase, colors))
	} else {
		parts = append(parts, strings.Repeat(" ", contentWidth), m.renderStyleTabs(contentWidth, colors))
		if m.err != nil {
			errorView := renderError(contentWidth, m.err, colors)
			parts = append(parts, errorView)
		}
		meters := m.snapshot.Meters()
		if len(meters) == 0 {
			emptyView := renderError(contentWidth, fmt.Errorf("no quota windows returned"), colors)
			parts = append(parts, emptyView)
		}
		footer := m.renderFooter(contentWidth, colors)
		if m.meterStyle == styleMonitor {
			parts = append(parts, m.renderMonitorArea(contentWidth, layout.meterHeight, colors).view)
		} else if m.meterStyle == styleBenchmark {
			parts = append(parts, m.renderBenchmarkArea(contentWidth, layout.meterHeight, colors))
		} else if len(meters) > 1 {
			parts = append(parts, renderMeterGrid(contentWidth, layout.meterHeight, meters, m.meterStyle, colors))
		} else if len(meters) == 1 {
			parts = append(parts, renderMeterArea(contentWidth, layout.meterHeight, meters[0], m.meterStyle, colors))
		}
		parts = append(parts, footer)
	}

	panel := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.NewStyle().Margin(1, 2).Render(panel)
}

func renderHeader(width, phase int, signal, account string, colors palette) string {
	if width < 64 {
		title := colors.header().Render("▰ CODEXOMETER ▰")
		subtitle := colors.dimmed().Render("QUOTA TELEMETRY CONSOLE // CRT-01")
		return joinRight(title, signal, width) + "\n" + joinRight(subtitle, account, width)
	}
	logo := []string{
		"█▀▀ █▀█ █▀▄ █▀▀ ▀▄▀ █▀█ █▀▄▀█ █▀▀ ▀█▀ █▀▀ █▀█",
		"█▄▄ █▄█ █▄▀ ██▄ █ █ █▄█ █ ▀ █ ██▄  █  ██▄ █▀▄",
	}
	beacon := []string{"◉", "◎", "◌", "◎"}[phase%4]
	subtitle := colors.dimmed().Render(fmt.Sprintf("%s QUOTA TELEMETRY CONSOLE // CRT-01 // SIGNAL LOCKED", beacon))
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

func (m Model) renderSignalStatus(colors palette) string {
	status := "ONLINE"
	color := colors.primary
	if m.err != nil {
		status = "STALE SIGNAL"
		color = colors.warning
	}
	if m.loading {
		status = "SCANNING"
		color = colors.accent
	}
	return lipgloss.NewStyle().Bold(true).Foreground(color).Render("● " + status)
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
	status := left
	buttons, separator := footerButtonLayoutWithTheme(width, colors.name)
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
	return colors.dimmed().Render(status) + "\n" + controlRow
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

func frame(width int, title, body string, color lipgloss.Color, colors palette) string {
	return frameSized(width, 0, title, body, color, colors)
}

func frameSized(width, height int, title, body string, color lipgloss.Color, colors palette) string {
	width = max(width, 1)
	style := lipgloss.NewStyle().
		Width(max(width-2, 1)).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(color).
		Foreground(colors.primary).
		Background(colors.background)
	if height > 0 {
		style = style.Height(height)
	}
	return renderFrameTitle(width, title, color, colors) + "\n" + style.Render(body)
}

func renderFrameTitle(width int, title string, color lipgloss.Color, colors palette) string {
	width = max(width, 1)
	border := lipgloss.RoundedBorder()
	borderStyle := lipgloss.NewStyle().Foreground(color).Background(colors.background)
	if width == 1 {
		return borderStyle.Render(border.Top)
	}
	if width == 2 {
		return borderStyle.Render(border.TopLeft + border.TopRight)
	}

	title = strings.TrimSpace(ansi.Truncate(title, max(width-6, 0), ""))
	if title == "" {
		return borderStyle.Render(border.TopLeft + strings.Repeat(border.Top, width-2) + border.TopRight)
	}
	prefix := border.TopLeft + border.Top + " "
	suffixWidth := max(width-lipgloss.Width(prefix)-lipgloss.Width(title)-1, 1)
	suffix := " " + strings.Repeat(border.Top, suffixWidth-1) + border.TopRight
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(color).Background(colors.background)
	return borderStyle.Render(prefix) + titleStyle.Render(title) + borderStyle.Render(suffix)
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

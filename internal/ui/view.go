package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/merefield/codexometer/internal/codex"
)

func (m Model) View() string {
	colors := paletteFor(m.theme)
	width := m.width
	if width == 0 {
		width = 80
	}
	height := m.height
	if height == 0 {
		height = 24
	}
	contentWidth := max(width-4, 1)
	contentHeight := max(height-2, 1)

	header := renderHeader(contentWidth, m.phase, colors)
	parts := []string{header}
	if m.loading && len(m.snapshot.Meters()) == 0 {
		parts = append(parts, renderBoot(contentWidth, m.phase, colors))
	} else {
		status := m.renderStatus(contentWidth, colors)
		parts = append(parts, status)
		reservedHeight := lipgloss.Height(header) + lipgloss.Height(status)
		if m.err != nil {
			errorView := renderError(contentWidth, m.err, colors)
			parts = append(parts, errorView)
			reservedHeight += lipgloss.Height(errorView)
		}
		meters := m.snapshot.Meters()
		if len(meters) == 0 {
			emptyView := renderError(contentWidth, fmt.Errorf("no quota windows returned"), colors)
			parts = append(parts, emptyView)
			reservedHeight += lipgloss.Height(emptyView)
		}
		footer := m.renderFooter(contentWidth, colors)
		reservedHeight += lipgloss.Height(footer)
		meterHeight := max(contentHeight-reservedHeight, 1)
		if len(meters) > 1 {
			parts = append(parts, renderMeterGrid(contentWidth, meterHeight, meters, m.meterStyle, colors))
		} else if len(meters) == 1 {
			parts = append(parts, renderMeterArea(contentWidth, meterHeight, meters[0], m.meterStyle, colors))
		}
		parts = append(parts, footer)
	}

	panel := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.NewStyle().Margin(1, 2).Render(panel)
}

func renderHeader(width, phase int, colors palette) string {
	if width < 64 {
		return colors.header().Render("▰ CODEXOMETER ▰") + "\n" + colors.dimmed().Render("QUOTA TELEMETRY CONSOLE // CRT-01")
	}
	logo := []string{
		"█▀▀ █▀█ █▀▄ █▀▀ ▀▄▀ █▀█ █▀▄▀█ █▀▀ ▀█▀ █▀▀ █▀█",
		"█▄▄ █▄█ █▄▀ ██▄ █ █ █▄█ █ ▀ █ ██▄  █  ██▄ █▀▄",
	}
	beacon := []string{"◉", "◎", "◌", "◎"}[phase%4]
	return colors.header().Render(strings.Join(logo, "\n")) + "\n" +
		colors.dimmed().Render(fmt.Sprintf("%s QUOTA TELEMETRY CONSOLE // CRT-01 // SIGNAL LOCKED", beacon))
}

func renderBoot(width, phase int, colors palette) string {
	trackWidth := max(width-18, 8)
	position := phase % trackWidth
	track := strings.Repeat("·", position) + "◆" + strings.Repeat("·", trackWidth-position-1)
	return frame(width, "ACQUIRING SIGNAL", colors.label().Render(track)+"\n"+colors.dimmed().Render("HANDSHAKE WITH CODEX APP-SERVER IN PROGRESS"), colors.primary, colors)
}

func (m Model) renderStatus(width int, colors palette) string {
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
	plan := "UNKNOWN PLAN"
	if m.snapshot.RateLimits.PlanType != nil {
		plan = codex.DisplayName(*m.snapshot.RateLimits.PlanType)
	}
	left := lipgloss.NewStyle().Bold(true).Foreground(color).Render("● " + status)
	right := colors.dimmed().Render("ACCOUNT // " + plan + " // " + colors.name + " // " + m.meterStyle.name())
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
	mode := fmt.Sprintf("THEME %s // VIEW %s", colors.name, m.meterStyle.name())
	gap := max(width-len(left)-len(mode), 1)
	status := left + strings.Repeat(" ", gap) + mode
	buttons, separator := footerButtonLayout(width)
	controls := make([]string, 0, len(buttons))
	for _, button := range buttons {
		controls = append(controls, footerButtonAppearance(
			colors,
			button.id == m.hoveredButton,
			button.id == m.flashedButton,
		).Render(button.label))
	}
	return colors.dimmed().Render(status) + "\n" + strings.Join(controls, separator)
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
	style := lipgloss.NewStyle().
		Width(max(width-2, 1)).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Foreground(colors.primary).
		Background(colors.background)
	if height > 0 {
		style = style.Height(height)
	}
	return style.Render("[ " + title + " ]\n" + body)
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

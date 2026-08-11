package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type styleTab struct {
	style meterStyleID
	label string
	x     int
	width int
}

func styleTabLayout(width int, recording bool) ([]styleTab, string) {
	monitorFull := "╭ MONITOR ╮"
	monitorCompact := "╭MON╮"
	monitorMinimal := "[M]"
	microMonitor := "M"
	if recording {
		monitorFull = "╭ MONITOR ●╮"
		monitorCompact = "╭MON●╮"
		monitorMinimal = "[M●]"
		microMonitor = "●"
	}
	tiers := [][]string{
		{"╭ BARS ╮", monitorFull, "╭ PIE ╮", "╭ CONSUMPTION PACE ╮", "╭ FUEL TANK ╮", "╭ BENCHMARK ╮"},
		{"╭BAR╮", monitorCompact, "╭PIE╮", "╭PACE╮", "╭FUEL╮", "╭TEST╮"},
		{"[B]", monitorMinimal, "[P]", "[C]", "[F]", "[X]"},
		{"B", microMonitor, "P", "C", "F", "X"},
	}
	separator := ""
	labels := tiers[len(tiers)-1]
	for _, candidate := range tiers {
		candidateSeparator := " "
		total := len(candidate) - 1
		for _, label := range candidate {
			total += lipgloss.Width(label)
		}
		if total > width {
			total -= len(candidate) - 1
			candidateSeparator = ""
		}
		if total <= width {
			labels = candidate
			separator = candidateSeparator
			break
		}
	}

	tabs := make([]styleTab, 0, styleCount)
	x := 0
	for style, label := range labels {
		tabWidth := lipgloss.Width(label)
		if x+tabWidth > width {
			break
		}
		tabs = append(tabs, styleTab{style: meterStyleID(style), label: label, x: x, width: tabWidth})
		x += tabWidth + len(separator)
	}
	return tabs, separator
}

func (m Model) renderStyleTabs(width int, colors palette) string {
	recording := m.monitorState == monitorRunning
	tabs, separator := styleTabLayout(width, recording)
	parts := make([]string, 0, len(tabs))
	used := 0
	for _, tab := range tabs {
		active := tab.style == m.meterStyle
		hovered := m.styleHovered && tab.style == m.hoveredStyle
		flashed := m.styleFlashing && tab.style == m.flashedStyle
		foreground, background := colors.dim, colors.background
		bold := false
		if active {
			foreground, background, bold = colors.background, colors.primary, true
		}
		if hovered {
			foreground, bold = colors.accent, true
			if active {
				foreground, background = colors.background, colors.accent
			}
		}
		if flashed {
			foreground, background, bold = colors.background, colors.accent, true
		}
		base := lipgloss.NewStyle().Foreground(foreground).Background(background).Bold(bold)
		parts = append(parts, renderStyleTabLabel(tab.label, base, background, recording && tab.style == styleMonitor, m.phase, colors))
		used += tab.width
	}
	if len(parts) > 1 {
		used += (len(parts) - 1) * len(separator)
	}
	return strings.Join(parts, colors.dimmed().Render(separator)) + strings.Repeat(" ", max(width-used, 0))
}

func renderStyleTabLabel(label string, base lipgloss.Style, background lipgloss.Color, recording bool, phase int, colors palette) string {
	if !recording || !strings.Contains(label, "●") {
		return base.Render(label)
	}
	pieces := strings.SplitN(label, "●", 2)
	dotColor := recordingDotColor(phase, colors)
	dot := lipgloss.NewStyle().Bold(true).Foreground(dotColor).Background(background).Render("●")
	return base.Render(pieces[0]) + dot + base.Render(pieces[1])
}

func recordingDotColor(phase int, colors palette) lipgloss.Color {
	if phase%2 == 0 {
		return colors.danger
	}
	return lipgloss.Color("#7A2633")
}

func (m Model) styleTabAt(x, y int) (meterStyleID, bool) {
	if x < 0 || y < 0 || (m.loading && len(m.snapshot.Meters()) == 0) {
		return styleBars, false
	}
	layout := m.dashboardLayout()
	if y != layout.tabsY {
		return styleBars, false
	}
	localX := x - 2
	tabs, _ := styleTabLayout(layout.contentWidth, m.monitorState == monitorRunning)
	for _, tab := range tabs {
		if localX >= tab.x && localX < tab.x+tab.width {
			return tab.style, true
		}
	}
	return styleBars, false
}

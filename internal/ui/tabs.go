package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type mainTabID int

const (
	mainTabQuota mainTabID = iota
	mainTabMonitor
	mainTabBenchmark
	mainTabCount
)

func (t mainTabID) next() mainTabID {
	return (t + 1) % mainTabCount
}

func (t mainTabID) previous() mainTabID {
	return (t - 1 + mainTabCount) % mainTabCount
}

type mainTab struct {
	tab   mainTabID
	label string
	x     int
	width int
}

type styleTab struct {
	style meterStyleID
	label string
	x     int
	width int
}

func responsiveTabLabels(width int, tiers [][]string) ([]string, string) {
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
	return labels, separator
}

func mainTabLayout(width int, recording bool) ([]mainTab, string) {
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
	labels, separator := responsiveTabLabels(width, [][]string{
		{"╭ QUOTA ╮", monitorFull, "╭ BENCHMARK ╮"},
		{"╭QTA╮", monitorCompact, "╭TEST╮"},
		{"[Q]", monitorMinimal, "[B]"},
		{"Q", microMonitor, "B"},
	})

	tabs := make([]mainTab, 0, mainTabCount)
	x := 0
	for tab, label := range labels {
		tabWidth := lipgloss.Width(label)
		if x+tabWidth > width {
			break
		}
		tabs = append(tabs, mainTab{tab: mainTabID(tab), label: label, x: x, width: tabWidth})
		x += tabWidth + len(separator)
	}
	return tabs, separator
}

func quotaStyleTabLayout(width int) ([]styleTab, string) {
	labels, separator := responsiveTabLabels(width, [][]string{
		{"╭ BARS ╮", "╭ PIE ╮", "╭ CONSUMPTION PACE ╮", "╭ FUEL TANK ╮"},
		{"╭BAR╮", "╭PIE╮", "╭PACE╮", "╭FUEL╮"},
		{"[B]", "[P]", "[C]", "[F]"},
		{"B", "P", "C", "F"},
	})

	tabs := make([]styleTab, 0, len(quotaStyleOrder))
	x := 0
	for index, label := range labels {
		tabWidth := lipgloss.Width(label)
		if x+tabWidth > width {
			break
		}
		tabs = append(tabs, styleTab{style: quotaStyleOrder[index], label: label, x: x, width: tabWidth})
		x += tabWidth + len(separator)
	}
	return tabs, separator
}

func (m Model) currentMainTab() mainTabID {
	switch m.meterStyle {
	case styleMonitor:
		return mainTabMonitor
	case styleBenchmark:
		return mainTabBenchmark
	default:
		return mainTabQuota
	}
}

func (m Model) selectedQuotaStyle() meterStyleID {
	if m.meterStyle.isQuota() {
		return m.meterStyle
	}
	if m.quotaMeterStyle.isQuota() {
		return m.quotaMeterStyle
	}
	return styleBars
}

func (m Model) pressMainTab(tab mainTabID) (tea.Model, tea.Cmd) {
	if m.meterStyle.isQuota() {
		m.quotaMeterStyle = m.meterStyle
	}
	switch tab {
	case mainTabQuota:
		return m.pressStyleTab(m.selectedQuotaStyle())
	case mainTabMonitor:
		return m.pressStyleTab(styleMonitor)
	case mainTabBenchmark:
		return m.pressStyleTab(styleBenchmark)
	default:
		return m, nil
	}
}

func mainTabForStyle(style meterStyleID) mainTabID {
	switch style {
	case styleMonitor:
		return mainTabMonitor
	case styleBenchmark:
		return mainTabBenchmark
	default:
		return mainTabQuota
	}
}

func (m Model) renderMainTabs(width int, colors palette) string {
	recording := m.monitorState == monitorRunning
	tabs, separator := mainTabLayout(width, recording)
	parts := make([]string, 0, len(tabs))
	used := 0
	for _, tab := range tabs {
		active := tab.tab == m.currentMainTab()
		hovered := m.mainTabHovered && tab.tab == m.hoveredMainTab
		flashed := m.styleFlashing && tab.tab == mainTabForStyle(m.flashedStyle)
		base, background := tabAppearance(active, hovered, flashed, colors)
		parts = append(parts, renderStyleTabLabel(tab.label, base, background, recording && tab.tab == mainTabMonitor, m.phase, colors))
		used += tab.width
	}
	if len(parts) > 1 {
		used += (len(parts) - 1) * len(separator)
	}
	return strings.Join(parts, colors.dimmed().Render(separator)) + strings.Repeat(" ", max(width-used, 0))
}

func (m Model) renderQuotaStyleTabs(width int, colors palette) string {
	tabs, separator := quotaStyleTabLayout(width)
	parts := make([]string, 0, len(tabs))
	used := 0
	for _, tab := range tabs {
		active := tab.style == m.meterStyle
		hovered := m.styleHovered && tab.style == m.hoveredStyle
		flashed := m.styleFlashing && tab.style == m.flashedStyle
		base, _ := tabAppearance(active, hovered, flashed, colors)
		parts = append(parts, base.Render(tab.label))
		used += tab.width
	}
	if len(parts) > 1 {
		used += (len(parts) - 1) * len(separator)
	}
	return strings.Join(parts, colors.dimmed().Render(separator)) + strings.Repeat(" ", max(width-used, 0))
}

func tabAppearance(active, hovered, flashed bool, colors palette) (lipgloss.Style, lipgloss.Color) {
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
	return lipgloss.NewStyle().Foreground(foreground).Background(background).Bold(bold), background
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

func (m Model) mainTabAt(x, y int) (mainTabID, bool) {
	if x < 0 || y < 0 || (m.loading && len(m.snapshot.Meters()) == 0) {
		return mainTabQuota, false
	}
	layout := m.dashboardLayout()
	if y != layout.tabsY {
		return mainTabQuota, false
	}
	localX := x - 2
	tabs, _ := mainTabLayout(layout.contentWidth, m.monitorState == monitorRunning)
	for _, tab := range tabs {
		if localX >= tab.x && localX < tab.x+tab.width {
			return tab.tab, true
		}
	}
	return mainTabQuota, false
}

func (m Model) quotaStyleTabAt(x, y int) (meterStyleID, bool) {
	if x < 0 || y < 0 || !m.meterStyle.isQuota() || (m.loading && len(m.snapshot.Meters()) == 0) {
		return styleBars, false
	}
	layout := m.dashboardLayout()
	if y != layout.quotaTabsY {
		return styleBars, false
	}
	localX := x - 2
	tabs, _ := quotaStyleTabLayout(layout.contentWidth)
	for _, tab := range tabs {
		if localX >= tab.x && localX < tab.x+tab.width {
			return tab.style, true
		}
	}
	return styleBars, false
}

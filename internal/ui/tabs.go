package ui

import (
	imagecolor "image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

type viewTab struct {
	view  meterViewID
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

func mainTabLayout(width int, showMonitorLight bool) ([]mainTab, string) {
	monitorFull := "╭ MONITOR ╮"
	monitorCompact := "╭MON╮"
	monitorMinimal := "[M]"
	microMonitor := "M"
	if showMonitorLight {
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

func quotaViewTabLayout(width int) ([]viewTab, string) {
	labels, separator := responsiveTabLabels(width, [][]string{
		{"╭ BARS ╮", "╭ CONSUMPTION PACE ╮", "╭ PIE ╮", "╭ FUEL TANK ╮"},
		{"╭BAR╮", "╭PACE╮", "╭PIE╮", "╭FUEL╮"},
		{"[B]", "[C]", "[P]", "[F]"},
		{"B", "C", "P", "F"},
	})

	tabs := make([]viewTab, 0, len(quotaViewOrder))
	x := 0
	for index, label := range labels {
		tabWidth := lipgloss.Width(label)
		if x+tabWidth > width {
			break
		}
		tabs = append(tabs, viewTab{view: quotaViewOrder[index], label: label, x: x, width: tabWidth})
		x += tabWidth + len(separator)
	}
	return tabs, separator
}

func (m Model) currentMainTab() mainTabID {
	switch m.meterView {
	case viewMonitor:
		return mainTabMonitor
	case viewBenchmark:
		return mainTabBenchmark
	default:
		return mainTabQuota
	}
}

func (m Model) selectedQuotaView() meterViewID {
	if m.meterView.isQuota() {
		return m.meterView
	}
	if m.quotaMeterView.isQuota() {
		return m.quotaMeterView
	}
	return viewBars
}

func (m Model) pressMainTab(tab mainTabID) (tea.Model, tea.Cmd) {
	if m.meterView.isQuota() {
		m.quotaMeterView = m.meterView
	}
	switch tab {
	case mainTabQuota:
		return m.pressViewTab(m.selectedQuotaView())
	case mainTabMonitor:
		return m.pressViewTab(viewMonitor)
	case mainTabBenchmark:
		return m.pressViewTab(viewBenchmark)
	default:
		return m, nil
	}
}

func mainTabForView(view meterViewID) mainTabID {
	switch view {
	case viewMonitor:
		return mainTabMonitor
	case viewBenchmark:
		return mainTabBenchmark
	default:
		return mainTabQuota
	}
}

func (m Model) renderMainTabs(width int, colors palette) string {
	tabWidth, resetLabel := m.resetLayout(width)
	tabs, separator := mainTabLayout(tabWidth, true)
	parts := make([]string, 0, len(tabs))
	used := 0
	for _, tab := range tabs {
		active := tab.tab == m.currentMainTab()
		hovered := m.mainTabHovered && tab.tab == m.hoveredMainTab
		flashed := m.viewFlashing && tab.tab == mainTabForView(m.flashedView)
		base, background := tabAppearance(active, hovered, flashed, colors)
		parts = append(parts, renderViewTabLabel(tab.label, base, background, tab.tab == mainTabMonitor, m.monitorIndicatorColor(colors)))
		used += tab.width
	}
	if len(parts) > 1 {
		used += (len(parts) - 1) * len(separator)
	}
	if resetLabel != "" {
		return strings.Join(parts, colors.dimmed().Render(separator)) + strings.Repeat(" ", max(tabWidth-used+1, 0)) + m.renderResetButton(resetLabel, colors)
	}
	if m.resetOwnRow(width) {
		return strings.Join(parts, colors.dimmed().Render(separator)) + "\n" + joinRight("", m.renderResetButton(m.resetLabel(), colors), width)
	}
	return strings.Join(parts, colors.dimmed().Render(separator)) + strings.Repeat(" ", max(width-used, 0))
}

func (m Model) renderQuotaViewTabs(width int, colors palette) string {
	tabs, separator := quotaViewTabLayout(width)
	parts := make([]string, 0, len(tabs))
	used := 0
	for _, tab := range tabs {
		active := tab.view == m.meterView
		hovered := m.viewHovered && tab.view == m.hoveredView
		flashed := m.viewFlashing && tab.view == m.flashedView
		base, _ := tabAppearance(active, hovered, flashed, colors)
		parts = append(parts, base.Render(tab.label))
		used += tab.width
	}
	if len(parts) > 1 {
		used += (len(parts) - 1) * len(separator)
	}
	return strings.Join(parts, colors.dimmed().Render(separator)) + strings.Repeat(" ", max(width-used, 0))
}

func tabAppearance(active, hovered, flashed bool, colors palette) (lipgloss.Style, imagecolor.Color) {
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

func renderViewTabLabel(label string, base lipgloss.Style, background imagecolor.Color, monitorLight bool, lightColor imagecolor.Color) string {
	if !monitorLight || !strings.Contains(label, "●") {
		return base.Render(label)
	}
	pieces := strings.SplitN(label, "●", 2)
	dot := lipgloss.NewStyle().Bold(true).Foreground(lightColor).Background(background).Render("●")
	return base.Render(pieces[0]) + dot + base.Render(pieces[1])
}

func (m Model) monitorIndicatorColor(colors palette) imagecolor.Color {
	if m.monitorState != monitorRunning {
		return colors.dim
	}
	if m.monitorHasVisibleWaitingSession() {
		if m.phase%2 == 0 {
			return colors.warning
		}
		return colors.warningDim
	}
	if m.monitorCodexWorking {
		if m.phase%2 == 0 {
			return colors.success
		}
		return colors.successDim
	}
	if !m.monitorCodexStatusKnown {
		return colors.dim
	}
	if m.monitorCodexUp {
		return colors.success
	}
	return colors.danger
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
	tabWidth, _ := m.resetLayout(layout.contentWidth)
	tabs, _ := mainTabLayout(tabWidth, true)
	for _, tab := range tabs {
		if localX >= tab.x && localX < tab.x+tab.width {
			return tab.tab, true
		}
	}
	return mainTabQuota, false
}

func (m Model) quotaViewTabAt(x, y int) (meterViewID, bool) {
	if x < 0 || y < 0 || !m.meterView.isQuota() || (m.loading && len(m.snapshot.Meters()) == 0) {
		return viewBars, false
	}
	layout := m.dashboardLayout()
	if y != layout.quotaTabsY {
		return viewBars, false
	}
	localX := x - 2
	tabs, _ := quotaViewTabLayout(layout.contentWidth)
	for _, tab := range tabs {
		if localX >= tab.x && localX < tab.x+tab.width {
			return tab.view, true
		}
	}
	return viewBars, false
}

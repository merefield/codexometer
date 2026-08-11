package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type monitorRect struct {
	x      int
	y      int
	width  int
	height int
}

func (r monitorRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

type monitorView struct {
	view     string
	goRect   monitorRect
	stopRect monitorRect
}

type monitorGeometry struct {
	width        int
	height       int
	gap          int
	topHeight    int
	graphHeight  int
	readoutWidth int
	buttonWidths [2]int
	goRect       monitorRect
	stopRect     monitorRect
}

func layoutMonitorArea(width, height int) monitorGeometry {
	width = max(width, 1)
	height = max(height, 1)
	gap := 1
	topHeight := min(max(height/3, 6), 10)
	if height < 10 {
		topHeight = max(height/2, 3)
	}
	graphHeight := max(height-topHeight-gap, 1)

	readoutWidth := max(width*3/5, 18)
	controlsWidth := width - readoutWidth - gap
	if controlsWidth < 17 {
		controlsWidth = min(17, max(width/2, 1))
		readoutWidth = max(width-controlsWidth-gap, 1)
	}
	buttonWidths := distributeSpace(max(controlsWidth-gap, 2), 2)
	goX := readoutWidth + gap
	return monitorGeometry{
		width:        width,
		height:       height,
		gap:          gap,
		topHeight:    topHeight,
		graphHeight:  graphHeight,
		readoutWidth: readoutWidth,
		buttonWidths: [2]int{buttonWidths[0], buttonWidths[1]},
		goRect:       monitorRect{x: goX, width: buttonWidths[0], height: topHeight},
		stopRect: monitorRect{
			x: goX + buttonWidths[0] + gap, width: buttonWidths[1], height: topHeight,
		},
	}
}

func (m Model) renderMonitorArea(width, height int, colors palette) monitorView {
	layout := layoutMonitorArea(width, height)

	readout := m.renderMonitorReadout(layout.readoutWidth, layout.topHeight, colors)
	goButton := m.renderMonitorButton(layout.buttonWidths[0], layout.topHeight, "(S)TART", footerButtonMonitorGo, m.monitorGoEnabled(), colors)
	stopButton := m.renderMonitorButton(layout.buttonWidths[1], layout.topHeight, "STO(P)", footerButtonMonitorStop, m.monitorStopEnabled(), colors)
	controls := lipgloss.JoinHorizontal(lipgloss.Top, goButton, strings.Repeat(" ", layout.gap), stopButton)
	top := lipgloss.JoinHorizontal(lipgloss.Top, readout, strings.Repeat(" ", layout.gap), controls)
	graph := m.renderMonitorGraph(layout.width, layout.graphHeight, colors)
	view := lipgloss.JoinVertical(lipgloss.Left, top, strings.Repeat("\n", layout.gap-1)+graph)
	if padding := layout.height - lipgloss.Height(view); padding > 0 {
		view += strings.Repeat("\n", padding)
	}

	return monitorView{
		view: view, goRect: layout.goRect, stopRect: layout.stopRect,
	}
}

func (m Model) renderMonitorReadout(width, height int, colors palette) string {
	state := "READY"
	hint := "PRESS S OR CLICK START // LOCAL SESSIONS"
	switch m.monitorState {
	case monitorStarting:
		state, hint = "ZEROING COUNTER", "SCANNING LOCAL CODEX TELEMETRY"
	case monitorRunning:
		state, hint = "RECORDING ●", fmt.Sprintf("LIVE LOCAL SESSIONS %d // P OR CLICK STOP", m.monitorSessions)
	case monitorStopping:
		state, hint = "FINAL SYNC", "READING APPENDED TOKEN TELEMETRY"
	case monitorStopped:
		state, hint = "STOPPED", fmt.Sprintf("FINAL CONSUMPTION // LOCAL SESSIONS %d", m.monitorSessions)
	}
	if m.monitorError != "" {
		state, hint = "NO TOKEN SIGNAL", m.monitorError
	}

	total := int64(0)
	if !m.monitorStartedAt.IsZero() && m.monitorLatest >= m.monitorBaseline {
		total = m.monitorLatest - m.monitorBaseline
	}
	elapsed := m.monitorElapsed(time.Now())
	rate := int64(0)
	if elapsed > 0 {
		rate = int64(math.Round(float64(total) / elapsed.Minutes()))
	}

	stateColor := colors.primary
	if m.monitorState == monitorRunning && m.monitorError == "" {
		stateColor = lipgloss.Color("#7A2633")
		if m.phase%2 == 0 {
			stateColor = colors.danger
		}
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(stateColor).Render(state) +
			lipgloss.NewStyle().Bold(true).Foreground(colors.primary).Render("  //  "+formatTokens(total)+" TOKENS"),
		colors.dimmed().Render(ansi.Truncate(hint, max(width-4, 1), "")),
	}
	if height >= 6 {
		lines = append(lines, colors.label().Render(fmt.Sprintf("ELAPSED %s  //  RATE %s/MIN", formatElapsed(elapsed), formatTokens(rate))))
	}
	if height >= 8 && !m.monitorStartedAt.IsZero() {
		lines = append(lines, colors.dimmed().Render(fmt.Sprintf("START %s  //  NOW %s", formatTokens(m.monitorBaseline), formatTokens(m.monitorLatest))))
	}
	if height >= 10 {
		lines = append(lines, colors.dimmed().Render(fmt.Sprintf("SAMPLES %d  //  NEXT %s", len(m.monitorSamples), m.monitorNextLabel())))
		last := "--:--"
		if !m.monitorLastActivity.IsZero() {
			last = compactDuration(time.Since(m.monitorLastActivity))
		}
		telemetry := fmt.Sprintf("LOCAL SESSIONS %d  //  LAST %s AGO", m.monitorSessions, last)
		lines = append(lines, colors.dimmed().Render(ansi.Truncate(telemetry, max(width-4, 1), "")))
	}
	return frameSized(width, max(height-2, 1), "SESSION READOUT", strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) renderMonitorButton(width, height int, label string, id footerButtonID, enabled bool, colors palette) string {
	border := colors.dim
	foreground := colors.dim
	if enabled {
		border = colors.primary
		foreground = colors.primary
	}
	style := lipgloss.NewStyle().
		Width(max(width-2, 1)).
		Height(max(height-2, 1)).
		Align(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Foreground(foreground).
		Background(colors.background)
	if id == m.hoveredButton && enabled {
		style = style.Bold(true).Foreground(colors.accent).BorderForeground(colors.accent)
	}
	if id == m.flashedButton {
		style = style.Bold(true).Foreground(colors.background).Background(colors.primary).BorderForeground(colors.primary)
	}
	return style.Render(label)
}

func (m Model) renderMonitorGraph(width, height int, colors palette) string {
	innerWidth := max(width-4, 1)
	plotHeight := max(height-3, 1)
	const sampleWidth = 2 // one block-wide bar plus one cell of breathing room
	samples := m.monitorSamples
	visibleSamples := max((innerWidth+1)/sampleWidth, 1)
	if len(samples) > visibleSamples {
		samples = samples[len(samples)-visibleSamples:]
	}
	maximum := int64(0)
	for _, sample := range samples {
		maximum = max(maximum, sample.intervalTokens)
	}
	yMax := niceTokenCeiling(maximum)
	title := fmt.Sprintf("30 SEC LOCAL TOKEN BARS // AUTO 0-%s", formatCompactTokens(yMax))

	canvas := make([][]rune, plotHeight)
	for row := range canvas {
		canvas[row] = []rune(strings.Repeat(" ", innerWidth))
	}
	if len(samples) == 0 {
		message := "WAITING FOR FIRST SAMPLE"
		if m.monitorState == monitorIdle {
			message = "PRESS START TO ARM RECORDER"
		} else if m.monitorState == monitorStopped {
			message = "NO COMPLETE 30 SEC SAMPLE"
		}
		message = ansi.Truncate(message, innerWidth, "")
		row := plotHeight / 2
		column := max((innerWidth-len([]rune(message)))/2, 0)
		copy(canvas[row][column:], []rune(message))
	} else {
		chartWidth := len(samples)*sampleWidth - 1
		startX := innerWidth - chartWidth
		for index, sample := range samples {
			ratio := float64(sample.intervalTokens) / float64(yMax)
			filledRows := int(math.Round(ratio * float64(plotHeight)))
			if sample.intervalTokens > 0 {
				filledRows = max(filledRows, 1)
			}
			filledRows = min(filledRows, plotHeight)
			x := startX + index*sampleWidth
			for row := range plotHeight {
				canvas[row][x] = '░'
				if row >= plotHeight-filledRows {
					canvas[row][x] = '█'
				}
			}
		}
	}
	lines := make([]string, plotHeight)
	for row, runes := range canvas {
		if row == plotHeight-1 {
			runes = []rune(strings.Map(func(r rune) rune {
				if r == ' ' {
					return '·'
				}
				return r
			}, string(runes)))
		}
		lines[row] = renderMonitorBarRow(runes, colors)
	}
	return frameSized(width, max(height-2, 1), ansi.Truncate(title, max(innerWidth-4, 1), ""), strings.Join(lines, "\n"), colors.primary, colors)
}

func renderMonitorBarRow(cells []rune, colors palette) string {
	active := lipgloss.NewStyle().Foreground(colors.primary)
	dimmed := lipgloss.NewStyle().Foreground(colors.dim)
	var row strings.Builder
	for _, cell := range cells {
		switch cell {
		case '░', '·':
			row.WriteString(dimmed.Render(string(cell)))
		case '█':
			row.WriteString(active.Render(string(cell)))
		default:
			row.WriteRune(cell)
		}
	}
	return row.String()
}

func (m Model) monitorButtonAt(x, y int) footerButtonID {
	if m.loading && len(m.snapshot.Meters()) == 0 {
		return footerButtonNone
	}
	dashboard := m.dashboardLayout()
	area := layoutMonitorArea(dashboard.contentWidth, dashboard.meterHeight)
	localX, localY := x-2, y-dashboard.meterY
	if area.goRect.contains(localX, localY) {
		return footerButtonMonitorGo
	}
	if area.stopRect.contains(localX, localY) {
		return footerButtonMonitorStop
	}
	return footerButtonNone
}

func (m Model) monitorGoEnabled() bool {
	return m.monitorState == monitorIdle || m.monitorState == monitorStopped
}

func (m Model) monitorStopEnabled() bool {
	return m.monitorState == monitorRunning
}

func (m Model) monitorElapsed(now time.Time) time.Duration {
	if m.monitorStartedAt.IsZero() {
		return 0
	}
	end := now
	if m.monitorState == monitorStopped && !m.monitorStoppedAt.IsZero() {
		end = m.monitorStoppedAt
	}
	if end.Before(m.monitorStartedAt) {
		return 0
	}
	return end.Sub(m.monitorStartedAt)
}

func (m Model) monitorNextLabel() string {
	if m.monitorState != monitorRunning || m.monitorNextSample.IsZero() {
		return "--:--"
	}
	return compactDuration(time.Until(m.monitorNextSample))
}

func niceTokenCeiling(value int64) int64 {
	if value <= 0 {
		return 1
	}
	power := int64(1)
	for value/power > 10 {
		if power > math.MaxInt64/10 {
			return math.MaxInt64
		}
		power *= 10
	}
	for _, multiple := range []int64{1, 2, 5, 10} {
		if power > math.MaxInt64/multiple {
			return math.MaxInt64
		}
		ceiling := multiple * power
		if value <= ceiling {
			return ceiling
		}
	}
	return 10 * power
}

func formatTokens(value int64) string {
	digits := strconv.FormatInt(value, 10)
	negative := strings.HasPrefix(digits, "-")
	if negative {
		digits = digits[1:]
	}
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	if negative {
		return "-" + digits
	}
	return digits
}

func formatCompactTokens(value int64) string {
	abs := math.Abs(float64(value))
	suffix := ""
	divisor := float64(1)
	switch {
	case abs >= 1_000_000_000:
		suffix, divisor = "B", 1_000_000_000
	case abs >= 1_000_000:
		suffix, divisor = "M", 1_000_000
	case abs >= 1_000:
		suffix, divisor = "K", 1_000
	default:
		return formatTokens(value)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(value)/divisor), ".0") + suffix
}

func formatElapsed(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	totalSeconds := int64(duration / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", totalSeconds/3600, totalSeconds/60%60, totalSeconds%60)
}

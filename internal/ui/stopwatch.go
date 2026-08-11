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

type stopwatchRect struct {
	x      int
	y      int
	width  int
	height int
}

func (r stopwatchRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

type stopwatchView struct {
	view     string
	goRect   stopwatchRect
	stopRect stopwatchRect
}

func (m Model) renderStopwatchArea(width, height int, colors palette) stopwatchView {
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

	readout := m.renderStopwatchReadout(readoutWidth, topHeight, colors)
	goButton := m.renderStopwatchButton(buttonWidths[0], topHeight, "(G)O", footerButtonStopwatchGo, m.stopwatchGoEnabled(), colors)
	stopButton := m.renderStopwatchButton(buttonWidths[1], topHeight, "STO(P)", footerButtonStopwatchStop, m.stopwatchStopEnabled(), colors)
	controls := lipgloss.JoinHorizontal(lipgloss.Top, goButton, strings.Repeat(" ", gap), stopButton)
	top := lipgloss.JoinHorizontal(lipgloss.Top, readout, strings.Repeat(" ", gap), controls)
	graph := m.renderStopwatchGraph(width, graphHeight, colors)
	view := lipgloss.JoinVertical(lipgloss.Left, top, strings.Repeat("\n", gap-1)+graph)
	if padding := height - lipgloss.Height(view); padding > 0 {
		view += strings.Repeat("\n", padding)
	}

	goX := lipgloss.Width(readout) + gap
	return stopwatchView{
		view: view,
		goRect: stopwatchRect{
			x: goX, y: 0, width: lipgloss.Width(goButton), height: lipgloss.Height(goButton),
		},
		stopRect: stopwatchRect{
			x: goX + lipgloss.Width(goButton) + gap,
			y: 0, width: lipgloss.Width(stopButton), height: lipgloss.Height(stopButton),
		},
	}
}

func (m Model) renderStopwatchReadout(width, height int, colors palette) string {
	state := "READY"
	hint := "PRESS G OR CLICK GO // LOCAL SESSIONS"
	switch m.stopwatchState {
	case stopwatchStarting:
		state, hint = "ZEROING COUNTER", "SCANNING LOCAL CODEX TELEMETRY"
	case stopwatchRunning:
		state, hint = "RECORDING ●", fmt.Sprintf("LIVE LOCAL SESSIONS %d // P OR CLICK STOP", m.stopwatchSessions)
	case stopwatchStopping:
		state, hint = "FINAL SYNC", "READING APPENDED TOKEN TELEMETRY"
	case stopwatchStopped:
		state, hint = "STOPPED", fmt.Sprintf("FINAL CONSUMPTION // LOCAL SESSIONS %d", m.stopwatchSessions)
	}
	if m.stopwatchError != "" {
		state, hint = "NO TOKEN SIGNAL", m.stopwatchError
	}

	total := int64(0)
	if !m.stopwatchStartedAt.IsZero() && m.stopwatchLatest >= m.stopwatchBaseline {
		total = m.stopwatchLatest - m.stopwatchBaseline
	}
	elapsed := m.stopwatchElapsed(time.Now())
	rate := int64(0)
	if elapsed > 0 {
		rate = int64(math.Round(float64(total) / elapsed.Minutes()))
	}

	stateColor := colors.primary
	if m.stopwatchState == stopwatchRunning && m.stopwatchError == "" {
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
	if height >= 8 && !m.stopwatchStartedAt.IsZero() {
		lines = append(lines, colors.dimmed().Render(fmt.Sprintf("START %s  //  NOW %s", formatTokens(m.stopwatchBaseline), formatTokens(m.stopwatchLatest))))
	}
	if height >= 10 {
		lines = append(lines, colors.dimmed().Render(fmt.Sprintf("SAMPLES %d  //  NEXT %s", len(m.stopwatchSamples), m.stopwatchNextLabel())))
		last := "--:--"
		if !m.stopwatchLastActivity.IsZero() {
			last = compactDuration(time.Since(m.stopwatchLastActivity))
		}
		telemetry := fmt.Sprintf("LOCAL SESSIONS %d  //  LAST %s AGO", m.stopwatchSessions, last)
		lines = append(lines, colors.dimmed().Render(ansi.Truncate(telemetry, max(width-4, 1), "")))
	}
	return frameSized(width, max(height-2, 1), "SESSION READOUT", strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) renderStopwatchButton(width, height int, label string, id footerButtonID, enabled bool, colors palette) string {
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

func (m Model) renderStopwatchGraph(width, height int, colors palette) string {
	innerWidth := max(width-4, 1)
	plotHeight := max(height-3, 1)
	const sampleWidth = 2 // one block-wide bar plus one cell of breathing room
	samples := m.stopwatchSamples
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
		if m.stopwatchState == stopwatchIdle {
			message = "PRESS GO TO ARM RECORDER"
		} else if m.stopwatchState == stopwatchStopped {
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
		lines[row] = renderStopwatchBarRow(runes, colors)
	}
	return frameSized(width, max(height-2, 1), ansi.Truncate(title, max(innerWidth-4, 1), ""), strings.Join(lines, "\n"), colors.primary, colors)
}

func renderStopwatchBarRow(cells []rune, colors palette) string {
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

func (m Model) stopwatchButtonAt(x, y int) footerButtonID {
	if m.loading && len(m.snapshot.Meters()) == 0 {
		return footerButtonNone
	}
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
	colors := paletteFor(m.theme)
	header := renderHeader(contentWidth, m.phase, colors)
	status := m.renderStatus(contentWidth, colors)
	originY := 1 + lipgloss.Height(header) + lipgloss.Height(status)
	reservedHeight := lipgloss.Height(header) + lipgloss.Height(status)
	if m.err != nil {
		errorView := renderError(contentWidth, m.err, colors)
		originY += lipgloss.Height(errorView)
		reservedHeight += lipgloss.Height(errorView)
	}
	if len(m.snapshot.Meters()) == 0 {
		emptyView := renderError(contentWidth, fmt.Errorf("no quota windows returned"), colors)
		originY += lipgloss.Height(emptyView)
		reservedHeight += lipgloss.Height(emptyView)
	}
	footer := m.renderFooter(contentWidth, colors)
	reservedHeight += lipgloss.Height(footer)
	area := m.renderStopwatchArea(contentWidth, max(contentHeight-reservedHeight, 1), colors)
	localX, localY := x-2, y-originY
	if area.goRect.contains(localX, localY) {
		return footerButtonStopwatchGo
	}
	if area.stopRect.contains(localX, localY) {
		return footerButtonStopwatchStop
	}
	return footerButtonNone
}

func (m Model) stopwatchGoEnabled() bool {
	return m.stopwatchState == stopwatchIdle || m.stopwatchState == stopwatchStopped
}

func (m Model) stopwatchStopEnabled() bool {
	return m.stopwatchState == stopwatchRunning
}

func (m Model) stopwatchElapsed(now time.Time) time.Duration {
	if m.stopwatchStartedAt.IsZero() {
		return 0
	}
	end := now
	if m.stopwatchState == stopwatchStopped && !m.stopwatchStoppedAt.IsZero() {
		end = m.stopwatchStoppedAt
	}
	if end.Before(m.stopwatchStartedAt) {
		return 0
	}
	return end.Sub(m.stopwatchStartedAt)
}

func (m Model) stopwatchNextLabel() string {
	if m.stopwatchState != stopwatchRunning || m.stopwatchNextSample.IsZero() {
		return "--:--"
	}
	return compactDuration(time.Until(m.stopwatchNextSample))
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

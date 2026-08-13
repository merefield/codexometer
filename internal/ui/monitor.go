package ui

import (
	"fmt"
	"math"
	"path/filepath"
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
	topHeight := min(max(height/5, 5), 8)
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
	goLabel, stopLabel := "(S)TART", "STO(P)"
	if layout.buttonWidths[0] < lipgloss.Width(goLabel)+2 {
		goLabel = "(S)"
	}
	if layout.buttonWidths[1] < lipgloss.Width(stopLabel)+2 {
		stopLabel = "(P)"
	}
	goButton := m.renderMonitorButton(layout.buttonWidths[0], layout.topHeight, goLabel, footerButtonMonitorGo, m.monitorGoEnabled(), colors)
	stopButton := m.renderMonitorButton(layout.buttonWidths[1], layout.topHeight, stopLabel, footerButtonMonitorStop, m.monitorStopEnabled(), colors)
	controls := lipgloss.JoinHorizontal(lipgloss.Top, goButton, strings.Repeat(" ", layout.gap), stopButton)
	top := lipgloss.JoinHorizontal(lipgloss.Top, readout, strings.Repeat(" ", layout.gap), controls)
	graph := m.renderMonitorSessions(layout.width, layout.graphHeight, colors)
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
	innerWidth := max(width-4, 1)
	lines := []string{
		ansi.Truncate(lipgloss.NewStyle().Bold(true).Foreground(stateColor).Render(state)+
			lipgloss.NewStyle().Bold(true).Foreground(colors.primary).Render("  //  "+formatTokens(total)+" TOKENS"), innerWidth, ""),
		colors.dimmed().Render(ansi.Truncate(hint, innerWidth, "")),
	}
	if height >= 6 {
		lines = append(lines, colors.label().Render(ansi.Truncate(fmt.Sprintf("ELAPSED %s  //  RATE %s/MIN", formatElapsed(elapsed), formatTokens(rate)), innerWidth, "")))
	}
	if height >= 5 {
		if quota := m.monitorQuotaReadout(); quota != "" {
			lines = append(lines, colors.dimmed().Render(ansi.Truncate(quota, innerWidth, "")))
		}
	}
	if height >= 8 && !m.monitorStartedAt.IsZero() {
		lines = append(lines, colors.dimmed().Render(ansi.Truncate(fmt.Sprintf("START %s  //  NOW %s", formatTokens(m.monitorBaseline), formatTokens(m.monitorLatest)), innerWidth, "")))
	}
	if height >= 10 {
		lines = append(lines, colors.dimmed().Render(ansi.Truncate(fmt.Sprintf("SAMPLES %d  //  NEXT %s", len(m.monitorSamples), m.monitorNextLabel()), innerWidth, "")))
		last := "--:--"
		if !m.monitorLastActivity.IsZero() {
			last = compactDuration(time.Since(m.monitorLastActivity))
		}
		telemetry := fmt.Sprintf("LOCAL SESSIONS %d  //  LAST %s AGO", m.monitorSessions, last)
		lines = append(lines, colors.dimmed().Render(ansi.Truncate(telemetry, innerWidth, "")))
	}
	return frameSized(width, max(height-2, 1), "MONITOR READOUT", strings.Join(lines, "\n"), colors.primary, colors)
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
	return m.renderMonitorGraphSamples(width, height, m.monitorSamples, "LOCAL TOKEN BARS", colors)
}

func (m Model) renderMonitorSessions(width, height int, colors palette) string {
	visible := make([]monitorSession, 0, len(m.monitorSessionData))
	for _, session := range m.monitorSessionData {
		if session.displayed {
			visible = append(visible, session)
		}
	}
	if len(visible) == 0 {
		return m.renderMonitorGraph(width, height, colors)
	}

	// Every row needs a title, body, and bottom border. When the terminal is too
	// short, show a scrollable window and report its position.
	visibleCount := len(visible)
	rowCount := min(visibleCount, max(height/3, 1))
	start := min(max(m.monitorScroll, 0), max(visibleCount-rowCount, 0))
	visible = visible[start : start+rowCount]
	pageLabel := ""
	if rowCount < visibleCount {
		pageLabel = fmt.Sprintf("ROWS %d-%d/%d", start+1, start+rowCount, visibleCount)
	}
	rowHeights := distributeSpace(max(height-(rowCount-1), rowCount), rowCount)
	rows := make([]string, 0, rowCount)
	for index, session := range visible {
		rowPageLabel := ""
		if index == rowCount-1 {
			rowPageLabel = pageLabel
		}
		rows = append(rows, m.renderMonitorSessionRow(width, rowHeights[index], session, rowPageLabel, colors))
	}
	view := strings.Join(rows, "\n")
	if padding := height - lipgloss.Height(view); padding > 0 {
		view += strings.Repeat("\n", padding)
	}
	return view
}

func (m Model) renderMonitorSessionRow(width, height int, session monitorSession, pageLabel string, colors palette) string {
	const gap = 1
	if width <= gap+1 {
		return m.renderMonitorGraphSamples(width, height, session.samples, "TOKENS", colors)
	}
	available := max(width-gap, 2)
	metricsWidth := max(available/3, 1)
	graphWidth := max(available-metricsWidth, 1)
	metrics := m.renderMonitorSessionMetrics(metricsWidth, height, session, pageLabel, colors)
	title := "TOKEN BARS"
	graph := m.renderMonitorGraphSamples(graphWidth, height, session.samples, title, colors)
	return lipgloss.JoinHorizontal(lipgloss.Top, metrics, strings.Repeat(" ", gap), graph)
}

func (m Model) renderMonitorSessionMetrics(width, height int, session monitorSession, pageLabel string, colors palette) string {
	title := "SESSION // " + shortSessionID(session.id)
	if session.workingDirectory != "" {
		title = shortSessionID(session.id) + " // " + strings.ToUpper(filepath.Base(session.workingDirectory))
	}
	if session.unattributed {
		title = "UNATTRIBUTED // INTERNAL"
	}
	total := max(session.latest-session.baseline, int64(0))
	elapsed := m.monitorSessionElapsed(session, time.Now())
	rate := int64(0)
	if elapsed > 0 {
		rate = int64(math.Round(float64(total) / elapsed.Minutes()))
	}
	status := "IDLE"
	if session.active {
		status = "ACTIVE"
	}
	if session.awaitingInput {
		status = "AWAITING"
	}
	innerWidth := max(width-4, 1)
	memberLabel := "ROOT"
	if session.agentCount > 0 {
		memberLabel = fmt.Sprintf("ROOT + %d %s", session.agentCount, plural(session.agentCount, "AGENT", "AGENTS"))
		if lipgloss.Width(status+" // "+memberLabel) > innerWidth {
			memberLabel = fmt.Sprintf("%d %s", session.agentCount, plural(session.agentCount, "AGENT", "AGENTS"))
		}
	}
	if session.unattributed {
		memberLabel = "UNLINKED ACTIVITY"
	}
	bodyRows := max(height-2, 1)
	share := m.monitorSessionShare(total)
	usageLine := fmt.Sprintf("%s TOKENS // %.0f%% LOCAL", formatTokens(total), share*100)
	lines := make([]string, 0, bodyRows)
	if session.awaitingInput {
		badge := lipgloss.NewStyle().Bold(true).Foreground(colors.background).Background(colors.warning)
		lines = append(lines, badge.Render(ansi.Truncate(" ● AWAITING YOU ", innerWidth, "")))
	}
	if len(lines) < bodyRows {
		lines = append(lines, colors.label().Render(ansi.Truncate(usageLine, innerWidth, "")))
	}
	appendLine := func(value string) {
		if len(lines) < bodyRows {
			lines = append(lines, colors.dimmed().Render(ansi.Truncate(value, innerWidth, "")))
		}
	}
	if estimate := m.monitorSessionQuotaEstimate(share); estimate != "" {
		appendLine(estimate)
	}
	appendLine(status + " // " + memberLabel)
	appendLine(formatMonitorCallActivity(session, time.Now()))
	appendLine(formatMonitorTTFT(session))
	appendLine(formatMonitorOutput(session))
	appendLine("RATE " + formatTokens(rate) + "/MIN")
	if session.workingDirectory != "" {
		appendLine("DIR // " + filepath.Base(session.workingDirectory))
	}
	if !session.lastActivity.IsZero() {
		appendLine("LAST // " + compactDuration(time.Since(session.lastActivity)) + " AGO")
	}
	if pageLabel != "" && (!session.awaitingInput || len(lines) > 1) {
		lines[len(lines)-1] = colors.dimmed().Render(ansi.Truncate(pageLabel+" // PGUP/PGDN", innerWidth, ""))
	}
	borderColor := colors.primary
	if session.awaitingInput {
		borderColor = colors.warning
	}
	return frameSized(width, max(height-2, 1), title, strings.Join(lines, "\n"), borderColor, colors)
}

func formatMonitorCallActivity(session monitorSession, now time.Time) string {
	last := "--"
	if !session.lastCallAt.IsZero() {
		last = formatMonitorAge(now.Sub(session.lastCallAt)) + " AGO"
	}
	return fmt.Sprintf("CALLS %d // LAST %s", session.modelCalls, last)
}

func formatMonitorTTFT(session monitorSession) string {
	latest := "N/A"
	if session.latestTTFTOK {
		latest = formatMonitorLatency(session.latestTTFT)
	}
	peak := "N/A"
	if session.peakTTFTOK {
		peak = formatMonitorLatency(session.peakTTFT)
	}
	return "TTFT " + latest + " // PEAK " + peak
}

func formatMonitorOutput(session monitorSession) string {
	latest := "N/A"
	if session.latestOutputOK {
		latest = formatTokens(session.latestOutput)
	}
	peak := "N/A"
	if session.peakOutputOK {
		peak = formatTokens(session.peakOutput)
	}
	return "LAST OUT " + latest + " // PEAK " + peak
}

func formatMonitorAge(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	seconds := int(math.Ceil(duration.Seconds()))
	switch {
	case seconds < 60:
		return fmt.Sprintf("%dS", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dM%02dS", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%dH%02dM", seconds/3600, seconds/60%60)
	}
}

func formatMonitorLatency(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	if duration < time.Second {
		return fmt.Sprintf("%dMS", duration.Milliseconds())
	}
	if duration < time.Minute {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", duration.Seconds()), ".0") + "S"
	}
	return fmt.Sprintf("%dM%02dS", int(duration/time.Minute), int(duration/time.Second)%60)
}

func (m Model) monitorRecordedTokens() int64 {
	if m.monitorStartedAt.IsZero() || m.monitorLatest < m.monitorBaseline {
		return 0
	}
	return m.monitorLatest - m.monitorBaseline
}

func (m Model) monitorSessionShare(tokens int64) float64 {
	total := m.monitorRecordedTokens()
	if total <= 0 || tokens <= 0 {
		return 0
	}
	return min(float64(tokens)/float64(total), 1)
}

func (m Model) monitorQuotaReadout() string {
	if m.monitorStartedAt.IsZero() {
		return ""
	}
	if len(m.monitorQuotaWindows) == 0 {
		if m.monitorQuotaError != "" {
			return "ACCOUNT QUOTA Δ // UNAVAILABLE"
		}
		return "ACCOUNT QUOTA Δ // NO WINDOWS"
	}
	parts := []string{"ACCOUNT QUOTA Δ"}
	for _, window := range m.monitorQuotaWindows {
		value := "RESET"
		if m.monitorQuotaError != "" || window.stale {
			value = "STALE"
		} else if !window.resetDetected {
			value = fmt.Sprintf("%+dPP", window.latestUsed-window.baselineUsed)
			if window.partial {
				value += " PARTIAL"
			}
		}
		parts = append(parts, window.label+" "+value)
	}
	return strings.Join(parts, " // ")
}

func (m Model) monitorSessionQuotaEstimate(share float64) string {
	if len(m.monitorQuotaWindows) == 0 {
		return ""
	}
	window := m.monitorQuotaWindows[0]
	prefix := "EST LOCAL-ONLY " + compactMonitorQuotaLabel(window.label)
	if m.monitorError != "" {
		return prefix + " // LOCAL STALE"
	}
	if m.monitorQuotaError != "" || window.stale {
		return prefix + " // STALE"
	}
	if window.resetDetected {
		return prefix + " // RESET"
	}
	if window.partial {
		return prefix + " // PARTIAL"
	}
	delta := window.latestUsed - window.baselineUsed
	if delta == 0 {
		return prefix + " // NO INTEGER Δ"
	}
	estimate := float64(delta) * share
	switch {
	case estimate == 0:
		return prefix + " 0PP"
	case estimate < 1:
		return prefix + " <1PP"
	default:
		return fmt.Sprintf("%s ~%.0fPP", prefix, math.Round(estimate))
	}
}

func compactMonitorQuotaLabel(label string) string {
	for _, unit := range []struct{ suffix, compact string }{
		{" MINUTES", "M"}, {" MINUTE", "M"}, {" HOURS", "H"}, {" HOUR", "H"},
		{" DAYS", "D"}, {" DAY", "D"}, {" WEEKS", "W"}, {" WEEK", "W"},
	} {
		if strings.HasSuffix(label, unit.suffix) {
			return strings.TrimSuffix(label, unit.suffix) + unit.compact
		}
	}
	return label
}

func shortSessionID(id string) string {
	if id == "" {
		return "UNKNOWN"
	}
	if len(id) <= 5 {
		return strings.ToUpper(id)
	}
	return strings.ToUpper(id[len(id)-5:])
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func (m Model) renderMonitorGraphSamples(width, height int, samples []monitorSample, heading string, colors palette) string {
	innerWidth := max(width-4, 1)
	plotHeight := max(height-2, 1)
	const sampleWidth = 2 // one block-wide bar plus one cell of breathing room
	visibleSamples := max((innerWidth+1)/sampleWidth, 1)
	if len(samples) > visibleSamples {
		samples = samples[len(samples)-visibleSamples:]
	}
	maximum := int64(0)
	for _, sample := range samples {
		maximum = max(maximum, sample.intervalTokens)
	}
	yMax := niceTokenCeiling(maximum)
	span := monitorSampleInterval
	if len(samples) > 0 && samples[len(samples)-1].duration > 0 {
		span = samples[len(samples)-1].duration
	}
	spanSeconds := max(int(math.Ceil(span.Seconds())), 1)
	title := fmt.Sprintf("%d SEC %s // AUTO 0-%s", spanSeconds, heading, formatCompactTokens(yMax))

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
	if m.monitorGoEnabled() && area.goRect.contains(localX, localY) {
		return footerButtonMonitorGo
	}
	if m.monitorStopEnabled() && area.stopRect.contains(localX, localY) {
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

func (m Model) monitorSessionElapsed(session monitorSession, now time.Time) time.Duration {
	start := session.startedAt
	if start.IsZero() {
		start = m.monitorStartedAt
	}
	end := now
	if m.monitorState == monitorStopped && !m.monitorStoppedAt.IsZero() {
		end = m.monitorStoppedAt
	}
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
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

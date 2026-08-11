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

func usesMeterGrid(style meterStyleID) bool {
	return style >= styleBars && style < styleStopwatch
}

func renderMeterGrid(width, height int, meters []codex.Meter, style meterStyleID, colors palette) string {
	if len(meters) == 0 {
		return ""
	}
	const gap = 1
	columns := meterGridColumns(width, height, len(meters), style)
	rows := (len(meters) + columns - 1) / columns
	columnWidths := distributeSpace(width-(columns-1)*gap, columns)
	// Keep every meter card exactly the same height. Any indivisible remainder is
	// left below the grid, immediately above the footer, rather than making the
	// first card's visualization taller than the others.
	rowHeight := max((height-(rows-1)*gap)/rows, 1)

	renderedRows := make([]string, 0, rows)
	for row := 0; row < rows; row++ {
		pieces := make([]string, 0, columns*2-1)
		for column := 0; column < columns; column++ {
			meterIndex := row*columns + column
			if column > 0 {
				pieces = append(pieces, strings.Repeat(" ", gap))
			}
			if meterIndex < len(meters) {
				pieces = append(pieces, renderMeterArea(columnWidths[column], rowHeight, meters[meterIndex], style, colors))
			} else {
				pieces = append(pieces, strings.Repeat(" ", columnWidths[column]))
			}
		}
		renderedRows = append(renderedRows, lipgloss.JoinHorizontal(lipgloss.Top, pieces...))
	}
	grid := strings.Join(renderedRows, "\n\n")
	if padding := height - lipgloss.Height(grid); padding > 0 {
		grid += strings.Repeat("\n", padding)
	}
	return grid
}

func meterGridColumns(width, height, meterCount int, style meterStyleID) int {
	if style == styleBars || style == styleConsumptionPace || style == styleFuel {
		return 1
	}
	minimumColumns := 1
	if isRadialStyle(style) && meterCount > 1 {
		minimumColumns = 2
	}
	bestColumns := minimumColumns
	bestDetail := -1
	for columns := minimumColumns; columns <= meterCount; columns++ {
		rows := (meterCount + columns - 1) / columns
		cellWidth := max((width-(columns-1))/columns, 1)
		cellHeight := max((height-(rows-1))/rows, 1)
		innerWidth := max(cellWidth-4, 1)
		visualChrome := 6
		visualHeight := max(cellHeight-visualChrome, 1)
		detail := innerWidth * visualHeight
		switch style {
		case stylePie:
			canvasWidth, canvasHeight := radialCanvasSize(innerWidth, visualHeight, 11)
			detail = canvasWidth * canvasHeight
		}
		if detail > bestDetail || detail == bestDetail && columns > bestColumns {
			bestDetail = detail
			bestColumns = columns
		}
	}
	return bestColumns
}

func isRadialStyle(style meterStyleID) bool {
	return style == stylePie
}

func distributeSpace(total, parts int) []int {
	parts = max(parts, 1)
	total = max(total, parts)
	sizes := make([]int, parts)
	for index := range sizes {
		sizes[index] = total / parts
		if index < total%parts {
			sizes[index]++
		}
	}
	return sizes
}

func renderMeter(width int, meter codex.Meter, style meterStyleID, colors palette) string {
	return renderMeterArea(width, 0, meter, style, colors)
}

func renderMeterArea(width, height int, meter codex.Meter, style meterStyleID, colors palette) string {
	used := min(max(meter.Window.UsedPercent, 0), 100)
	free := 100 - used
	color := meterColor(used, colors)
	innerWidth := max(width-4, 1)

	usedText := lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("USED %3d%%", used))
	freeText := colors.dimmed().Render(fmt.Sprintf("FREE %3d%%", free))
	if style == styleFuel {
		usedText = colors.dimmed().Render(fmt.Sprintf("USED %3d%%", used))
		freeText = lipgloss.NewStyle().Bold(true).Foreground(colors.primary).Render(fmt.Sprintf("FREE %3d%%", free))
	}
	gap := max(innerWidth-lipgloss.Width(usedText)-lipgloss.Width(freeText), 1)
	stats := usedText + strings.Repeat(" ", gap) + freeText
	if style == styleFuel {
		stats = freeText + strings.Repeat(" ", gap) + usedText
	}
	if lipgloss.Width(stats) > innerWidth {
		if style == styleFuel {
			stats = freeText
		} else {
			stats = usedText
		}
	}

	reset := "RESET DATA UNAVAILABLE"
	if meter.Window.ResetsAt != nil {
		at := time.Unix(*meter.Window.ResetsAt, 0)
		reset = fmt.Sprintf("RESET T-%s  //  %s", countdown(at), at.Local().Format("MON 15:04:05"))
		if lipgloss.Width(reset) > innerWidth {
			reset = fmt.Sprintf("RESET T-%s", countdown(at))
		}
	}
	reset = ansi.Truncate(reset, innerWidth, "")

	visualHeight := 0
	if height > 0 {
		visualHeight = max(height-6, 1)
	}
	now := time.Now()
	visual := renderVisualizationSized(innerWidth, visualHeight, used, style, color, colors)
	if style == styleConsumptionPace {
		pace, available := consumptionPace(meter.Window, now)
		visual = renderConsumptionPaceSized(innerWidth, visualHeight, pace, available, colors)
	}
	gaugeWidth := min(max(lipgloss.Width(visual), 1), innerWidth)
	resetGauge := renderResetGauge(innerWidth, gaugeWidth, meter.Window, now, reset, color, colors)
	if style == styleFuel {
		gaugeWidth = max(innerWidth-6, 1)
		resetGauge = renderReverseResetGauge(innerWidth, gaugeWidth, meter.Window, now, reset, color, colors)
	}
	bodyParts := []string{stats, visual, resetGauge}
	body := strings.Join(bodyParts, "\n")
	title := meter.Name
	if meter.Bucket != "codex" {
		title = codex.DisplayName(meter.Bucket) + " // " + title
	}
	title = ansi.Truncate(title+" LOOP", max(innerWidth-4, 1), "")
	frameHeight := 0
	if height > 0 {
		frameHeight = max(height-2, 1)
	}
	return frameSized(width, frameHeight, title, body, color, colors)
}

func renderResetGauge(width, gaugeWidth int, window codex.Window, now time.Time, resetLabel string, color lipgloss.Color, colors palette) string {
	return renderResetGaugeWithOptions(width, gaugeWidth, window, now, resetLabel, color, colors, false, lipgloss.Left)
}

func renderReverseResetGauge(width, gaugeWidth int, window codex.Window, now time.Time, resetLabel string, color lipgloss.Color, colors palette) string {
	return renderResetGaugeWithOptions(width, gaugeWidth, window, now, resetLabel, color, colors, true, lipgloss.Center)
}

func renderResetGaugeWithOptions(width, gaugeWidth int, window codex.Window, now time.Time, resetLabel string, color lipgloss.Color, colors palette, reverse bool, alignment lipgloss.Position) string {
	gaugeWidth = min(max(gaugeWidth, 1), max(width, 1))
	progress, ok := resetProgress(window, now)
	if !ok {
		label := colors.dimmed().Render(ansi.Truncate("RESET CYCLE // RESET DATA UNAVAILABLE", width, ""))
		bar := lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("░", gaugeWidth))
		return label + "\n" + lipgloss.PlaceHorizontal(width, alignment, bar)
	}
	qualifier := ""
	if reverse {
		progress = 100 - progress
		qualifier = " LEFT"
	}
	label := ansi.Truncate(fmt.Sprintf("RESET CYCLE %3d%%%s // %s", progress, qualifier, resetLabel), width, "")
	filled := int(math.Round(float64(gaugeWidth) * float64(progress) / 100))
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("░", gaugeWidth-filled))
	return colors.dimmed().Render(label) + "\n" + lipgloss.PlaceHorizontal(width, alignment, bar)
}

func resetProgress(window codex.Window, now time.Time) (int, bool) {
	if window.WindowDurationMins == nil || window.ResetsAt == nil || *window.WindowDurationMins <= 0 {
		return 0, false
	}
	duration := time.Duration(*window.WindowDurationMins) * time.Minute
	resetAt := time.Unix(*window.ResetsAt, 0)
	startAt := resetAt.Add(-duration)
	progress := int(math.Round(float64(now.Sub(startAt)) / float64(duration) * 100))
	return min(max(progress, 0), 100), true
}

// consumptionPace compares how much of the window has elapsed with how much
// quota has been consumed. Positive headroom means consumption is behind time;
// negative headroom means quota is being consumed too quickly.
func consumptionPace(window codex.Window, now time.Time) (int, bool) {
	elapsed, ok := resetProgress(window, now)
	if !ok {
		return 0, false
	}
	used := min(max(window.UsedPercent, 0), 100)
	return min(max(elapsed-used, -100), 100), true
}

func meterColor(used int, colors palette) lipgloss.Color {
	if used >= 90 {
		return colors.danger
	}
	if used >= 70 {
		return colors.warning
	}
	return colors.primary
}

func renderVisualization(width, used int, style meterStyleID, color lipgloss.Color, colors palette) string {
	return renderVisualizationSized(width, 0, used, style, color, colors)
}

func renderVisualizationSized(width, height, used int, style meterStyleID, color lipgloss.Color, colors palette) string {
	switch style {
	case stylePie:
		return renderPieSized(width, height, used, color, colors)
	case styleConsumptionPace:
		return renderConsumptionPaceSized(width, height, 0, false, colors)
	case styleFuel:
		return renderFuelTankSized(width, height, used, color, colors)
	case styleBars:
		return renderClassicBarSized(width, height, used, color, colors)
	default:
		return renderClassicBarSized(width, height, used, color, colors)
	}
}

func renderClassicBar(width, used int, color lipgloss.Color, colors palette) string {
	return renderClassicBarSized(width, 0, used, color, colors)
}

func renderClassicBarSized(width, height, used int, color lipgloss.Color, colors palette) string {
	filled := int(math.Round(float64(width) * float64(used) / 100))
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("░", width-filled))
	if height <= 0 {
		height = 5
	}
	rows := make([]string, height)
	for row := range rows {
		rows[row] = bar
	}
	if height >= 3 {
		rows[0] = colors.dimmed().Render("0%" + strings.Repeat(" ", max(width-6, 1)) + "100%")
		rows[height-1] = lipgloss.PlaceHorizontal(width, lipgloss.Center,
			lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("LOAD %3d%%", used)))
	}
	return strings.Join(rows, "\n")
}

func renderPie(width, used int, color lipgloss.Color, colors palette) string {
	return renderPieSized(width, 0, used, color, colors)
}

func renderPieSized(width, height, used int, color lipgloss.Color, colors palette) string {
	cellWidth, cellHeight := radialCanvasSize(width, height, 11)
	pixelWidth := cellWidth * 2
	pixelHeight := cellHeight * 4
	centerX := float64(pixelWidth-1) / 2
	centerY := float64(pixelHeight-1) / 2
	radius := float64(min(pixelWidth, pixelHeight))/2 - 1

	rows := make([]string, 0, cellHeight)
	for cellY := 0; cellY < cellHeight; cellY++ {
		var row strings.Builder
		for cellX := 0; cellX < cellWidth; cellX++ {
			mask := 0
			usedDots := 0
			freeDots := 0
			for subY := 0; subY < 4; subY++ {
				for subX := 0; subX < 2; subX++ {
					x := float64(cellX*2+subX) - centerX
					y := float64(cellY*4+subY) - centerY
					if math.Hypot(x, y) > radius {
						continue
					}
					mask |= brailleDot(subX, subY)
					angle := math.Atan2(x, -y)
					if angle < 0 {
						angle += 2 * math.Pi
					}
					if angle/(2*math.Pi)*100 < float64(used) {
						usedDots++
					} else {
						freeDots++
					}
				}
			}
			if mask == 0 {
				row.WriteByte(' ')
				continue
			}
			cellColor := colors.dim
			if usedDots >= freeDots {
				cellColor = color
			}
			row.WriteString(lipgloss.NewStyle().Foreground(cellColor).Render(string(rune(0x2800 + mask))))
		}
		rows = append(rows, row.String())
	}
	label := []string{
		colors.dimmed().Render("BRAILLE PIE"),
		lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("  %3d%%", used)),
		colors.dimmed().Render("CLOCKWISE"),
	}
	label = verticallyCenterLines(label, cellHeight)
	pie := strings.Join(rows, "\n")
	legend := strings.Join(label, "\n")
	visual := lipgloss.JoinHorizontal(lipgloss.Center, pie, "   ", legend)
	if height > 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, visual)
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, visual)
}

func brailleDot(x, y int) int {
	return [...][4]int{
		{1 << 0, 1 << 1, 1 << 2, 1 << 6},
		{1 << 3, 1 << 4, 1 << 5, 1 << 7},
	}[x][y]
}

func radialCanvasSize(width, height, legendWidth int) (int, int) {
	availableWidth := max(width-legendWidth-5, 2)
	if height <= 0 {
		height = max((availableWidth+1)/2, 1)
	}
	cellWidth := max(min(availableWidth, height*2), 1)
	cellHeight := max(min(height, (availableWidth+1)/2), 1)
	return cellWidth, cellHeight
}

func renderConsumptionPaceSized(width, height, pace int, available bool, colors palette) string {
	width = max(width, 1)
	pace = min(max(pace, -100), 100)
	center := (width - 1) / 2
	marker := center
	if available && width > 1 {
		marker = int(math.Round(float64(pace+100) * float64(width-1) / 200))
	}

	markerColor := colors.accent
	if pace > 0 {
		markerColor = colors.primary
	} else if pace < 0 {
		markerColor = colors.danger
	}
	var axis strings.Builder
	for column := 0; column < width; column++ {
		r := '─'
		switch column {
		case 0:
			r = '├'
		case center:
			r = '┼'
		case width - 1:
			r = '┤'
		}
		if column == marker {
			if available {
				axis.WriteString(lipgloss.NewStyle().Bold(true).Foreground(markerColor).Render("◆"))
			} else {
				axis.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colors.warning).Render("?"))
			}
			continue
		}
		axis.WriteString(colors.dimmed().Render(string(r)))
	}

	status := "PACE DATA UNAVAILABLE"
	if available {
		switch {
		case pace > 0:
			status = fmt.Sprintf("HEADROOM %+d POINTS // UNDER PACE", pace)
		case pace < 0:
			status = fmt.Sprintf("DEFICIT %+d POINTS // OVER PACE", pace)
		default:
			status = "BALANCED +0 POINTS // ON PACE"
		}
	}
	status = lipgloss.NewStyle().Bold(true).Foreground(markerColor).Render(ansi.Truncate(status, width, ""))
	caption := colors.dimmed().Render(ansi.Truncate("CONSUMPTION PACE // TIME - USAGE", width, ""))
	labels := colors.dimmed().Render(paceScaleLabels(width))

	lines := []string{caption, labels, axis.String(), status}
	if height <= 0 {
		height = len(lines)
	}
	switch height {
	case 1:
		lines = lines[2:3]
	case 2:
		lines = lines[2:]
	case 3:
		lines = lines[1:]
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}

func paceScaleLabels(width int) string {
	if width < 11 {
		return ansi.Truncate(lipgloss.PlaceHorizontal(width, lipgloss.Center, "-  0  +"), width, "")
	}
	labels := make([]rune, width)
	for index := range labels {
		labels[index] = ' '
	}
	writeAt := func(offset int, value string) {
		for index, r := range []rune(value) {
			if offset+index >= 0 && offset+index < len(labels) {
				labels[offset+index] = r
			}
		}
	}
	writeAt(0, "-100")
	writeAt((width-1)/2, "0")
	writeAt(width-4, "+100")
	return string(labels)
}

func verticallyCenterLines(lines []string, height int) []string {
	if height <= len(lines) {
		return lines[:height]
	}
	top := (height - len(lines)) / 2
	centered := make([]string, 0, height)
	centered = append(centered, make([]string, top)...)
	centered = append(centered, lines...)
	centered = append(centered, make([]string, height-len(centered))...)
	return centered
}

func renderFuelTank(width, used int, color lipgloss.Color, colors palette) string {
	return renderFuelTankSized(width, 6, used, color, colors)
}

func renderFuelTankSized(width, height, used int, color lipgloss.Color, colors palette) string {
	height = max(height, 1)
	remaining := 100 - used
	tankWidth := max(width-6, 1)
	available := int(math.Round(float64(tankWidth) * float64(remaining) / 100))
	tank := lipgloss.NewStyle().Foreground(colors.primary).Render(strings.Repeat("▰", available)) +
		lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("▱", tankWidth-available))
	label := lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("RANGE %3d%%", remaining))
	top := "╭" + strings.Repeat("─", tankWidth+2) + "╮"
	middle := "│ " + tank + " │"
	bottom := "╰" + strings.Repeat("─", tankWidth+2) + "╯"
	if height == 1 {
		return lipgloss.PlaceHorizontal(width, lipgloss.Center, label)
	}
	rows := make([]string, height)
	rows[0] = colors.dimmed().Render(top)
	rows[height-1] = colors.dimmed().Render(bottom)
	for row := 1; row < height-1; row++ {
		rows[row] = middle
	}
	if height >= 4 {
		rows[0] = colors.dimmed().Render("E" + strings.Repeat(" ", max(tankWidth, 1)) + "F")
		rows[height-1] = label
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, strings.Join(rows, "\n"))
}

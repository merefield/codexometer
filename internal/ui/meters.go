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
	return style >= styleBars && style < styleCount
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
	if style == styleBars || style == styleFuel {
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
		case styleRotary, styleTachometer:
			canvasWidth, canvasHeight := radialCanvasSize(innerWidth, visualHeight, 12)
			detail = canvasWidth * canvasHeight
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
	return style == styleRotary || style == stylePie || style == styleTachometer
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
	gap := max(innerWidth-lipgloss.Width(usedText)-lipgloss.Width(freeText), 1)
	stats := usedText + strings.Repeat(" ", gap) + freeText
	if lipgloss.Width(stats) > innerWidth {
		stats = usedText
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
	visual := renderVisualizationSized(innerWidth, visualHeight, used, style, color, colors)
	gaugeWidth := min(max(lipgloss.Width(visual), 1), innerWidth)
	bodyParts := []string{stats, visual, renderResetGauge(innerWidth, gaugeWidth, meter.Window, time.Now(), reset, color, colors)}
	body := strings.Join(bodyParts, "\n")
	title := meter.Name
	if meter.Bucket != "codex" {
		title = codex.DisplayName(meter.Bucket) + " // " + title
	}
	title = ansi.Truncate(title, max(innerWidth-4, 1), "")
	frameHeight := 0
	if height > 0 {
		frameHeight = max(height-2, 1)
	}
	return frameSized(width, frameHeight, title+" LOOP", body, color, colors)
}

func renderResetGauge(width, gaugeWidth int, window codex.Window, now time.Time, resetLabel string, color lipgloss.Color, colors palette) string {
	gaugeWidth = min(max(gaugeWidth, 1), max(width, 1))
	progress, ok := resetProgress(window, now)
	if !ok {
		label := colors.dimmed().Render(ansi.Truncate("RESET CYCLE // RESET DATA UNAVAILABLE", width, ""))
		bar := lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("░", gaugeWidth))
		return label + "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Left, bar)
	}
	label := ansi.Truncate(fmt.Sprintf("RESET CYCLE %3d%% // %s", progress, resetLabel), width, "")
	filled := int(math.Round(float64(gaugeWidth) * float64(progress) / 100))
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("░", gaugeWidth-filled))
	return colors.dimmed().Render(label) + "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Left, bar)
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
	case styleRotary:
		return renderRotarySized(width, height, used, color, colors)
	case stylePie:
		return renderPieSized(width, height, used, color, colors)
	case styleTachometer:
		return renderTachometerSized(width, height, used, color, colors)
	case styleCrashBar:
		return renderCrashBarSized(width, height, used, color, colors)
	case styleFuel:
		return renderFuelTankSized(width, height, used, color, colors)
	case styleFuse:
		return renderFuseSized(width, height, used, color, colors)
	case stylePac:
		return renderPelletRunSized(width, height, used, color, colors)
	case styleBoat:
		return renderSinkingShipSized(width, height, used, color, colors)
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

func renderRotary(width, used int, color lipgloss.Color, colors palette) string {
	return renderRotarySized(width, 0, used, color, colors)
}

func renderRotarySized(width, height, used int, color lipgloss.Color, colors palette) string {
	return renderBrailleGauge(width, height, used, color, colors, false)
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

type braillePixel uint8

const (
	pixelEmpty braillePixel = iota
	pixelDim
	pixelActive
	pixelDanger
)

type brailleCanvas struct {
	cellWidth  int
	cellHeight int
	pixels     []braillePixel
}

func newBrailleCanvas(cellWidth, cellHeight int) brailleCanvas {
	return brailleCanvas{
		cellWidth:  cellWidth,
		cellHeight: cellHeight,
		pixels:     make([]braillePixel, cellWidth*2*cellHeight*4),
	}
}

func (c brailleCanvas) pixelWidth() int  { return c.cellWidth * 2 }
func (c brailleCanvas) pixelHeight() int { return c.cellHeight * 4 }

func (c brailleCanvas) set(x, y int, pixel braillePixel) {
	if x < 0 || x >= c.pixelWidth() || y < 0 || y >= c.pixelHeight() {
		return
	}
	index := y*c.pixelWidth() + x
	if pixel > c.pixels[index] {
		c.pixels[index] = pixel
	}
}

func (c brailleCanvas) render(color lipgloss.Color, colors palette) string {
	rows := make([]string, 0, c.cellHeight)
	for cellY := 0; cellY < c.cellHeight; cellY++ {
		var row strings.Builder
		for cellX := 0; cellX < c.cellWidth; cellX++ {
			mask := 0
			cellPixel := pixelEmpty
			for subY := 0; subY < 4; subY++ {
				for subX := 0; subX < 2; subX++ {
					pixel := c.pixels[(cellY*4+subY)*c.pixelWidth()+cellX*2+subX]
					if pixel == pixelEmpty {
						continue
					}
					mask |= brailleDot(subX, subY)
					cellPixel = max(cellPixel, pixel)
				}
			}
			if mask == 0 {
				row.WriteByte(' ')
				continue
			}
			cellColor := colors.dim
			switch cellPixel {
			case pixelActive:
				cellColor = color
			case pixelDanger:
				cellColor = colors.danger
			}
			row.WriteString(lipgloss.NewStyle().Foreground(cellColor).Render(string(rune(0x2800 + mask))))
		}
		rows = append(rows, row.String())
	}
	return strings.Join(rows, "\n")
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

func renderBrailleGauge(width, height, used int, color lipgloss.Color, colors palette, tachometer bool) string {
	cellWidth, cellHeight := radialCanvasSize(width, height, 12)
	canvas := newBrailleCanvas(cellWidth, cellHeight)
	centerX := float64(canvas.pixelWidth()-1) / 2
	centerY := float64(canvas.pixelHeight()-1) / 2
	radius := float64(min(canvas.pixelWidth(), canvas.pixelHeight()))/2 - 1.5

	for y := 0; y < canvas.pixelHeight(); y++ {
		for x := 0; x < canvas.pixelWidth(); x++ {
			distance := math.Hypot(float64(x)-centerX, float64(y)-centerY)
			if math.Abs(distance-radius) <= 0.7 {
				canvas.set(x, y, pixelDim)
			}
		}
	}

	for tick := 0; tick <= 10; tick++ {
		progress := float64(tick) / 10
		theta := 5*math.Pi/4 - progress*3*math.Pi/2
		for inset := 0.0; inset <= 2.0; inset += 0.5 {
			x := int(math.Round(centerX + math.Cos(theta)*(radius-inset)))
			y := int(math.Round(centerY - math.Sin(theta)*(radius-inset)))
			pixel := pixelDim
			if tachometer && tick >= 8 {
				pixel = pixelDanger
			}
			canvas.set(x, y, pixel)
		}
	}

	theta := 5*math.Pi/4 - float64(used)/100*3*math.Pi/2
	needleLength := radius * 0.72
	steps := max(int(math.Ceil(needleLength*2)), 1)
	for step := 0; step <= steps; step++ {
		distance := needleLength * float64(step) / float64(steps)
		x := int(math.Round(centerX + math.Cos(theta)*distance))
		y := int(math.Round(centerY - math.Sin(theta)*distance))
		canvas.set(x, y, pixelActive)
	}
	for y := int(centerY) - 1; y <= int(centerY)+1; y++ {
		for x := int(centerX) - 1; x <= int(centerX)+1; x++ {
			canvas.set(x, y, pixelActive)
		}
	}

	legend := []string{}
	if tachometer {
		rpm := float64(used) * 80
		legend = append(legend,
			colors.dimmed().Render("QUOTA RPM"),
			lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("%3d%%", used)),
			lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("%3.1fK RPM", rpm/1000)),
			lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render("REDLINE 80"),
		)
	} else {
		legend = append(legend,
			colors.dimmed().Render("ROTARY"),
			lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("%3d%%", used)),
			colors.dimmed().Render("0 ↔ 100"),
		)
	}
	legend = verticallyCenterLines(legend, cellHeight)
	visual := lipgloss.JoinHorizontal(lipgloss.Center, canvas.render(color, colors), "   ", strings.Join(legend, "\n"))
	if height > 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, visual)
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, visual)
}

func renderTachometer(width, used int, color lipgloss.Color, colors palette) string {
	return renderTachometerSized(width, 0, used, color, colors)
}

func renderTachometerSized(width, height, used int, color lipgloss.Color, colors palette) string {
	return renderBrailleGauge(width, height, used, color, colors, true)
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

func renderCrashBar(width, used int, color lipgloss.Color, colors palette) string {
	return renderCrashBarSized(width, 5, used, color, colors)
}

func renderCrashBarSized(width, height, used int, color lipgloss.Color, colors palette) string {
	height = max(height, 1)
	roadWidth := max(width-8, 8)
	distance := int(math.Round(float64(roadWidth-1) * float64(100-used) / 100))
	ahead := strings.Repeat("─", distance)
	behind := strings.Repeat("━", max(roadWidth-distance-1, 0))
	car := "🚗"
	wall := "█▌"
	if used >= 100 {
		car = "💥🚗"
		ahead = ""
	}
	road := lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render(wall) +
		lipgloss.NewStyle().Foreground(colors.dim).Render(ahead) + car +
		lipgloss.NewStyle().Foreground(color).Render(behind)
	rows := make([]string, height)
	roadRow := max((height-1)/2, 0)
	for row := range rows {
		switch {
		case row == roadRow:
			rows[row] = road
		case row == roadRow-1:
			rows[row] = colors.dimmed().Render(strings.Repeat("─", width))
		case row == roadRow+1:
			rows[row] = colors.dimmed().Render(strings.Repeat("═", width))
		case row == height-1 && height >= 5:
			caption := colors.dimmed().Render(ansi.Truncate("QUOTA RUN // IMPACT AT 100%", width, ""))
			rows[row] = lipgloss.PlaceHorizontal(width, lipgloss.Center, caption)
		default:
			starOffset := (row * 7) % max(width, 1)
			rows[row] = colors.dimmed().Render(strings.Repeat(" ", starOffset) + ".")
		}
	}
	return strings.Join(rows, "\n")
}

func renderFuelTank(width, used int, color lipgloss.Color, colors palette) string {
	return renderFuelTankSized(width, 6, used, color, colors)
}

func renderFuelTankSized(width, height, used int, color lipgloss.Color, colors palette) string {
	height = max(height, 1)
	remaining := 100 - used
	tankWidth := max(width-6, 1)
	filled := int(math.Round(float64(tankWidth) * float64(remaining) / 100))
	tank := lipgloss.NewStyle().Foreground(colors.primary).Render(strings.Repeat("▰", filled)) +
		lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("▱", tankWidth-filled))
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
		rows[0] = colors.dimmed().Render("F" + strings.Repeat(" ", max(tankWidth, 1)) + "E")
		rows[height-1] = label
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, strings.Join(rows, "\n"))
}

func renderFuse(width, used int, color lipgloss.Color, colors palette) string {
	fuseWidth := max(width-20, 10)
	position := int(math.Round(float64(fuseWidth-1) * float64(used) / 100))
	if used >= 100 {
		explosion := []string{
			"      💥     💥      ",
			"   💥    ╲ │ ╱   💥  ",
			" 💥    ── BOOM! ──   💥",
			"   💥    ╱ │ ╲   💥  ",
			"      DETONATED       ",
		}
		return lipgloss.PlaceHorizontal(width, lipgloss.Center,
			lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render(strings.Join(explosion, "\n")))
	}
	burned := lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("·", position))
	live := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("━", fuseWidth-position-1))
	bomb := lipgloss.NewStyle().Bold(true).Foreground(colors.danger)
	lines := []string{
		strings.Repeat(" ", fuseWidth+4) + bomb.Render("╭─────╮"),
		strings.Repeat(" ", fuseWidth+2) + bomb.Render("╭─┤     ├─╮"),
		burned + "🔥" + live + "━━" + bomb.Render("┤  ●  │") + " " + colors.dimmed().Render("FUSE"),
		strings.Repeat(" ", fuseWidth+2) + bomb.Render("│     │"),
		strings.Repeat(" ", fuseWidth+2) + bomb.Render("│ TNT │"),
		strings.Repeat(" ", fuseWidth+2) + bomb.Render("╰─────╯"),
	}
	return strings.Join(lines, "\n")
}

func renderFuseSized(width, height, used int, color lipgloss.Color, colors palette) string {
	if height <= 0 {
		return renderFuse(width, used, color, colors)
	}
	height = max(height, 1)
	if used >= 100 {
		rows := make([]string, height)
		for row := range rows {
			burst := "💥"
			if row == height/2 {
				burst = "💥  BOOM! // DETONATED  💥"
			}
			rows[row] = lipgloss.PlaceHorizontal(width, lipgloss.Center,
				lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render(burst))
		}
		return strings.Join(rows, "\n")
	}

	bombInnerWidth := max(min(width/3, height*2), 3)
	bombWidth := bombInnerWidth + 2
	bombLeft := max(width-bombWidth, 0)
	fuseWidth := max(bombLeft-2, 1)
	position := int(math.Round(float64(fuseWidth-1) * float64(used) / 100))
	rows := make([]string, height)
	middle := height / 2
	for row := range rows {
		prefix := strings.Repeat(" ", bombLeft)
		switch row {
		case 0:
			rows[row] = prefix + lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render("╭"+strings.Repeat("─", bombInnerWidth)+"╮")
		case height - 1:
			rows[row] = prefix + lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render("╰"+strings.Repeat("─", bombInnerWidth)+"╯")
		case middle:
			burned := lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("·", position))
			live := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("━", max(fuseWidth-position-1, 0)))
			rows[row] = burned + "🔥" + live + lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render("┤"+centerText("●", bombInnerWidth)+"│")
		default:
			label := ""
			if row == middle+1 {
				label = centerText("TNT", bombInnerWidth)
			} else {
				label = strings.Repeat(" ", bombInnerWidth)
			}
			rows[row] = prefix + lipgloss.NewStyle().Bold(true).Foreground(colors.danger).Render("│"+label+"│")
		}
	}
	return strings.Join(rows, "\n")
}

func centerText(value string, width int) string {
	value = ansi.Truncate(value, width, "")
	left := max((width-lipgloss.Width(value))/2, 0)
	right := max(width-lipgloss.Width(value)-left, 0)
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", right)
}

func renderPelletRun(width, used int, color lipgloss.Color, colors palette) string {
	pelletCount := max((width-12)/3, 5)
	position := int(math.Round(float64(pelletCount-1) * float64(used) / 100))
	indent := strings.Repeat("   ", position)
	pellets := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("  •", pelletCount-position-1))
	finish := "👻"
	if used >= 100 {
		finish = "💥"
	}
	pac := lipgloss.NewStyle().Bold(true).Foreground(color)
	return strings.Join([]string{
		indent + pac.Render(" ╭──╮"),
		indent + pac.Render("╭╯  ╲"),
		colors.dimmed().Render(strings.Repeat("  ·", position)) + pac.Render("│   <") + pellets + "  " + finish,
		indent + pac.Render("╰╮  ╱"),
		indent + pac.Render(" ╰──╯") + "  " + colors.dimmed().Render("PELLET RUN"),
	}, "\n")
}

func renderPelletRunSized(width, height, used int, color lipgloss.Color, colors palette) string {
	if height <= 0 {
		return renderPelletRun(width, used, color, colors)
	}
	legendWidth := max(width/2, 1)
	cellWidth, cellHeight := radialCanvasSize(width, height, legendWidth)
	canvas := newBrailleCanvas(cellWidth, cellHeight)
	centerX := float64(canvas.pixelWidth()-1) / 2
	centerY := float64(canvas.pixelHeight()-1) / 2
	radius := float64(min(canvas.pixelWidth(), canvas.pixelHeight()))/2 - 1
	for y := 0; y < canvas.pixelHeight(); y++ {
		for x := 0; x < canvas.pixelWidth(); x++ {
			dx := float64(x) - centerX
			dy := float64(y) - centerY
			if math.Hypot(dx, dy) > radius {
				continue
			}
			if dx > 0 && math.Abs(dy) < dx*0.55 {
				continue
			}
			canvas.set(x, y, pixelActive)
		}
	}
	pelletCount := max((width-cellWidth-5)/3, 1)
	remaining := int(math.Round(float64(pelletCount) * float64(100-used) / 100))
	pellets := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("•  ", remaining))
	finish := "👻"
	if used >= 100 {
		finish = "💥"
	}
	legend := verticallyCenterLines([]string{
		pellets + finish,
		colors.dimmed().Render("PELLET RUN"),
		lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("EATEN %3d%%", used)),
	}, cellHeight)
	visual := lipgloss.JoinHorizontal(lipgloss.Center, canvas.render(color, colors), "   ", strings.Join(legend, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, visual)
}

func renderSinkingShip(width, used int, color lipgloss.Color, colors palette) string {
	return renderSinkingShipSized(width, 7, used, color, colors)
}

func renderSinkingShipSized(width, height, used int, color lipgloss.Color, colors palette) string {
	height = max(height, 1)
	legendWidth := max(min(12, width-4), 1)
	seaWidth := max(width-legendWidth-3, 1)
	boatRow := int(math.Round(float64(height-1) * float64(used) / 100))
	if used >= 100 {
		boatRow = height - 1
	}
	waterRow := min(max(height*2/5, 0), height-1)
	rows := make([]string, height)
	for row := range rows {
		if row == waterRow {
			rows[row] = lipgloss.NewStyle().Foreground(colors.accent).Render(strings.Repeat("≈", seaWidth))
		} else {
			rows[row] = strings.Repeat(" ", seaWidth)
		}
		if row == boatRow {
			boat := "⛵⛵"
			if used >= 100 {
				boat = "⚓⚓"
			}
			boatWidth := lipgloss.Width(boat)
			left := max((seaWidth-boatWidth)/2, 0)
			right := max(seaWidth-left-boatWidth, 0)
			fill := " "
			if row == waterRow {
				fill = "≈"
			}
			rows[row] = lipgloss.NewStyle().Foreground(colors.accent).Render(strings.Repeat(fill, left)) +
				lipgloss.NewStyle().Bold(true).Foreground(color).Render(boat) +
				lipgloss.NewStyle().Foreground(colors.accent).Render(strings.Repeat(fill, right))
		}
	}
	state := "AFLOAT"
	if used >= 100 {
		state = "SUNK"
	} else if used >= 67 {
		state = "SWAMPED"
	} else if used >= 34 {
		state = "TAKING WATER"
	}
	legend := []string{
		colors.dimmed().Render("HULL STATUS"),
		lipgloss.NewStyle().Bold(true).Foreground(color).Render(state),
		colors.dimmed().Render(fmt.Sprintf("FLOODED %3d%%", used)),
	}
	legend = verticallyCenterLines(legend, height)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinHorizontal(lipgloss.Center, strings.Join(rows, "\n"), "   ", strings.Join(legend, "\n")))
}

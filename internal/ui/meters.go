package ui

import (
	"fmt"
	imagecolor "image/color"
	"math"
	"math/bits"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
	"github.com/merefield/codexometer/internal/quotagraph"
)

func usesMeterGrid(view meterViewID) bool {
	switch view {
	case viewBars, viewPie, viewPace, viewFuel:
		return true
	default:
		return false
	}
}

func renderMeterGrid(width, height int, meters []codex.Meter, view meterViewID, colors palette, graphs ...quotaPlotOptions) string {
	if len(meters) == 0 {
		return ""
	}
	const columnGap = 1
	columns := meterGridColumns(width, height, len(meters), view)
	rows := (len(meters) + columns - 1) / columns
	rowGap := 1
	// A framed meter needs at least its title, body, and bottom border. Drop the
	// decorative blank row before shrinking cards below that useful minimum.
	if height < rows*3+(rows-1)*rowGap {
		rowGap = 0
	}
	columnWidths := distributeSpace(width-(columns-1)*columnGap, columns)
	// Keep every meter card exactly the same height. Any indivisible remainder is
	// left below the grid, immediately above the footer, rather than making the
	// first card's visualization taller than the others.
	rowHeight := max((height-(rows-1)*rowGap)/rows, 1)

	renderedRows := make([]string, 0, rows)
	for row := 0; row < rows; row++ {
		pieces := make([]string, 0, columns*2-1)
		for column := 0; column < columns; column++ {
			meterIndex := row*columns + column
			if column > 0 {
				pieces = append(pieces, strings.Repeat(" ", columnGap))
			}
			if meterIndex < len(meters) {
				var options []quotaPlotOptions
				if meterIndex < len(graphs) {
					options = graphs[meterIndex : meterIndex+1]
				}
				pieces = append(pieces, renderMeterArea(columnWidths[column], rowHeight, meters[meterIndex], view, colors, options...))
			} else {
				pieces = append(pieces, strings.Repeat(" ", columnWidths[column]))
			}
		}
		renderedRows = append(renderedRows, lipgloss.JoinHorizontal(lipgloss.Top, pieces...))
	}
	grid := strings.Join(renderedRows, strings.Repeat("\n", rowGap+1))
	if padding := height - lipgloss.Height(grid); padding > 0 {
		grid += strings.Repeat("\n", padding)
	}
	return grid
}

func meterGridColumns(width, height, meterCount int, view meterViewID) int {
	if view == viewBars || view == viewFuel {
		return 1
	}
	if isQuotaGraph(view) {
		return max(1, min(meterCount, width/36))
	}
	minimumColumns := 1
	maximumColumns := meterCount
	if isRadialView(view) && meterCount > 1 {
		minimumColumns = 2
		// Below this width the circular canvas and title both become ambiguous.
		// Retain at least two columns, then add more only when each card remains
		// meaningfully readable.
		const minimumRadialCardWidth = 28
		maximumColumns = min(meterCount, max(width/minimumRadialCardWidth, minimumColumns))
	}
	bestColumns := minimumColumns
	bestDetail := -1
	for columns := minimumColumns; columns <= maximumColumns; columns++ {
		rows := (meterCount + columns - 1) / columns
		cellWidth := max((width-(columns-1))/columns, 1)
		cellHeight := max((height-(rows-1))/rows, 1)
		innerWidth := max(cellWidth-4, 1)
		visualChrome := 6
		if view == viewPie {
			visualChrome-- // The reset comparison is part of the canvas.
		}
		visualHeight := max(cellHeight-visualChrome, 1)
		detail := innerWidth * visualHeight
		switch view {
		case viewPie:
			fullCycle := 100
			pie := layoutPie(innerWidth, visualHeight, 100, &fullCycle)
			detail = pie.width * pie.height
		}
		if detail > bestDetail || detail == bestDetail && columns > bestColumns {
			bestDetail = detail
			bestColumns = columns
		}
	}
	return bestColumns
}

func isRadialView(view meterViewID) bool {
	return view == viewPie
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

func renderMeter(width int, meter codex.Meter, view meterViewID, colors palette) string {
	return renderMeterArea(width, 0, meter, view, colors)
}

func renderMeterArea(width, height int, meter codex.Meter, view meterViewID, colors palette, graphs ...quotaPlotOptions) string {
	used := min(max(meter.Window.UsedPercent, 0), 100)
	free := 100 - used
	color := meterColor(used, colors)
	innerWidth := max(width-4, 1)

	usedText := lipgloss.NewStyle().Bold(true).Foreground(color).Render(i18n.Format("USED %3d%%", used))
	freeText := colors.dimmed().Render(i18n.Format("FREE %3d%%", free))
	if view == viewFuel {
		usedText = colors.dimmed().Render(i18n.Format("USED %3d%%", used))
		freeText = lipgloss.NewStyle().Bold(true).Foreground(colors.primary).Render(i18n.Format("FREE %3d%%", free))
	}
	gap := max(innerWidth-lipgloss.Width(usedText)-lipgloss.Width(freeText), 1)
	stats := usedText + strings.Repeat(" ", gap) + freeText
	if view == viewFuel {
		stats = freeText + strings.Repeat(" ", gap) + usedText
	}
	if lipgloss.Width(stats) > innerWidth {
		if view == viewFuel {
			stats = freeText
		} else {
			stats = usedText
		}
	}
	stats = ansi.Truncate(stats, innerWidth, "")

	reset := i18n.Text("RESET DATA UNAVAILABLE")
	remaining := ""
	if meter.Window.ResetsAt != nil {
		at := time.Unix(*meter.Window.ResetsAt, 0)
		remaining = countdown(at)
		reset = i18n.Format("RESET T-%s  //  %s", remaining, formatResetDeadline(at))
		if lipgloss.Width(reset) > innerWidth {
			reset = i18n.Format("RESET T-%s", remaining)
		}
	}
	reset = ansi.Truncate(reset, innerWidth, "")

	bodyHeight := 0
	visualHeight := 0
	detailLines := meterDetailLines(meter.Details)
	if isQuotaGraph(view) && height > 0 {
		// Preserve the plot before optional API-equivalent telemetry.
		detailLines = detailLines[:min(len(detailLines), max(height-10, 0))]
	} else if view == viewPie && height > 0 {
		// Leave room for the ring before adding optional telemetry.
		detailLines = detailLines[:min(len(detailLines), max(height-8, 0))]
	}
	if height > 0 {
		bodyHeight = max(height-2, 1)
		chromeHeight := 3 + len(detailLines)
		if view == viewPie || isQuotaGraph(view) {
			chromeHeight = 2 + len(detailLines)
		}
		visualHeight = max(bodyHeight-chromeHeight, 1)
	}
	now := time.Now()
	var visual, resetGauge string
	switch {
	case view == viewPie:
		progress, known := resetProgress(meter.Window, now)
		var cycle *int
		if known {
			cycle = &progress
		}
		pie := layoutPie(innerWidth, visualHeight, used, cycle)
		visual = renderPieCanvas(innerWidth, visualHeight, used, cycle, pie, color, colors)
		if len(pie.legend) > 0 {
			// The legend already shows usage; keep only the free capacity up top.
			stats = lipgloss.PlaceHorizontal(innerWidth, lipgloss.Right, freeText)
		}
		label := reset
		if !pie.ring {
			label = resetCycleLabel(meter.Window, progress, known, reset, false)
		} else if len(pie.legend) == 0 {
			label = compactPieCycleLabel(progress, reset, remaining, innerWidth)
		}
		resetGauge = colors.dimmed().Render(ansi.Truncate(label, innerWidth, ""))
	case isQuotaGraph(view):
		options := quotaPlotOptions{}
		if len(graphs) > 0 {
			options = graphs[0]
		}
		visual = renderQuotaPlot(innerWidth, visualHeight, meter.Window, now, options, colors)
		resetGauge = colors.dimmed().Render(reset)
	default:
		visual = renderVisualizationSized(innerWidth, visualHeight, used, view, color, colors)
		if view == viewFuel {
			resetGauge = renderReverseResetGauge(innerWidth, max(innerWidth-6, 1), meter.Window, now, reset, color, colors)
		} else {
			resetGauge = renderResetGauge(innerWidth, lipgloss.Width(visual), meter.Window, now, reset, color, colors)
		}
	}
	bodyParts := []string{stats}
	for _, detail := range detailLines {
		bodyParts = append(bodyParts, colors.dimmed().Render(ansi.Truncate(detail, innerWidth, "")))
	}
	bodyParts = append(bodyParts, visual, resetGauge)
	if height > 0 && bodyHeight < 4 {
		switch bodyHeight {
		case 1:
			bodyParts = []string{visual}
		case 2:
			bodyParts = []string{stats, visual}
		case 3:
			resetLine := strings.Split(resetGauge, "\n")[0]
			bodyParts = []string{stats, visual, resetLine}
		}
	} else if height > 0 && bodyHeight == 4 && len(detailLines) > 0 {
		resetLine := strings.Split(resetGauge, "\n")[0]
		bodyParts = []string{stats, colors.dimmed().Render(ansi.Truncate(detailLines[0], innerWidth, "")), visual, resetLine}
	}
	body := strings.Join(bodyParts, "\n")
	title := i18n.WindowName(meter.Name)
	if meter.Bucket != "codex" {
		title = codex.DisplayName(meter.Bucket) + " // " + title
	}
	if meter.Kind == codex.MeterQuotaWindow {
		title += i18n.Text(" LOOP")
	}
	title = ansi.Truncate(title, max(innerWidth-4, 1), "")
	frameHeight := 0
	if height > 0 {
		frameHeight = max(height-2, 1)
	}
	return frameSized(width, frameHeight, title, body, color, colors)
}

func formatResetDeadline(at time.Time) string {
	return strings.ToUpper(i18n.Text(at.Local().Format("Mon")) + " " + at.Local().Format("15:04:05"))
}

func meterDetailLines(details string) []string {
	if strings.TrimSpace(details) == "" {
		return nil
	}
	lines := strings.Split(details, "\n")
	visible := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			visible = append(visible, line)
		}
	}
	return visible
}

func renderResetGauge(width, gaugeWidth int, window codex.Window, now time.Time, resetLabel string, color imagecolor.Color, colors palette) string {
	return renderResetGaugeWithOptions(width, gaugeWidth, window, now, resetLabel, color, colors, false, lipgloss.Left)
}

func renderReverseResetGauge(width, gaugeWidth int, window codex.Window, now time.Time, resetLabel string, color imagecolor.Color, colors palette) string {
	return renderResetGaugeWithOptions(width, gaugeWidth, window, now, resetLabel, color, colors, true, lipgloss.Center)
}

func renderResetGaugeWithOptions(width, gaugeWidth int, window codex.Window, now time.Time, resetLabel string, color imagecolor.Color, colors palette, reverse bool, alignment lipgloss.Position) string {
	gaugeWidth = min(max(gaugeWidth, 1), max(width, 1))
	progress, ok := resetProgress(window, now)
	if reverse {
		progress = 100 - progress
	}
	label := colors.dimmed().Render(ansi.Truncate(resetCycleLabel(window, progress, ok, resetLabel, reverse), width, ""))
	if !ok {
		bar := colors.dimmed().Render(strings.Repeat("░", gaugeWidth))
		return label + "\n" + lipgloss.PlaceHorizontal(width, alignment, bar)
	}
	filled := int(math.Round(float64(gaugeWidth) * float64(progress) / 100))
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("░", gaugeWidth-filled))
	return label + "\n" + lipgloss.PlaceHorizontal(width, alignment, bar)
}

func resetCycleLabel(window codex.Window, progress int, known bool, resetLabel string, reverse bool) string {
	if !known {
		if window.ResetsAt != nil {
			return i18n.Text("CYCLE START UNAVAILABLE // ") + resetLabel
		}
		return i18n.Text("RESET CYCLE // RESET DATA UNAVAILABLE")
	}
	qualifier := ""
	if reverse {
		qualifier = " LEFT"
	}
	return i18n.Format("RESET CYCLE %3d%%%s // %s", progress, qualifier, resetLabel)
}

func resetProgress(window codex.Window, now time.Time) (int, bool) {
	fraction, known := quotagraph.ElapsedFraction(window, now)
	return int(math.Round(fraction * 100)), known
}

func meterColor(used int, colors palette) imagecolor.Color {
	if used >= 90 {
		return colors.danger
	}
	if used >= 70 {
		return colors.warning
	}
	return colors.primary
}

func renderVisualization(width, used int, view meterViewID, color imagecolor.Color, colors palette) string {
	return renderVisualizationSized(width, 0, used, view, color, colors)
}

func renderVisualizationSized(width, height, used int, view meterViewID, color imagecolor.Color, colors palette) string {
	switch view {
	case viewPie:
		return renderPieSized(width, height, used, nil, color, colors)
	case viewPace:
		return renderQuotaPlot(width, height, codex.Window{}, time.Now(), quotaPlotOptions{}, colors)
	case viewFuel:
		return renderFuelTankSized(width, height, used, color, colors)
	case viewBars:
		return renderClassicBarSized(width, height, used, color, colors)
	default:
		return renderClassicBarSized(width, height, used, color, colors)
	}
}

func renderClassicBar(width, used int, color imagecolor.Color, colors palette) string {
	return renderClassicBarSized(width, 0, used, color, colors)
}

func renderClassicBarSized(width, height, used int, color imagecolor.Color, colors palette) string {
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

func renderPie(width, used int, color imagecolor.Color, colors palette) string {
	return renderPieSized(width, 0, used, nil, color, colors)
}

type pieLayout struct {
	width, height int
	ring          bool
	legend        []string
}

func layoutPie(width, height, used int, cycle *int) pieLayout {
	canvasWidth, canvasHeight := radialCanvasSize(width, height, 0)
	layout := pieLayout{width: canvasWidth, height: canvasHeight, ring: pieRingFits(canvasWidth, canvasHeight, cycle)}
	labels := []string{i18n.Format("PIE  // %d%% USED", used)}
	if layout.ring {
		labels = append(labels, i18n.Format("RING // %d%% ELAPSED", *cycle))
	}
	legendWidth := 0
	for _, label := range labels {
		legendWidth = max(legendWidth, lipgloss.Width(label))
	}
	const minCanvasWidth = 8
	if width < legendWidth+radialLegendOverhead+minCanvasWidth {
		return layout
	}
	w, h := radialCanvasSize(width, height, legendWidth)
	ring := pieRingFits(w, h, cycle)
	if h >= len(labels) && (!layout.ring || ring) {
		layout = pieLayout{width: w, height: h, ring: ring, legend: labels}
	}
	return layout
}

func pieRingFits(width, height int, cycle *int) bool {
	return cycle != nil && min(width, height*2) >= 8
}

func compactPieCycleLabel(progress int, reset, countdown string, width int) string {
	ring := i18n.Format("RING %d%% ELAPSED", progress)
	for _, deadline := range []string{reset, "T-" + countdown} {
		if label := ring + " // " + deadline; lipgloss.Width(label) <= width {
			return label
		}
	}
	return ring
}

// cycle is nil when the elapsed percentage is unknown.
func renderPieSized(width, height, used int, cycle *int, color imagecolor.Color, colors palette) string {
	return renderPieCanvas(width, height, used, cycle, layoutPie(width, height, used, cycle), color, colors)
}

func renderPieCanvas(width, height, used int, cycle *int, layout pieLayout, color imagecolor.Color, colors palette) string {
	cellWidth, cellHeight := layout.width, layout.height
	pixelWidth := cellWidth * 2
	pixelHeight := cellHeight * 4
	centerX := float64(pixelWidth-1) / 2
	centerY := float64(pixelHeight-1) / 2
	radius := float64(min(pixelWidth, pixelHeight))/2 - 1
	sliceRadius, ringInner := radius, radius
	percent := [2]int{used}
	if layout.ring {
		// Tiny cards keep the quota slice and textual cycle readout instead
		// of forcing two shapes into the same handful of Braille cells.
		percent[1] = *cycle
		ringInner = radius - max(2, radius*0.14)
		sliceRadius = ringInner - max(2, radius*0.07)
	}
	sliceSquared, ringSquared, outerSquared := sliceRadius*sliceRadius, ringInner*ringInner, radius*radius
	styles := [2]lipgloss.Style{lipgloss.NewStyle().Foreground(colors.dim), lipgloss.NewStyle().Foreground(color)}

	rows := make([]string, 0, cellHeight)
	for cellY := 0; cellY < cellHeight; cellY++ {
		var row strings.Builder
		for cellX := 0; cellX < cellWidth; cellX++ {
			var masks, activeDots [2]int // Inner slice and reset band.
			for subY := 0; subY < 4; subY++ {
				for subX := 0; subX < 2; subX++ {
					x := float64(cellX*2+subX) - centerX
					y := float64(cellY*4+subY) - centerY
					distance := x*x + y*y
					if radius < 0 || distance > outerSquared {
						continue
					}
					layer := 0
					if distance > sliceSquared {
						if distance < ringSquared {
							continue
						}
						layer = 1
					}
					masks[layer] |= brailleDot(subX, subY)
					angle := math.Atan2(x, -y)
					if angle < 0 {
						angle += 2 * math.Pi
					}
					if angle/(2*math.Pi)*100 < float64(percent[layer]) {
						activeDots[layer]++
					}
				}
			}
			layer := 0
			dots := bits.OnesCount8(uint8(masks[0]))
			if ringDots := bits.OnesCount8(uint8(masks[1])); ringDots > 0 && ringDots >= dots {
				layer, dots = 1, ringDots
			}
			if dots == 0 {
				row.WriteByte(' ')
				continue
			}
			tone := 0
			if activeDots[layer]*2 >= dots {
				tone = 1
			}
			// A terminal cell has one foreground. Keep one layer per cell
			// rather than connecting the band to the pie through the gap.
			row.WriteString(styles[tone].Render(string(rune(0x2800 + masks[layer]))))
		}
		rows = append(rows, row.String())
	}
	pie := strings.Join(rows, "\n")
	visual := pie
	if len(layout.legend) != 0 {
		style := lipgloss.NewStyle().Foreground(color)
		label := make([]string, len(layout.legend))
		for i, line := range layout.legend {
			label[i] = style.Render(line)
		}
		label = verticallyCenterLines(label, cellHeight)
		legend := strings.Join(label, "\n")
		visual = lipgloss.JoinHorizontal(lipgloss.Center, pie, "   ", legend)
	}
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

const radialLegendOverhead = 5

func radialCanvasSize(width, height, legendWidth int) (int, int) {
	overhead := 0
	if legendWidth > 0 {
		overhead = legendWidth + radialLegendOverhead
	}
	availableWidth := max(width-overhead, 2)
	if height <= 0 {
		height = max((availableWidth+1)/2, 1)
	}
	cellWidth := max(min(availableWidth, height*2), 1)
	cellHeight := max(min(height, (availableWidth+1)/2), 1)
	return cellWidth, cellHeight
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

func renderFuelTank(width, used int, color imagecolor.Color, colors palette) string {
	return renderFuelTankSized(width, 6, used, color, colors)
}

func renderFuelTankSized(width, height, used int, color imagecolor.Color, colors palette) string {
	if height <= 0 {
		height = 6
	}
	remaining := 100 - used
	tankWidth := max(width-6, 1)
	available := int(math.Round(float64(tankWidth) * float64(remaining) / 100))
	tank := lipgloss.NewStyle().Foreground(colors.primary).Render(strings.Repeat("▰", available)) +
		lipgloss.NewStyle().Foreground(colors.dim).Render(strings.Repeat("▱", tankWidth-available))
	label := lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf("RANGE %3d%%", remaining))
	middle := "│ " + tank + " │"
	// The fuel itself is the primary signal. At constrained heights retain the
	// tank row first, then restore the range readout, and only then the dim
	// endpoint context. The enclosing meter layout independently drops the reset
	// timeline and other telemetry before it reaches this one-row form.
	if height == 1 {
		return lipgloss.PlaceHorizontal(width, lipgloss.Center, middle)
	}
	if height == 2 {
		return lipgloss.PlaceHorizontal(width, lipgloss.Center, middle+"\n"+label)
	}
	rows := make([]string, height)
	rows[0] = colors.dimmed().Render("E" + strings.Repeat(" ", max(tankWidth, 1)) + "F")
	rows[height-1] = label
	for row := 1; row < height-1; row++ {
		rows[row] = middle
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, strings.Join(rows, "\n"))
}

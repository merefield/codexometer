package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
	"github.com/merefield/codexometer/internal/quotagraph"
)

func isQuotaGraph(view meterViewID) bool { return view == viewPace || view == viewZone }

type quotaPlotOptions struct {
	points    []quotagraph.Point
	mode      quotagraph.Mode
	hideTrace bool
}

func (m Model) quotaPlotOptions() []quotaPlotOptions {
	keys := quotagraph.Keys(m.snapshot)
	options := make([]quotaPlotOptions, len(keys))
	for i, key := range keys {
		options[i] = quotaPlotOptions{points: m.quotaGraphs.Series[key].Points, mode: m.quotaGraphMode, hideTrace: m.quotaGraphHideTrace}
	}
	return options
}

func (m *Model) cycleQuotaTrend() {
	options := m.quotaPlotOptions()
	meters := m.snapshot.Meters()
	for step := 1; step <= int(quotagraph.ModeCount); step++ {
		candidate := (m.quotaGraphMode + quotagraph.Mode(step)) % quotagraph.ModeCount
		available := candidate == quotagraph.WindowStart || candidate == quotagraph.Off
		for i, meter := range meters {
			available = available || quotagraph.Available(candidate, meter.Window, options[i].points, time.Now())
		}
		if available {
			m.quotaGraphMode = candidate
			return
		}
	}
}

// Rendering and hit testing consume the same labels and widths.
func (m Model) quotaGraphButtons(width int) []footerButton {
	trace := "ON"
	if m.quotaGraphHideTrace {
		trace = "OFF"
	}
	trend := i18n.Text(m.quotaGraphMode.Label())
	buttons := []footerButton{
		{id: footerButtonQuotaTrend, label: i18n.Format("[ (G)TREND // %s ]", trend), compact: fmt.Sprintf("[G:%s]", trend)},
		{id: footerButtonQuotaTrace, label: i18n.Format("[ (H)TRACE // %s ]", i18n.Text(trace)), compact: fmt.Sprintf("[H:%s]", i18n.Text(trace))},
	}
	if lipgloss.Width(buttons[0].label)+lipgloss.Width(buttons[1].label)+1 > width {
		for i := range buttons {
			buttons[i].label = buttons[i].compact
		}
	}
	if lipgloss.Width(buttons[0].label)+lipgloss.Width(buttons[1].label)+1 > width {
		buttons[0].label, buttons[1].label = "[G]", "[H]"
	}
	if width < 7 {
		return buttons[:0]
	}
	return buttons
}

func (m Model) renderQuotaGraphControls(width int, colors palette) string {
	var parts []string
	for _, button := range m.quotaGraphButtons(width) {
		parts = append(parts, footerButtonAppearance(colors, m.hoveredButton == button.id, m.flashedButton == button.id).Render(button.label))
	}
	return strings.Join(parts, " ")
}

func (m Model) quotaGraphButtonAt(x, y int) footerButtonID {
	if !isQuotaGraph(m.meterView) || y != m.dashboardLayout().quotaTabsY+1 {
		return footerButtonNone
	}
	x -= 2
	start := 0
	for _, button := range m.quotaGraphButtons(m.dashboardLayout().contentWidth) {
		width := lipgloss.Width(button.label)
		if x >= start && x < start+width {
			return button.id
		}
		start += width + 1
	}
	return footerButtonNone
}

type quotaPlotCell struct {
	mask     int
	ink      color.Color
	priority int
	symbol   rune
}

type quotaCanvas struct {
	width, height int
	pace          bool
	cells         []quotaPlotCell
}

func (c quotaCanvas) position(x, y float64) (int, int) {
	if c.pace {
		y = (y + 100) / 2
	}
	return int(math.Round(x / 100 * float64(c.width*2-1))), int(math.Round((1 - y/100) * float64(c.height*4-1)))
}

func (c *quotaCanvas) pixel(x, y int, ink color.Color, priority int) {
	if x < 0 || y < 0 || x >= c.width*2 || y >= c.height*4 {
		return
	}
	cell := &c.cells[(y/4)*c.width+x/2]
	if priority < cell.priority {
		return
	}
	if priority > cell.priority {
		cell.mask, cell.symbol = 0, 0
	}
	cell.priority, cell.ink = priority, ink
	cell.mask |= brailleDot(x%2, y%4)
}

func (c *quotaCanvas) line(x1, y1, x2, y2 float64, ink color.Color, priority, dash int) {
	xa, ya := c.position(x1, y1)
	xb, yb := c.position(x2, y2)
	steps := max(int(math.Abs(float64(xb-xa))), int(math.Abs(float64(yb-ya))), 1)
	for i := 0; i <= steps; i++ {
		if dash > 0 && (i/dash)%2 != 0 {
			continue
		}
		x := xa + int(math.Round(float64(xb-xa)*float64(i)/float64(steps)))
		y := ya + int(math.Round(float64(yb-ya)*float64(i)/float64(steps)))
		c.pixel(x, y, ink, priority)
	}
}

func (c *quotaCanvas) mark(x, y float64, symbol rune, ink color.Color, priority int) {
	px, py := c.position(x, y)
	if px < 0 || py < 0 || px >= c.width*2 || py >= c.height*4 {
		return
	}
	cell := &c.cells[(py/4)*c.width+px/2]
	if priority >= cell.priority {
		cell.symbol, cell.ink, cell.priority = symbol, ink, priority
	}
}

func quotaFieldColor(risk float64) color.Color {
	amber := color.RGBA{R: 83, G: 66, B: 25, A: 255}
	end := color.RGBA{R: 66, G: 27, B: 37, A: 255}
	if risk < 0 {
		end = color.RGBA{R: 17, G: 52, B: 32, A: 255}
	}
	weight := min(math.Abs(risk), 1)
	return color.RGBA{R: uint8(float64(amber.R)*(1-weight) + float64(end.R)*weight), G: uint8(float64(amber.G)*(1-weight) + float64(end.G)*weight), B: uint8(float64(amber.B)*(1-weight) + float64(end.B)*weight), A: 255}
}

func renderQuotaPlot(width, height int, window codex.Window, now time.Time, pace bool, options quotaPlotOptions, colors palette) string {
	width = max(width, 1)
	if height <= 0 {
		height = 10
	}
	elapsed, known := quotagraph.Elapsed(window, now)
	if !known {
		return quotaPlotFit([]string{colors.dimmed().Render(i18n.Text("GRAPH DATA UNAVAILABLE"))}, width, height)
	}
	used := float64(max(0, min(100, window.UsedPercent)))
	status := i18n.Format("%.0f%% USED // %.1f%% TIME", used, elapsed)
	if pace {
		status = i18n.Format("%+.1f PP FROM SAFETY", used-elapsed)
	}
	trend, projected := quotagraph.Project(options.mode, window, options.points, now)
	trendInk := lipgloss.Color("#9D2537")
	if trend.Projected <= 100 {
		trendInk = lipgloss.Color("#45DB79")
	}
	projection := ""
	if projected {
		projection = i18n.Format("TREND // %.1f%% AT RESET", trend.Projected)
		if pace {
			projection = i18n.Format("TREND // %+.1f PP AT RESET", trend.Projected-100)
		}
	} else if options.mode != quotagraph.Off {
		projection = i18n.Text("TREND UNAVAILABLE")
	}
	status = colors.label().Render(ansi.Truncate(status, width, ""))
	projection = lipgloss.NewStyle().Foreground(trendInk).Render(ansi.Truncate(projection, width, ""))
	if width < 18 || height < 6 {
		return quotaPlotFit([]string{status, projection}, width, height)
	}
	plotWidth, plotHeight := width-6, height-4
	canvas := quotaCanvas{width: plotWidth, height: plotHeight, pace: pace, cells: make([]quotaPlotCell, plotWidth*plotHeight)}
	value := func(x, y float64) float64 {
		if pace {
			return y - x
		}
		return y
	}
	guide := lipgloss.Color("#708276")
	canvas.line(0, 0, 100, value(100, 100), guide, 1, 0)
	if !options.hideTrace {
		for i, point := range options.points {
			if i > 0 && !point.Break {
				before := options.points[i-1]
				canvas.line(before.Elapsed, value(before.Elapsed, float64(before.Used)), point.Elapsed, value(point.Elapsed, float64(point.Used)), lipgloss.Color("#BAD4C7"), 2, 0)
			}
		}
		if len(options.points) > 0 {
			p := options.points[0]
			canvas.mark(p.Elapsed, value(p.Elapsed, float64(p.Used)), '○', guide, 2)
		}
	}
	if projected {
		if segment, ok := trend.Segment(pace); ok {
			canvas.line(segment.X1, segment.Y1, segment.X2, segment.Y2, trendInk, 3, 0)
		}
	}
	canvas.mark(elapsed, value(elapsed, used), '●', lipgloss.Color("#FFFFFF"), 4)
	caption := i18n.Text("CONSUMPTION")
	if pace {
		caption = i18n.Text("DISTANCE FROM SAFETY (PP)")
	}
	lines := []string{colors.dimmed().Render(ansi.Truncate(caption, width, ""))}
	for row := 0; row < plotHeight; row++ {
		label := "    "
		if row == 0 {
			if pace {
				label = "+100"
			} else {
				label = "100%"
			}
		} else if row == plotHeight-1 {
			if pace {
				label = "-100"
			} else {
				label = "  0%"
			}
		} else if row == plotHeight/2 && pace {
			label = "   0"
		}
		var line strings.Builder
		line.WriteString(colors.dimmed().Render(label + "│"))
		for col := 0; col < plotWidth; col++ {
			cell := canvas.cells[row*plotWidth+col]
			x := float64(col) / float64(max(plotWidth-1, 1))
			y := 1 - float64(row)/float64(max(plotHeight-1, 1))
			risk := y - x
			if pace {
				risk = 2*y - 1
			}
			style := lipgloss.NewStyle().Background(quotaFieldColor(risk))
			r := ' '
			if cell.mask != 0 {
				r = rune(0x2800 + cell.mask)
			}
			if cell.symbol != 0 {
				r = cell.symbol
			}
			if cell.ink != nil {
				style = style.Foreground(cell.ink)
			}
			line.WriteString(style.Render(string(r)))
		}
		lines = append(lines, line.String())
	}
	lines = append(lines, colors.dimmed().Render("     0%"+lipgloss.PlaceHorizontal(max(plotWidth-6, 0), lipgloss.Center, ansi.Truncate(i18n.Text("TIME ELAPSED"), max(plotWidth-6, 0), ""))+"100%"), status, projection)
	return quotaPlotFit(lines, width, height)
}

func quotaPlotFit(lines []string, width, height int) string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

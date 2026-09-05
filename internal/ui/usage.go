package ui

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type accountUsageFetcher interface {
	FetchAccountUsage(context.Context) (codex.AccountUsage, error)
}

type accountHistoryState struct {
	data     codex.AccountUsage
	err      error
	loading  bool
	sequence uint64
	mode     int
	offset   int // periods back from the newest page
	hovered  int // action + 1; zero means none
}

type accountHistoryMsg struct {
	data     codex.AccountUsage
	err      error
	sequence uint64
}

func (m *Model) requestHistory() tea.Cmd {
	if m.meterView != viewUsage || m.history.loading {
		return nil
	}
	fetcher, ok := m.fetcher.(accountUsageFetcher)
	if !ok {
		m.history.err = fmt.Errorf("Account usage is unavailable from this data source")
		return nil
	}
	m.history.loading = true
	m.history.sequence++
	sequence := m.history.sequence
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		data, err := fetcher.FetchAccountUsage(ctx)
		return accountHistoryMsg{data: data, err: err, sequence: sequence}
	}
}

type historyPoint struct {
	date   time.Time
	tokens int64
}

func addTokens(a, b int64) int64 {
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}

// Like Codex /usage, use 52 Sunday-based weeks ending in the current UTC
// week. Missing days within a supplied history are zero; future days are not
// plotted. Cumulative totals cover this window, not the lifetime summary.
func historyPoints(data codex.AccountUsage, now time.Time, mode int) []historyPoint {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -int(today.Weekday())-51*7)
	days := int(today.Sub(start)/(24*time.Hour)) + 1
	points := make([]historyPoint, days)
	for i := range points {
		points[i].date = start.AddDate(0, 0, i)
	}
	for _, day := range data.DailyUsageBuckets {
		date, err := time.Parse("2006-01-02", day.StartDate)
		if err != nil || day.Tokens < 0 || date.Before(start) || date.After(today) {
			continue
		}
		i := int(date.Sub(start) / (24 * time.Hour))
		points[i].tokens = addTokens(points[i].tokens, day.Tokens)
	}
	if mode == 0 {
		return points
	}
	weeks := make([]historyPoint, 52)
	for i := range weeks {
		weeks[i].date = start.AddDate(0, 0, i*7)
	}
	for i, p := range points {
		weeks[i/7].tokens = addTokens(weeks[i/7].tokens, p.tokens)
	}
	if mode == 2 {
		for i := 1; i < len(weeks); i++ {
			weeks[i].tokens = addTokens(weeks[i].tokens, weeks[i-1].tokens)
		}
	}
	return weeks
}

type historyButton struct {
	action, x int
	label     string
}

func historyButtons(width int) []historyButton {
	labels, separator := responsiveTabLabels(width, [][]string{
		{"[ (D)AILY ]", "[ (W)EEKLY ]", "[ (C)UMULATIVE ]", "[ ← OLDER ]", "[ NEWER → ]"},
		{"[D]", "[W]", "[C]", "[←]", "[→]"},
		{"D", "W", "C", "←", "→"},
	})
	buttons := []historyButton{}
	x := 0
	for i, label := range labels {
		if x+lipgloss.Width(label) > width {
			break
		}
		buttons = append(buttons, historyButton{i, x, label})
		x += lipgloss.Width(label) + len(separator)
	}
	return buttons
}

func historyKey(key string) (int, bool) {
	switch key {
	case "d":
		return 0, true
	case "w":
		return 1, true
	case "c":
		return 2, true
	case "left", "pgup":
		return 3, true
	case "right", "pgdown":
		return 4, true
	}
	return 0, false
}

func historyCapacity(width int) int { return historyBarGeometry(width).columns }

func historyBarGeometry(width int) historyCalendarLayout {
	available := max(width-8, 1)
	layout := historyCalendarLayout{cellWidth: 1, cellHeight: 1, columns: min(available, 52)}
	if available >= 52+51 {
		layout.gap = 1
	}
	if available >= 52*2+51 {
		layout.cellWidth = (available - 51) / 52
	}
	return layout
}

func (m *Model) activateHistory(action int) {
	if action < 3 {
		m.history.mode = action
		m.history.offset = 0
		return
	}
	layout := m.dashboardLayout()
	count := len(historyPoints(m.history.data, time.Now(), m.history.mode))
	step := historyCapacity(layout.contentWidth)
	if m.history.mode == 0 {
		count = 52
		step = historyCalendarGeometry(layout.contentWidth, layout.meterHeight-3).columns
	}
	m.history.offset = min(m.history.offset, max(count-step, 0))
	if action == 3 {
		m.history.offset = min(m.history.offset+step, max(count-step, 0))
	}
	if action == 4 {
		m.history.offset = max(m.history.offset-step, 0)
	}
}

func (m Model) historyButtonAt(x, y int) (int, bool) {
	layout := m.dashboardLayout()
	if y != layout.meterY {
		return 0, false
	}
	for _, button := range historyButtons(layout.contentWidth) {
		if x >= 2+button.x && x < 2+button.x+lipgloss.Width(button.label) {
			return button.action, true
		}
	}
	return 0, false
}

func usageNumber(n int64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func optionalUsage(n *int64) string {
	if n == nil || *n < 0 {
		return "—"
	}
	return usageNumber(*n)
}

func (m Model) renderHistory(width, height int, colors palette) string {
	lines := []string{}
	buttons := ""
	for _, button := range historyButtons(width) {
		buttons += strings.Repeat(" ", max(button.x-lipgloss.Width(buttons), 0))
		label := colors.dimmed().Render(button.label)
		if button.action == m.history.mode {
			label = colors.label().Bold(true).Render(button.label)
		}
		if m.history.hovered == button.action+1 {
			label = colors.label().Reverse(true).Render(button.label)
		}
		buttons += label
	}
	lines = append(lines, buttons)
	data := m.history.data
	lines = append(lines, colors.label().Render(fmt.Sprintf("LIFETIME // %s TOKENS   PEAK DAY // %s   STREAK // %s DAYS", optionalUsage(data.Summary.LifetimeTokens), optionalUsage(data.Summary.PeakDailyTokens), optionalUsage(data.Summary.CurrentStreakDays))))
	status := "ACCOUNT HISTORY // UTC // 52 WEEKS // R REFRESH"
	if !data.FetchedAt.IsZero() {
		status = "ACCOUNT HISTORY // UTC // UPDATED " + data.FetchedAt.Local().Format("15:04:05") + " // R REFRESH"
	}
	if m.history.loading {
		status = "FETCHING ACCOUNT HISTORY…"
	}
	if m.history.err != nil {
		status = "UNAVAILABLE // " + m.history.err.Error() + " // R RETRY"
	}
	if m.history.err != nil && !data.FetchedAt.IsZero() {
		status = "STALE // " + m.history.err.Error() + " // R RETRY"
	}
	lines = append(lines, colors.dimmed().Render(status))
	if data.DailyUsageBuckets == nil {
		lines = append(lines, colors.dimmed().Render("Token activity history unavailable. Requires a supported Codex CLI and ChatGPT login."))
	} else if m.history.mode == 0 {
		lines = append(lines, m.renderHistoryCalendar(width, height-len(lines), colors)...)
	} else {
		points := historyPoints(data, time.Now(), m.history.mode)
		barLayout := historyBarGeometry(width)
		capacity := barLayout.columns
		offset := min(m.history.offset, max(len(points)-capacity, 0))
		end := len(points) - offset
		start := max(end-capacity, 0)
		visible := points[start:end]
		var peak int64
		for _, p := range visible {
			peak = max(peak, p.tokens)
		}
		caption := []string{"DAILY TOKENS", "WEEKLY TOKENS (SUN–SAT)", "CUMULATIVE TOKENS (52-WEEK WINDOW)"}[m.history.mode]
		lines = append(lines, colors.dimmed().Render(caption+" // SCALE "+usageNumber(peak)))
		chartHeight := max(height-len(lines)-2, 0)
		for row := chartHeight - 1; row >= 0; row-- {
			axis := ""
			if row == chartHeight-1 {
				axis = usageNumber(peak)
			}
			if row == 0 && chartHeight > 1 {
				axis = "0"
			}
			line := colors.dimmed().Render(fmt.Sprintf("%7s│", axis))
			for i, p := range visible {
				fraction := 0.0
				if peak > 0 {
					fraction = float64(p.tokens)/float64(peak)*float64(chartHeight) - float64(row)
				}
				level := min(max(int(math.Ceil(fraction*8)), 0), 8)
				cell := string([]rune(" ▁▂▃▄▅▆▇█")[level])
				line += colors.label().Render(strings.Repeat(cell, barLayout.cellWidth))
				if i+1 < len(visible) {
					line += strings.Repeat(" ", barLayout.gap)
				}
			}
			lines = append(lines, line)
		}
		plotWidth := len(visible)*(barLayout.cellWidth+barLayout.gap) - barLayout.gap
		lines = append(lines, colors.dimmed().Render("        "+strings.Repeat("─", min(plotWidth, max(width-8, 0)))))
		first, last := visible[0].date.Format("02 Jan 2006"), visible[len(visible)-1].date.Format("02 Jan 2006")
		lines = append(lines, colors.dimmed().Render(first+" → "+last+" // ←/→ OR PGUP/PGDN"))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	lines = lines[:min(len(lines), height)]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

type historyCalendarLayout struct {
	cellWidth, cellHeight, gap, columns int
}

// Prioritise all 52 weeks over cell size. Below 56 content columns, even
// single-column cells cannot show a year beside weekday labels; page then.
func historyCalendarGeometry(width, height int) historyCalendarLayout {
	available := max(width-4, 1)
	layout := historyCalendarLayout{cellWidth: 1, cellHeight: 1, columns: min(available, 52)}
	if available >= 52+51 {
		layout.gap = 1
	}
	if available >= 52*2+51 {
		layout.cellHeight = max(min((height-4)/7, (available-51)/(52*2)), 1)
		layout.cellWidth = layout.cellHeight * 2
	}
	return layout
}

func historyHeatLevel(tokens, peak int64) int {
	if tokens <= 0 || peak <= 0 {
		return 0
	}
	return min(max(int(math.Ceil(float64(tokens)/float64(peak)*4)), 1), 4)
}

func historyHeatColors(colors palette) []color.Color {
	blend := func(a, b color.Color) color.Color {
		ar, ag, ab, _ := a.RGBA()
		br, bg, bb, _ := b.RGBA()
		return color.RGBA{uint8((ar + br) / 2 >> 8), uint8((ag + bg) / 2 >> 8), uint8((ab + bb) / 2 >> 8), 255}
	}
	return []color.Color{blend(colors.background, colors.dim), colors.dim, blend(colors.dim, colors.primary), colors.primary, colors.accent}
}

func (m Model) renderHistoryCalendar(width, height int, colors palette) []string {
	if height < 11 || width < 12 {
		return []string{colors.dimmed().Render("Enlarge terminal for daily activity grid (7 weekday rows).")}
	}
	points := historyPoints(m.history.data, time.Now(), 0)
	layout := historyCalendarGeometry(width, height)
	cellHeight, columns := layout.cellHeight, layout.columns
	offset := min(m.history.offset, 52-columns)
	end := 52 - offset
	start := max(end-columns, 0)
	var peak int64
	for _, p := range points {
		peak = max(peak, p.tokens)
	}
	heatColors := historyHeatColors(colors)
	lines := []string{colors.dimmed().Render("DAILY ACTIVITY // 52 WEEKS // UTC")}
	stride := layout.cellWidth + layout.gap
	monthLabels := []rune(strings.Repeat(" ", columns*stride-layout.gap))
	labelEnd := 0
	lastMonth := ""
	for week := start; week < end; week++ {
		month := points[week*7].date.Format("Jan")
		x := (week - start) * stride
		if month != lastMonth && x >= labelEnd && x+3 <= len(monthLabels) {
			copy(monthLabels[x:x+3], []rune(month))
			labelEnd = x + 4
		}
		lastMonth = month
	}
	lines = append(lines, colors.dimmed().Render("    "+string(monthLabels)))
	for day, label := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
		for row := 0; row < cellHeight; row++ {
			prefix := "    "
			if row == 0 {
				prefix = label + " "
			}
			line := colors.dimmed().Render(prefix)
			for week := start; week < end; week++ {
				i := week*7 + day
				cell := strings.Repeat(" ", layout.cellWidth)
				if i < len(points) {
					level := historyHeatLevel(points[i].tokens, peak)
					glyph := "█"
					if layout.cellWidth == 1 {
						glyph = "■"
					}
					cell = lipgloss.NewStyle().Foreground(heatColors[level]).Render(strings.Repeat(glyph, layout.cellWidth))
				}
				line += cell
				if week+1 < end {
					line += strings.Repeat(" ", layout.gap)
				}
			}
			lines = append(lines, line)
		}
	}
	legend := "LESS "
	for _, c := range heatColors {
		legend += lipgloss.NewStyle().Foreground(c).Render("██") + " "
	}
	legend += fmt.Sprintf("MORE // PEAK DAY %s", usageNumber(peak))
	lines = append(lines, legend)
	first, last := points[start*7].date, points[min(end*7, len(points))-1].date
	lines = append(lines, colors.dimmed().Render(first.Format("02 Jan 2006")+" → "+last.Format("02 Jan 2006")))
	return lines
}

package ui

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

var reportDimensions = []string{"surface", "model", "feature", "task start"}

func usageReportValueStyle(colors palette, known bool, percent float64) lipgloss.Style {
	if !known {
		return colors.dimmed()
	}
	style := colors.header()
	if percent >= 100 {
		return style.Foreground(colors.danger)
	}
	if percent >= 80 {
		return style.Foreground(colors.warning)
	}
	return style
}

func (m Model) renderUsageReport(width, height int, colors palette, lines []string) string {
	dim := colors.dimmed().Render
	strong := colors.header().Render
	warn := lipgloss.NewStyle().Foreground(colors.warning).Render
	dimension := reportDimensions[m.history.group%len(reportDimensions)]
	reports := m.history.data.Reports
	if m.history.err != nil && reports != nil {
		lines = append(lines, warn("STALE // refresh failed; last in-memory observation"))
	}
	var rows []codex.UsageCategory
	total := 0.0
	knownEmpty := false
	if reports == nil {
		lines = append(lines, dim("Account breakdowns unavailable. Token history remains available."))
	} else if m.history.mode == 7 {
		lines = append(lines, strong("DAILY BREAKDOWN // "+reports.DailyStatus+" // UTC"))
		if r := reports.Daily; r != nil {
			unit := r.Units
			lines = append(lines, dim("LATEST FETCH // "+r.FetchedAt.UTC().Format("2006-01-02 15:04")+" UTC"))
			if r.DataAsOf != "" && height >= 18 {
				lines = append(lines, dim("DATA AS OF // "+r.DataAsOf))
			}
			lines = append(lines, dim("REPORTED RANGE // "+r.From+" → "+r.Through))
			if len(r.Days) > 0 {
				i := len(r.Days) - 1 - min(m.history.offset, len(r.Days)-1)
				day := r.Days[i]
				total = day.Total
				rows = codex.UsageCategories(day.Groups, dimension)
				knownEmpty = day.Total == 0 && day.Groups[dimension] != nil
				lines = append(lines, strong(fmt.Sprintf("%s // %.2f %s // %s", day.Date, total, unit, strings.ToUpper(dimension))))
			} else {
				lines = append(lines, dim("No daily buckets reported in the requested 30 days."))
			}
			lines = append(lines, dim("Relative usage/credits are not tokens or quota percentages."))
		}
	} else {
		lines = append(lines, strong("QUOTA WINDOWS // "+reports.PlanStatus+" // UTC"))
		if r := reports.Plan; r != nil {
			coverage := warn("PARTIAL COVERAGE")
			if r.CoverageComplete {
				coverage = dim("REPORTED RANGE COMPLETE")
			}
			if r.Approximate {
				coverage += dim(" // ") + warn("APPROXIMATE")
			}
			lines = append(lines, coverage)
			if height >= 20 {
				lines = append(lines, dim("COVERAGE START // "+r.CoverageStart), dim("DATA AS OF // "+r.DataAsOf))
				if r.BoundaryTolerance != nil {
					lines = append(lines, dim(fmt.Sprintf("BOUNDARY TOLERANCE // %d SECONDS", *r.BoundaryTolerance)))
				}
			}
			lines = append(lines, dim("LATEST FETCH // "+r.FetchedAt.UTC().Format("2006-01-02 15:04")+" UTC"))
			if len(r.Periods) > 0 {
				p := r.Periods[min(m.history.offset, len(r.Periods)-1)]
				used := "UNKNOWN"
				if p.UsedBasisPoints != nil {
					total = *p.UsedBasisPoints / 100
					used = fmt.Sprintf("%.2f%%", total)
				}
				usageStyle := usageReportValueStyle(colors, p.UsedBasisPoints != nil, total)
				lines = append(lines, strong(fmt.Sprintf("%d MIN // %s // ", p.WindowMinutes, p.PlanType))+usageStyle.Render("USED "+used), strong(p.StartsAt+" → "+p.EndsAt))
				if !p.AccountingComplete {
					lines = append(lines, warn("PARTIAL ACCOUNTING // totals may still change"))
				}
				wireDimension := dimension
				if dimension == "feature" {
					wireDimension = "thread_source"
				}
				if dimension == "task start" {
					wireDimension = "turn_trigger"
				}
				groups := map[string]map[string]float64{}
				for _, b := range p.Breakdowns {
					if b.Dimension == wireDimension {
						if groups[dimension] == nil {
							groups[dimension] = map[string]float64{}
						}
						for _, v := range b.Rows {
							groups[dimension][v.Key] += v.BasisPoints / 100
						}
					}
				}
				rows = codex.UsageCategories(groups, dimension)
				lines = append(lines, dim("BY "+strings.ToUpper(dimension)+" // historical allowance, not today's limit"))
			} else {
				lines = append(lines, dim("No quota windows reported."))
			}
		}
	}
	if len(rows) == 0 {
		if knownEmpty {
			lines = append(lines, dim("No usage reported for this day."))
		} else {
			lines = append(lines, dim("Selected breakdown unavailable; missing does not mean zero."))
		}
	}
	heading, suffix := "QUOTA USED", "%"
	if m.history.mode == 7 {
		heading, suffix = "RELATIVE USAGE", ""
		if reports != nil && reports.Daily != nil {
			heading = reports.Daily.Units
		}
	}
	valueWidth := ansi.StringWidth(heading)
	for _, row := range rows {
		valueWidth = max(valueWidth, ansi.StringWidth(fmt.Sprintf("%.2f%s", row.Value, suffix)))
	}
	valueWidth = min(valueWidth, max(width/2, 1))
	nameWidth := min(28, max(width/3, 5))
	barWidth := max(width-nameWidth-valueWidth-2, 0)
	if len(rows) > 0 && height-len(lines) >= 3 {
		lines = append(lines, dim(strings.Repeat(" ", max(width-valueWidth, 0))+ansi.Truncate(heading, valueWidth, "")))
	}
	capacity := max(height-len(lines)-1, 0)
	start := min(m.history.rowOffset, max(len(rows)-capacity, 0))
	peak := total
	for _, row := range rows {
		peak = max(peak, row.Value)
	}
	for index, row := range rows[start:min(start+capacity, len(rows))] {
		name := ansi.Truncate(row.Name, nameWidth, "")
		name += strings.Repeat(" ", max(nameWidth-ansi.StringWidth(name), 0))
		fill := 0
		if peak > 0 {
			fill = max(0, min(barWidth, int(math.Round(row.Value/peak*float64(barWidth)))))
		}
		barStyle := lipgloss.NewStyle().Foreground(colors.primary)
		nameStyle := colors.label().Bold(false)
		valueStyle := colors.header()
		fillChar := "█"
		if (start+index)%2 == 1 {
			fillChar = "▓"
		}
		if strings.EqualFold(strings.TrimSpace(row.Name), "unknown") {
			barStyle, nameStyle, valueStyle = colors.dimmed(), colors.dimmed(), colors.dimmed()
			fillChar = "█"
		}
		value := fmt.Sprintf("%.2f%s", row.Value, suffix)
		lines = append(lines, nameStyle.Render(name)+" "+barStyle.Render(strings.Repeat(fillChar, fill))+strings.Repeat(" ", barWidth-fill)+" "+valueStyle.Render(fmt.Sprintf("%*s", valueWidth, value)))
	}
	lines = append(lines, dim("←/→ DATE/WINDOW // ↑/↓ SCROLL"))
	if m.history.loading {
		lines[len(lines)-1] = "FETCHING ACCOUNT HISTORY…"
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	lines = lines[:min(len(lines), height)]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(width, 0), "")
	}
	return strings.Join(lines, "\n")
}

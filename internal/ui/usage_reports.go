package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

var reportDimensions = []string{"surface", "model", "feature", "task start"}

func (m Model) renderUsageReport(width, height int, colors palette, lines []string) string {
	dimension := reportDimensions[m.history.group%len(reportDimensions)]
	reports := m.history.data.Reports
	if m.history.err != nil && reports != nil {
		lines = append(lines, "STALE // refresh failed; last in-memory observation")
	}
	var rows []codex.UsageCategory
	total := 0.0
	knownEmpty := false
	if reports == nil {
		lines = append(lines, "Account breakdowns unavailable. Token history remains available.")
	} else if m.history.mode == 7 {
		lines = append(lines, "DAILY BREAKDOWN // "+reports.DailyStatus+" // UTC")
		if r := reports.Daily; r != nil {
			unit := r.Units
			lines = append(lines, "LATEST FETCH // "+r.FetchedAt.UTC().Format("2006-01-02 15:04")+" UTC")
			if r.DataAsOf != "" && height >= 18 {
				lines = append(lines, "DATA AS OF // "+r.DataAsOf)
			}
			lines = append(lines, "REPORTED RANGE // "+r.From+" → "+r.Through)
			if len(r.Days) > 0 {
				i := len(r.Days) - 1 - min(m.history.offset, len(r.Days)-1)
				day := r.Days[i]
				total = day.Total
				rows = codex.UsageCategories(day.Groups, dimension)
				knownEmpty = day.Total == 0 && day.Groups[dimension] != nil
				lines = append(lines, fmt.Sprintf("%s // %.2f %s // %s", day.Date, total, unit, strings.ToUpper(dimension)))
			} else {
				lines = append(lines, "No daily buckets reported in the requested 30 days.")
			}
			lines = append(lines, "Relative usage/credits are not tokens or quota percentages.")
		}
	} else {
		lines = append(lines, "ALLOWANCE PERIODS // "+reports.PlanStatus+" // UTC")
		if r := reports.Plan; r != nil {
			coverage := "PARTIAL COVERAGE"
			if r.CoverageComplete {
				coverage = "REPORTED RANGE COMPLETE"
			}
			if r.Approximate {
				coverage += " // APPROXIMATE"
			}
			lines = append(lines, coverage)
			if height >= 20 {
				lines = append(lines, "COVERAGE START // "+r.CoverageStart, "DATA AS OF // "+r.DataAsOf)
				if r.BoundaryTolerance != nil {
					lines = append(lines, fmt.Sprintf("BOUNDARY TOLERANCE // %d SECONDS", *r.BoundaryTolerance))
				}
			}
			lines = append(lines, "LATEST FETCH // "+r.FetchedAt.UTC().Format("2006-01-02 15:04")+" UTC")
			if len(r.Periods) > 0 {
				p := r.Periods[min(m.history.offset, len(r.Periods)-1)]
				used := "UNKNOWN"
				if p.UsedBasisPoints != nil {
					total = *p.UsedBasisPoints / 100
					used = fmt.Sprintf("%.2f%%", total)
				}
				lines = append(lines, fmt.Sprintf("%d MIN // %s // USED %s", p.WindowMinutes, p.PlanType, used), p.StartsAt+" → "+p.EndsAt)
				if !p.AccountingComplete {
					lines = append(lines, "PARTIAL ACCOUNTING // totals may still change")
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
				lines = append(lines, "BY "+strings.ToUpper(dimension)+" // historical allowance, not today's limit")
			} else {
				lines = append(lines, "No allowance periods reported.")
			}
		}
	}
	if len(rows) == 0 {
		if knownEmpty {
			lines = append(lines, "No usage reported for this day.")
		} else {
			lines = append(lines, "Selected breakdown unavailable; missing does not mean zero.")
		}
	}
	capacity := max(height-len(lines)-1, 0)
	start := min(m.history.rowOffset, max(len(rows)-capacity, 0))
	peak := total
	for _, row := range rows {
		peak = max(peak, row.Value)
	}
	nameWidth := min(28, max(width/3, 5))
	barWidth := max(width-nameWidth-12, 0)
	for _, row := range rows[start:min(start+capacity, len(rows))] {
		name := ansi.Truncate(row.Name, nameWidth, "")
		name += strings.Repeat(" ", max(nameWidth-ansi.StringWidth(name), 0))
		fill := 0
		if peak > 0 {
			fill = max(0, min(barWidth, int(math.Round(row.Value/peak*float64(barWidth)))))
		}
		lines = append(lines, colors.label().Render(name+" "+strings.Repeat("█", fill))+strings.Repeat(" ", barWidth-fill)+fmt.Sprintf(" %9.2f", row.Value))
	}
	lines = append(lines, "G GROUP // ←/→ PERIOD // ↑/↓ SCROLL // R REFRESH")
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

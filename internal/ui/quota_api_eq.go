package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
	"github.com/merefield/codexometer/internal/quotaestimate"
)

var errQuotaObservationChanged = fmt.Errorf("local accounting changed during quota read")

func (m Model) quotaMetersWithInsights(width int) []codex.Meter {
	meters := m.snapshot.Meters()
	lineWidth := max(width-4, 1)
	if len(meters) > 0 {
		columns := meterGridColumns(width, m.dashboardLayout().meterHeight, len(meters), m.meterView)
		lineWidth = max((width-(columns-1))/columns-4, 1)
	}
	for index := range meters {
		if meters[index].Kind != codex.MeterQuotaWindow {
			continue
		}
		line := i18n.Text("API-EQ N/A // LIMIT ATTRIBUTION UNKNOWN")
		if m.snapshot.AccountFingerprint == "" {
			line = i18n.Text("API-EQ N/A // ACCOUNT ATTRIBUTION UNKNOWN")
		} else if quotaestimate.Eligible(m.snapshot, meters[index]) {
			line = m.quotaAPILine(meters[index], lineWidth)
		}
		if meters[index].Details == "" {
			meters[index].Details = line
		} else {
			meters[index].Details += "\n" + line
		}
	}
	return meters
}

func (m Model) quotaAPILine(meter codex.Meter, width int) (line string) {
	if m.benchmarkQuotaAccounting.deferred() {
		return m.benchmarkQuotaAccounting.deferredLabel(width)
	}
	if m.quotaAPI.CoverageIssue != "" {
		return "API-EQ // " + i18n.Text(quotaestimate.RestartCoverageGap)
	}
	if m.quotaAPITelemetryIssue != "" {
		if width < 44 {
			return "API-EQ // " + m.quotaAPITelemetryIssue
		}
		return i18n.Text("API-EQ LEARNING // ") + m.quotaAPITelemetryIssue
	}
	if estimate, ok := m.quotaAPIEstimate(m.snapshot, meter); ok {
		basis := ""
		if estimate.HasPremium {
			basis = " TIER*"
			if estimate.UnknownTier {
				basis += "?"
			}
		} else if estimate.UnknownTier {
			basis = " STD?"
		}
		// Compact, language-neutral accounting notation. The asterisk is
		// explained in the README: requested tier, not confirmed billing.
		defer func() {
			line = strings.Replace(line, "API-EQ", "API-EQ"+basis, 1)
			if !strings.Contains(line, "API-EQ") {
				line = strings.Replace(line, "EQ", "EQ"+basis, 1)
			}
			if estimate.HasPremium {
				comparison := fmt.Sprintf(" // STD 100%% ~%s", formatUSD(estimate.StandardFull))
				if ansi.StringWidth(line+comparison) <= width {
					line += comparison
				}
			}
		}()
		current := formatAPIRange(estimate.CurrentLow, estimate.CurrentHigh)
		full := formatAPIRange(estimate.FullLow, estimate.FullHigh)
		switch {
		case width-len(basis) >= 72:
			return i18n.Format("OBSERVED API-EQ // SPEND ~%s // 100%% ~%s // %s · N=%d", current, full, i18n.Text(estimate.Confidence), estimate.Samples)
		case width-len(basis) >= 52:
			return i18n.Format("API-EQ NOW ~%s // 100%% ~%s · %s%d", current, full, estimate.Confidence[:1], estimate.Samples)
		default:
			currentMid := formatUSD((estimate.CurrentLow + estimate.CurrentHigh) / 2)
			fullMid := formatUSD((estimate.FullLow + estimate.FullHigh) / 2)
			return i18n.Format("EQ NOW ~%s // FULL ~%s %s%d", currentMid, fullMid, estimate.Confidence[:1], estimate.Samples)
		}
	}
	key := quotaestimate.Key(m.snapshot, meter)
	progress := 0
	if anchor, ok := m.quotaAPI.Anchors[key]; ok {
		progress = min(max(meter.Window.UsedPercent-anchor.UsedPercent, 0), quotaestimate.MinimumDelta)
		if anchor.RestartReason != "" {
			shortReason := shortQuotaAPIRestartReason(anchor.RestartReason)
			switch {
			case width >= 72:
				return i18n.Format("API-EQ LEARNING // %d/%dPP CLEAN MOVEMENT // RESTARTED: %s", progress, quotaestimate.MinimumDelta, anchor.RestartReason)
			case width >= 44:
				return fmt.Sprintf("API-EQ %d/%dPP // RESTARTED: %s", progress, quotaestimate.MinimumDelta, shortReason)
			default:
				return fmt.Sprintf("EQ %d/%dPP // %s", progress, quotaestimate.MinimumDelta, shortReason)
			}
		}
	}
	if width < 44 {
		return i18n.Format("API-EQ LEARNING %d/%dPP", progress, quotaestimate.MinimumDelta)
	}
	return i18n.Format("API-EQ LEARNING // %d/%dPP CLEAN MOVEMENT", progress, quotaestimate.MinimumDelta)
}

// Restart reasons are compact diagnostic codes, not translated labels. Keep
// every code canonical so changing language does not partly translate them.
func shortQuotaAPIRestartReason(reason string) string {
	switch reason {
	case quotaestimate.RestartAccountChanged:
		return "ACCOUNT"
	case quotaestimate.RestartAccountingRebased:
		return "REBASED"
	case quotaestimate.RestartCoverageGap:
		return "COVERAGE GAP"
	case quotaestimate.RestartUnpricedUsage:
		return "UNPRICED"
	case quotaestimate.RestartMissingUsage:
		return "USAGE MISSING"
	case quotaestimate.RestartInconsistentUsage:
		return "USAGE INCONSISTENT"
	case quotaestimate.RestartWindowChanged:
		return "WINDOW CHANGED"
	case quotaestimate.RestartWindowReset:
		return "RESET"
	default:
		return reason
	}
}

func formatAPIRange(low, high float64) string {
	left, right := formatUSD(low), formatUSD(high)
	if left == right {
		return left
	}
	return left + "–" + right
}

func formatUSD(value float64) string {
	switch {
	case value < 0.01:
		return fmt.Sprintf("$%.4f", value)
	case value < 1:
		return fmt.Sprintf("$%.3f", value)
	case value < 10:
		return fmt.Sprintf("$%.2f", value)
	case value < 100:
		return fmt.Sprintf("$%.1f", value)
	default:
		return fmt.Sprintf("$%.0f", value)
	}
}

func (m Model) quotaAPIDiagnostic() codex.APIEqDiagnostic {
	if !m.meterView.isQuota() {
		return codex.APIEqDiagnostic{}
	}
	if m.quotaAPI.CoverageIssue != "" {
		return codex.APIEqDiagnostic{Kind: codex.UsageIssueCoverage, Source: "usage coverage", Reason: m.quotaAPI.CoverageIssue}
	}
	for _, meter := range m.snapshot.Meters() {
		if quotaestimate.Eligible(m.snapshot, meter) {
			if issue := m.quotaAPI.Anchors[quotaestimate.Key(m.snapshot, meter)].RestartIssue; issue.Kind != "" {
				return issue
			}
		}
	}
	return codex.APIEqDiagnostic{}
}

func (m Model) renderQuotaAPIDiagnostic(width int) string {
	issue := m.quotaAPIDiagnostic()
	if issue.Kind == "" {
		return ""
	}
	c := paletteFor(m.theme)
	text := codex.DiagnosticText(issue)
	lines := strings.Split(ansi.Hardwrap(text, max(width-4, 1), true), "\n")
	// A diagnostic must not push the footer off a short terminal. Preserve the
	// reason first, with bounded metadata rows; the browser retains the full text.
	height := m.height
	if height == 0 {
		height = 24
	}
	header := 3
	if width < 64 {
		header = 2
	}
	tabs := 2
	if isQuotaGraph(m.meterView) {
		tabs++
	}
	meters := len(m.snapshot.Meters())
	columns := meterGridColumns(width, max(height-2, 1), meters, m.meterView)
	minimumMeters := ((meters + columns - 1) / columns) * 3
	available := height - 2 - header - 1 - tabs - 2 - minimumMeters - m.resetNoticeHeight(width)
	if m.err != nil {
		available -= 3
	}
	if available < 3 {
		return ""
	}
	limit := min(max(height/3-2, 1), available-2)
	if len(lines) > limit {
		lines = lines[:limit]
		lines[len(lines)-1] = ansi.Truncate(lines[len(lines)-1], max(width-5, 1), "") + "…"
	}
	return frame(width, "API-EQ ISSUE", c.label().Render(strings.Join(lines, "\n")), c.primary, c)
}

func (m Model) quotaAPIDiagnosticHeight(width int) int {
	if view := m.renderQuotaAPIDiagnostic(width); view != "" {
		return strings.Count(view, "\n") + 1
	}
	return 0
}

func (m *Model) observeQuotaAPICoverage(issue string) { m.quotaAPI.ObserveCoverage(issue) }
func (m *Model) observeQuotaAPIEq(snapshot codex.Snapshot, usage codex.LiveUsageSnapshot, at time.Time) {
	m.quotaAPI.Observe(snapshot, usage, at)
}
func (m Model) quotaAPIEstimate(snapshot codex.Snapshot, meter codex.Meter) (quotaestimate.Estimate, bool) {
	return m.quotaAPI.Estimate(snapshot, meter, time.Now())
}

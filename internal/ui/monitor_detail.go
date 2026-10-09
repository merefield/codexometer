package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type detailLine struct {
	text string
	kind string
}

func (line detailLine) render(colors palette) string {
	style := colors.label()
	switch line.kind {
	case "heading":
		style = style.Bold(true).Foreground(colors.primary)
	case "metadata":
		style = colors.dimmed()
	case "warning":
		style = style.Foreground(colors.warning)
	case "command":
		style = style.Foreground(colors.primary)
	case "addition":
		style = style.Foreground(colors.success)
	case "removal":
		style = style.Foreground(colors.danger)
	case "diff-summary":
		if parts := strings.SplitN(line.text, " / ", 2); len(parts) == 2 {
			return style.Foreground(colors.success).Render(parts[0]) + style.Render(" / ") + style.Foreground(colors.danger).Render(parts[1])
		}
	}
	return style.Render(line.text)
}

// A single document owns both display and scroll geometry. Formatting never
// changes the stored request, capability or text sent back to Codex.
func (m Model) contextDetailDocument(width int) (document []detailLine) {
	width = max(width, 1)
	s, ok := m.contextDetailSession()
	if !ok {
		return []detailLine{{i18n.Text("NO CONTEXT"), "metadata"}}
	}
	if c, historical := m.historicalContext(); historical {
		return m.historyDocument(width, s, c)
	}
	if doc := m.profileDocument(s, width); doc != nil {
		return doc
	}
	if s.preview.Text == "" && s.preview.CurrentTask == "" && s.preview.LatestGuidance == "" {
		identity := terminalLabel(s.id) + " // " + terminalLabel(s.workingDirectory)
		var lines []detailLine
		for _, line := range strings.Split(ansi.Hardwrap(identity, width, true), "\n") {
			lines = append(lines, detailLine{ansi.Truncate(line, width, ""), "metadata"})
		}
		lines = append(lines, detailLine{i18n.Text("NO CONTEXT"), "metadata"})
		if notice := codex.SanitizeSessionContext(m.quota.notices[s.id]); notice != "" {
			for _, line := range strings.Split(ansi.Hardwrap(notice, width, true), "\n") {
				lines = append(lines, detailLine{ansi.Truncate(line, width, ""), "warning"})
			}
		}
		return lines
	}
	c := s.preview
	sanitize := codex.SanitizeSessionContext
	if c.Kind == codex.SessionContextApproval {
		sanitize = codex.SanitizeApprovalText
	}
	var lines []detailLine
	appendText := func(text, kind, prefix string) {
		for _, line := range strings.Split(ansi.Hardwrap(text, max(width-lipgloss.Width(prefix), 1), true), "\n") {
			// A double-width glyph cannot fit a one-cell viewport. Clip that
			// degenerate case; normal widths retain every wrapped character.
			lines = append(lines, detailLine{ansi.Truncate(prefix+line, width, ""), kind})
		}
	}
	section := func(title string) {
		if len(lines) > 0 {
			lines = append(lines, detailLine{})
		}
		title = ansi.Truncate(title, width, "")
		rule := max(width-lipgloss.Width(title)-1, 0)
		if rule > 0 {
			title += " " + strings.Repeat("─", rule)
		}
		lines = append(lines, detailLine{title, "heading"})
	}
	appendText(terminalLabel(c.Source)+" // "+terminalLabel(c.ThreadID)+" // "+contextAge(c), "metadata", "")
	if c.CurrentTask != "" {
		section(contextTaskTitle(c))
		appendText(codex.SanitizeSessionContext(c.CurrentTask), "body", "")
	}
	if c.LatestGuidance != "" {
		section(i18n.Text("LATEST GUIDANCE"))
		appendText(codex.SanitizeSessionContext(c.LatestGuidance), "body", "")
	}
	if c.Kind == codex.SessionContextApproval && c.ApprovalContext != "" {
		section(i18n.Text("CONTEXT"))
		appendText(codex.SanitizeSessionContext(c.ApprovalContext), "body", "")
	}
	g := m.monitorDashboardLayout()
	if (c.Kind == codex.SessionContextApproval || c.Kind == codex.SessionContextQuestion) && !m.monitorApprovalHasOutcome() && !m.monitorApprovalControls(g.contentWidth, g.meterHeight) && m.monitorPromptRows(g.contentWidth, g.meterHeight) == 0 {
		section(i18n.Text("REPLY IN CODEX"))
		appendText(m.monitorApprovalBlockReason(c), "warning", "")
	}
	if hasWorkingCommand(c) {
		if c.Activity.Prose != "" {
			section(i18n.Text("COMMENTARY"))
			appendText(codex.SanitizeSessionContext(c.Activity.Prose), "body", "")
		}
		section(workingCommandTitle(c.Activity))
		prefix := "│ "
		if width < 3 {
			prefix = ""
		}
		appendText(codex.SanitizeSessionContext(c.Activity.Command), "command", prefix)
	} else if c.Kind == codex.SessionContextApproval && c.FileChanges != "" {
		section(contextTitle(c))
		appendText(sanitize(c.Text), "body", "")
		lines = append(lines, detailLine{})
		lines = append(lines, fileApprovalDocument(c.FileChanges, width)...)
	} else if c.Kind == codex.SessionContextApproval && c.CommandDetails.Command != "" {
		section(contextTitle(c))
		d := c.CommandDetails
		if strings.TrimSpace(d.Justification) != "" {
			section(i18n.Text("JUSTIFICATION"))
			appendText(sanitize(d.Justification), "body", "")
		}
		section(i18n.Text("COMMAND"))
		prefix := "│ "
		if width < 3 {
			prefix = ""
		}
		appendText(sanitize(d.Command), "command", prefix)
		section(i18n.Text("WORKING DIRECTORY"))
		appendText(sanitize(d.Directory), "metadata", "")
		for _, option := range c.ApprovalOptions {
			if option.Detail != "" {
				section(i18n.Text("PERSISTENT PERMISSION RULE"))
				appendText(sanitize(option.Detail), "warning", "")
			}
		}
	} else if c.Text != "" {
		section(contextTitle(c))
		text := sanitize(c.Text)
		// Legacy/unstructured requests remain verbatim. This cosmetic spacer
		// does not claim to identify an authoritative command boundary.
		if c.Kind == codex.SessionContextApproval {
			parts := strings.Split(text, "\n")
			for i, line := range parts {
				if i > 0 && strings.HasPrefix(line, "Command: ") && strings.TrimSpace(parts[i-1]) != "" {
					parts[i] = "\n" + line
					break
				}
			}
			text = strings.Join(parts, "\n")
		}
		appendText(text, "body", "")
	}
	if c.ApprovalDecisions != "" {
		section(i18n.Text("OFFERED DECISIONS"))
		appendText(c.ApprovalDecisions, "metadata", "")
	}
	if notice := m.quota.notices[s.id]; notice != "" {
		appendText(codex.SanitizeSessionContext(notice), "warning", "")
	}
	return lines
}

func fileApprovalDocument(changes string, width int) (lines []detailLine) {
	diff := codex.FileDiffLines(changes)
	added, removed := codex.FileDiffTotals(diff)
	for _, text := range strings.Split(ansi.Hardwrap(fmt.Sprintf("+%d / −%d", added, removed), max(width, 1), true), "\n") {
		lines = append(lines, detailLine{ansi.Truncate(text, max(width, 1), ""), "diff-summary"})
	}
	for _, line := range diff {
		for _, text := range strings.Split(ansi.Hardwrap(line.NumberedText(), max(width, 1), true), "\n") {
			lines = append(lines, detailLine{ansi.Truncate(text, max(width, 1), ""), line.Kind})
		}
	}
	return lines
}

func (m Model) contextDetailLines(width int) []string {
	doc := m.contextDetailDocument(width)
	lines := make([]string, len(doc))
	for i, line := range doc {
		lines[i] = line.text
	}
	return lines
}

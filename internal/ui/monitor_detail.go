package ui

import (
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
	}
	return style.Render(line.text)
}

// A single document owns both display and scroll geometry. Formatting never
// changes the stored request, capability or text sent back to Codex.
func (m Model) contextDetailDocument(width int) []detailLine {
	width = max(width, 1)
	s, ok := m.contextDetailSession()
	if !ok || s.preview.Text == "" {
		return []detailLine{{i18n.Text("NO CONTEXT"), "metadata"}}
	}
	c := s.preview
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
	if status := monitorSessionAttentionLabel(s); status != "" {
		appendText(status, "heading", "")
	}
	appendText(c.Source+" // "+c.ThreadID+" // "+contextAge(c), "metadata", "")
	g := m.dashboardLayout()
	if (c.Kind == codex.SessionContextApproval || c.Kind == codex.SessionContextQuestion) && !m.monitorApprovalHasOutcome() && !m.monitorApprovalControls(g.contentWidth, g.meterHeight) && m.monitorPromptRows(g.contentWidth, g.meterHeight) == 0 {
		section(i18n.Text("REPLY IN CODEX"))
		appendText(m.monitorApprovalBlockReason(c), "warning", "")
	}
	if c.Kind == codex.SessionContextApproval && c.CommandDetails.Command != "" {
		d := c.CommandDetails
		if strings.TrimSpace(d.Justification) != "" {
			section(i18n.Text("JUSTIFICATION"))
			appendText(codex.SanitizeSessionContext(d.Justification), "body", "")
		}
		section(i18n.Text("COMMAND"))
		prefix := "│ "
		if width < 3 {
			prefix = ""
		}
		appendText(codex.SanitizeSessionContext(d.Command), "command", prefix)
		section(i18n.Text("WORKING DIRECTORY"))
		appendText(codex.SanitizeSessionContext(d.Directory), "metadata", "")
		for _, option := range c.ApprovalOptions {
			if option.Detail != "" {
				section(i18n.Text("PERSISTENT PERMISSION RULE"))
				appendText(codex.SanitizeSessionContext(option.Detail), "warning", "")
			}
		}
	} else {
		section(contextTitle(c))
		text := codex.SanitizeSessionContext(c.Text)
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

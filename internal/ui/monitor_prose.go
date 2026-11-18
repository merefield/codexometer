package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/prose"
)

func (m Model) proseLines(text string, width int) []string {
	doc := m.proseCache.Get(text)
	if len(doc) == 0 {
		return sessionWebTextLines(text, width)
	}
	colors := paletteFor(m.theme)
	width = max(1, width)
	var lines []string
	for _, b := range doc {
		if len(b.Rows) > 0 {
			lines = append(lines, proseTableLines(b, width, colors)...)
		} else {
			lines = append(lines, wrapProse(styledRuns(b.Runs, width, colors, false), width)...)
		}
	}
	return lines
}

func styledRuns(runs []prose.ProseRun, width int, p palette, header bool) string {
	var b strings.Builder
	for _, r := range runs {
		text := r.Text
		if width >= 12 {
			text = formatSessionWebLinks(text, !r.Code)
		}
		if r.Bold || r.Italic || r.Code || header {
			style := lipgloss.NewStyle().Bold(header || r.Bold).Italic(r.Italic)
			if r.Bold || header {
				style = style.Foreground(p.primary)
			}
			if r.Code {
				style = style.Background(blendColors(p.background, p.dim))
			}
			text = style.Render(text)
		}
		b.WriteString(text)
	}
	return b.String()
}

// Serialize each wrapped row independently. Styles and OSC links must survive
// scrolling into the middle of a span, without leaking onto the panel border.
func wrapProse(s string, width int) []string {
	wrapped := ansi.Hardwrap(s, width, true)
	if !strings.Contains(wrapped, "\x1b") {
		return strings.Split(wrapped, "\n")
	}
	lines := strings.Split(wrapped, "\n")
	var style uv.Style
	var link uv.Link
	for i, line := range lines {
		prefix := ""
		if !style.IsZero() {
			prefix = style.String()
		}
		if !link.IsZero() {
			prefix += ansi.SetHyperlink(link.URL, link.Params)
		}
		walkTerminalText(line, func(seq string, _ int, p *ansi.Parser) {
			switch {
			case ansi.HasCsiPrefix(seq) && p.Command() == 'm':
				uv.ReadStyle(p.Params(), &style)
			case ansi.HasOscPrefix(seq) && p.Command() == 8:
				uv.ReadLink(p.Data(), &link)
			}
		})
		lines[i] = prefix + line
		if !link.IsZero() {
			lines[i] += ansi.ResetHyperlink()
		}
		if !style.IsZero() {
			lines[i] += ansi.ResetStyle
		}
	}
	return lines
}

func proseTableLines(b prose.ProseBlock, width int, p palette) []string {
	header := b.Rows[0]
	sizes := make([]int, len(header))
	rows := make([][]string, len(b.Rows))
	for i, cells := range b.Rows {
		for j, cell := range cells {
			value := styledRuns(cell, width, p, i == 0)
			rows[i] = append(rows[i], value)
			sizes[j] = max(sizes[j], ansi.StringWidth(value))
		}
	}
	total := 2 * (len(sizes) - 1)
	for _, w := range sizes {
		total += w
	}
	var lines []string
	if total <= width {
		for i, cells := range rows {
			cols := make([]string, len(cells))
			for j, v := range cells {
				pad := sizes[j] - ansi.StringWidth(v)
				left := 0
				switch b.Align[j] {
				case "right":
					left = pad
				case "center":
					left = pad / 2
				}
				cols[j] = strings.Repeat(" ", left) + v + strings.Repeat(" ", pad-left)
			}
			lines = append(lines, strings.TrimRight(strings.Join(cols, "  "), " "))
			if i == 0 {
				lines = append(lines, p.dimmed().Render(strings.Repeat("─", total)))
			}
		}
	} else {
		for i, cells := range rows {
			if i == 0 && len(rows) > 1 {
				continue
			}
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			for j, value := range cells {
				label := rows[0][j]
				if label == "" {
					label = fmt.Sprintf("#%d", j+1)
				}
				if i > 0 {
					value = label + ": " + value
				}
				lines = append(lines, wrapProse(value, width)...)
			}
		}
	}
	return lines
}

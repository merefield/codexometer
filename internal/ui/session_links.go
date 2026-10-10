package ui

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

const sessionWebLinkID = "id=codexometer-session-web"

var sessionWebURL = regexp.MustCompile(`(?i)https?://[^\s<>"'\x60\x00-\x20]+`)

// Only display copies gain links. Clipboard text and approval payloads stay plain.
func linkSessionWebText(text string) string {
	return formatSessionWebLinks(text, true)
}

func formatSessionWebLinks(text string, cook bool) string {
	matches := sessionWebURL.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	literal := sessionLinkLiteralRanges(text)
	var out strings.Builder
	previous, literalIndex := 0, 0
	for _, match := range matches {
		start, end := match[0], match[1]
		end = sessionWebURLEnd(text, start, end)
		target := text[start:end]
		if !validSessionWebURL(target) {
			continue
		}
		labelStart, labelEnd, markdownEnd := start, start-2, end
		if start >= 3 && text[start-3:start] == "](<" {
			labelEnd = start - 3
		}
		if labelEnd >= previous && (text[labelEnd:start] == "](" || text[labelEnd:start] == "](<") {
			if index := strings.LastIndex(text[previous:labelEnd], "["); index >= 0 {
				index += previous
				label := text[index+1 : labelEnd]
				// Keep incomplete, escaped, image and nested Markdown literal.
				closing := ")"
				if text[labelEnd:start] == "](<" {
					closing = ">)"
				}
				if label != "" && !strings.ContainsAny(label, "[]\n\r\\") &&
					(index == 0 || (text[index-1] != '\\' && text[index-1] != '!')) &&
					strings.HasPrefix(text[end:], closing) {
					labelStart, markdownEnd = index, end+len(closing)
				}
			}
		}
		for literalIndex < len(literal) && literal[literalIndex][1] <= labelStart {
			literalIndex++
		}
		isLiteral := literalIndex < len(literal) && literal[literalIndex][0] < markdownEnd
		out.WriteString(text[previous:labelStart])
		if cook && !isLiteral && labelStart != start && cookSessionWebURL(target) {
			out.WriteString(sessionWebLink(target, text[labelStart+1:labelEnd]))
			previous = markdownEnd
		} else if labelStart != start {
			out.WriteByte('[')
			out.WriteString(sessionWebLink(target, text[labelStart+1:labelEnd]))
			out.WriteString(text[labelEnd:start])
			out.WriteString(sessionWebLink(target, target))
			previous = end
		} else {
			out.WriteString(sessionWebLink(target, target))
			previous = end
		}
	}
	out.WriteString(text[previous:])
	return out.String()
}

// Inline Markdown has an explicit closing delimiter, so trailing punctuation
// inside its destination is part of the URL. Plain prose uses balanced trimming.
func sessionWebURLEnd(text string, start, end int) int {
	if start >= 3 && text[start-3:start] == "](<" && strings.HasPrefix(text[end:], ">)") {
		return end
	}
	if start >= 2 && text[start-2:start] == "](" {
		depth := 0
		for index := start; index < end; index++ {
			switch text[index] {
			case '(':
				depth++
			case ')':
				if depth == 0 {
					return index
				}
				depth--
			}
		}
	}
	for end > start {
		candidate := text[start:end]
		last := candidate[len(candidate)-1]
		if strings.ContainsRune(".,!?;:`", rune(last)) ||
			last == ')' && strings.Count(candidate, ")") > strings.Count(candidate, "(") ||
			last == ']' && strings.Count(candidate, "]") > strings.Count(candidate, "[") ||
			last == '}' && strings.Count(candidate, "}") > strings.Count(candidate, "{") {
			end--
			continue
		}
		break
	}
	return end
}

func sessionWebLink(target, label string) string {
	return ansi.SetHyperlink(target, sessionWebLinkID) + "\x1b[4m" + label + "\x1b[24m" + ansi.ResetHyperlink()
}

// This is a display rule, independent of Codex's network-access permissions.
// Match domain boundaries; lookalike suffixes, credentials and custom ports
// never cook. Subdomains of these three domains use the same display rule.
func cookSessionWebURL(target string) bool {
	u, err := url.Parse(target)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"github.com", "openai.com", "chatgpt.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// Be conservative about literal code: lines containing backticks, indented code
// and fenced blocks retain their exact visible text even on allowlisted hosts.
func sessionLinkLiteralRanges(text string) (ranges [][2]int) {
	var fence byte
	var fenceLength, inlineTicks int
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		run := 0
		if len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			for run < len(trimmed) && trimmed[run] == trimmed[0] {
				run++
			}
		}
		inFence := fence != 0
		if run >= 3 && len(line)-len(trimmed) <= 3 {
			if fence == 0 {
				fence, fenceLength = trimmed[0], run
			} else if trimmed[0] == fence && run >= fenceLength && strings.TrimSpace(trimmed[run:]) == "" {
				fence = 0
			}
		}
		inInlineCode := inlineTicks != 0
		if !inFence && fence == 0 {
			for index := 0; index < len(line); index++ {
				if line[index] != '`' {
					continue
				}
				end := index + 1
				for end < len(line) && line[end] == '`' {
					end++
				}
				if inlineTicks == 0 {
					inlineTicks = end - index
				} else if inlineTicks == end-index {
					inlineTicks = 0
				}
				index = end - 1
			}
		}
		if inFence || fence != 0 || inInlineCode || inlineTicks != 0 || strings.ContainsRune(line, '`') || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			ranges = append(ranges, [2]int{offset, offset + len(line)})
		}
		offset += len(line)
	}
	return ranges
}

func validSessionWebURL(target string) bool {
	// Ultraviolet's OSC 8 reader cannot retain unescaped semicolons in a URI.
	// Leave those URLs plain rather than silently changing their destination.
	if len(target) > 2048 || strings.HasSuffix(target, "…") || strings.Contains(target, ";") {
		return false
	}
	for _, r := range target {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			return false
		}
	}
	u, err := url.Parse(target)
	return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && u.Hostname() != "" && u.User == nil
}

// Each wrapped line opens/closes its own link so scroll slices, box borders and
// adjacent columns cannot inherit a preceding URL. Tiny layouts stay plain,
// bounding repeated destination metadata even in large approval documents.
func sessionWebTextLines(text string, width int) []string {
	return wrappedSessionWebText(text, width, true)
}

func sessionWebLiteralTextLines(text string, width int) []string {
	return wrappedSessionWebText(text, width, false)
}

func wrappedSessionWebText(text string, width int, cook bool) []string {
	if width < 12 {
		return strings.Split(ansi.Hardwrap(text, max(width, 1), true), "\n")
	}
	linked := formatSessionWebLinks(text, cook)
	lines := strings.Split(ansi.Hardwrap(linked, width, true), "\n")
	if linked == text {
		return lines
	}
	var active string
	for i, line := range lines {
		prefix := ""
		if active != "" {
			prefix = ansi.SetHyperlink(active, sessionWebLinkID) + "\x1b[4m"
		}
		walkTerminalText(line, func(seq string, _ int, p *ansi.Parser) {
			if ansi.HasOscPrefix(seq) && p.Command() == 8 {
				fields := strings.SplitN(string(p.Data()), ";", 3)
				if len(fields) == 3 {
					active = fields[2]
				}
			}
		})
		lines[i] = prefix + line
		if active != "" {
			lines[i] += "\x1b[24m" + ansi.ResetHyperlink()
		}
	}
	return lines
}

// The same grapheme widths as the renderer identify the clicked cell, including
// Unicode labels and URLs wrapped over multiple lines. Only our generated
// session links are actionable; header/pricing and arbitrary OSCs stay native.
func sessionWebLinkAt(content string, x, y int) string {
	if x < 0 || y < 0 {
		return ""
	}
	var row, col int
	var target, params, found string
	walkTerminalText(content, func(seq string, width int, p *ansi.Parser) {
		if found != "" {
			return
		}
		if ansi.HasOscPrefix(seq) && p.Command() == 8 {
			fields := strings.SplitN(string(p.Data()), ";", 3)
			if len(fields) == 3 {
				params, target = fields[1], fields[2]
			}
		} else if seq == "\n" {
			row++
			col = 0
		} else if width > 0 {
			if row == y && x >= col && x < col+width && params == sessionWebLinkID && validSessionWebURL(target) {
				found = target
			}
			col += width
		}
	})
	return found
}

func walkTerminalText(text string, visit func(string, int, *ansi.Parser)) {
	p := ansi.GetParser()
	defer ansi.PutParser(p)
	var state byte
	for len(text) > 0 {
		seq, width, n, next := ansi.DecodeSequence(text, state, p)
		visit(seq, width, p)
		text, state = text[n:], next
	}
}

type sessionWebLinkOpenedMsg struct{ err error }

func (m Model) sessionWebLinkClick(msg tea.MouseMsg) (tea.Cmd, bool) {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft || m.meterView != viewMonitor || !localSessionWebLinkOpening() {
		return nil, false
	}
	// Rendering is only needed when a source could contain an actionable link.
	// Ordinary clicks retain the existing geometry/hit-testing cost.
	if !m.sessionHasWebLinkSource() {
		return nil, false
	}
	target := sessionWebLinkAt(m.View().Content, click.X, click.Y)
	if target == "" {
		return nil, false
	}
	return openSessionWebLink(target), true
}

func (m Model) sessionHasWebLinkSource() bool {
	if c, ok := m.historicalContext(); ok && sessionContextHasWebLinkSource(c) {
		return true
	}
	for _, s := range m.monitorSessionData {
		if m.monitorSessionVisible(s) && (sessionContextHasWebLinkSource(s.preview) || sessionTextHasWebURL(m.quota.notices[s.id])) {
			return true
		}
	}
	return false
}

func sessionContextHasWebLinkSource(c codex.SessionContext) bool {
	for _, text := range []string{
		c.Text, c.CurrentTask, c.LatestGuidance, c.ApprovalContext, c.ApprovalDecisions,
		c.Source, c.ThreadID, c.Activity.Prose, c.Activity.Command,
		c.CommandDetails.Command, c.CommandDetails.Justification, c.CommandDetails.Directory,
	} {
		if sessionTextHasWebURL(text) {
			return true
		}
	}
	for _, option := range c.ApprovalOptions {
		if sessionTextHasWebURL(option.Detail) {
			return true
		}
	}
	return false
}

func sessionTextHasWebURL(text string) bool {
	return strings.Contains(text, "://") && sessionWebURL.MatchString(text)
}

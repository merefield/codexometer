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

var sessionMarkdownBlankLine = regexp.MustCompile(`\r?\n[ \t]*\r?\n`)

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
		// A cooked link consumes its title too, including any URL-like text.
		if match[0] < previous {
			continue
		}
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
				angle := text[labelEnd:start] == "](<"
				closing, complete := sessionMarkdownLinkEnd(text, end, angle)
				if !angle && strings.Count(target, "(") != strings.Count(target, ")") {
					complete = false
				}
				if label != "" && !strings.ContainsAny(label, "[]\n\r\\") &&
					(index == 0 || (text[index-1] != '\\' && text[index-1] != '!')) && complete {
					labelStart, markdownEnd = index, closing
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
	if start >= 3 && text[start-3:start] == "](<" && strings.HasPrefix(text[end:], ">") {
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
		// A title follows whitespace, outside the URL captured by the regexp.
		return end
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

// Consume the remainder of a complete inline link, including an optional title
// delimited by double quotes, single quotes or parentheses. Invalid/incomplete
// titles stay visible, and the destination is never inferred from title text.
func sessionMarkdownLinkEnd(text string, end int, angle bool) (int, bool) {
	index := end
	if angle {
		if index >= len(text) || text[index] != '>' {
			return end, false
		}
		index++
	}
	if index < len(text) && text[index] == ')' {
		return index + 1, !sessionMarkdownBlankLine.MatchString(text[end : index+1])
	}
	spaced := index
	for index < len(text) && strings.ContainsRune(" \t\r\n", rune(text[index])) {
		index++
	}
	if index == spaced || index >= len(text) {
		return end, false
	}
	if text[index] == ')' {
		return index + 1, !sessionMarkdownBlankLine.MatchString(text[end : index+1])
	}
	closing := text[index]
	if closing == '(' {
		closing = ')'
	} else if closing != '"' && closing != '\'' {
		return end, false
	}
	index++
	for index < len(text) {
		ch := text[index]
		if ch == '\\' && index+1 < len(text) {
			index += 2
			continue
		}
		if ch == closing {
			index++
			for index < len(text) && strings.ContainsRune(" \t\r\n", rune(text[index])) {
				index++
			}
			if index < len(text) && text[index] == ')' {
				return index + 1, !sessionMarkdownBlankLine.MatchString(text[end : index+1])
			}
			return end, false
		}
		if ch == '(' && closing == ')' {
			return end, false
		}
		index++
	}
	return end, false
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

// Code blocks and matched inline code spans retain their exact visible text.
// Inline delimiters protect their span rather than unrelated prose on the line.
func sessionLinkLiteralRanges(text string) (ranges [][2]int) {
	var fence byte
	var fenceLength int
	offset, inlineStart := 0, 0
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
		block := inFence || fence != 0 || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
		end := offset + len(line)
		// Inline code cannot cross a code block or paragraph boundary.
		if block || strings.TrimSpace(line) == "" {
			ranges = append(ranges, sessionInlineCodeRanges(text[inlineStart:offset], inlineStart)...)
			if block {
				ranges = append(ranges, [2]int{offset, end})
			}
			inlineStart = end
		}
		offset = end
	}
	ranges = append(ranges, sessionInlineCodeRanges(text[inlineStart:], inlineStart)...)
	return ranges
}

func sessionInlineCodeRanges(text string, offset int) (ranges [][2]int) {
	type delimiter struct {
		start, end, next int
		escaped          bool
	}
	var runs []delimiter
	for index := 0; index < len(text); {
		if text[index] != '`' {
			index++
			continue
		}
		end := index + 1
		for end < len(text) && text[end] == '`' {
			end++
		}
		slashes := 0
		for previous := index - 1; previous >= 0 && text[previous] == '\\'; previous-- {
			slashes++
		}
		runs = append(runs, delimiter{index, end, -1, slashes%2 != 0})
		index = end
	}
	if len(runs) < 2 {
		return nil
	}
	// Find matching runs once, bounding work even with many unmatched delimiters.
	next := make(map[int]int)
	for index := len(runs) - 1; index >= 0; index-- {
		length := runs[index].end - runs[index].start
		// Outside code a backslash escapes only the first backtick of a run.
		openingLength := length
		if runs[index].escaped {
			openingLength--
		}
		if following, ok := next[openingLength]; ok {
			runs[index].next = following
		}
		next[length] = index
	}
	for index := 0; index < len(runs); index++ {
		opening := runs[index]
		if opening.next < 0 {
			continue
		}
		if opening.escaped {
			opening.start++
		}
		// Backslashes are literal inside code, so they cannot escape a closing run.
		closing := runs[opening.next]
		ranges = append(ranges, [2]int{offset + opening.start, offset + closing.end})
		index = opening.next
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

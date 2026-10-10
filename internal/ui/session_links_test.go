package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestSessionWebLinksPreserveDestinationsAndVisibleText(t *testing.T) {
	for _, test := range []struct{ text, target string }{
		{"See https://example.com/path?q=one&two=3#section.", "https://example.com/path?q=one&two=3#section"},
		{"See [release](https://example.com/a_(b)).", "https://example.com/a_(b)"},
		{"[punctuation](https://example.com/path.?)", "https://example.com/path.?"},
		{"[angle](<https://example.com/path.!>)", "https://example.com/path.!"},
		{"[界 label](<https://example.com/release>)", "https://example.com/release"},
		{"`https://example.com/code`", "https://example.com/code"},
		{"curl 'https://example.com/api'", "https://example.com/api"},
		{"[A](https://example.com/a) [B](https://example.com/b)", "https://example.com/b"},
	} {
		out := linkSessionWebText(test.text)
		if ansi.Strip(out) != test.text {
			t.Fatalf("visible text changed: %q", out)
		}
		if !strings.Contains(out, ansi.SetHyperlink(test.target, sessionWebLinkID)) {
			t.Fatalf("missing exact destination %q: %q", test.target, out)
		}
	}
	for _, text := range []string{"file:///tmp/foo", "javascript:alert(1)", "https://", "https://user:password@example.com", "https://example.com/part…", "https://example.com/a;b", "https://example.com/" + strings.Repeat("x", 2050)} {
		if out := linkSessionWebText(text); out != text {
			t.Fatalf("unsupported/truncated URL gained a link: %q", text)
		}
	}
}

func TestSessionWebLinksSurviveWrapSlicesAndBorders(t *testing.T) {
	target := "https://example.com/" + strings.Repeat("long-", 12) + "?q=one&x=2#fragment"
	text := "界 [read this release](" + target + ") tail"
	for _, width := range []int{12, 20, 40, 80} {
		lines := sessionWebTextLines(text, width)
		if strings.Join(stripLinkLines(lines), "\n") != ansi.Hardwrap(text, width, true) {
			t.Fatal("link wrapping changed geometry")
		}
		linkedRows := 0
		for _, line := range lines {
			if lipgloss.Width(line) > width {
				t.Fatal("wrapped line overflow")
			}
			cells := backgroundTestLines(line + "│ next")[0]
			linked := false
			for _, cell := range cells {
				if cell.Link.URL == target {
					if cell.Style.Underline != uv.UnderlineSingle {
						t.Fatal("wrapped link fragment is not underlined")
					}
					linked = true
				}
			}
			if linked {
				linkedRows++
			}
			for _, cell := range cells[len(cells)-6:] {
				if cell.Link.URL != "" || cell.Style.Underline != uv.UnderlineNone {
					t.Fatal("link or underline leaked to adjacent cells")
				}
			}
		}
		if linkedRows < 2 {
			t.Fatal("wrapped destination lost its link")
		}
		// A slice starting halfway through a link must remain clickable on its own.
		for _, line := range lines[1:] {
			if strings.Contains(line, sessionWebLinkID) {
				found := false
				for x := 0; x < width; x++ {
					if sessionWebLinkAt(line, x, 0) == target {
						found = true
					}
				}
				if !found {
					t.Fatal("scrolled-in link fragment lost its destination")
				}
			}
		}
	}
}
func stripLinkLines(lines []string) []string {
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = ansi.Strip(line)
	}
	return plain
}

func TestSessionWebLinksRenderAcrossContextViewsWithoutChangingCopyOrApprovals(t *testing.T) {
	target := "https://example.com/releases/" + strings.Repeat("long-path-", 5)
	text := "[release](" + target + ")"
	m := contextTestModel()
	m.width, m.height = 120, 40
	m.monitorSessionData[0].preview = codex.SessionContext{Kind: codex.SessionContextReply, Text: text, ThreadID: "root-one"}
	before := m.monitorSessionData[0].preview
	for _, mode := range []int{contextSplit, contextWide, contextFull} {
		m.setRowContext("root-one", mode)
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
			var output bytes.Buffer
			w := colorprofile.Writer{Forward: &output, Profile: profile}
			_, _ = w.Write([]byte(m.View().Content))
			linked := false
			for _, line := range backgroundTestLines(output.String()) {
				for _, cell := range line {
					if cell.Link.URL == target {
						if cell.Style.Underline != uv.UnderlineSingle {
							t.Fatal("rendered session link lost underline")
						}
						linked = true
					}
				}
			}
			if !linked {
				t.Fatalf("link missing in mode=%v profile=%v", mode, profile)
			}
		}
		if m.monitorCopyText("root-one") != text || m.monitorSessionData[0].preview != before {
			t.Fatal("link formatting changed copy or source")
		}
	}
	m = approvalTestModel()
	m.width, m.height = 120, 40
	m.monitorSessionData[0].preview.CommandDetails.Justification = text
	before = m.monitorSessionData[0].preview
	_ = m.View()
	if before != m.monitorSessionData[0].preview {
		t.Fatal("link formatting changed approval action/capability")
	}
	// Raw OSC destinations are discarded before regenerated display links.
	m.monitorSessionData[0].preview.Text = ansi.SetHyperlink("https://evil.example", sessionWebLinkID) + "innocent" + ansi.ResetHyperlink()
	if strings.Contains(m.View().Content, "https://evil.example") {
		t.Fatal("untrusted OSC link retained")
	}
}

func TestSessionWebLinkClickScopesAndFailures(t *testing.T) {
	target := "https://example.com/release"
	tagged := ansi.SetHyperlink(target, sessionWebLinkID) + "界label" + ansi.ResetHyperlink() + " tail"
	if sessionWebLinkAt(tagged, 1, 0) != target || sessionWebLinkAt(tagged, 7, 0) != "" || sessionWebLinkAt(tagged, -1, 0) != "" {
		t.Fatal("grapheme hit testing wrong")
	}
	if sessionWebLinkAt(ansi.SetHyperlink(target)+"label"+ansi.ResetHyperlink(), 0, 0) != "" {
		t.Fatal("native-only link became app action")
	}
	m := contextTestModel()
	for _, msg := range []tea.MouseMsg{tea.MouseMotionMsg{}, tea.MouseReleaseMsg{}, tea.MouseClickMsg{Button: tea.MouseRight}, tea.MouseWheelMsg{Button: tea.MouseWheelDown}} {
		if _, handled := m.sessionWebLinkClick(msg); handled {
			t.Fatal("non-left-click opened URL")
		}
	}
	next, _ := m.Update(sessionWebLinkOpenedMsg{err: errors.New("open failed")})
	m = next.(Model)
	if !m.sessionWebLinkError || !strings.Contains(ansi.Strip(m.renderFooter(120, paletteFor(m.theme))), "Could not open web link.") {
		t.Fatal("opener failure is invisible")
	}
	next, _ = m.Update(sessionWebLinkOpenedMsg{})
	if next.(Model).sessionWebLinkError {
		t.Fatal("successful opener retained error")
	}
}

func TestSessionWebLinksCookOnlyAllowlistedHTTPSDomains(t *testing.T) {
	for _, host := range []string{"github.com", "openai.com", "chatgpt.com", "GITHUB.COM", "docs.github.com", "developers.openai.com", "platform.openai.com", "learn.chatgpt.com", "nested.docs.openai.com"} {
		target := "https://" + host + "/owner/project?q=one&x=2#fragment"
		text := "See [界 docs](" + target + ")."
		linked := linkSessionWebText(text)
		if ansi.Strip(linked) != "See 界 docs." || sessionWebLinkAt(linked, 5, 0) != target {
			t.Fatalf("allowlisted link not cooked with exact destination: %q", linked)
		}
		if plain := ansi.Strip(strings.Join(sessionWebLiteralTextLines(text, 40), "\n")); plain != ansi.Hardwrap(text, 40, true) {
			t.Fatal("literal command/approval display cooked its source")
		}
	}
	for _, target := range []string{
		"https://example.com/path", "http://github.com/path", "https://github.com.evil.example/path",
		"https://evilgithub.com/path", "https://openai.com.evil.example/path", "https://fakeopenai.com/path", "https://chatgpt.com.evil.example/path", "https://github.com:8443/path",
		"https://github.com./path", "https://github.com@evil.example/path", "https://evil.example/?next=https://github.com",
	} {
		text := "[label](" + target + ")"
		if got := ansi.Strip(linkSessionWebText(text)); got != text {
			t.Fatalf("non-allowlisted destination hidden: %q => %q", text, got)
		}
	}
	for _, text := range []string{
		"https://github.com/owner/project", "`[label](https://github.com/a)`", "`start\n[label](https://github.com/a)\nend`",
		"```markdown\n[label](https://github.com/a)\n```",
		"~~~markdown\n[label](https://openai.com/a)\n~~~",
		"    [label](https://chatgpt.com/a)", "\\[label](https://github.com/a)",
		"![image](https://github.com/a)", "[label](https://github.com/a", "[label](https://github.com/a…)",
	} {
		if got := ansi.Strip(linkSessionWebText(text)); got != text {
			t.Fatalf("bare URL, code or incomplete Markdown cooked: %q => %q", text, got)
		}
	}
	text := "~~~\n[label](https://github.com/a)\n~~~\n[docs](<https://openai.com/docs>)"
	if got := ansi.Strip(linkSessionWebText(text)); got != "~~~\n[label](https://github.com/a)\n~~~\ndocs" {
		t.Fatalf("fence closing or angle destination mishandled: %q", got)
	}
}

func TestSessionWebLinkLabelsAndDestinationsAreUnderlined(t *testing.T) {
	target := "https://example.com/docs"
	linked := linkSessionWebText("before [界 docs](" + target + ") after")
	cells := backgroundTestLines(linked)[0]
	label, destination := 0, 0
	for x, cell := range cells {
		if cell.Link.URL != "" {
			if cell.Link.URL != target || cell.Style.Underline != uv.UnderlineSingle {
				t.Fatal("label or destination is not an underlined link")
			}
			if x < 14 {
				label++
			} else {
				destination++
			}
		} else if cell.Style.Underline != uv.UnderlineNone {
			t.Fatal("surrounding prose inherited underline")
		}
	}
	if label == 0 || destination == 0 {
		t.Fatal("label or destination has no link cells")
	}
}

func TestSessionCookedLinksKeepHistoryAndApprovalCommandsIntact(t *testing.T) {
	target := "https://github.com/owner/project/pull/81"
	text := "See [PR 81](" + target + ")"
	m := historyTestModel()
	c := historyContext(4)
	c.Text = text
	m.monitorSessionData[0].preview = c
	m.observeMonitorHistory()
	m.monitorSessionData[0].preview = historyContext(5)
	m.observeMonitorHistory()
	m.moveMonitorHistory(-1)
	if m.monitorCopyText("root-one") != text {
		t.Fatal("history clipboard lost original Markdown")
	}
	var display []string
	for _, line := range m.contextDetailDocument(60) {
		display = append(display, line.text)
	}
	if content := strings.Join(display, "\n"); !strings.Contains(ansi.Strip(content), "See PR 81") || !strings.Contains(content, target) {
		t.Fatal("historical reply lost cooked label or destination")
	}
	m = approvalTestModel()
	m.monitorSessionData[0].preview.CommandDetails.Command = "printf '[PR 81](" + target + ")'"
	before := m.monitorSessionData[0].preview
	display = nil
	for _, line := range m.contextDetailDocument(200) {
		display = append(display, ansi.Strip(line.text))
	}
	if !strings.Contains(strings.Join(display, "\n"), before.CommandDetails.Command) || m.monitorSessionData[0].preview != before {
		t.Fatal("approval command lost exact reviewed text")
	}
}

func TestSessionCookedLinkSurvivesWrappedLabelAndScroll(t *testing.T) {
	target := "https://github.com/owner/project/pull/81"
	label := strings.Repeat("界 release notes ", 8)
	for _, width := range []int{12, 20, 40} {
		lines := sessionWebTextLines("["+label+"]("+target+") tail", width)
		if strings.Join(stripLinkLines(lines), "\n") != ansi.Hardwrap(label+" tail", width, true) {
			t.Fatal("cooked label wrapping changed text")
		}
		for _, line := range lines[:len(lines)-1] {
			if sessionWebLinkAt(line, 0, 0) != target {
				t.Fatal("scrolled cooked label lost its destination")
			}
			cells := backgroundTestLines(line + "│")[0]
			for _, cell := range cells[:len(cells)-1] {
				if cell.Link.URL == target && cell.Style.Underline != uv.UnderlineSingle {
					t.Fatal("cooked wrapped label lost underline")
				}
				if cell.Link.URL != "" && cell.Link.URL != target {
					t.Fatal("cooked wrapped label changed destination")
				}
			}
			if last := cells[len(cells)-1]; last.Link.URL != "" || last.Style.Underline != uv.UnderlineNone {
				t.Fatal("cooked link leaked to border")
			}
		}
	}
}

func TestSessionCookedWebLinksRenderAcrossProfilesAndKeepCopy(t *testing.T) {
	target := "https://developers.openai.com/codex/app-server"
	text := "See [API documentation](" + target + ")"
	m := contextTestModel()
	m.width, m.height = 120, 40
	m.monitorSessionData[0].preview = codex.SessionContext{Kind: codex.SessionContextReply, Text: text, ThreadID: "root-one"}
	for _, mode := range []int{contextSplit, contextWide, contextFull} {
		m.setRowContext("root-one", mode)
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
			var output bytes.Buffer
			w := colorprofile.Writer{Forward: &output, Profile: profile}
			_, _ = w.Write([]byte(m.View().Content))
			if strings.Contains(ansi.Strip(output.String()), target) || m.monitorCopyText("root-one") != text {
				t.Fatal("cooked URL remained visible or original copy changed")
			}
			linked := false
			for _, line := range backgroundTestLines(output.String()) {
				for _, cell := range line {
					if cell.Link.URL == target && cell.Style.Underline == uv.UnderlineSingle {
						linked = true
					}
				}
			}
			if !linked {
				t.Fatalf("cooked link missing in mode=%v profile=%v", mode, profile)
			}
		}
	}
}

func TestSessionCookedLinksWithMarkdownTitles(t *testing.T) {
	for _, tc := range []struct{ name, source, target string }{
		{"double quotes", `[docs](https://github.com/openai/codex "Codex")`, "https://github.com/openai/codex"},
		{"single quotes", `[docs](https://developers.openai.com/codex 'Codex')`, "https://developers.openai.com/codex"},
		{"parentheses", `[docs](https://chatgpt.com/help (Help))`, "https://chatgpt.com/help"},
		{"angle destination", `[docs](<https://github.com/openai/codex> "Codex")`, "https://github.com/openai/codex"},
		{"empty title", `[docs](https://github.com/openai/codex "")`, "https://github.com/openai/codex"},
		{"escaped quotes", `[docs](https://github.com/openai/codex "Say \"hello\"")`, "https://github.com/openai/codex"},
		{"balanced destination", `[docs](https://github.com/a_(b) "Codex")`, "https://github.com/a_(b)"},
		{"destination punctuation", `[docs](https://github.com/a! "Codex")`, "https://github.com/a!"},
		{"angle punctuation", `[docs](<https://github.com/a!> 'Codex')`, "https://github.com/a!"},
		{"query", `[docs](https://github.com/a?x=1&y=2 "Codex")`, "https://github.com/a?x=1&y=2"},
		{"URL in title", `[docs](https://github.com/a "https://example.com/title")`, "https://github.com/a"},
		{"multiline title", "[docs](https://github.com/a \"line one\nline two\")", "https://github.com/a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linked := linkSessionWebText("See " + tc.source + " after https://example.com/next")
			if got := ansi.Strip(linked); got != "See docs after https://example.com/next" {
				t.Fatalf("unexpected display: %q", got)
			}
			cells := backgroundTestLines(linked)[0]
			for _, cell := range cells[4:8] {
				if cell.Link.URL != tc.target || cell.Style.Underline != uv.UnderlineSingle {
					t.Fatalf("label changed destination or underline: %+v", cell)
				}
			}
			if sessionWebLinkAt(linked, 15, 0) != "https://example.com/next" {
				t.Fatal("title consumed following URL")
			}
			if got := ansi.Strip(formatSessionWebLinks(tc.source, false)); got != tc.source {
				t.Fatalf("literal display changed: %q", got)
			}
		})
	}
}

func TestSessionTitledLinksKeepMalformedAndUntrustedSource(t *testing.T) {
	for _, source := range []string{
		`[docs](https://github.com/a "unterminated)`,
		`[docs](https://github.com/a 'mismatched")`,
		`[docs](https://github.com/a"no space")`,
		`[docs](<https://github.com/a>"no space")`,
		`[docs](https://github.com/a "title" junk)`,
		`[docs](https://github.com/a (nested (title)))`,
		`[docs](https://github.com/a_(b "unbalanced destination")`,
		"[docs](https://github.com/a\n\n\"blank line\")",
		"[docs](https://github.com/a \"blank\n \nline\")",
		"[docs](https://github.com/a \"title\"\r\n\t\r\n)",
		`[docs](https://example.com/a "untrusted host")`,
		"`[docs](https://github.com/a \"literal code\")`",
	} {
		if got := ansi.Strip(linkSessionWebText(source)); got != source {
			t.Fatalf("invalid/untrusted/literal link cooked: %q => %q", source, got)
		}
	}
	linked := linkSessionWebText(`[docs](https://example.com/a "untrusted host")`)
	for _, cell := range backgroundTestLines(linked)[0] {
		if cell.Link.URL != "" && (cell.Link.URL != "https://example.com/a" || cell.Style.Underline != uv.UnderlineSingle) {
			t.Fatal("untrusted label/URL lost destination or underline")
		}
	}
}

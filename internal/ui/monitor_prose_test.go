package ui

import (
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestSessionInlineColoursStayInTheme(t *testing.T) {
	m := historyTestModel()
	m.width, m.height = 120, 42
	s := &m.monitorSessionData[0]
	s.name, s.attention = "Ω-session", codex.SessionAttentionComplete
	s.preview = codex.SessionContext{Kind: codex.SessionContextReply, ThreadID: s.id, Text: "α `β` γ **δ** *ε* ζ."}
	m.setRowContext(s.id, contextFull)
	for theme := themeHacker; theme < themeCount; theme++ {
		m.theme = theme
		p := paletteFor(theme)
		cells := backgroundTestLines(m.View().Content)
		for _, tc := range []struct {
			char string
			want color.Color
		}{{"α", p.accent}, {"β", p.accent}, {"γ", p.accent}, {"δ", p.primary}, {"ε", p.accent}, {"ζ", p.accent}, {"Ω", p.primary}} {
			found := false
			for _, row := range cells {
				for _, c := range row {
					if c.Content != tc.char {
						continue
					}
					found = true
					if c.Style.Fg == nil || color.RGBA64Model.Convert(c.Style.Fg) != color.RGBA64Model.Convert(tc.want) {
						t.Errorf("%s %q: foreground %v, want %v", p.name, tc.char, c.Style.Fg, tc.want)
					}
					if tc.char == "β" {
						assertBackground(t, c.Style.Bg, blendColors(p.background, p.dim))
					}
				}
			}
			if !found {
				t.Fatalf("missing %q in detail view", tc.char)
			}
		}
	}
}

func TestMonitorProseWrapsWithStylesAndResponsiveTable(t *testing.T) {
	m := contextTestModel()
	wrapped := m.proseLines("***abcdefghij***", 4)
	if got := strings.Join(stripLinkLines(wrapped), "/"); got != "abcd/efgh/ij" {
		t.Fatal(got)
	}
	for _, line := range wrapped {
		cells := backgroundTestLines(line + "│")[0]
		for _, cell := range cells[:len(cells)-1] {
			if cell.Style.Attrs&(uv.AttrBold|uv.AttrItalic) != uv.AttrBold|uv.AttrItalic {
				t.Fatalf("wrapped emphasis lost on %q: %v", cell.Content, cell.Style)
			}
		}
		if cells[len(cells)-1].Style.Attrs != 0 {
			t.Fatalf("style leaked onto border: %q", line)
		}
	}
	source := "| City | Total |\n|:---|---:|\n| 界 | 3 |\n| London | 100 |"
	wide := stripLinkLines(m.proseLines(source, 32))
	if got := strings.Join(wide, "\n"); got != "City    Total\n─────────────\n界          3\nLondon    100" {
		t.Fatalf("unaligned wide table:\n%s", got)
	}
	narrow := stripLinkLines(m.proseLines(source, 10))
	for _, row := range narrow {
		if ansi.StringWidth(row) > 10 {
			t.Fatalf("table escaped narrow panel: %q", row)
		}
	}
	if got := strings.Join(narrow, ""); got != "City: 界Total: 3City: LondonTotal: 100" {
		t.Fatalf("lost label or value in narrow table: %q", got)
	}
}

func TestMonitorMarkdownDoesNotChangeApprovalCommandsOrCopiedHistory(t *testing.T) {
	const raw = "**Passed**: use `**literal**`."
	m := historyTestModel()
	c := historyContext(4)
	c.Text = raw
	m.monitorSessionData[0].preview = c
	m.observeMonitorHistory()
	check := func() {
		t.Helper()
		var lines []string
		for _, l := range m.contextDetailDocument(60) {
			lines = append(lines, ansi.Strip(l.text))
		}
		if !strings.Contains(strings.Join(lines, "\n"), "Passed: use **literal**.") || m.monitorCopyText("root-one") != raw {
			t.Fatalf("unexpected detail or clipboard: %q, %q", lines, m.monitorCopyText("root-one"))
		}
	}
	check()
	m.monitorSessionData[0].preview = historyContext(5)
	m.observeMonitorHistory()
	m.moveMonitorHistory(-1)
	check()
	m = approvalTestModel()
	m.monitorSessionData[0].preview.CommandDetails.Command = raw
	for _, line := range m.contextDetailDocument(120) {
		if strings.Contains(ansi.Strip(line.text), raw) {
			return
		}
	}
	t.Fatal("approval command must retain its exact syntax")
}

func TestMonitorWorkingOnlyFormatsCommentary(t *testing.T) {
	m := contextTestModel()
	c := codex.SessionContext{Kind: codex.SessionContextActivity, Activity: codex.SessionActivity{
		Prose: "**Progress**", Command: "echo '**Progress**'", CommandStatus: "running",
	}}
	lines := stripLinkLines(m.workingContextLines(c, 60, 8, false))
	if lines[0] != "Progress" || lines[len(lines)-1] != c.Activity.Command {
		t.Fatal(lines)
	}
}

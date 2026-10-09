package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorDetailStructuredSections(t *testing.T) {
	m := approvalTestModel()
	c := &m.monitorSessionData[0].preview
	c.CommandDetails = codex.ApprovalCommandDetails{
		Justification: "Check the repository.\nCommand: this is just explanatory prose.",
		Command:       "printf 'Directory: not metadata'\nprintf ok",
		Directory:     "/work/project",
	}
	c.ApprovalOptions[4].Detail = `["printf"]`
	c.ApprovalDecisions = "accept, acceptForSession, cancel"
	before := *c
	for _, width := range []int{1, 20, 80, 120} {
		doc := m.contextDetailDocument(width)
		for _, line := range doc {
			if lipgloss.Width(line.text) > width {
				t.Fatalf("line overflow at %d: %q", width, line.text)
			}
		}
		if width < 80 {
			continue
		}
		var command []string
		for _, line := range doc {
			if line.kind == "command" {
				command = append(command, strings.TrimPrefix(line.text, "│ "))
			}
		}
		if strings.Join(command, "\n") != c.CommandDetails.Command {
			t.Fatal("command text/line breaks changed", command)
		}
		plain := strings.Join(m.contextDetailLines(width), "\n")
		for _, label := range []string{i18n.Text("JUSTIFICATION"), i18n.Text("COMMAND"), i18n.Text("WORKING DIRECTORY"), i18n.Text("PERSISTENT PERMISSION RULE"), i18n.Text("OFFERED DECISIONS")} {
			if !strings.Contains(plain, "\n\n"+label) {
				t.Fatal("section lacks separation", label, plain)
			}
		}
		if !strings.Contains(plain, c.CommandDetails.Justification) {
			t.Fatal("justification split by untrusted label")
		}
	}
	if *c != before {
		t.Fatal("formatting changed request or capability")
	}
	for theme := themeHacker; theme < themeCount; theme++ {
		colors := paletteFor(theme)
		meta := (detailLine{"same", "metadata"}).render(colors)
		head := (detailLine{"same", "heading"}).render(colors)
		if meta == head || ansi.Strip(meta) != "same" || ansi.Strip(head) != "same" {
			t.Fatal("section styles indistinguishable")
		}
	}
}

func TestApprovalCommentaryFullDetailOnly(t *testing.T) {
	m := approvalTestModel()
	c := &m.monitorSessionData[0].preview
	c.ApprovalContext = "The preceding explanation"
	c.CommandDetails = codex.ApprovalCommandDetails{Justification: "Allow the check?", Command: "git status", Directory: "/work"}
	before := *c
	for _, width := range []int{1, 20, 80} {
		for _, line := range m.contextDetailDocument(width) {
			if lipgloss.Width(line.text) > width {
				t.Fatal("context overflow", line)
			}
		}
	}
	text := strings.Join(m.contextDetailLines(100), "\n")
	contextAt, reasonAt := strings.Index(text, c.ApprovalContext), strings.Index(text, c.CommandDetails.Justification)
	if contextAt < 0 || reasonAt <= contextAt || !strings.Contains(text, "\n\n"+i18n.Text("CONTEXT")) {
		t.Fatal(text)
	}
	if *c != before {
		t.Fatal("rendering changed approval")
	}
	colors := paletteFor(m.theme)
	if text := ansi.Strip(m.renderExpandedContext(100, 30, m.monitorSessionData[0], colors)); strings.Contains(text, c.ApprovalContext) {
		t.Fatal("context leaked into row")
	}
}

func TestApprovalDetailKeepsCommandWhitespaceAndControls(t *testing.T) {
	m := approvalTestModel()
	c := &m.monitorSessionData[0].preview
	c.CommandDetails = codex.ApprovalCommandDetails{Command: "\n  printf 'a\tb'  \r\n\n", Directory: "/work"}
	before := *c
	var command []string
	for _, line := range m.contextDetailDocument(120) {
		if line.kind == "command" {
			command = append(command, strings.TrimPrefix(line.text, "│ "))
		}
	}
	if got := strings.Join(command, "\n"); got != "\n  printf 'a    b'  \n\n" {
		t.Fatalf("command formatting lost: %q", got)
	}
	g := m.monitorDashboardLayout()
	if !m.monitorApprovalControls(g.contentWidth, g.meterHeight) {
		t.Fatal("whitespace disabled approval controls")
	}
	if *c != before {
		t.Fatal("display formatting changed the original approval")
	}
}

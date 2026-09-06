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

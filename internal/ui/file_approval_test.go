package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/merefield/codexometer/internal/codex"
)

func TestFileApprovalDetailAndInlineFit(t *testing.T) {
	m := approvalTestModel()
	c := &m.monitorSessionData[0].preview
	c.CommandDetails = codex.ApprovalCommandDetails{}
	c.Text = "Review these changes"
	c.FileChanges = `[{"path":"/work/a.go","kind":{"type":"update"},"diff":"@@ -4 +4 @@\n-before\n+after\n"}]`
	for _, width := range []int{1, 20, 80, 120} {
		doc := m.contextDetailDocument(width)
		add, remove := false, false
		for _, line := range doc {
			if lipgloss.Width(line.text) > width {
				t.Fatal("overflow", width, line)
			}
			add = add || line.kind == "addition"
			remove = remove || line.kind == "removal"
		}
		if !add || !remove {
			t.Fatal("missing coloured diffs")
		}
	}
	text := strings.Join(m.contextDetailLines(100), "\n")
	if !strings.Contains(text, "/work/a.go") || !strings.Contains(text, "    4       │ -before") || !strings.Contains(text, "          4 │ +after") {
		t.Fatal(text)
	}
	if len(m.expandedApprovalButtons(80, 6, m.monitorSessionData[0])) != 0 {
		t.Fatal("inline approval offered without room for full patch")
	}
}

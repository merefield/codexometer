package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestWorkingContextResponsive(t *testing.T) {
	c := codex.SessionContext{Kind: codex.SessionContextActivity, Text: "Checking the build", Activity: codex.SessionActivity{
		Prose: "Checking the build", Command: "go test ./...", CommandStatus: "running", RunningCommands: 2,
	}}
	for _, width := range []int{1, 5, 20, 80} {
		for rows := 1; rows <= 12; rows++ {
			for _, compact := range []bool{true, false} {
				lines := workingContextLines(c, width, rows, compact)
				if len(lines) > rows {
					t.Fatalf("height overflow: %v", lines)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > width {
						t.Fatalf("width overflow: %q", line)
					}
				}
				if width == 80 && rows >= 4 {
					text := strings.Join(lines, "\n")
					for _, want := range []string{c.Activity.Prose, c.Activity.Command, "+1 RUNNING"} {
						if !strings.Contains(text, want) {
							t.Fatal(text, want)
						}
					}
				}
			}
		}
	}
	m := approvalTestModel()
	m.monitorSessionData[0].preview = c
	for _, width := range []int{1, 20, 80} {
		doc := m.contextDetailDocument(width)
		var text strings.Builder
		for _, line := range doc {
			if ansi.StringWidth(line.text) > width {
				t.Fatal("detail overflow", line)
			}
			text.WriteString(line.text + "\n")
		}
		if width == 80 && (!strings.Contains(text.String(), "COMMENTARY") || !strings.Contains(text.String(), "COMMAND // RUNNING")) {
			t.Fatal(text.String())
		}
	}
	c.Kind = codex.SessionContextApproval
	if hasWorkingCommand(c) {
		t.Fatal("working command overrides approval")
	}
	c.Kind = codex.SessionContextReply
	if hasWorkingCommand(c) {
		t.Fatal("working command overrides final reply")
	}
}

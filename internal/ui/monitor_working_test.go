package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestWorkingContextResponsive(t *testing.T) {
	m := approvalTestModel()
	c := codex.SessionContext{Kind: codex.SessionContextActivity, Text: "Checking the build", Activity: codex.SessionActivity{
		Prose: "Checking the build", Command: "go test ./...", CommandStatus: "running", RunningCommands: 2,
	}}
	for _, width := range []int{1, 5, 20, 80} {
		for rows := 1; rows <= 12; rows++ {
			for _, compact := range []bool{true, false} {
				lines := m.workingContextLines(c, width, rows, compact)
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

func TestWorkingCommandRecordedDurationRemainsResponsive(t *testing.T) {
	m := contextTestModel()
	start := time.Now().Add(-time.Minute)
	a := codex.SessionActivity{Command: "go test", CommandStatus: "completed", CommandStartedAt: start, CommandCompletedAt: start.Add(7 * time.Second)}
	if got := m.workingCommandTitle(a); got != "COMMAND // COMPLETED // 7s" {
		t.Fatal("recorded duration missing", got)
	}
	c := codex.SessionContext{Kind: codex.SessionContextActivity, Activity: a}
	for _, width := range []int{1, 8, 20, 80} {
		for _, compact := range []bool{false, true} {
			lines := m.workingContextLines(c, width, 4, compact)
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatal("duration caused layout overflow", line)
				}
			}
			if width == 80 && !strings.Contains(strings.Join(lines, "\n"), "COMPLETED // 7s") {
				t.Fatal(lines)
			}
		}
	}
}

func TestWorkingCommandTimingRespectsTelemetryFreshnessInEveryView(t *testing.T) {
	for _, tc := range []struct {
		name, observationError, commandStatus string
		state                                 monitorState
		wantTiming                            bool
	}{
		{"live", "", "running", monitorRunning, true},
		{"failed observation", "telemetry unavailable", "running", monitorRunning, false},
		{"starting", "", "running", monitorStarting, false},
		{"resetting", "", "running", monitorResetting, false},
		{"idle", "", "running", monitorIdle, false},
		{"completed with stale telemetry", "telemetry unavailable", "completed", monitorRunning, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := contextTestModel()
			m.monitorState, m.monitorError = tc.state, tc.observationError
			start := time.Now().Add(-time.Minute)
			s := m.monitorSessionData[0]
			s.preview = codex.SessionContext{Kind: codex.SessionContextActivity, Text: "Checking", Activity: codex.SessionActivity{
				Prose: "Checking", Command: "go test", CommandStatus: tc.commandStatus,
				CommandStartedAt: start, CommandCompletedAt: start.Add(7 * time.Second),
			}}
			m.monitorSessionData[0] = s
			colors := paletteFor(m.theme)
			m.setRowContext(s.id, contextSplit)
			compact := m.renderMonitorContextRow(240, 15, "", s, colors)
			m.setRowContext(s.id, contextWide)
			wide := m.renderExpandedContext(120, 15, s, colors)
			m.setRowContext(s.id, contextFull)
			full := strings.Join(m.contextDetailLines(120), "\n")
			for view, text := range map[string]string{"compact": compact, "wide": wide, "full": full} {
				text = ansi.Strip(text)
				heading := "COMMAND // " + strings.ToUpper(tc.commandStatus)
				if !strings.Contains(text, heading) {
					t.Fatalf("%s lost retained command context: %s", view, text)
				}
				if strings.Contains(text, heading+" //") != tc.wantTiming {
					t.Fatalf("%s: timing freshness mismatch (want timing=%v): %s", view, tc.wantTiming, text)
				}
				if tc.commandStatus == "completed" && !strings.Contains(text, heading+" // 7s") {
					t.Fatalf("%s lost recorded completed duration: %s", view, text)
				}
			}
		})
	}
}

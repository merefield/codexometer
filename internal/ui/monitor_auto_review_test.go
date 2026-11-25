package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorNativeReviewPreservesConversationAndHumanApproval(t *testing.T) {
	m := contextTestModel()
	s := &m.monitorSessionData[0]
	s.working, s.active, s.attention = true, true, codex.SessionAttentionNone
	s.preview.Reviewer = "auto_review"
	s.preview.Kind = codex.SessionContextActivity
	s.preview.AutoReview = codex.SessionAutoReview{ID: "review", Status: "inProgress", Action: "ls /work", ReviewFinding: codex.ReviewFinding{Rationale: "Inspect repository", Risk: "low", Authorization: "high"}}
	m.monitorContextDetail = s.id
	badge := ansi.Strip(m.renderMonitorSessionBadge(*s, 80, paletteFor(m.theme)))
	if !strings.Contains(badge, i18n.Text("AUTO REVIEWING")) {
		t.Fatal(badge)
	}
	if strings.Contains(badge, "APPROVAL") {
		t.Fatal("native reviewer became human request")
	}
	doc := strings.Join(m.contextDetailLines(120), "\n")
	for _, text := range []string{s.preview.Text, "● AUTO REVIEWING", "ls /work", "Inspect repository", "RISK: LOW", "USER AUTHORISATION: HIGH"} {
		if !strings.Contains(doc, text) {
			t.Fatalf("lost context/review %q: %s", text, doc)
		}
	}
	s.attention = codex.SessionAttentionApproval
	badge = ansi.Strip(m.renderMonitorSessionBadge(*s, 80, paletteFor(m.theme)))
	if !strings.Contains(badge, monitorSessionAttentionLabel(*s)) || strings.Contains(badge, "AUTO REVIEWING") {
		t.Fatal("native review hid human checkpoint", badge)
	}
	metrics := ansi.Strip(m.renderMonitorSessionMetrics(48, 16, *s, "", paletteFor(m.theme)))
	if !strings.Contains(metrics, i18n.Text("DEFAULT REVIEW // AUTO")) {
		t.Fatal("missing reviewer metadata", metrics)
	}
}

func TestAssessmentLayoutWrapAndCommandDeduplication(t *testing.T) {
	r := &codex.ReviewAssessment{Title: "COMMAND REVIEW", Decision: "AUTO-APPROVED", Status: "approved", Subject: "git status", Attached: true, ReviewFinding: codex.ReviewFinding{Risk: "low", Authorization: "high", Rationale: "Read-only inspection."}}
	wide := assessmentLines(r, 100, true)
	if len(wide) != 2 || !strings.Contains(wide[0].text, "● AUTO-APPROVED   RISK: LOW") || wide[1].text != r.Rationale {
		t.Fatal(wide)
	}
	for _, width := range []int{1, 12, 24} {
		for _, line := range assessmentLines(r, width, true) {
			if ansi.StringWidth(line.text) > width {
				t.Fatal(width, line)
			}
		}
	}
	compact := assessmentLines(r, 90, false)
	if len(compact) != 1 || strings.Contains(compact[0].text, "AUTHORISATION") {
		t.Fatal(compact)
	}
	r.Attached = false
	if got := assessmentLines(r, 90, true)[0].text; got != "git status" {
		t.Fatal(got)
	}
}

func TestReviewGapUsesOnlySpareRows(t *testing.T) {
	command := detailLine{"last wrapped command line", "command"}
	review := detailLine{"COMMAND REVIEW", "assessment:approved"}
	note := detailLine{"reason", "assessment-note"}
	for _, tc := range []struct {
		name    string
		lines   []detailLine
		follows bool
		gap     int
	}{
		{"full after wrapped command", []detailLine{{"first line", "command"}, command, review, note}, false, 2},
		{"preview with attached command", []detailLine{review}, true, 0},
		{"preview with own subject", []detailLine{command, review}, false, 1},
		{"no visible command", []detailLine{review, note}, false, -1},
		{"no assessment", []detailLine{command, note}, false, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := spaceCommandReview(slices.Clone(tc.lines), len(tc.lines), tc.follows); !slices.Equal(got, tc.lines) {
				t.Fatal("spacing displaced content", got)
			}
			want := slices.Clone(tc.lines)
			if tc.gap >= 0 {
				want = slices.Insert(want, tc.gap, detailLine{})
			}
			if got := spaceCommandReview(slices.Clone(tc.lines), len(tc.lines)+4, tc.follows); !slices.Equal(got, want) {
				t.Fatal("expected exactly one conditional gap", got)
			}
		})
	}
}

func TestReviewOnlyColoursTheStatusDot(t *testing.T) {
	p := paletteFor(themeRust)
	line := detailLine{"● AUTO-APPROVED", "assessment:approved"}
	plain := lipgloss.NewStyle()
	want := plain.Foreground(p.success).Render("●") + plain.Foreground(p.accent).Render(" AUTO-APPROVED")
	if got := line.render(p); got != want {
		t.Fatalf("review verdict should be ordinary text with a coloured dot: %q", got)
	}
	note := detailLine{"reason", "assessment-note"}
	if got := note.render(p); got != plain.Foreground(blendColors(p.dim, p.accent)).Render("reason") {
		t.Fatalf("rationale should use the intermediate tone: %q", got)
	}
}

func TestNativeDenialConsentUsesScrollableReview(t *testing.T) {
	m, f := commandTestModel(t)
	m.monitorCommands.inline = true
	m.monitorCommands.menu.Path = "approve/denial"
	m.monitorCommands.menu.Choices[0].Help = strings.Repeat("Review this action carefully.\n", 100) + "FINAL RATIONALE SENTINEL"
	m.monitorPrompt.input.Focus()
	next, cmd, _ := m.chooseMonitorCommand()
	if cmd != nil || f.calls != 0 || next.monitorCommands.inline || !next.monitorCommands.detail || next.monitorPrompt.input.Focused() {
		t.Fatal("retry consent retained truncated inline review or sent early")
	}
	var visible strings.Builder
	for i := 0; i < 20; i++ {
		for _, row := range next.monitorCommandRows(80, 20) {
			visible.WriteString(row.text)
			visible.WriteByte('\n')
		}
		next, cmd, _ = next.updateMonitorCommands(tea.KeyPressMsg{Code: tea.KeyPgDown})
		if cmd != nil {
			t.Fatal("scrolling sent a retry")
		}
	}
	if !strings.Contains(visible.String(), "FINAL RATIONALE SENTINEL") {
		t.Fatal("complete retry rationale cannot be reached by scrolling")
	}
}

func TestNativeDenialNoConfirmStillRequiresInspectThenApply(t *testing.T) {
	m, f := commandTestModel(t)
	m.SetConfirmationMode(confirmation.None)
	m.monitorCommands.inline = true
	m.monitorCommands.menu.Path = "approve/denial"
	m.monitorPrompt.input.Focus()
	m, cmd, _ := m.chooseMonitorCommand()
	if cmd != nil || f.calls != 0 || !m.monitorCommands.detail || m.monitorCommands.inline {
		t.Fatal("no-confirm skipped denial inspection")
	}
	visible := false
	for _, b := range m.commandFooter(120) {
		visible = visible || b.action == "confirm"
	}
	if !visible {
		t.Fatal("no explicit Apply control on inspected denial")
	}
	m, cmd, _ = m.updateMonitorCommands(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("second explicit Enter did not send inspected consent")
	}
	_, _ = m.Update(cmd())
	if f.calls != 1 {
		t.Fatal("inspected retry consent not sent exactly once")
	}
}

func TestInlinePermissionsQuotedProfileUsesNativeParser(t *testing.T) {
	for _, query := range []string{"':workspace'", ":workspace", "Reviewer"} {
		m, _ := commandTestModel(t)
		p := &m.monitorCommands
		p.inline = true
		p.prefix = "/permissions "
		p.menu.Path = "permissions"
		p.menu.Choices = []codex.SessionCommandChoice{{Label: ":workspace", Action: true}, {Label: "Reviewer", Next: "permissions/reviewer"}}
		m.monitorPrompt.input.SetValue(p.prefix + query)
		m.monitorPrompt.input.Focus()
		indices := m.commandChoiceIndices()
		p.selected = -1
		if len(indices) > 0 {
			p.selected = indices[0]
		}
		next, cmd, _ := m.updateMonitorCommands(tea.KeyPressMsg{Code: tea.KeyEnter})
		if query == "':workspace'" {
			if cmd == nil || next.monitorCommands.menu.Path != "permissions "+query {
				t.Fatal("quoted profile did not reach native parser")
			}
		} else if query == ":workspace" {
			if cmd != nil || !next.monitorCommands.detail {
				t.Fatal("matching profile selection changed")
			}
		} else if cmd == nil || next.monitorCommands.menu.Path != "permissions/reviewer" {
			t.Fatal("matching submenu selection changed")
		}
	}
}

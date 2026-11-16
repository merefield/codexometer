package ui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

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
	s.preview.AutoReview = codex.SessionAutoReview{ID: "review", Status: "inProgress", Action: "ls /work", Rationale: "Inspect repository", Risk: "low", Authorization: "high"}
	m.monitorContextDetail = s.id
	badge := ansi.Strip(m.renderMonitorSessionBadge(*s, 80, paletteFor(m.theme)))
	if !strings.Contains(badge, i18n.Text("AUTO REVIEWING")) {
		t.Fatal(badge)
	}
	if strings.Contains(badge, "APPROVAL") {
		t.Fatal("native reviewer became human request")
	}
	doc := strings.Join(m.contextDetailLines(120), "\n")
	for _, text := range []string{s.preview.Text, "AUTO REVIEW", "ls /work", "Inspect repository"} {
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
	if !strings.Contains(metrics, i18n.Text("REVIEW // AUTO")) {
		t.Fatal("missing reviewer metadata", metrics)
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

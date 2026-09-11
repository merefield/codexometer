package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func inlinePromptTestModel() (Model, *promptTestClient) {
	m, c := promptTestModel()
	m.width, m.height = 160, 40
	m.monitorSessionData = m.monitorSessionData[:1]
	m.monitorSessionData[0].preview.Text = "Finished the task."
	m.setRowContext("root-one", contextWide)
	return m, c
}

func TestInlineMonitorPromptClicksAndSend(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		m, c := inlinePromptTestModel()
		m.width = width
		w, h := m.monitorPromptSize()
		rows := m.monitorPromptRows(w, h)
		if rows != 3 || !strings.Contains(ansi.Strip(m.render()), i18n.Text("FOLLOW-UP")) {
			t.Fatalf("missing inline composer at width %d", width)
		}
		g := m.dashboardLayout()
		a := layoutMonitorArea(g.contentWidth, g.meterHeight)
		mw, _, _ := monitorSessionColumnWidths(a.width)
		_, _, cy := monitorContextBodyLayout(h, rows)
		y := g.meterY + a.topHeight + a.gap - 1 + cy + 1
		for x := mw + 5; x < mw+3+w-2; x++ {
			if got := m.monitorContextAt(x, y); got != "prompt" {
				t.Fatalf("width %d click %d,%d = %q", width, x, y, got)
			}
		}
		next, _ := m.Update(tea.MouseClickMsg{X: mw + 5, Y: y, Button: tea.MouseLeft})
		m = next.(Model)
		if !m.monitorPrompt.input.Focused() || m.monitorContextDetail != "" {
			t.Fatal("composer click navigated instead of focusing")
		}
		m, _ = promptKey(m, 't', "t")
		m, cmd := promptKey(m, tea.KeyEnter, "")
		if cmd == nil || !m.monitorPrompt.busy {
			t.Fatal("inline Enter did not send")
		}
		next, _ = m.Update(cmd())
		m = next.(Model)
		if len(c.answers) != 1 || c.answers[0] != "t" || m.monitorPrompt.input.Focused() {
			t.Fatal("inline send destination/content or focus incorrect")
		}
	}
}

func TestInlineMonitorPromptEligibilityAndResize(t *testing.T) {
	m, c := inlinePromptTestModel()
	w, h := m.monitorPromptSize()
	m.monitorSelectedID = "another"
	if m.monitorPromptRows(w, h) != 0 || m.monitorPromptOffer().Token != "" {
		t.Fatal("composer available for unselected row")
	}
	m.monitorSelectedID = "root-one"
	m.monitorSessionData[0].preview.Kind = codex.SessionContextApproval
	if m.monitorPromptRows(w, h) != 0 {
		t.Fatal("composer overlaps approval")
	}
	m.monitorSessionData[0].preview.Kind = codex.SessionContextReply
	m.monitorSessionData[0].preview.Text = strings.Repeat("context\n", 100)
	if m.monitorPromptRows(w, h) != 0 {
		t.Fatal("composer hides source context")
	}
	m.monitorSessionData[0].preview.Text = "Finished the task."
	m, _ = promptKey(m, tea.KeyEnter, "")
	if !m.monitorPrompt.input.Focused() {
		t.Fatal("Enter did not focus visible inline composer")
	}
	next, _ := m.Update(tea.PasteMsg{Content: strings.Repeat("long draft ", 100)})
	m = next.(Model)
	rows := m.monitorPromptRows(w, h)
	textRows, _, _ := monitorContextBodyLayout(h, rows)
	if rows <= 3 || len(expandedContextLines(w, m.monitorSessionData[0])) > textRows {
		t.Fatal("draft did not wrap within spare space")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	m = next.(Model)
	w, h = m.monitorPromptSize()
	if m.monitorPrompt.input.Focused() || m.monitorPromptRows(w, h) != 0 {
		t.Fatal("hidden composer retained focus after resize")
	}
	m, cmd := promptKey(m, tea.KeyEnter, "")
	if cmd != nil || len(c.answers) != 0 {
		t.Fatal("hidden composer submitted")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	m, _ = promptKey(m, tea.KeyEnter, "")
	if !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() == "" {
		t.Fatal("draft did not survive resize")
	}
	m, _ = promptKey(m, tea.KeyEscape, "")
	if m.monitorPrompt.input.Focused() || m.rowContextMode("root-one") != contextWide {
		t.Fatal("Esc navigated instead of releasing editor focus")
	}
	c.offer.Token = "replacement"
	m, _ = promptKey(m, tea.KeyEnter, "")
	if len(c.answers) != 0 {
		t.Fatal("stale offer submitted")
	}
}

func TestInlineMonitorPromptSingleOwner(t *testing.T) {
	m, c := promptTestModel()
	m.width, m.height = 180, 65
	m.monitorSessionData = m.monitorSessionData[:2]
	for i := range m.monitorSessionData {
		m.monitorSessionData[i].preview.Text = "Finished."
		m.setRowContext(m.monitorSessionData[i].id, contextWide)
	}
	id := m.monitorSessionData[1].id
	c.offer.ThreadID = id
	if got := strings.Count(ansi.Strip(m.render()), i18n.Text("FOLLOW-UP")); got != 1 {
		t.Fatalf("rendered %d composers, want only selected row", got)
	}
	m, _ = promptKey(m, tea.KeyEnter, "")
	if m.monitorPrompt.session != id || !m.monitorPrompt.input.Focused() {
		t.Fatal("composer targeted wrong session")
	}
	m, _ = promptKey(m, tea.KeyEscape, "")
	m.selectMonitorSession(-1)
	if m.monitorPromptOffer().Token != "" || m.monitorPrompt.input.Focused() {
		t.Fatal("selection change retained previous session's composer")
	}
}

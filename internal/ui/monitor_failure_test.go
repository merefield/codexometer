package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorFailureSummaryFilterPillAndContext(t *testing.T) {
	m := attentionTestModel()
	s := &m.monitorSessionData[2]
	s.attention, s.working = codex.SessionAttentionError, false
	s.preview = codex.SessionContext{Kind: codex.SessionContextFailure, FailureCode: "serverOverloaded", Text: "Selected model is at capacity.", ThreadID: s.id}
	if monitorSessionStatus(*s) != monitorFilterErrors || monitorSessionRecentlyActive(*s) {
		t.Fatal("failure filter/active state wrong")
	}
	if monitorSessionAttentionLabel(*s) != "MODEL BUSY" || contextTitle(s.preview) != "TURN ERROR" {
		t.Fatal("failure label missing")
	}
	items := m.monitorAttentionSessions()
	if len(items) != 3 || items[0].attention != codex.SessionAttentionApproval || items[1].attention != codex.SessionAttentionInput || items[2].attention != codex.SessionAttentionError {
		t.Fatal("failure pill priority", items)
	}
	buttons, _ := m.monitorAttentionButtons(300, 1)
	if len(buttons) != 3 || !strings.Contains(buttons[2].label, "MODEL BUSY") {
		t.Fatal("failure pill missing", buttons)
	}
	id := s.id
	m.setMonitorFilter(monitorFilterErrors)
	rows, _, _ := m.monitorSessionPage(100)
	if len(rows) != 1 || rows[0].id != id {
		t.Fatal("error filtering", rows)
	}
	m.setRowContext(id, contextFull)
	if text := ansi.Strip(m.render()); !strings.Contains(text, "MODEL BUSY") || !strings.Contains(text, "Selected model is at capacity.") {
		t.Fatal("detail omitted failure")
	}
}

package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorFailureSummaryFilterPillAndContext(t *testing.T) {
	for code, label := range map[string]string{
		"serverOverloaded": "MODEL BUSY", "modelNotFound": "MODEL UNAVAILABLE", "policyCapacityUnavailable": "CAPACITY RESTRICTED",
	} {
		t.Run(code, func(t *testing.T) {
			m := attentionTestModel()
			s := &m.monitorSessionData[2]
			s.attention, s.working = codex.SessionAttentionError, false
			s.preview = codex.SessionContext{Kind: codex.SessionContextFailure, FailureCode: code, Text: "Upstream failure explanation.", ThreadID: s.id}
			if monitorSessionStatus(*s) != monitorFilterErrors || monitorSessionRecentlyActive(*s) {
				t.Fatal("failure filter/active state wrong")
			}
			if monitorSessionAttentionLabel(*s) != label || contextTitle(s.preview) != "TURN ERROR" {
				t.Fatal("failure label missing")
			}
			items := m.monitorAttentionSessions()
			if len(items) != 3 || items[0].attention != codex.SessionAttentionApproval || items[1].attention != codex.SessionAttentionInput || items[2].attention != codex.SessionAttentionError {
				t.Fatal("failure pill priority", items)
			}
			buttons, _ := m.monitorAttentionButtons(300, 1)
			if len(buttons) != 3 || !strings.Contains(buttons[2].label, label) {
				t.Fatal("failure pill missing", buttons)
			}
			id := s.id
			m.setMonitorFilter(monitorFilterErrors)
			rows, _, _ := m.monitorSessionPage(100)
			if len(rows) != 1 || rows[0].id != id {
				t.Fatal("error filtering", rows)
			}
			m.setRowContext(id, contextFull)
			if text := ansi.Strip(m.render()); !strings.Contains(text, label) || !strings.Contains(text, "Upstream failure explanation.") {
				t.Fatal("detail omitted failure")
			}
		})
	}
}

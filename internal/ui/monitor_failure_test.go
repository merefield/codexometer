package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestMonitorFailureKeepsRecoveryComposerAndSlashCommands(t *testing.T) {
	for _, code := range []string{"serverOverloaded", "modelNotFound", "policyCapacityUnavailable", "httpConnectionFailed"} {
		for _, mode := range []int{contextFull, contextWide} {
			for _, text := range []string{"continue", "/model"} {
				m, f := suggestionTestModel(t)
				m.width, m.height = 160, 65
				for i := range m.monitorSessionData {
					s := &m.monitorSessionData[i]
					if s.id == "root-one" {
						s.attention, s.working = codex.SessionAttentionError, false
						s.preview = codex.SessionContext{Kind: codex.SessionContextFailure, FailureCode: code, ThreadID: s.id, Text: "Failed turn explanation."}
					}
				}
				m.setRowContext("root-one", mode)
				w, h := m.monitorPromptSize()
				if m.monitorPromptRows(w, h) == 0 || !strings.Contains(ansi.Strip(m.render()), "Failed turn explanation.") {
					t.Fatal("failure hid the recovery composer or error context", code, mode)
				}
				m.focusMonitorPrompt()
				m.monitorPrompt.input.SetValue(text)
				next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(Model)
				if cmd == nil || len(f.answers) != 0 || f.calls != 0 {
					t.Fatal("recovery was unavailable or wrote before explicit command execution", code, mode, text)
				}
				next, _ = m.Update(cmd())
				m = next.(Model)
				if text == "continue" {
					if len(f.answers) != 1 || f.answers[0] != "continue" || f.calls != 0 {
						t.Fatal("explicit retry was not sent as ordinary text")
					}
				} else if !m.monitorCommands.open || len(f.answers) != 0 || f.calls != 0 {
					t.Fatal("/model did not open recovery choices without starting a turn")
				}
			}
		}
	}
}

func TestMonitorFailureWithoutPromptCapabilityStillAllowsCommandDiscovery(t *testing.T) {
	m, f := commandTestModel(t)
	m.closeMonitorCommands()
	f.offer = codex.SessionPromptOffer{}
	m.monitorSessionData[0].attention = codex.SessionAttentionError
	m.monitorSessionData[0].preview.Kind = codex.SessionContextFailure
	w, h := m.monitorPromptSize()
	if m.monitorPromptRows(w, h) != 0 {
		t.Fatal("error label alone invented a prompt capability")
	}
	next, cmd := m.Update(key('/'))
	if !next.(Model).monitorCommands.open || cmd == nil || f.calls != 0 || len(f.answers) != 0 {
		t.Fatal("missing prompt capability blocked independent slash discovery")
	}
}

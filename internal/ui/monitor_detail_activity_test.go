package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestDetailSentWaveLifecycle(t *testing.T) {
	for _, notice := range []string{i18n.Text("Text sent ..."), i18n.Text("Decision sent ...")} {
		m := contextTestModel()
		m.monitorContextDetail = "root-one"
		m.recordDetailSent(notice)
		first := m.detailActivity()
		m.phase++
		if first == m.detailActivity() || !strings.HasPrefix(first, strings.TrimSuffix(notice, "...")) {
			t.Fatal("sent notice did not animate")
		}
		m.monitorSessionData[0].preview.Text = "new activity"
		if got := m.detailActivityAt(m.monitorDetailSent.visibleUntil.Add(-time.Nanosecond)); !strings.HasPrefix(got, strings.TrimSuffix(notice, "...")) {
			t.Fatal("fast update replaced the sent notice too soon")
		}
		if got := m.detailActivityAt(m.monitorDetailSent.visibleUntil); got != "·●·" {
			t.Fatalf("new context did not replace notice: %q", got)
		}
		for _, attention := range []codex.SessionAttention{codex.SessionAttentionComplete, codex.SessionAttentionInput, codex.SessionAttentionApproval, codex.SessionAttentionCheck} {
			m.monitorSessionData[0].attention = attention
			if m.detailActivity() != "" {
				t.Fatal("animation continued after stop/attention")
			}
		}
		m.monitorSessionData[0].attention = codex.SessionAttentionNone
		m.monitorError = "disconnected"
		if m.detailActivity() != "" {
			t.Fatal("stale telemetry animated")
		}
		m.monitorError = ""
		m.monitorSessionData[0].active = false
		if m.detailActivity() != "" {
			t.Fatal("inactive session animated")
		}
		m.monitorContextDetail = ""
		if m.detailActivity() != "" {
			t.Fatal("animation escaped detail")
		}
	}
}

func TestDetailWaveScrollReachesEnd(t *testing.T) {
	m := contextTestModel()
	m.monitorContextDetail = "root-one"
	m.monitorSessionData[0].preview.Text = strings.Repeat("activity\n", 100) + "FINAL LINE"
	m.scrollMonitorContext(10000)
	g := m.dashboardLayout()
	view := ansi.Strip(m.renderMonitorContextDetail(g.contentWidth, g.meterHeight, paletteFor(m.theme)))
	if !strings.Contains(view, "FINAL LINE") || !strings.Contains(view, "●··") {
		t.Fatal("activity row broke scrolling to the last line")
	}
}

func TestSuppressedActivityKeepsPlainAcknowledgement(t *testing.T) {
	t.Run("prompt footer", func(t *testing.T) {
		m, _ := promptTestModel()
		m.monitorPrompt.notice = i18n.Text("Text sent ...")
		m.recordDetailSent(m.monitorPrompt.notice)
		m.monitorState = monitorPaused
		layout := m.layoutDetailControls(100, 30)
		if layout.kind != "prompt" || !strings.Contains(ansi.Strip(layout.render(m, 100, 30, paletteFor(m.theme))), m.monitorPrompt.notice) {
			t.Fatal("prompt footer lost the successful acknowledgement")
		}
	})
	for _, text := range []string{i18n.Text("Text sent ..."), i18n.Text("Decision sent ...")} {
		for name, suppress := range map[string]func(*Model){
			"paused":   func(m *Model) { m.monitorState = monitorPaused },
			"error":    func(m *Model) { m.monitorError = "observation unavailable" },
			"inactive": func(m *Model) { m.monitorSessionData[0].active = false },
			"complete": func(m *Model) { m.monitorSessionData[0].attention = codex.SessionAttentionComplete },
			"input":    func(m *Model) { m.monitorSessionData[0].attention = codex.SessionAttentionInput },
			"approval": func(m *Model) { m.monitorSessionData[0].attention = codex.SessionAttentionApproval },
			"check":    func(m *Model) { m.monitorSessionData[0].attention = codex.SessionAttentionCheck },
		} {
			t.Run(text+"/"+name, func(t *testing.T) {
				m := contextTestModel()
				m.monitorContextDetail = "root-one"
				m.recordDetailSent(text)
				suppress(&m)
				if got := m.detailFeedback(); got != text {
					t.Fatalf("plain acknowledgement lost: %q", got)
				}
				if layout := m.layoutDetailControls(100, 30); layout.notice != text || layout.rows != 1 {
					t.Fatal("acknowledgement missing from layout")
				}
				m.monitorSessionData[0].preview.Text = "replacement context"
				if got := m.detailFeedbackAt(m.monitorDetailSent.visibleUntil); got != "" {
					t.Fatalf("expired acknowledgement leaked into new context: %q", got)
				}
				m.monitorContextDetail = "root-two"
				m.monitorState = monitorPaused
				if got := m.detailFeedback(); got != "" {
					t.Fatal("acknowledgement leaked into another session")
				}
			})
		}
	}
}

func TestDetailControlLayoutMatchesRender(t *testing.T) {
	prompt, _ := promptTestModel()
	activity := contextTestModel()
	activity.monitorContextDetail = "root-one"
	for _, m := range []Model{approvalTestModel(), prompt, activity} {
		for _, width := range []int{20, 40, 80, 120} {
			for _, height := range []int{6, 12, 30} {
				layout := m.layoutDetailControls(width, height)
				rendered := layout.render(m, width, height, paletteFor(m.theme))
				rows := 0
				if rendered != "" {
					rows = strings.Count(rendered, "\n") + 1
				}
				if rows != layout.rows {
					t.Fatalf("%s %dx%d: layout %d rows, rendered %d", layout.kind, width, height, layout.rows, rows)
				}
			}
		}
	}
}

func TestDetailSuccessResultStartsWave(t *testing.T) {
	m := approvalTestModel()
	m, _, _ = m.monitorApprovalAction("decision:0")
	m, cmd, _ := m.monitorApprovalAction("decision:0")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !strings.HasPrefix(m.detailActivity(), strings.TrimSuffix(i18n.Text("Decision sent ..."), "...")) {
		t.Fatal("decision result did not start the wave")
	}
	p, _ := promptTestModel()
	p.focusMonitorPrompt()
	p.monitorPrompt.input.SetValue("next task")
	p, cmd, _ = p.submitMonitorPrompt()
	if cmd == nil {
		t.Fatal("missing send command")
	}
	next, _ = p.Update(cmd())
	p = next.(Model)
	if !strings.HasPrefix(p.detailActivity(), strings.TrimSuffix(i18n.Text("Text sent ..."), "...")) {
		t.Fatal("text result did not start the wave")
	}
}

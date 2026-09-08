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

func TestMainSessionContextDots(t *testing.T) {
	m := contextTestModel()
	s := m.monitorSessionData[0]
	idle := s
	idle.working = false
	if m.sessionActivityDots(idle) != "" {
		t.Fatal("recently active but idle session animated")
	}
	colors := paletteFor(m.theme)
	for _, expanded := range []bool{false, true} {
		if expanded {
			m.monitorContextExpanded = s.id
		}
		for _, size := range [][2]int{{80, 5}, {120, 8}, {200, 12}} {
			render := func() string { return ansi.Strip(m.renderMonitorContextRow(size[0], size[1], "", s, colors)) }
			m.phase = 0
			lines := strings.Split(render(), "\n")
			if !strings.Contains(lines[len(lines)-2], "●··") {
				t.Fatalf("expanded=%v %v: dots not on bottom body row: %q", expanded, size, lines)
			}
			m.phase = 1
			if !strings.Contains(render(), "·●·") {
				t.Fatal("row dots did not animate")
			}
			for _, attention := range []codex.SessionAttention{codex.SessionAttentionComplete, codex.SessionAttentionInput, codex.SessionAttentionApproval, codex.SessionAttentionCheck} {
				s.attention = attention
				if strings.Contains(render(), "·●·") {
					t.Fatal("stopped session still animated")
				}
			}
			s.attention = codex.SessionAttentionNone
			m.monitorError = "unavailable"
			if strings.Contains(render(), "·●·") {
				t.Fatal("observation error still animated")
			}
			m.monitorError = ""
		}
	}
	other := m.monitorSessionData[1]
	other.attention = codex.SessionAttentionComplete
	if m.sessionActivityDots(s) == "" || m.sessionActivityDots(other) != "" {
		t.Fatal("dots not session-specific")
	}
	m.monitorState = monitorPaused
	if m.sessionActivityDots(s) != "" {
		t.Fatal("paused monitor animated")
	}
	m.monitorState = monitorRunning
	m.monitorContextHidden = true
	if m.sessionActivityDots(s) != "" {
		t.Fatal("hidden context animated")
	}
}

func TestWorkingSignalReachesMonitor(t *testing.T) {
	m := contextTestModel()
	now := time.Now()
	u := codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "root-one", Active: true, Working: true}}}
	m.startMonitorSessions(u, now)
	if !m.monitorSessionData[0].working {
		t.Fatal("start dropped working state")
	}
	u.Sessions[0].Working = false
	m.syncMonitorSessions(u, now)
	if m.monitorSessionData[0].working {
		t.Fatal("idle update retained working state")
	}
	u.Sessions[0].Working = true
	m.resumeMonitorSessions(u, now, time.Minute)
	if !m.monitorSessionData[0].working {
		t.Fatal("resume dropped working state")
	}
	m.syncMonitorSessions(codex.LiveUsageSnapshot{}, now)
	if m.monitorSessionData[0].working {
		t.Fatal("missing session retained working state")
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
			"idle":     func(m *Model) { m.monitorSessionData[0].working = false },
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

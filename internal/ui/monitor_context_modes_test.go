package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorContextThreeStageCycleAndTarget(t *testing.T) {
	m := contextTestModel()
	m.monitorSessionData[1].attention = codex.SessionAttentionApproval
	m.monitorSessionData[2].attention = codex.SessionAttentionApproval
	m.monitorSessionData[2].preview.At = time.Now().Add(time.Minute)
	step := func(k tea.KeyPressMsg) { updated, _ := m.Update(k); m = updated.(Model) }
	step(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.monitorContextExpanded != "root-three" || m.monitorContextDetail != "" {
		t.Fatal("did not choose newest approval")
	}
	m.monitorSessionData[1].preview.At = time.Now().Add(time.Hour)
	step(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.monitorContextDetail != "root-three" || m.monitorContextExpanded != "" {
		t.Fatal("target changed on new approval")
	}
	step(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.monitorContextExpanded != "root-three" {
		t.Fatal("escape did not step back to expanded")
	}
	step(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.monitorContextTarget() != "" {
		t.Fatal("escape did not collapse")
	}
	m.monitorSelectedID = "root-one"
	m.monitorSessionData[0].preview = codex.SessionContext{}
	step(tea.KeyPressMsg{Code: tea.KeyRight})
	step(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.monitorContextDetail != "root-one" {
		t.Fatal("explicit selection ignored because text was missing")
	}
	step(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.monitorContextExpanded != "root-one" || m.monitorContextDetail != "" {
		t.Fatal("full detail did not step back to inline")
	}
	step(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.monitorContextTarget() != "" {
		t.Fatal("detail did not cycle to compact")
	}
}

func TestMonitorStatusAndContentHeadings(t *testing.T) {
	for _, tc := range []struct {
		name            string
		state           monitorState
		active, working bool
		attention       codex.SessionAttention
		err             string
	}{
		{"working", monitorRunning, true, true, codex.SessionAttentionNone, ""},
		{"recent only", monitorRunning, true, false, codex.SessionAttentionNone, ""},
		{"inactive", monitorRunning, false, true, codex.SessionAttentionNone, ""},
		{"paused", monitorPaused, true, true, codex.SessionAttentionNone, ""},
		{"starting", monitorStarting, true, true, codex.SessionAttentionNone, ""},
		{"pausing", monitorPausing, true, true, codex.SessionAttentionNone, ""},
		{"resuming", monitorResuming, true, true, codex.SessionAttentionNone, ""},
		{"resetting", monitorResetting, true, true, codex.SessionAttentionNone, ""},
		{"error", monitorRunning, true, true, codex.SessionAttentionNone, "offline"},
		{"complete", monitorRunning, true, true, codex.SessionAttentionComplete, ""},
		{"input", monitorRunning, true, true, codex.SessionAttentionInput, ""},
		{"approval", monitorRunning, true, true, codex.SessionAttentionApproval, ""},
		{"check", monitorRunning, true, true, codex.SessionAttentionCheck, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := contextTestModel()
			m.setRowContext("root-one", contextWide)
			m.monitorState, m.monitorError = tc.state, tc.err
			s := m.monitorSessionData[0]
			s.active, s.working, s.attention = tc.active, tc.working, tc.attention
			m.monitorSessionData[0] = s
			for _, height := range []int{3, 12} {
				left := ansi.Strip(m.renderMonitorSessionMetrics(80, height, s, "1/3", paletteFor(m.theme)))
				hasWorking := strings.Contains(left, i18n.Text("WORKING"))
				if hasWorking != (tc.name == "working") || hasWorking != (m.sessionActivityDots(s) != "") {
					t.Fatalf("working badge/dots disagree: %q", left)
				}
				if s.attention != codex.SessionAttentionNone && !strings.Contains(left, monitorSessionAttentionLabel(s)) {
					t.Fatal("attention badge lost priority")
				}
				m.phase = 1
				off := ansi.Strip(m.renderMonitorSessionMetrics(80, height, s, "1/3", paletteFor(m.theme)))
				m.phase = 2
				on := ansi.Strip(m.renderMonitorSessionMetrics(80, height, s, "1/3", paletteFor(m.theme)))
				m.phase = 0
				if tc.name == "working" {
					if off != strings.Replace(left, "●", " ", 1) || on != left || lipgloss.Width(off) != lipgloss.Width(left) {
						t.Fatal("working blink must change only the ball, retaining its space")
					}
				} else if off != left || on != left {
					t.Fatal("non-working badge should not blink")
				}
			}
			m.setRowContext(s.id, contextFull)
			for phase := 0; phase < 2; phase++ {
				m.phase = phase
				full := strings.Split(ansi.Strip(m.renderMonitorContextDetail(120, 20, paletteFor(m.theme))), "\n")
				badge := ansi.Strip(m.renderMonitorSessionBadge(s, 100, paletteFor(m.theme)))
				if badge == "" {
					badge = i18n.Text("SESSION CONTEXT")
				}
				if !strings.Contains(full[0], badge) {
					t.Fatalf("full title lost telemetry badge %q: %q", badge, full[0])
				}
				body := strings.Join(full[1:], "\n")
				if s.attention != codex.SessionAttentionNone && strings.Contains(body, monitorSessionAttentionLabel(s)) {
					t.Fatal("full detail repeats status in body")
				}
				if !strings.Contains(body, contextTitle(s.preview)) || !strings.Contains(body, s.preview.ThreadID) || !strings.Contains(body, s.preview.Source) {
					t.Fatal("full detail lost content heading or source metadata")
				}
			}
			m.phase = 0
			m.setRowContext(s.id, contextWide)
			for _, kind := range []codex.SessionContextKind{codex.SessionContextReply, codex.SessionContextActivity} {
				s.preview.Kind = kind
				out := strings.Split(ansi.Strip(m.renderExpandedContext(100, 10, s, paletteFor(m.theme))), "\n")
				if !strings.Contains(out[0], contextTitle(s.preview)) || strings.Contains(out[0], i18n.Text("WORKING")) {
					t.Fatalf("wide border should show content type and action: %q", out[0])
				}
				if strings.Contains(strings.Join(out[1:], "\n"), contextTitle(s.preview)) {
					t.Fatal("wide body repeats content type")
				}
				m.setRowContext(s.id, contextSplit)
				compact := strings.Split(ansi.Strip(m.renderMonitorContextRow(120, 10, "", s, paletteFor(m.theme))), "\n")
				if !strings.Contains(compact[0], contextTitle(s.preview)) {
					t.Fatal("compact title changed")
				}
				m.setRowContext(s.id, contextWide)
			}
		})
	}
}

func TestMonitorContextBackAndForthCycle(t *testing.T) {
	for _, back := range []tea.KeyPressMsg{{Code: tea.KeyLeft}, {Code: tea.KeyEscape}} {
		m := contextTestModel()
		m.monitorSelectedID = "root-two"
		m.monitorContextHidden = true
		for _, delta := range []int{-1, 1, 1, 1, 1, -1, -1, -1, -1} {
			k := tea.KeyPressMsg{Code: tea.KeyRight}
			if delta < 0 {
				k = back
			}
			before := m.rowContextMode("root-two")
			n, _ := m.Update(k)
			m = n.(Model)
			if got, want := m.rowContextMode("root-two"), min(max(before+delta, contextGraph), contextFull); got != want {
				t.Fatalf("got %d want %d", got, want)
			}
			if m.rowContextMode("root-one") != contextGraph || !m.monitorContextHidden {
				t.Fatal("changed another row")
			}
		}
	}
}

func TestMonitorRowModesIndependentAndGlobalToggle(t *testing.T) {
	m := contextTestModel()
	m.monitorContextHidden = true
	m.changeMonitorContext("root-one", 1)
	if m.rowContextMode("root-one") != contextSplit || m.rowContextMode("root-two") != contextGraph {
		t.Fatal("first row affected second row")
	}
	m.changeMonitorContext("root-one", 1)
	m.changeMonitorContext("root-two", 1)
	if m.rowContextMode("root-one") != contextWide || m.rowContextMode("root-two") != contextSplit {
		t.Fatal("switching rows discarded presentation")
	}
	m.changeMonitorContext("root-two", 1)
	if m.rowContextMode("root-one") != contextWide || m.rowContextMode("root-two") != contextWide {
		t.Fatal("cannot independently expand two rows")
	}
	m.toggleMonitorContext() // global Show overrides individual choices
	for _, s := range m.monitorSessionData {
		if m.rowContextMode(s.id) != contextSplit {
			t.Fatal("Show did not reset every row to split")
		}
	}
	m.toggleMonitorContext()
	for _, s := range m.monitorSessionData {
		if m.rowContextMode(s.id) != contextGraph {
			t.Fatal("Hide did not reset every row to graph")
		}
	}
	if len(m.monitorContextRows) != 0 || m.monitorContextTarget() != "" {
		t.Fatal("global reset retained row overrides")
	}
}

func monitorDetailPoint(m Model, id string, delta int) (int, int) {
	g := m.dashboardLayout()
	if m.monitorContextDetail != "" {
		x := 3
		if delta > 0 {
			x = 2 + g.contentWidth - 2
		}
		return x, g.meterY + 1
	}
	a := layoutMonitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	mw, rw, _ := monitorSessionColumnWidths(a.width)
	x := 2 + mw + 1
	if delta > 0 {
		x += rw - 1
	}
	y := g.meterY + a.topHeight + a.gap
	for i, s := range sessions {
		if s.id == id {
			return x, y
		}
		y += heights[i]
	}
	panic("session not on page")
}

func TestMonitorRowSurfaceCyclesOnlyClickedSession(t *testing.T) {
	for _, width := range []int{40, 80, 120, 200} {
		m := contextTestModel()
		m.width = width
		m.monitorContextHidden = true
		m.monitorSelectedID = "root-one"
		for _, delta := range []int{-1, 1, 1, 1, 1, -1, -1, -1, -1} {
			before := m.rowContextMode("root-two")
			x, y := monitorDetailPoint(m, "root-two", delta)
			n, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			m = n.(Model)
			if got, want := m.rowContextMode("root-two"), min(max(before+delta, contextGraph), contextFull); got != want {
				t.Fatalf("width %d got %d want %d", width, got, want)
			}
			if m.rowContextMode("root-one") != contextGraph || m.rowContextMode("root-three") != contextGraph {
				t.Fatal("changed other session")
			}
		}
	}
}

func TestIndependentRowLayoutsAndApprovalOwnership(t *testing.T) {
	m := approvalTestModel()
	m.setRowContext("root-one", contextWide)
	m.setRowContext("root-two", contextWide)
	x, y := monitorDetailPoint(m, "root-two", 1)
	if hit := m.monitorContextAt(x, y); hit != "more:root-two" {
		t.Fatalf("earlier wide row swallowed second row's detail click: %q", hit)
	}
	if len(m.expandedApprovalButtons(100, 20, m.monitorSessionData[0])) != 0 {
		t.Fatal("unselected row exposed another target's buttons")
	}
	m.monitorSelectedID = "root-one"
	m.selectMonitorSession(0)
	if len(m.expandedApprovalButtons(100, 20, m.monitorSessionData[0])) == 0 {
		t.Fatal("selected wide row lost approval controls")
	}
	for _, width := range []int{40, 80, 120, 200} {
		for _, mode := range []int{contextGraph, contextSplit, contextWide} {
			m.setRowContext("root-one", mode)
			m.width = width
			out := m.render()
			if lipgloss.Width(out) > width {
				t.Fatalf("mode %d overflowed width %d", mode, width)
			}
			_, cw, gw := m.contextColumns(width, m.monitorSessionData[0])
			if mode == contextGraph && (cw != 0 || gw == 0) {
				t.Fatal("graph-only layout contains detail")
			}
			if mode == contextSplit && width >= 80 && (cw == 0 || gw == 0) {
				t.Fatal("split layout missing a pane")
			}
		}
	}
}

func TestMonitorContextSelectionOverridesExpandedRow(t *testing.T) {
	m := contextTestModel()
	m.monitorSessionData[1].preview = codex.SessionContext{}
	m.changeMonitorContext("root-one", 1)
	m.selectMonitorSession(1)
	if m.monitorSelectedID != "root-two" || m.monitorContextExpanded != "" {
		t.Fatal("selection retained wrong target")
	}
	m.monitorContextExpanded = "root-one"
	for _, want := range []int{contextWide, contextFull} {
		n, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		m = n.(Model)
		if m.rowContextMode("root-two") != want {
			t.Fatal("right ignored empty selected session")
		}
	}
}

func TestExpandedContextResponsiveHitTargets(t *testing.T) {
	for _, size := range []struct{ w, h, n int }{{40, 16, 1}, {80, 24, 1}, {120, 40, 1}, {180, 45, 1}, {120, 30, 3}, {180, 40, 8}} {
		m := approvalTestModel()
		m.width = size.w
		m.height = size.h
		m.monitorContextDetail = ""
		m.monitorContextExpanded = ""
		m.monitorContextRows = nil
		m.monitorSessionData[0].preview.ThreadID = m.monitorSessionData[0].id
		for len(m.monitorSessionData) < size.n {
			s := m.monitorSessionData[1]
			s.id += "x" + strings.Repeat("x", len(m.monitorSessionData))
			m.monitorSessionData = append(m.monitorSessionData, s)
		}
		m.monitorSessionData = m.monitorSessionData[:size.n]
		if strings.Contains(ansi.Strip(m.render()), approvalShortcutLabel("accept", false, 0)) {
			t.Fatal("compact exposed approval")
		}
		m.changeMonitorContext(m.monitorSessionData[0].id, 1)
		for _, long := range []bool{false, true} {
			if long {
				m.monitorSessionData[0].preview.Text = strings.Repeat("long request\n", 200)
			}
			out := m.render()
			if lipgloss.Width(out) > size.w || lipgloss.Height(out) > size.h {
				t.Fatalf("expanded overflow %dx%d", size.w, size.h)
			}
			g := m.dashboardLayout()
			a := layoutMonitorArea(g.contentWidth, g.meterHeight)
			sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
			_, cw, _ := monitorSessionColumnWidths(a.width)
			var buttons []monitorApprovalButton
			for i, s := range sessions {
				if s.id == m.monitorContextExpanded {
					buttons = m.expandedApprovalButtons(cw, heights[i], s)
				}
			}
			if long && len(buttons) != 0 {
				t.Fatal("truncated inline request exposed decisions")
			}
			found := 0
			for y, line := range strings.Split(ansi.Strip(out), "\n") {
				for _, b := range buttons {
					pos := strings.Index(line, b.label)
					if pos < 0 {
						continue
					}
					found++
					x := lipgloss.Width(line[:pos])
					for dx := 0; dx < lipgloss.Width(b.label); dx++ {
						if got := m.monitorContextAt(x+dx, y); got != b.action {
							t.Fatalf("inline decision miss %dx%d %d,%d %q", size.w, size.h, x+dx, y, got)
						}
					}
					updated, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
					if b.action == "decision:0" && (cmd != nil || updated.(Model).monitorApprovalConfirm == "") {
						t.Fatal("inline grant skipped confirmation")
					}
				}
			}
			if found != len(buttons) {
				t.Fatal("rendered decision count mismatch")
			}
		}
	}
}

func TestMonitorExpandedSelectionDismissalAndConfirmation(t *testing.T) {
	m := approvalTestModel()
	m.monitorContextDetail = ""
	m.monitorContextExpanded = ""
	m.changeMonitorContext("root-one", 1)
	m, _, _ = m.monitorApprovalAction("decision:0")
	if m.monitorApprovalConfirm == "" {
		t.Fatal("confirmation missing")
	}
	m.changeMonitorContext("", -1)
	if m.monitorApprovalConfirm != "" {
		t.Fatal("confirmation survived mode transition")
	}
	m.stepBackMonitorContext()
	m.dismissMonitorSession("root-one")
	if m.monitorContextTarget() != "" {
		t.Fatal("dismissed row retained expansion")
	}
}

func TestMonitorObsoleteNavigationKeysDoNotChangeDetail(t *testing.T) {
	m := contextTestModel()
	for _, mode := range []int{contextGraph, contextSplit, contextWide, contextFull} {
		m.setRowContext("root-one", mode)
		for _, k := range []tea.KeyPressMsg{key('i'), {Code: tea.KeyEnter}} {
			n, _ := m.Update(k)
			m = n.(Model)
			if m.rowContextMode("root-one") != mode {
				t.Fatal("obsolete navigation key changed detail")
			}
		}
	}
}

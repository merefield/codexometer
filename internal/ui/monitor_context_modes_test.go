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
	step(key('i'))
	if m.monitorContextExpanded != "root-three" || m.monitorContextDetail != "" {
		t.Fatal("did not choose newest approval")
	}
	m.monitorSessionData[1].preview.At = time.Now().Add(time.Hour)
	step(key('i'))
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
	step(key('i'))
	step(key('i'))
	if m.monitorContextDetail != "root-one" {
		t.Fatal("explicit selection ignored because text was missing")
	}
	step(key('i'))
	if m.monitorContextExpanded != "root-one" || m.monitorContextDetail != "" {
		t.Fatal("full detail did not step back to inline")
	}
	step(key('i'))
	if m.monitorContextTarget() != "" {
		t.Fatal("detail did not cycle to compact")
	}
}

func TestMonitorContextBackAndForthCycle(t *testing.T) {
	for _, back := range []tea.KeyPressMsg{key('i'), {Code: tea.KeyEscape}} {
		m := contextTestModel()
		m.monitorSelectedID = "root-two"
		m.monitorContextHidden = true
		step := func(k tea.KeyPressMsg) { n, _ := m.Update(k); m = n.(Model) }
		for cycle := 0; cycle < 2; cycle++ {
			for _, mode := range []int{contextSplit, contextWide, contextFull, contextWide, contextSplit, contextGraph} {
				if m.monitorContextDetail != "" {
					step(back)
				} else {
					step(key('i'))
				}
				if got := m.rowContextMode("root-two"); got != mode {
					t.Fatalf("row mode %d, want %d", got, mode)
				}
				if m.rowContextMode("root-one") != contextGraph || !m.monitorContextHidden {
					t.Fatal("per-row cycle changed another row/global default")
				}
			}
		}
	}
}

func TestMonitorRowModesIndependentAndGlobalToggle(t *testing.T) {
	m := contextTestModel()
	m.monitorContextHidden = true
	m.cycleMonitorContext("root-one")
	if m.rowContextMode("root-one") != contextSplit || m.rowContextMode("root-two") != contextGraph {
		t.Fatal("first row affected second row")
	}
	m.cycleMonitorContext("root-one")
	m.cycleMonitorContext("root-two")
	if m.rowContextMode("root-one") != contextWide || m.rowContextMode("root-two") != contextSplit {
		t.Fatal("switching rows discarded presentation")
	}
	m.cycleMonitorContext("root-two")
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

func TestMonitorRowSurfaceCyclesOnlyClickedSession(t *testing.T) {
	for _, width := range []int{40, 80, 120, 200} {
		for _, edge := range []bool{false, true} {
			m := contextTestModel()
			m.width = width
			m.monitorContextHidden = true
			m.monitorSelectedID = "root-one"
			for _, want := range []int{contextSplit, contextWide, contextFull, contextWide, contextSplit, contextGraph} {
				g := m.dashboardLayout()
				a := layoutMonitorArea(g.contentWidth, g.meterHeight)
				_, heights, _ := m.monitorSessionPage(a.graphHeight)
				mw, rw, _ := monitorSessionColumnWidths(a.width)
				x, y := 2+mw+1, g.meterY+a.topHeight+a.gap-1+heights[0]+1
				if edge {
					x += rw - 1
				}
				if m.monitorContextDetail != "" {
					x, y = 4, g.meterY+2
				} else if got := m.monitorContextAt(2+mw-1, y); got != "" {
					t.Fatalf("metrics area cycles row: %q", got)
				}
				updated, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				m = updated.(Model)
				if got := m.rowContextMode("root-two"); got != want {
					t.Fatalf("width %d edge %v: mode %d, want %d", width, edge, got, want)
				}
				if m.rowContextMode("root-one") != contextGraph || m.rowContextMode("root-three") != contextGraph {
					t.Fatal("click changed another session")
				}
			}
		}
	}
}

func TestIndependentRowLayoutsAndApprovalOwnership(t *testing.T) {
	m := approvalTestModel()
	m.setRowContext("root-one", contextWide, false)
	m.setRowContext("root-two", contextWide, false)
	for y, line := range strings.Split(ansi.Strip(m.render()), "\n") {
		if pos := strings.Index(line, monitorContextInfo); pos >= 0 {
			if hit := m.monitorContextAt(lipgloss.Width(line[:pos]), y); hit != "root-two" {
				t.Fatalf("earlier wide row swallowed second row's info click: %q", hit)
			}
		}
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
			m.setRowContext("root-one", mode, false)
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
	m.cycleMonitorContext("root-one")
	m.monitorSelectedID = "root-one"
	m.selectMonitorSession(1)
	if m.monitorSelectedID != "root-two" || m.monitorContextExpanded != "" {
		t.Fatal("selection left an unrelated expanded target", m.monitorSelectedID, m.monitorContextExpanded)
	}
	for _, size := range [][2]int{{40, 16}, {80, 24}, {120, 40}, {180, 50}} {
		m.width, m.height = size[0], size[1]
		m.selectMonitorSession(0)
		out := ansi.Strip(m.render())
		if got := strings.Count(out, monitorContextInfo); got != 1 {
			t.Fatalf("%v: got %d info actions\n%s", size, got, out)
		}
		for y, line := range strings.Split(out, "\n") {
			if pos := strings.Index(line, monitorContextInfo); pos >= 0 {
				x := lipgloss.Width(line[:pos])
				for dx := 0; dx < 3; dx++ {
					if got := m.monitorContextAt(x+dx, y); got != "root-two" {
						t.Fatalf("selected empty row click target=%q", got)
					}
				}
			}
		}
	}
	// Also defend against a pre-existing expansion restored alongside selection.
	m.monitorContextExpanded = "root-one"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.monitorContextExpanded != "root-two" {
		t.Fatal("Enter followed old expansion")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.monitorContextDetail != "root-two" {
		t.Fatal("empty selected row could not open detail")
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
		m.cycleMonitorContext(m.monitorSessionData[0].id)
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
			if len(buttons) == 0 && cw >= lipgloss.Width(monitorContextInfo+" "+i18n.Text("OPEN DETAIL"))+8 && !strings.Contains(ansi.Strip(out), i18n.Text("OPEN DETAIL")) {
				t.Fatal("missing detail fallback")
			}
		}
	}
}

func TestMonitorExpandedSelectionDismissalAndConfirmation(t *testing.T) {
	m := approvalTestModel()
	m.monitorContextDetail = ""
	m.monitorContextExpanded = ""
	m.cycleMonitorContext("root-one")
	m, _, _ = m.monitorApprovalAction("decision:0")
	if m.monitorApprovalConfirm == "" {
		t.Fatal("confirmation missing")
	}
	m.cycleMonitorContext("")
	if m.monitorApprovalConfirm != "" {
		t.Fatal("confirmation survived mode transition")
	}
	m.stepBackMonitorContext()
	m.dismissMonitorSession("root-one")
	if m.monitorContextTarget() != "" {
		t.Fatal("dismissed row retained expansion")
	}
}

func TestMonitorContextMouseCyclesAllThreeStages(t *testing.T) {
	m := contextTestModel()
	for stage := 0; stage < 5; stage++ {
		wanted := "root-one"
		if stage == 2 {
			wanted = "cycle"
		}
		clicked := false
		for y, line := range strings.Split(ansi.Strip(m.render()), "\n") {
			if clicked {
				break
			}
			pos := strings.Index(line, monitorContextInfo)
			if pos < 0 {
				continue
			}
			x := lipgloss.Width(line[:pos])
			if m.monitorContextAt(x, y) != wanted {
				continue
			}
			next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
			m = next.(Model)
			clicked = true
		}
		if !clicked {
			t.Fatalf("stage %d info action missing", stage)
		}
		if stage == 0 && m.monitorContextExpanded != "root-one" {
			t.Fatal("mouse did not expand")
		}
		if stage == 1 && m.monitorContextDetail != "root-one" {
			t.Fatal("mouse did not open detail")
		}
		if stage == 2 && m.monitorContextExpanded != "root-one" {
			t.Fatal("mouse did not return to inline detail")
		}
		if stage == 3 && m.rowContextMode("root-one") != contextSplit {
			t.Fatal("mouse skipped split view")
		}
		if stage == 4 && m.rowContextMode("root-one") != contextGraph {
			t.Fatal("mouse did not show full graph")
		}
	}
}

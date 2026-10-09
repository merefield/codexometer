package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorCloseAllDismissesOffScreenRowsAndPreservesMeasurement(t *testing.T) {
	now := time.Unix(1000, 0)
	usage := codex.LiveUsageSnapshot{}
	for i := range 12 {
		usage.Sessions = append(usage.Sessions, codex.LiveUsageSession{
			ID: fmt.Sprintf("session-%d", i), TotalTokens: 100, Active: true, LastActivity: now,
		})
	}
	m := Model{snapshot: codex.DemoSnapshot(), width: 100, height: 24,
		meterView: viewMonitor, monitorState: monitorRunning, monitorStartedAt: now,
		monitorBaseline: 1200, monitorLatest: 1320,
		monitorSamples: []monitorSample{{intervalTokens: 120}},
	}
	m.startMonitorSessions(usage, now)
	for i := range usage.Sessions {
		usage.Sessions[i].TotalTokens = 110
	}
	m.syncMonitorSessions(usage, now.Add(time.Second))
	m.captureMonitorSamples(now.Add(monitorSampleInterval))
	m.dismissMonitorSession("session-11")
	oldDismissal := m.monitorDismissed["session-11"]
	oldDismissal.inactiveObserved, oldDismissal.attentionCleared = true, true
	m.monitorDismissed["session-11"] = oldDismissal
	m.monitorSelectedID, m.monitorContextExpanded = "session-0", "session-0"
	m.monitorApprovalConfirm = "pending-confirmation"
	m.monitorPrompt = monitorPromptState{session: "session-0", input: newMonitorEditor()}
	m.monitorPrompt.input.SetValue("keep my draft")
	m.monitorScroll = 2
	if m.monitorPageSize() >= m.visibleMonitorSessionCount() {
		t.Fatal("test requires rows beyond the current page")
	}
	before := append([]monitorSession(nil), m.monitorSessionData...)
	samplesBefore := append([]monitorSample(nil), m.monitorSamples...)
	g := m.monitorDashboardLayout()
	r := m.monitorArea(g.contentWidth, g.meterHeight).closeAllRect
	updated, cmd := m.Update(tea.MouseClickMsg{X: 2 + r.x + 1, Y: g.meterY + r.y + 1, Button: tea.MouseLeft})
	m = updated.(Model)
	if !m.monitorCloseAllArmed() || m.visibleMonitorSessionCount() != 11 {
		t.Fatal("first Close All click did not require confirmation")
	}
	updated, cmd = m.Update(tea.MouseClickMsg{X: 2 + r.x + 1, Y: g.meterY + r.y + 1, Button: tea.MouseLeft})
	m = updated.(Model)
	if cmd == nil || m.flashedButton != footerButtonMonitorCloseAll || m.visibleMonitorSessionCount() != 0 || m.monitorSessions != 0 {
		t.Fatal("Close All click did not dismiss all rows and flash the control")
	}
	if m.monitorState != monitorRunning || m.monitorRecordedTokens() != 120 || m.monitorBaseline != 1200 || !m.monitorStartedAt.Equal(now) || !reflect.DeepEqual(samplesBefore, m.monitorSamples) || !reflect.DeepEqual(before, m.monitorSessionData) {
		t.Fatal("Close All changed measurements or session telemetry")
	}
	if m.monitorSelectedID != "" || m.monitorScroll != 0 || m.monitorContextExpanded != "" || m.monitorApprovalConfirm != "" || m.monitorPrompt.session != "" || m.monitorDrafts["session-0"] != "keep my draft" {
		t.Fatal("Close All left stale controls or discarded an ordinary draft")
	}
	if m.monitorDismissed["session-11"] != oldDismissal {
		t.Fatal("Close All replaced a previous dismissal watermark")
	}
	// Exclude the earlier dismissed row: its saved inactive transition makes it
	// eligible to return independently, and must remain effective after Close All.
	m.syncMonitorSessions(codex.LiveUsageSnapshot{Sessions: usage.Sessions[:11]}, now.Add(2*time.Second))
	if m.visibleMonitorSessionCount() != 0 {
		t.Fatal("unchanged telemetry immediately restored closed rows")
	}
	m.captureMonitorSamples(now.Add(2 * monitorSampleInterval))
	if len(m.monitorSessionData[0].samples) != len(before[0].samples)+1 {
		t.Fatal("Close All stopped collecting hidden-row telemetry")
	}
	usage.Sessions[0].TotalTokens++
	m.syncMonitorSessions(codex.LiveUsageSnapshot{Sessions: usage.Sessions[:11]}, now.Add(3*time.Second))
	if m.visibleMonitorSessionCount() != 1 || !m.monitorSessionVisible(m.monitorSessionData[0]) {
		t.Fatal("fresh activity did not restore only the renewed row")
	}
}

func TestMonitorCloseAllRenderedControlAndEmptyList(t *testing.T) {
	m := Model{snapshot: codex.DemoSnapshot(), width: 120, height: 36,
		meterView: viewMonitor, monitorState: monitorRunning,
		monitorSessionData: []monitorSession{{id: "one", displayed: true}},
	}
	g := m.monitorDashboardLayout()
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	view := m.renderMonitorArea(g.contentWidth, g.meterHeight, paletteFor(m.theme))
	if a.closeAllRect.x <= a.resetRect.x+a.resetRect.width || a.closeAllRect.y != a.resetRect.y || a.closeAllRect.height != a.resetRect.height {
		t.Fatal("Close All is not beside Zero")
	}
	if !strings.Contains(ansi.Strip(view.view), i18n.Text("CL(O)SE ALL")) || lipgloss.Width(view.view) > g.contentWidth {
		t.Fatal("Close All label is missing or overflows the header")
	}
	updated, cmd := m.Update(tea.MouseMotionMsg{X: 2 + a.closeAllRect.x + 1, Y: g.meterY + 1})
	m = updated.(Model)
	if cmd != nil || m.hoveredButton != footerButtonMonitorCloseAll {
		t.Fatal("Close All hover did not select the button")
	}
	m.dismissAllMonitorSessions()
	if m.monitorCloseAllEnabled() || m.monitorButtonAt(2+a.closeAllRect.x+1, g.meterY+1) != footerButtonNone || !m.monitorResetEnabled() {
		t.Fatal("empty list did not disable Close All while leaving Zero available")
	}
}

func TestMonitorControlsHaveDistinctScopedShortcuts(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{key('z'), key('Z')} {
		m := contextTestModel()
		next, cmd := m.Update(k)
		m = next.(Model)
		if cmd == nil || m.monitorState != monitorResetting || m.flashedButton != footerButtonMonitorReset || m.visibleMonitorSessionCount() != 3 {
			t.Fatal("Z did not exclusively zero the measurement")
		}
	}
	for _, k := range []tea.KeyPressMsg{key('s'), key('S')} {
		m := contextTestModel()
		next, cmd := m.Update(k)
		m = next.(Model)
		if cmd != nil || m.monitorState != monitorRunning || m.flashedButton != footerButtonNone {
			t.Fatal("S remains a Sessions hotkey")
		}
	}
	for _, k := range []tea.KeyPressMsg{key('o'), key('O')} {
		m := contextTestModel()
		next, cmd := m.Update(k)
		m = next.(Model)
		if cmd == nil || !m.monitorCloseAllArmed() || m.visibleMonitorSessionCount() != 3 {
			t.Fatal("O did not arm Close All without dismissing rows")
		}
		next, cmd = m.Update(k)
		m = next.(Model)
		if cmd == nil || m.visibleMonitorSessionCount() != 0 || m.monitorState != monitorRunning || m.flashedButton != footerButtonMonitorCloseAll {
			t.Fatal("second O did not confirm Close All")
		}
	}
	m := contextTestModel()
	m.monitorSelectedID = "root-one"
	next, _ := m.Update(key('x'))
	m = next.(Model)
	if m.visibleMonitorSessionCount() != 2 {
		t.Fatal("plain x no longer closes only the selected row")
	}
	m = contextTestModel()
	m.requestMonitorCloseAll()
	repeated := key('o')
	repeated.IsRepeat = true
	next, cmd := m.Update(repeated)
	m = next.(Model)
	if cmd != nil || m.visibleMonitorSessionCount() != 3 {
		t.Fatal("repeated O confirmed Close All")
	}
	m = contextTestModel()
	m.meterView = viewBars
	for _, k := range []tea.KeyPressMsg{key('z'), key('o')} {
		next, cmd = m.Update(k)
		m = next.(Model)
		if cmd != nil || m.monitorState != monitorRunning || m.visibleMonitorSessionCount() != 3 {
			t.Fatal("Sessions shortcut acted outside Sessions")
		}
	}
}

func TestMonitorControlShortcutsTypeNormallyInComposer(t *testing.T) {
	m, _ := inlinePromptTestModel()
	m, _ = promptKey(m, tea.KeyEnter, "")
	if !m.monitorPrompt.input.Focused() {
		t.Fatal("inline composer did not focus")
	}
	for _, k := range []tea.KeyPressMsg{key('z'), key('o'), key('O')} {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	if m.monitorPrompt.input.Value() != "zoO" || m.monitorState != monitorRunning || m.visibleMonitorSessionCount() != 1 || m.flashedButton != footerButtonNone {
		t.Fatal("typing Z or O triggered a Sessions control")
	}
}

func TestMonitorCompactControlsKeepShortcutHints(t *testing.T) {
	for _, size := range []struct{ w, h int }{{40, 16}, {80, 24}, {120, 40}} {
		m := contextTestModel()
		m.width, m.height = size.w, size.h
		g := m.monitorDashboardLayout()
		a := m.monitorArea(g.contentWidth, g.meterHeight)
		view := m.renderMonitorArea(g.contentWidth, g.meterHeight, paletteFor(m.theme)).view
		if lipgloss.Width(view) > g.contentWidth || lipgloss.Height(view) > g.meterHeight {
			t.Fatalf("controls overflow at %dx%d", size.w, size.h)
		}
		lines := strings.Split(view, "\n")
		for _, b := range []struct {
			r    monitorRect
			hint string
		}{{a.resetRect, "(Z)"}, {a.closeAllRect, "(O)"}} {
			var rendered []string
			for y := b.r.y; y < b.r.y+b.r.height; y++ {
				rendered = append(rendered, ansi.Strip(ansi.Cut(lines[y], b.r.x, b.r.x+b.r.width)))
			}
			text := strings.Join(rendered, "\n")
			if !strings.Contains(text, b.hint) {
				t.Fatalf("%dx%d button is missing its shortcut: %q", size.w, size.h, text)
			}
		}
	}
}

func TestMonitorCloseAllConfirmationCancelExpiryAndChangedRows(t *testing.T) {
	m := contextTestModel()
	m.requestMonitorCloseAll()
	next, cmd := m.Update(specialKey(tea.KeyEscape))
	m = next.(Model)
	if cmd != nil || m.monitorCloseAllArmed() || m.visibleMonitorSessionCount() != 3 {
		t.Fatal("Escape did not cancel without closing rows or quitting")
	}
	m.requestMonitorCloseAll()
	m.monitorCloseAllUntil = time.Now().Add(-time.Second)
	next, _ = m.Update(key('o'))
	m = next.(Model)
	if !m.monitorCloseAllArmed() || m.visibleMonitorSessionCount() != 3 {
		t.Fatal("expired confirmation did not require a fresh arm")
	}
	m.monitorSessionData = append(m.monitorSessionData, monitorSession{id: "new", displayed: true})
	next, _ = m.Update(key('o'))
	m = next.(Model)
	if !m.monitorCloseAllArmed() || m.visibleMonitorSessionCount() != 4 {
		t.Fatal("changed rows were closed without fresh confirmation")
	}
	next, _ = m.Update(key('z'))
	m = next.(Model)
	if len(m.monitorCloseAllConfirm) != 0 {
		t.Fatal("Zero retained the armed Close All")
	}
	m = contextTestModel()
	m.requestMonitorCloseAll()
	next, _ = m.pressViewTab(viewBars)
	m = next.(Model)
	if len(m.monitorCloseAllConfirm) != 0 {
		t.Fatal("switching views retained the armed Close All")
	}
}

func TestMonitorCloseAllArmingShowsConfirmationAndReleasesTypingFocus(t *testing.T) {
	m, _ := inlinePromptTestModel()
	m, _ = promptKey(m, tea.KeyEnter, "")
	m.monitorPrompt.input.SetValue("draft")
	m.requestMonitorCloseAll()
	if !m.monitorCloseAllArmed() || m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "draft" {
		t.Fatal("arming did not preserve the draft and focus the confirmation")
	}
	g := m.monitorDashboardLayout()
	r := m.monitorArea(g.contentWidth, g.meterHeight).closeAllRect
	view := m.renderMonitorArea(g.contentWidth, g.meterHeight, paletteFor(m.theme)).view
	var text []string
	for y, line := range strings.Split(view, "\n") {
		if y >= r.y && y < r.y+r.height {
			text = append(text, ansi.Strip(ansi.Cut(line, r.x, r.x+r.width)))
		}
	}
	label := strings.Join(text, "\n")
	if !strings.Contains(label, "C(O)NFIRM") || !strings.Contains(label, "Esc") {
		t.Fatal("armed button does not clearly offer confirmation and cancellation")
	}
}

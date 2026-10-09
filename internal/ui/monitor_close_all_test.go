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
	if !strings.Contains(ansi.Strip(view.view), i18n.Text("CLOSE ALL")) || lipgloss.Width(view.view) > g.contentWidth {
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

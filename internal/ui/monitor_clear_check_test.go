package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorClearCheckDismissesOnlyMatchingRowsIncludingOffPage(t *testing.T) {
	m := statusFilterTestModel()
	m.height = 24
	m.finder.editInput().SetValue("match")
	for i := range m.monitorSessionData {
		m.monitorSessionData[i].name = "match"
	}
	m.setRowContext("work", contextWide)
	m.monitorPrompt.session = "work"
	m.monitorPrompt.input = newMonitorEditor()
	m.monitorPrompt.input.SetValue("keep this draft")
	check := &m.monitorSessionData[3]
	check.latest, check.turnSequence, check.lastActivity = 42, 6, time.Unix(100, 0)
	checkID := check.id
	for _, id := range []string{"new-check", "off-page", "other-check", "priority"} {
		name := "match"
		if id == "other-check" {
			name = "excluded"
		}
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{id: id, name: name, displayed: true, attention: codex.SessionAttentionCheck})
	}
	m.quotaStepPending = &codex.QuotaStep{Threshold: 80}
	m.quota.sessions = []codex.QuotaSession{{ID: "priority"}}
	geometry := m.monitorDashboardLayout()
	r := m.monitorArea(geometry.contentWidth, geometry.meterHeight).clearCheckRect
	x, y := 2+r.x+1, geometry.meterY+r.y+1
	next, _ := m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if next.(Model).hoveredButton != footerButtonMonitorClearCheck {
		t.Fatal("Clear Check* did not hover")
	}
	next, cmd := next.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	n := next.(Model)
	if cmd == nil || n.flashedButton != footerButtonMonitorClearCheck || n.monitorClearCheckEnabled() {
		t.Fatal("Clear Check* should flash and then disable with no matching candidates")
	}
	if len(n.monitorDismissed) != 4 || len(m.monitorDismissed) != 1 || n.monitorRecordedTokens() != 625 ||
		n.monitorSelectedID != "work" || n.monitorPrompt.input.Value() != "keep this draft" {
		t.Fatalf("unexpected bookkeeping, accounting, selection or draft: got %d new/%d previous dismissals, %d tokens, selection %q, editor %q",
			len(n.monitorDismissed), len(m.monitorDismissed), n.monitorRecordedTokens(), n.monitorSelectedID, n.monitorPrompt.input.Value())
	}
	for _, id := range []string{checkID, "new-check", "off-page"} {
		if _, ok := n.monitorDismissed[id]; !ok {
			t.Fatal("matching Check* left visible:", id)
		}
	}
	want := []string{"work", "approval", "input", "complete", "idle", "priority"}
	if got, _ := n.monitorListSelection(); !slices.Equal(got, want) {
		t.Fatalf("unexpected remaining rows %v; want %v", got, want)
	}
	row := n.monitorSessionData[3]
	n.restoreMonitorSessionOnActivity(&row)
	if n.monitorSessionVisible(row) {
		t.Fatal("unchanged Check* returned immediately")
	}
	row.turnSequence++
	n.restoreMonitorSessionOnActivity(&row)
	if !n.monitorSessionVisible(row) {
		t.Fatal("new activity did not restore the row")
	}
}

func TestMonitorClearCheckStrictConfirmationIsSeparateFromCloseAll(t *testing.T) {
	m := statusFilterTestModel()
	m.monitorFilter = monitorFilterCheck // Both actions now have exactly the same targets.
	m.monitorSummaryFocused = true
	m.monitorSelectedID = "check"
	m.SetConfirmationMode(confirmation.Require)
	press := func(k rune) {
		next, _ := m.Update(key(k))
		m = next.(Model)
	}
	press('o')
	if !m.monitorCloseAllArmed() {
		t.Fatal("Close All did not arm")
	}
	press('K')
	if !m.monitorBulkDismissArmed(true) || m.monitorCloseAllArmed() || m.monitorListSessionCount() != 1 {
		t.Fatal("Clear Check* reused a different action's confirmation")
	}
	m.monitorSessionData[2].attention = codex.SessionAttentionCheck
	press('k')
	if !m.monitorBulkDismissArmed(true) || m.monitorListSessionCount() != 2 {
		t.Fatal("changed targets did not require fresh confirmation")
	}
	press('k')
	if m.monitorListSessionCount() != 0 || m.monitorFilter != monitorFilterCheck || m.monitorSelectedID != "" {
		t.Fatal("confirmed clear did not update the list while preserving its filter")
	}
}

func TestMonitorClearCheckShortcutDoesNotInterruptTyping(t *testing.T) {
	m, _ := inlinePromptTestModel()
	m.monitorSessionData = append(m.monitorSessionData, monitorSession{
		id: "check", displayed: true, attention: codex.SessionAttentionCheck,
	})
	m, _ = promptKey(m, tea.KeyEnter, "")
	if !m.monitorPrompt.input.Focused() {
		t.Fatal("test needs a focused inline composer")
	}
	next, _ := m.Update(key('k'))
	m = next.(Model)
	if m.monitorPrompt.input.Value() != "k" || !m.monitorClearCheckEnabled() || m.flashedButton != footerButtonNone {
		t.Fatal("K typed in the composer unexpectedly dismissed Check*")
	}
}

func TestMonitorClearCheckResponsiveHitTargets(t *testing.T) {
	for _, size := range []struct{ w, h int }{{40, 16}, {48, 20}, {52, 24}, {84, 25}, {136, 40}, {240, 42}} {
		m := statusFilterTestModel()
		m.width, m.height = size.w, size.h
		g := m.monitorDashboardLayout()
		a := m.monitorArea(g.contentWidth, g.meterHeight)
		view := m.renderMonitorArea(g.contentWidth, g.meterHeight, paletteFor(m.theme)).view
		if lipgloss.Width(view) > g.contentWidth || lipgloss.Height(view) > g.meterHeight {
			t.Fatalf("header overflow at %v", size)
		}
		r := a.clearCheckRect
		if r.width == 0 {
			next, _ := m.Update(key('k'))
			if size.w != 40 || next.(Model).monitorClearCheckEnabled() {
				t.Fatalf("missing Clear Check* button or keyboard fallback at %v", size)
			}
			continue
		}
		x, y := 2+r.x, g.meterY+r.y
		if m.monitorButtonAt(x, y) != footerButtonMonitorClearCheck ||
			m.monitorButtonAt(x+r.width-1, y+r.height-1) != footerButtonMonitorClearCheck ||
			m.monitorButtonAt(x-1, y) != footerButtonNone || m.monitorButtonAt(x+r.width, y) != footerButtonNone {
			t.Fatalf("misaligned Clear Check* button at %v", size)
		}
		if size.w == 240 && !strings.Contains(ansi.Strip(view), i18n.Text("CLEAR CHEC(K)*")) {
			t.Fatal("full button label missing at wide size")
		}
		m.monitorFilter = monitorFilterWorking
		if m.monitorButtonAt(x, y) != footerButtonNone {
			t.Fatal("excluded Check* still gave the button a hit target")
		}
	}
}

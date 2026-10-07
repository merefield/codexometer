package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSchedulePaneKeepsDashboardAndComposerAnchored(t *testing.T) {
	for _, height := range []int{24, 40, 60} {
		for _, draft := range []string{"Draft to keep", strings.Repeat("Wrapped draft ", 60)} {
			m, _ := promptTestModel()
			m.height = height
			m.focusMonitorPrompt()
			m.monitorPrompt.input.SetValue(draft)
			before := strings.Split(ansi.Strip(m.render()), "\n")
			find := func(lines []string, marker string) int {
				for i, line := range lines {
					if strings.Contains(line, marker) {
						return i
					}
				}
				return -1
			}
			composer := find(before, "[ Ctrl+S:")
			if composer < 0 {
				t.Fatal("missing original composer")
			}
			m.openSchedule()
			after := strings.Split(ansi.Strip(m.render()), "\n")
			if find(after, "[ Ctrl+S:") != composer {
				t.Fatalf("height %d composer moved: %d -> %d", height, composer, find(after, "[ Ctrl+S:"))
			}
			g := m.monitorDashboardLayout()
			for i := 0; i < g.meterY; i++ {
				if before[i] != after[i] {
					t.Fatalf("dashboard changed at row %d", i)
				}
			}
			if !strings.Contains(strings.Join(after, "\n"), "SCHEDULE FOLLOW-UP") {
				t.Fatal("form missing")
			}
			m.scheduleUI.input.SetValue("Edited draft")
			m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyEsc})
			if m.scheduleUI.open || m.monitorPrompt.input.Value() != "Edited draft" || m.monitorContextDetail != "root-one" {
				t.Fatal("back lost draft or session")
			}
		}
	}
}

func TestSchedulePaneCancelKeepsExistingTriggerAndSeparateDraft(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("Separate reply draft")
	before := m.scheduleUI.queue.List("root-one")[0]
	m.openSchedule()
	m.scheduleUI.input.SetValue("Uncommitted trigger edit")
	m.closeSchedule()
	if m.monitorPrompt.input.Value() != "Separate reply draft" || m.scheduleUI.queue.List("root-one")[0].Text != before.Text {
		t.Fatal("cancel changed draft or saved trigger")
	}
}

func TestSchedulePaneScrollAndHitTargets(t *testing.T) {
	m, _ := promptTestModel()
	m.height = 26
	m.openSchedule()
	m.scheduleUI.mode = 2
	m.scheduleUI.date = time.Date(2026, time.October, 28, 12, 0, 0, 0, time.Local)
	m.scheduleUI.focus = 2
	m.revealScheduleFocus()
	g := m.monitorDashboardLayout()
	l := m.schedulePaneLayout(g.contentWidth, g.meterHeight)
	if l.scroll == 0 {
		t.Fatal("calendar focus did not scroll into view")
	}
	// The selected date is Wednesday in the fifth calendar row.
	m, _, _ = m.updateSchedule(scheduleFormClick(m, 2+3*4, 15))
	if m.scheduleUI.date.Day() != 28 {
		t.Fatal("scrolled calendar click missed date", m.scheduleUI.date)
	}
	m.scheduleUI.focus = 5
	m.revealScheduleFocus()
	l = m.schedulePaneLayout(g.contentWidth, g.meterHeight)
	actionVisible := false
	for _, row := range l.indices[l.scroll:min(l.scroll+l.textRows, len(l.indices))] {
		if row == 22 {
			actionVisible = true
		}
	}
	if !actionVisible {
		t.Fatal("keyboard focus left action offscreen")
	}
	// Clicking composer focuses it without changing the scroll/trigger.
	m, _, _ = m.updateSchedule(tea.MouseClickMsg{X: 6, Y: g.meterY + l.composerY + 1, Button: tea.MouseLeft})
	if m.scheduleUI.focus != 0 || !m.scheduleUI.input.Focused() {
		t.Fatal("anchored composer click missed")
	}
	m, _, _ = m.updateSchedule(tea.MouseWheelMsg{X: 6, Y: g.meterY + 2, Button: tea.MouseWheelUp})
	if m.scheduleUI.scroll >= l.scroll {
		t.Fatal("form wheel did not scroll")
	}
	m, _, _ = m.updateSchedule(tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.width != 100 || m.height != 40 || !m.scheduleUI.open {
		t.Fatal("resize lost schedule")
	}
}

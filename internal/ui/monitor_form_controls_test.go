package ui

import (
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

// Also run in every locale subprocess: real rendered cells must match the
// interaction document, regardless of label length or double-width glyphs.
func TestScheduleRenderedControlSurfaces(t *testing.T) {
	// The dashboard countdowns and delay preview use time.Now(). Keep the clock
	// fixed across renders so a clock tick cannot resemble a hover-induced text
	// change. Retain the whole-screen text comparison, not just the button.
	synctest.Test(t, testScheduleRenderedControlSurfaces)
}

func testScheduleRenderedControlSurfaces(t *testing.T) {
	for _, width := range []int{48, 80, 140} {
		for mode := 0; mode < 3; mode++ {
			m, _ := promptTestModel()
			m.width, m.height = width, 60
			m.openSchedule()
			m.scheduleUI.mode = mode
			m.scheduleUI.input.SetValue("A follow-up")
			g := m.monitorDashboardLayout()
			l := m.schedulePaneLayout(g.contentWidth, g.meterHeight)
			copy := m
			copy.width = g.contentWidth
			_, controls := copy.scheduleFormDocument()
			plain := strings.Split(ansi.Strip(m.render()), "\n")
			modes := 0
			for _, c := range controls {
				if strings.HasPrefix(c.key, "mode:") {
					modes++
				}
				row := -1
				for i, r := range l.indices {
					if r == c.row {
						row = g.meterY + 1 + i - l.scroll
						break
					}
				}
				if row < g.meterY+1 || row >= g.meterY+1+l.textRows {
					continue
				}
				for _, x := range []int{4 + c.x, 4 + c.x + c.width - 1} {
					if hit := m.scheduleControlAt(x, row); hit.key != c.key {
						t.Fatalf("width %d mode %d hit %q, want %q at %d,%d", width, mode, hit.key, c.key, x, row)
					}
				}
				if strings.TrimSpace(ansi.Cut(plain[row], 4+c.x, 4+c.x+c.width)) == "" {
					t.Fatal("invisible control", c.key)
				}
				// One date suffices for hover; all dates still have their hit extents checked.
				if strings.HasPrefix(c.key, "day:") && c.value != m.scheduleUI.date.Day() {
					continue
				}
				before := m.render()
				next, _ := m.Update(tea.MouseMotionMsg{X: 4 + c.x, Y: row})
				n := next.(Model)
				after := n.render()
				if n.scheduleUI.hover != c.key || before == after || ansi.Strip(before) != ansi.Strip(after) {
					t.Fatal("hover missing or changed layout", width, mode, c.key)
				}
				if c.focus != 5 && c.focus != 6 {
					next, _ = m.Update(tea.MouseClickMsg{X: 4 + c.x, Y: row, Button: tea.MouseLeft})
					if next.(Model).scheduleUI.focus != c.focus {
						t.Fatal("click missed field", c.key)
					}
				}
			}
			if modes != 3 {
				t.Fatal("clipping hid a trigger choice", width, mode)
			}
		}
	}
}

func TestScheduleDelayPreviewClockChangesOnlyPreview(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, _ := promptTestModel()
		m.openSchedule()
		m.scheduleUI.mode = 1
		m.scheduleUI.input.SetValue("A follow-up")
		before, beforeControls := m.scheduleFormDocument()
		previewRow := m.scheduleUI.actionRow() - 2
		if !strings.Contains(ansi.Strip(before[previewRow]), scheduleDate(m.scheduleUI.targetTime(time.Now()))) {
			t.Fatal("delay preview does not show the current target time")
		}
		time.Sleep(time.Minute) // Preview has minute precision; no real sleep.
		after, afterControls := m.scheduleFormDocument()
		if before[previewRow] == after[previewRow] || !strings.Contains(ansi.Strip(after[previewRow]), scheduleDate(m.scheduleUI.targetTime(time.Now()))) {
			t.Fatal("delay preview did not advance with the clock")
		}
		before[previewRow] = after[previewRow]
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeControls, afterControls) {
			t.Fatal("clock update changed form content or control geometry beyond the preview")
		}
	})
}

func clickFormTab(t *testing.T, m Model, tab mainTabID) Model {
	t.Helper()
	y := m.dashboardLayout().tabsY
	for x := 0; x < m.width; x++ {
		if hit, ok := m.mainTabAt(x, y); ok && hit == tab {
			next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			return next.(Model)
		}
	}
	t.Fatal("tab missing")
	return m
}

func TestFormNavigationPreservesDraftsWithoutSubmitting(t *testing.T) {
	m, _ := promptTestModel()
	m.openSchedule()
	m.scheduleUI.input.SetValue("Keep this schedule draft")
	m.scheduleUI.mode, m.scheduleUI.hours, m.scheduleUI.minutes = 1, 7, 23
	m = clickFormTab(t, m, mainTabQuota)
	if m.scheduleUI.open || m.meterView == viewMonitor || len(m.scheduleUI.queue.List("")) != 0 {
		t.Fatal("navigation failed or saved a trigger")
	}
	m = clickFormTab(t, m, mainTabMonitor)
	m.setRowContext("root-one", contextFull)
	m.openSchedule()
	if m.scheduleUI.input.Value() != "Keep this schedule draft" || m.scheduleUI.hours != 7 || m.scheduleUI.minutes != 23 || m.scheduleUI.mode != 1 {
		t.Fatal("schedule draft lost")
	}
	m.closeSchedule()
	m, c := queueTestModel(false)
	m.openQueueItem(0, false)
	m.monitorQueue.input.SetValue("Unsent queue edit")
	m = clickFormTab(t, m, mainTabQuota)
	if m.monitorQueue.open || c.changed != "" {
		t.Fatal("navigation submitted queue edit")
	}
	m = clickFormTab(t, m, mainTabMonitor)
	m.setRowContext("root-one", contextFull)
	m.openQueueItem(0, false)
	if m.monitorQueue.input.Value() != "Unsent queue edit" {
		t.Fatal("queue draft lost")
	}
}

func TestScheduleSmallPaneKeepsComposerAndFooter(t *testing.T) {
	for _, height := range []int{18, 22, 28} {
		m, _ := promptTestModel()
		m.width, m.height = 44, height
		m.openSchedule()
		m.scheduleUI.input.SetValue("Draft")
		if !strings.Contains(ansi.Strip(m.render()), "Draft") {
			t.Fatal("composer disappeared", height)
		}
		y := m.dashboardLayout().footerY + 1
		found := false
		for x := 0; x < m.width; x++ {
			if m.footerButtonAt(x, y) != footerButtonTheme {
				continue
			}
			next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if next.(Model).theme == m.theme || !next.(Model).scheduleUI.open {
				t.Fatal("small pane swallowed theme click")
			}
			found = true
			break
		}
		if !found {
			t.Fatal("footer missing", height)
		}
	}
}

func TestScheduleSingleHintAndQueueEmbeddedKeyboard(t *testing.T) {
	m, _ := promptTestModel()
	m.openSchedule()
	if strings.Count(ansi.Strip(m.render()), m.scheduleUI.focusHint()) != 1 {
		t.Fatal("duplicate focus hints")
	}
	m, c := queueTestModel(false)
	before := strings.Split(ansi.Strip(m.render()), "\n")
	m.openQueueItem(0, false)
	after := strings.Split(ansi.Strip(m.render()), "\n")
	for i := 0; i < m.monitorDashboardLayout().meterY; i++ {
		if before[i] != after[i] {
			t.Fatal("queue editor replaced dashboard", i)
		}
	}
	m, _, _ = m.updateMonitorQueue(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.monitorQueue.editorFocus != 1 || m.monitorQueue.input.Focused() {
		t.Fatal("save not keyboard focusable")
	}
	m, _, _ = m.updateMonitorQueue(tea.KeyPressMsg{Code: tea.KeyTab})
	m, _, _ = m.updateMonitorQueue(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.monitorQueue.open || c.changed != "" {
		t.Fatal("keyboard cancel wrote queue")
	}
}

func TestScheduleInvisibleConfirmationCannotSave(t *testing.T) {
	m, _ := promptTestModel()
	m.openSchedule()
	m.scheduleUI.input.SetValue("Do not send blindly")
	m.scheduleUI.focus = 5
	m.width, m.height = 28, 14
	m, cmd, _ := m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || len(m.scheduleUI.queue.List("")) != 0 || !m.scheduleUI.open || m.scheduleUI.notice == "" {
		t.Fatal("invisible confirmation accepted")
	}
}

func TestScheduleAttentionNavigationKeepsDraft(t *testing.T) {
	m, _ := promptTestModel()
	m.height = 60
	m.monitorSessionData[0].attention = codex.SessionAttentionComplete
	m.monitorSessionData[0].working = false
	m.openSchedule()
	m.scheduleUI.input.SetValue("Unsaved attention draft")
	g := m.monitorDashboardLayout()
	for y := 0; y < g.meterY; y++ {
		for x := 0; x < m.width; x++ {
			if m.monitorAttentionAt(x, y) == "" {
				continue
			}
			next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			m = next.(Model)
			if m.scheduleUI.open {
				t.Fatal("attention pill swallowed")
			}
			m.openSchedule()
			if m.scheduleUI.input.Value() != "Unsaved attention draft" {
				t.Fatal("attention navigation lost draft")
			}
			return
		}
	}
	t.Fatal("attention pill missing from fixture")
}

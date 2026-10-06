package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
	"strings"
	"testing"
	"time"
)

func TestScheduleFormSaveSignpostAndCancel(t *testing.T) {
	m, c := promptTestModel()
	m.width = 100
	m.height = 40
	m.snapshot = codex.Snapshot{AccountFingerprint: "a", FetchedAt: time.Now(), RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 100}}}
	m.openSchedule()
	if !m.scheduleUI.open {
		t.Fatal("form unavailable")
	}
	m.scheduleUI.input.SetValue("Do this later")
	m.scheduleUI.focus = 5
	m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.scheduleUI.open || !m.hasSchedule("root-one") || c.answers != nil {
		t.Fatal("save sent immediately or lost schedule")
	}
	if !strings.Contains(m.renderMonitorContextDetail(100, 30, paletteFor(m.theme)), "TRIGGER SET") {
		t.Fatal("missing signpost")
	}
	if cmd := m.dispatchSchedules(); cmd != nil {
		t.Fatal("dispatch despite exhausted quota")
	}
	m.openSchedule()
	m.scheduleUI.focus = 6
	m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.hasSchedule("root-one") {
		t.Fatal("cancel failed")
	}
}

func TestScheduleDispatchAndBusyGuard(t *testing.T) {
	m, c := promptTestModel()
	m.monitorPrompt.input.Blur()
	m.snapshot = codex.Snapshot{AccountFingerprint: "a", FetchedAt: time.Now(), RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 10}}}
	m.scheduleUI.queue = schedule.New()
	if err := m.scheduleUI.queue.Save(schedule.Job{Session: "root-one", Text: "hello", Trigger: "quota"}, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	m.scheduleUI.open = true
	if m.dispatchSchedules() != nil {
		t.Fatal("competed with editor")
	}
	m.scheduleUI.open = false
	cmd := m.dispatchSchedules()
	if cmd == nil {
		t.Fatal("missing dispatch")
	}
	cmd()
	if len(c.answers) != 1 || c.answers[0] != "hello" {
		t.Fatal(c.answers)
	}
	if m.hasSchedule("root-one") {
		t.Fatal("sent job still pending")
	}
}

func TestScheduleMouseSurfacesAndCalendar(t *testing.T) {
	m, _ := promptTestModel()
	m.width = 100
	m.height = 40
	m.openSchedule()
	for _, tc := range []struct{ x, mode int }{{3, 0}, {26, 1}, {36, 2}} {
		m, _, _ = m.updateSchedule(tea.MouseClickMsg{X: tc.x, Y: 7, Button: tea.MouseLeft})
		if m.scheduleUI.mode != tc.mode {
			t.Fatal("wrong trigger click", tc)
		}
	}
	m.scheduleUI.date = time.Date(2026, time.January, 31, 12, 0, 0, 0, time.UTC)
	m.scheduleUI.focus = 2
	m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.scheduleUI.date.Month() != time.February {
		t.Fatal("calendar skipped February")
	}
	m.width = 30
	_, _, handled := m.updateSchedule(tea.MouseClickMsg{X: 3, Y: 7, Button: tea.MouseLeft})
	if !handled {
		t.Fatal("short terminal passed click to underlying screen")
	}
}

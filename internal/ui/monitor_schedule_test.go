package ui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
	"strings"
	"testing"
	"time"
)

func TestScheduleTriggerSelectionDoesNotHighlightWholeGroup(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.openSchedule()
	colors := paletteFor(m.theme)
	labels := []string{"AFTER QUOTA REFRESH", "IN…", "AT…"}
	for _, focus := range []int{0, 1, 2} {
		m.scheduleUI.focus = focus
		for _, selected := range []int{0, 1, 2, 1, 0} {
			m.scheduleUI.mode = selected
			row := m.scheduleUI.renderTriggerModes(colors)
			var expected []string
			for i, name := range labels {
				label := fmt.Sprintf("%-9s", "[ "+name+" ]")
				style := colors.label()
				if i == selected && focus == 1 {
					style = style.Foreground(colors.primary).Reverse(true)
				}
				expected = append(expected, style.Render(label))
			}
			if row != strings.Join(expected, " ") {
				t.Fatalf("focus %d selected %d changed the group styling: %q", focus, selected, row)
			}
		}
	}
}

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
	if !m.hasSchedule("root-one") {
		t.Fatal("Back deleted existing trigger")
	}
	m.triggerAction("ctrl+d")
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

func scheduledTestModel(t *testing.T) (Model, *promptTestClient) {
	t.Helper()
	m, c := promptTestModel()
	m.width = 120
	m.height = 45
	m.snapshot = codex.Snapshot{AccountFingerprint: "a", FetchedAt: time.Now(), RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 0}}}
	m.scheduleUI.queue = schedule.New()
	if err := m.scheduleUI.queue.Save(schedule.Job{Session: "root-one", Text: "The saved trigger prompt", Trigger: "at", At: time.Now().Add(time.Hour)}, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	return m, c
}

func TestSchedulePanelReadOnlyAndSendNow(t *testing.T) {
	m, c := scheduledTestModel(t)
	if m.monitorPromptRows(100, 30) != 0 {
		t.Fatal("composer remains visible")
	}
	out := m.renderMonitorContextDetail(100, 30, paletteFor(m.theme))
	if !strings.Contains(out, "The saved trigger prompt") || !strings.Contains(out, "Not before") {
		t.Fatal("missing pending prompt/time")
	}
	m.triggerAction("ctrl+n")
	if m.scheduleUI.confirmID == "" || c.answers != nil {
		t.Fatal("send skipped confirmation")
	}
	cmd := m.triggerAction("ctrl+y")
	if cmd == nil {
		t.Fatal("missing confirmed send")
	}
	cmd()
	if len(c.answers) != 1 || c.answers[0] != "The saved trigger prompt" || m.hasSchedule("root-one") {
		t.Fatal("wrong dispatch")
	}
}

func TestScheduleFormKeyboardVisitsOnlyVisibleFields(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.openSchedule()
	for mode, want := range map[int][]int{0: {1, 5, 6, 0}, 1: {1, 2, 3, 5, 6, 0}, 2: {1, 2, 3, 4, 5, 6, 0}} {
		m.scheduleUI.mode = mode
		m.scheduleUI.focus = 0
		for _, f := range want {
			m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyTab})
			if m.scheduleUI.focus != f {
				t.Fatalf("mode %d: wanted %d got %d", mode, f, m.scheduleUI.focus)
			}
		}
	}
	m.scheduleUI.mode = 1
	m.scheduleUI.focus = 2
	for _, digit := range "12" {
		m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: digit, Text: string(digit)})
	}
	if m.scheduleUI.hours != 12 {
		t.Fatal("numeric hour entry failed")
	}
	m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyTab})
	m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: '5', Text: "5"})
	if m.scheduleUI.minutes != 5 {
		t.Fatal("numeric buffer carried between fields")
	}
}

func TestSchedulePillLastAndBothClickTargets(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.monitorContextDetail = ""
	m.monitorSessionData[1].working = false
	m.monitorSessionData[1].attention = codex.SessionAttentionComplete
	items := m.monitorAttentionSessions()
	if len(items) < 2 || !items[len(items)-1].trigger {
		t.Fatal("trigger not lowest priority")
	}
	g := m.monitorDashboardLayout()
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	foundPill, foundRow := false, false
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			if m.monitorContextAt(x, y) != "attention-trigger:root-one" {
				continue
			}
			n, _, handled := m.updateSchedule(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if !handled || !n.scheduleUI.open || n.scheduleUI.session != "root-one" {
				t.Fatal("trigger click did not open correct scheduler")
			}
			if y < g.meterY+a.topHeight+a.gap {
				foundPill = true
			} else {
				foundRow = true
			}
		}
	}
	if !foundPill || !foundRow {
		t.Fatal("missing click surface", foundPill, foundRow)
	}
}

func TestScheduleHeadingNamesSessionBeforeID(t *testing.T) {
	for _, tc := range []struct{ name, directory, want string }{
		{"Fix dashboard", "/work/project", "Fix dashboard"},
		{"", "/work/project", "project"},
		{"", "", "Unnamed session"},
	} {
		m, _ := scheduledTestModel(t)
		m.monitorSessionData[0].name = tc.name
		m.monitorSessionData[0].workingDirectory = tc.directory
		m.openSchedule()
		heading := strings.Split(ansi.Strip(m.renderScheduleForm()), "\n")[0]
		if !strings.Contains(heading, "SCHEDULE FOLLOW-UP // "+tc.want+" // "+shortSessionID("root-one")) {
			t.Fatalf("wrong heading: %q", heading)
		}
		m.width = 48
		m.monitorSessionData[0].name = strings.Repeat("Long name ", 20)
		heading = strings.Split(ansi.Strip(m.renderScheduleForm()), "\n")[0]
		if !strings.Contains(heading, " // "+shortSessionID("root-one")) {
			t.Fatalf("narrow heading lost ID: %q", heading)
		}
	}
}

func TestScheduleFrameFitsAndPreservesControlRows(t *testing.T) {
	for _, size := range [][2]int{{48, 24}, {80, 30}, {120, 45}} {
		m, _ := scheduledTestModel(t)
		m.width, m.height = size[0], size[1]
		m.openSchedule()
		for mode := range 3 {
			m.scheduleUI.mode = mode
			m.scheduleUI.notice = "Test notice"
			lines := strings.Split(ansi.Strip(m.renderScheduleForm()), "\n")
			if len(lines) != m.height {
				t.Fatalf("frame height %d, want %d", len(lines), m.height)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != m.width {
					t.Fatalf("frame width mismatch: %q", line)
				}
			}
			if !strings.HasPrefix(lines[0], "╭") || !strings.HasSuffix(lines[len(lines)-1], "╯") {
				t.Fatal("missing frame")
			}
			noticeRow := 19
			if mode != 2 {
				noticeRow = m.scheduleUI.actionRow() + 1
			}
			if !strings.Contains(lines[7], "AFTER QUOTA REFRESH") || !strings.Contains(lines[noticeRow], "Test notice") || !strings.Contains(lines[m.scheduleUI.actionRow()], "CONFIRM SCHEDULE") {
				t.Fatal("control rows moved")
			}
		}
	}
}

func TestDelayFieldsAndActionsStayTogether(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.openSchedule()
	m.scheduleUI.mode = 1
	lines := strings.Split(ansi.Strip(m.renderScheduleForm()), "\n")
	if !strings.Contains(lines[9], "Hours:") || !strings.Contains(lines[9], "Minutes:") || !strings.Contains(lines[13], "CONFIRM SCHEDULE") {
		t.Fatal("delay layout still reserves calendar space")
	}
	m, _, _ = m.updateSchedule(tea.MouseClickMsg{X: 23, Y: 9, Button: tea.MouseLeft})
	if m.scheduleUI.focus != 3 {
		t.Fatal("minutes click misaligned")
	}
	m, _, _ = m.updateSchedule(tea.MouseClickMsg{X: 3, Y: 9, Button: tea.MouseLeft})
	if m.scheduleUI.focus != 2 {
		t.Fatal("hours click misaligned")
	}
	m, _, _ = m.updateSchedule(tea.MouseClickMsg{X: 26, Y: 13, Button: tea.MouseLeft})
	if m.scheduleUI.open || !m.hasSchedule("root-one") {
		t.Fatal("Back target misaligned or deleted trigger")
	}
}

func TestDismissedSessionKeepsManageableTriggerPill(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.dismissMonitorSession("root-one")
	found := false
	for _, item := range m.monitorAttentionSessions() {
		if item.id == "root-one" && item.trigger {
			found = true
		}
	}
	if !found {
		t.Fatal("dismissal hid pending trigger")
	}
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			if m.monitorAttentionAt(x, y) != "attention-trigger:root-one" {
				continue
			}
			n, _, handled := m.updateSchedule(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if !handled || !n.scheduleUI.open || !n.monitorSessionVisible(n.monitorSessionData[0]) {
				t.Fatal("trigger pill cannot reopen dismissed session")
			}
			return
		}
	}
	t.Fatal("trigger pill not clickable")
}

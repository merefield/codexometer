package ui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
	"image"
	"strings"
	"testing"
	"time"
)

func TestScheduleFormRefinements(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.openSchedule()
	m.scheduleUI.mode, m.scheduleUI.focus = 1, 2
	m.scheduleUI.hours, m.scheduleUI.minutes = 2, 15
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.FixedZone("BST", 3600))
	if got := m.scheduleUI.targetTime(now); !got.Equal(now.Add(135 * time.Minute)) {
		t.Fatal("delay summary target differs from dispatch target")
	}
	lines := strings.Split(ansi.Strip(m.renderScheduleForm()), "\n")
	if !strings.Contains(lines[0], "EDIT SCHEDULED FOLLOW-UP") || !strings.Contains(lines[13], "SAVE CHANGES") || !strings.Contains(lines[11], "Will send:") || !strings.Contains(lines[6], "Digits/arrows") {
		t.Fatal("missing edit title, summary, action or contextual hint")
	}
	cells := uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(m.renderScheduleForm()).Draw(&cells, image.Rect(0, 0, m.width, m.height))
	for x := 2 + len("Hours: 2"); x < 22; x++ {
		cell := cells.CellAt(x, 9)
		if cell == nil || cell.Style.Attrs&uv.AttrReverse != 0 {
			t.Fatalf("hours highlight leaked into spacer at %d", x)
		}
	}
	for _, tc := range []struct {
		text                 string
		mode, hours, minutes int
		at                   time.Time
		want                 string
	}{
		{"", 0, 0, 0, now, "Enter a message"},
		{"Test", 1, 0, 0, now, "Choose a future"},
		{"Test", 2, 0, 0, now.Add(-time.Minute), "Choose a future"},
		{"Test", 2, 0, 0, now.AddDate(2, 0, 0), "within one year"},
		{"Test", 0, 0, 0, now, ""},
		{"Test", 1, 0, 1, now, ""},
	} {
		p := m.scheduleUI
		p.input.SetValue(tc.text)
		p.mode, p.hours, p.minutes, p.date = tc.mode, tc.hours, tc.minutes, tc.at
		if got := p.validation(now); (tc.want == "" && got != "") || (tc.want != "" && !strings.Contains(got, tc.want)) {
			t.Fatalf("validation = %q, want %q", got, tc.want)
		}
	}
	m.scheduleUI.input.SetValue("")
	m.scheduleUI.focus = 5
	before := m.scheduleUI.queue.List("root-one")[0].ID
	n, _, _ := m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !n.scheduleUI.open || n.scheduleUI.queue.List("root-one")[0].ID != before || !strings.Contains(n.renderScheduleForm(), "Enter a message") {
		t.Fatal("invalid save changed the job or omitted its explanation")
	}
}

func TestScheduleWaitingSummary(t *testing.T) {
	m, _ := scheduledTestModel(t)
	j := m.scheduleUI.queue.List("root-one")[0]
	now := time.Now()
	if got := m.triggerSummaryAt(j, now); !strings.Contains(got, "Due in") || strings.Contains(got, "waiting for") {
		t.Fatal("future job described as overdue", got)
	}
	j.At = now.Add(-time.Minute)
	m.snapshot.RateLimits.Primary.UsedPercent = 100
	if got := m.triggerSummaryAt(j, now); !strings.Contains(got, "Time reached // waiting for quota") {
		t.Fatal("missing quota wait", got)
	}
	m.snapshot.FetchedAt = now.Add(-10 * time.Minute)
	if got := m.triggerSummaryAt(j, now); !strings.Contains(got, "waiting for fresh quota") {
		t.Fatal("missing fresh quota wait", got)
	}
}

func TestScheduleCalendarCellStyles(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.openSchedule()
	m.scheduleUI.mode = 2
	// Exercise both sides of the selection, week boundaries and blank cells.
	for day := 1; day <= 31; day++ {
		m.scheduleUI.date = time.Date(2026, time.October, day, 12, 0, 0, 0, time.UTC)
		for focus := 0; focus <= 6; focus++ {
			m.scheduleUI.focus = focus
			cells := uv.NewScreenBuffer(m.width, m.height)
			uv.NewStyledString(m.renderScheduleForm()).Draw(&cells, image.Rect(0, 0, m.width, m.height))
			c := paletteFor(m.theme)
			for row := 0; row < 6; row++ {
				for col := 0; col < 7; col++ {
					date := row*7 + col - int(time.Thursday) + 1
					for offset := 0; offset < 4; offset++ {
						want := uv.Style{Fg: c.primary, Bg: c.background}
						if date == day && offset < 2 {
							if focus == 2 {
								want.Fg, want.Bg = c.background, c.primary
							} else {
								want.Underline = uv.UnderlineSingle
							}
						}
						cell := cells.CellAt(2+col*4+offset, 11+row)
						if cell == nil || !cell.Style.Equal(&want) {
							t.Fatalf("day %d focus %d row %d col %d offset %d: unexpected cell style: %+v, want %+v", day, focus, row, col, offset, cell, want)
						}
					}
				}
			}
		}
	}
}

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
				if i == selected {
					style = style.Foreground(colors.primary).Underline(true)
					if focus == 1 {
						style = style.Underline(false).Foreground(colors.background).Background(colors.primary)
					}
				}
				expected = append(expected, style.Render(label))
			}
			if row != strings.Join(expected, " ") {
				t.Fatalf("focus %d selected %d changed the group styling: %q", focus, selected, row)
			}
		}
	}
}

func TestScheduleTriggerSelectionSurvivesTab(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		for mode := 0; mode < 3; mode++ {
			m, _ := scheduledTestModel(t)
			m.theme = theme
			m.openSchedule()
			m.scheduleUI.mode, m.scheduleUI.focus = mode, 1
			focused := m.scheduleUI.renderTriggerModes(paletteFor(theme))
			m, _, _ = m.updateSchedule(tea.KeyPressMsg{Code: tea.KeyTab})
			if m.scheduleUI.mode != mode || m.scheduleUI.focus == 1 {
				t.Fatal("Tab changed selection or failed to move focus")
			}
			unfocused := m.scheduleUI.renderTriggerModes(paletteFor(theme))
			if focused == unfocused || ansi.Strip(focused) != ansi.Strip(unfocused) {
				t.Fatal("focus must change appearance without moving buttons")
			}
			cells := uv.NewScreenBuffer(m.width, m.height)
			uv.NewStyledString(m.renderScheduleForm()).Draw(&cells, image.Rect(0, 0, m.width, m.height))
			// Check actual rendered cells, not just the styling helper's output.
			x := 2
			for i, label := range []string{"[ AFTER QUOTA REFRESH ]", "[ IN… ]  ", "[ AT… ]  "} {
				cell := cells.CellAt(x+2, 7)
				if cell == nil || (cell.Style.Underline == uv.UnderlineSingle) != (i == mode) || cell.Style.Attrs&uv.AttrReverse != 0 {
					t.Fatalf("theme %d mode %d button %d: selection lost after Tab", theme, mode, i)
				}
				x += ansi.StringWidth(label) + 1
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
			if !handled || n.scheduleUI.open || n.monitorContextDetail != "root-one" {
				t.Fatal("trigger click did not open correct session detail")
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
		m.scheduleUI.id = "" // Exercise the new-schedule title below.
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
			if !strings.Contains(lines[7], "AFTER QUOTA REFRESH") || !strings.Contains(lines[noticeRow], "Test notice") || !strings.Contains(lines[m.scheduleUI.actionRow()], "SAVE CHANGES") {
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
	if !strings.Contains(lines[9], "Hours:") || !strings.Contains(lines[9], "Minutes:") || !strings.Contains(lines[13], "SAVE CHANGES") {
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

func TestScheduleQuitDoesNotStealComposerText(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.openSchedule()
	m.scheduleUI.input.SetValue("")
	m, cmd, handled := m.updateSchedule(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !handled || m.scheduleUI.input.Value() != "q" {
		t.Fatal("q did not type into composer")
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q quit from composer")
		}
	}
	for focus := 1; focus <= 6; focus++ {
		m.scheduleUI.focus = focus
		_, cmd, handled = m.updateSchedule(tea.KeyPressMsg{Code: 'q', Text: "q"})
		if !handled || cmd == nil {
			t.Fatalf("q not handled at field %d", focus)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("q did not quit at field %d", focus)
		}
	}
	m.scheduleUI.focus = 0
	_, cmd, _ = m.updateSchedule(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+C did not quit from composer")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("Ctrl+C returned wrong command")
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
			if !handled || n.scheduleUI.open || n.monitorContextDetail != "root-one" || !n.monitorSessionVisible(n.monitorSessionData[0]) {
				t.Fatal("trigger pill cannot reopen dismissed session")
			}
			return
		}
	}
	t.Fatal("trigger pill not clickable")
}

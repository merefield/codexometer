package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestScheduleToggleRenderedHoverAndClicks(t *testing.T) {
	for _, width := range []int{60, 100, 160} {
		m, _ := promptTestModel()
		m.width = width
		m.monitorSessionData[0].name = strings.Repeat("Long session 日本語 ", 8)
		for _, open := range []bool{false, true} {
			label := "[ Ctrl+S: SCHEDULE ]"
			if open {
				label = "[ Ctrl+S: CLOSE SCHEDULE ]"
			}
			rendered := strings.Split(m.render(), "\n")
			x, y := -1, -1
			for row, line := range rendered {
				plain := ansi.Strip(line)
				if index := strings.Index(plain, label); index >= 0 {
					x, y = ansi.StringWidth(plain[:index]), row
					break
				}
			}
			if x < 0 {
				t.Fatalf("width %d missing complete button %q", width, label)
			}
			for column := x; column < x+ansi.StringWidth(label); column++ {
				if !m.scheduleToggleAt(column, y) {
					t.Fatalf("button cell not clickable: %d,%d", column, y)
				}
			}
			if m.scheduleToggleAt(x-1, y) || m.scheduleToggleAt(x+ansi.StringWidth(label), y) || m.scheduleToggleAt(x, y+1) {
				t.Fatal("hit surface leaked outside button")
			}
			next, _ := m.Update(tea.MouseMotionMsg{X: x, Y: y})
			m = next.(Model)
			if !m.scheduleUI.toggleHover {
				t.Fatal("hover not retained through Update")
			}
			hover := strings.Split(m.render(), "\n")[y]
			if hover == rendered[y] || ansi.Strip(hover) != ansi.Strip(rendered[y]) {
				t.Fatal("hover missing or moved heading")
			}
			next, _ = m.Update(tea.MouseMotionMsg{X: 0, Y: 0})
			m = next.(Model)
			if m.scheduleUI.toggleHover {
				t.Fatal("hover did not clear")
			}
			next, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			m = next.(Model)
			if m.scheduleUI.open == open {
				t.Fatalf("%q did not toggle", label)
			}
		}
	}
}

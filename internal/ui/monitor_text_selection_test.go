package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestMonitorOutputDragAcrossDetailModes(t *testing.T) {
	for mode, label := range map[int]string{contextSplit: "split", contextWide: "wide", contextFull: "full"} {
		t.Run(label, func(t *testing.T) {
			m := contextTestModel()
			const id = "root-one"
			m.monitorSessionData = m.monitorSessionData[:1]
			m.monitorSessionData[0].preview.Text = "Select alpha\nBeta ends here"
			m.setRowContext(id, mode)
			firstX, firstY := renderedTextStart(t, m, "alpha")
			lastX, lastY := renderedTextStart(t, m, "Beta ends here")
			// Select bottom to top, exercising the reverse endpoint order.
			next, command := m.Update(tea.MouseClickMsg{X: lastX + 3, Y: lastY, Button: tea.MouseLeft})
			if command != nil || next.(Model).rowContextMode(id) != mode {
				t.Fatal("press navigated instead of waiting for a drag")
			}
			next, _ = next.Update(tea.MouseMotionMsg{X: firstX, Y: firstY, Button: tea.MouseLeft})
			n := next.(Model)
			preview := n.View().Content
			if !strings.Contains(preview, "\x1b[7m") || ansi.Strip(preview) != ansi.Strip(m.View().Content) {
				t.Fatal("drag should highlight without altering the displayed text")
			}
			next, command = n.Update(tea.MouseReleaseMsg{X: firstX, Y: firstY, Button: tea.MouseLeft})
			n = next.(Model)
			if !commandCopiesText(command, "alpha\nBeta") || n.rowContextMode(id) != mode || n.monitorSelectionCopied != 9 {
				t.Fatal("release did not copy only the selection or changed detail mode")
			}
			if view := n.View().Content; !strings.Contains(ansi.Strip(view), "Copied 9 chars") || !strings.Contains(view, "\x1b[7m") {
				t.Fatal("missing selection flash or copy receipt")
			}
			next, _ = n.Update(monitorSelectionExpired(n.monitorSelectionSequence - 1))
			next, _ = next.Update(monitorCopyReceiptExpired(n.monitorSelectionSequence - 1))
			if updated := next.(Model); updated.monitorSelection == nil || updated.monitorSelectionCopied != 9 {
				t.Fatal("an old timer cleared the new copy highlight or receipt")
			}
			next, _ = next.Update(monitorSelectionExpired(n.monitorSelectionSequence))
			if updated := next.(Model); updated.monitorSelection != nil || !strings.Contains(ansi.Strip(updated.View().Content), "Copied 9 chars") {
				t.Fatal("the receipt should outlast the selection flash")
			}
			next, _ = next.Update(monitorCopyReceiptExpired(n.monitorSelectionSequence))
			if strings.Contains(ansi.Strip(next.(Model).View().Content), "Copied 9 chars") {
				t.Fatal("copy receipt did not clear")
			}
		})
	}
}

func TestMonitorCopyReceiptCentresWithoutDisplacingFooterButtons(t *testing.T) {
	const receipt = "Copied 123 chars"
	for _, tc := range []struct{ width, row int }{{36, 0}, {56, 0}, {80, 1}, {121, 1}} {
		m := contextTestModel()
		m.width = tc.width + 4
		colors := paletteFor(m.theme)
		before := strings.Split(ansi.Strip(m.renderFooter(tc.width, colors)), "\n")
		m.monitorSelectionCopied = 123
		got := strings.Split(ansi.Strip(m.renderFooter(tc.width, colors)), "\n")
		row := got[tc.row]
		index := strings.Index(row, receipt)
		if index < 0 || index != (tc.width-ansi.StringWidth(receipt))/2 || ansi.StringWidth(row) > tc.width {
			t.Fatalf("width %d: receipt not centred on row %d: %q", tc.width, tc.row, got)
		}
		if got[1-tc.row] != before[1-tc.row] {
			t.Fatalf("width %d: unaffected footer row changed: %q -> %q", tc.width, before, got)
		}
		buttons, separator := footerButtonLayoutWithTheme(tc.width, colors.name, viewMonitor)
		y, x := m.dashboardLayout().footerY+1, 0
		for _, b := range buttons {
			if !strings.HasPrefix(got[1][x:], b.label) || m.footerButtonAt(2+x, y) != b.id {
				t.Fatalf("width %d: footer button %q moved or lost its click target", tc.width, b.label)
			}
			x += ansi.StringWidth(b.label) + len(separator)
		}
		if tc.row == 1 && (!strings.HasSuffix(got[1], "THEME // "+colors.name) || m.footerButtonAt(2+index, y) != footerButtonNone) {
			t.Fatal("badge displaced the theme or gained a click target")
		}
	}
}

func TestMonitorOutputSelectionCopiesWholeGraphemesAndStripsStyles(t *testing.T) {
	rect := monitorRect{x: 2, y: 1, width: 8, height: 3}
	rows := monitorSelectionRows("ignored\n│ \x1b[31mAé界Z\x1b[0m   │\n│         │\n│ Done    │", rect)
	press := tea.MouseClickMsg{X: 3, Y: 1, Button: tea.MouseLeft}
	s := monitorTextSelection{press: press, rect: rect, last: 2, x: press.X, y: press.Y}
	s = s.move(5, 1) // the second terminal cell of 界
	if got, count := s.text(rows); got != "é界" || count != 2 {
		t.Fatalf("unicode selection = %q (%d chars)", got, count)
	}
	s = s.move(3, 3)
	if got, count := s.text(rows); got != "é界Z\n\nDo" || count != 5 {
		t.Fatalf("multiline selection = %q (%d chars)", got, count)
	}
}

func TestMonitorOutputDragDoesNotNavigateOrCopyStaleOrOtherPaneContent(t *testing.T) {
	m := contextTestModel()
	const id = "root-one"
	m.setRowContext(id, contextWide)
	m.monitorSessionData[0].preview.Text = "Alpha begins here"
	m.monitorSessionData[1].preview.Text = "Unrelated second row"
	m.setRowContext("root-two", contextWide)
	x, y := renderedTextStart(t, m, "Alpha begins here")
	press := tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
	release := tea.MouseReleaseMsg{X: x + 4, Y: y, Button: tea.MouseLeft}
	next, _ := m.Update(press)
	changed := next.(Model)
	changed.monitorSessionData = append([]monitorSession(nil), changed.monitorSessionData...)
	changed.monitorSessionData[0].preview.Text = "Freshly streamed text"
	if state, command := changed.Update(release); command != nil || state.(Model).monitorSelection != nil {
		t.Fatal("changed output was copied")
	}
	next, _ = m.Update(press)
	_, otherRowY := renderedTextStart(t, m, "Unrelated second row")
	other := tea.MouseReleaseMsg{X: x + 6, Y: otherRowY, Button: tea.MouseLeft}
	next, command := next.Update(other)
	n := next.(Model)
	s := n.monitorSelection
	if !commandCopiesText(command, "Alpha begins here") || s == nil || s.y != y || n.monitorSelectedID != m.monitorSelectedID {
		t.Fatal("drag did not stay inside its origin pane")
	}
}

func TestMonitorOutputStationaryClickKeepsNavigationAndComposerClick(t *testing.T) {
	m := contextTestModel()
	m.setRowContext("root-one", contextWide)
	x, y := renderedTextStart(t, m, "Finished updating")
	next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	next, _ = next.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	n := next.(Model)
	if n.rowContextMode("root-one") != contextSplit || n.monitorSelection != nil || n.monitorSelectionCopied != 0 {
		t.Fatal("stationary click did not perform the normal wide-to-split action")
	}

	m, _ = promptTestModel()
	g := m.monitorDashboardLayout()
	controls := m.layoutDetailControls(g.contentWidth, g.meterHeight)
	if controls.rows == 0 {
		t.Fatal("fixture needs a visible composer")
	}
	composerX, composerY := renderedTextStart(t, m, "Click here")
	next, _ = m.Update(tea.MouseClickMsg{X: composerX, Y: composerY, Button: tea.MouseLeft})
	if n := next.(Model); n.monitorSelection != nil || !n.monitorPrompt.input.Focused() {
		t.Fatal("composer did not retain immediate click-to-focus")
	}
}

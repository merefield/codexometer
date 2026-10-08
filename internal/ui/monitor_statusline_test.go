package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestStatusLineMultiSelectReorderCancelAndPersist(t *testing.T) {
	m := completedCopyModel()
	m.setRowContext("root-one", contextFull)
	store := &memoryPreferenceStore{}
	m.preferenceStore = store
	m, _, _ = m.openMonitorCommands("/statusline")
	if !m.monitorCommands.menu.Multiple {
		t.Fatal("not a multi-select picker")
	}
	before := m.monitorCommands.menu.Choices[0].Selected
	n, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = n.(Model)
	if m.monitorCommands.menu.Choices[0].Selected == before {
		t.Fatal("Space did not toggle")
	}
	n, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = n.(Model)
	if m.monitorCommands.selected != 1 || m.monitorCommands.menu.Choices[1].ID != "model-with-reasoning" {
		t.Fatal("Right did not reorder")
	}
	n, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = n.(Model)
	if len(store.saves) != 0 || m.monitorStatusLine != nil {
		t.Fatal("Cancel changed preferences")
	}
	m, _, _ = m.openMonitorCommands("statusline")
	x, y := renderedTextStart(t, m, "[x] Tokens")
	n, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = n.(Model)
	for _, o := range m.monitorCommands.menu.Choices {
		if o.ID == "used-tokens" && o.Selected {
			t.Fatal("checkbox click missed")
		}
	}
	x, y = renderedTextStart(t, m, "[ C APPLY ]")
	n, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = n.(Model)
	if m.monitorCommands.open || len(store.saves) != 1 {
		t.Fatal("Apply did not save and close")
	}
	if !reflect.DeepEqual(m.monitorStatusLine, []string{"model-with-reasoning", "fast-mode", "current-dir"}) {
		t.Fatal(m.monitorStatusLine)
	}
	fresh := Model{}
	fresh.applyPreferences(store.preferences)
	if !reflect.DeepEqual(fresh.monitorStatusLine, m.monitorStatusLine) {
		t.Fatal("preference not restored")
	}
}

func TestStatusLineFooterPreservesCopyGeometry(t *testing.T) {
	for _, width := range []int{24, 60, 120, 180} {
		m := completedCopyModel()
		m.width = width
		m.setRowContext("root-one", contextFull)
		m.monitorSessionData[0].modelSettings = codex.SessionModelSettings{Model: "model", ReasoningEffort: "high", ServiceTier: "fast"}
		m.monitorSessionData[0].latest = 1200
		m.monitorSessionData[0].workingDirectory = "/work\nproject"
		g := m.monitorDashboardLayout()
		colors := paletteFor(m.theme)
		panel := m.renderMonitorContextDetail(g.contentWidth, g.meterHeight, colors)
		lines := strings.Split(ansi.Strip(panel), "\n")
		if len(lines) != g.meterHeight {
			t.Fatalf("width %d changed frame height: %d vs %d", width, len(lines), g.meterHeight)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > g.contentWidth {
				t.Fatalf("footer overflow at width %d", width)
			}
		}
		if width >= 60 && !strings.Contains(lines[len(lines)-1], "model high") {
			t.Fatal("footer not at bottom", lines[len(lines)-1])
		}
		if strings.Contains(ansi.Strip(m.render()), benchmarkDetailCopyLabel) {
			x, y := renderedTextStart(t, m, benchmarkDetailCopyLabel)
			if m.monitorContextAt(x, y) != "copy:root-one" {
				t.Fatal("footer moved Copy target")
			}
		}
		m.monitorStatusLine = []string{}
		if strings.Contains(ansi.Strip(m.renderMonitorContextDetail(g.contentWidth, g.meterHeight, colors)), "model high") {
			t.Fatal("empty selection retained footer")
		}
	}
}

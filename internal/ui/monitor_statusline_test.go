package ui

import (
	"image"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
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

func TestStatusLineReservesFooterGapOnlyWhenDisplayed(t *testing.T) {
	for _, size := range [][2]int{{60, 12}, {60, 16}, {60, 24}, {120, 24}, {180, 65}} {
		for _, scenario := range []string{"shown", "hidden", "missing", "commands", "scheduler", "queue", "wide"} {
			if scenario == "wide" && size[1] < 24 {
				continue // overview rows require more height than full detail
			}
			m := completedCopyModel()
			m.width, m.height = size[0], size[1]
			m.setRowContext("root-one", contextFull)
			m.monitorSessionData[0].modelSettings = codex.SessionModelSettings{Model: "model", ReasoningEffort: "high"}
			baseline := m
			baseline.monitorStatusLine = []string{}
			base := baseline.dashboardLayout()
			switch scenario {
			case "hidden":
				m.monitorStatusLine = []string{}
			case "missing":
				m.monitorStatusLine = []string{"fast-mode"}
			case "commands":
				m.monitorCommands.open = true
			case "scheduler":
				m.scheduleUI.open = true
			case "queue":
				m.monitorQueue.open = true
			case "wide":
				m.setRowContext("root-one", contextWide)
			}
			g := m.dashboardLayout()
			wantGap := (scenario == "shown" || scenario == "scheduler") && base.meterHeight >= 9
			if g.footerSpacer != wantGap || g.footerY != base.footerY {
				t.Fatalf("%v %s: incorrect footer geometry: %+v", size, scenario, g)
			}
			// Modal editors have their own setup and rendering tests; the
			// scheduler retains the status line to keep its composer anchored.
			if scenario == "commands" || scenario == "scheduler" || scenario == "queue" {
				continue
			}
			view := ansi.Strip(m.render())
			lines := strings.Split(view, "\n")
			if lipgloss.Height(view) > m.height {
				t.Fatalf("%v %s: footer gap overflowed terminal", size, scenario)
			}
			if wantGap && (strings.TrimSpace(lines[g.footerY-1]) != "" || !strings.Contains(lines[g.footerY-2], "model high")) {
				t.Fatalf("%v: status line missing its separating blank row", size)
			}
			if !wantGap && strings.Contains(lines[g.footerY-1], "model high") {
				t.Fatal("status line shown without space for its gap")
			}
			x, y := renderedTextStart(t, m, "[ (T)HEME ]")
			if m.footerButtonAt(x, y) != footerButtonTheme {
				t.Fatalf("%v %s: footer gap moved the theme click target (%d,%d) expected row %d", size, scenario, x, y, g.footerY+1)
			}
		}
	}
}

func TestStatusLineAndComposerTipEmphasis(t *testing.T) {
	m, _ := promptTestModel()
	m.width, m.height = 120, 40
	m.monitorSessionData[0].modelSettings.Model = "my-model"
	m.focusMonitorPrompt()
	colors := paletteFor(m.theme)
	foreground := func(text string) any {
		cells := uv.NewScreenBuffer(120, 1)
		uv.NewStyledString(text).Draw(&cells, image.Rect(0, 0, 120, 1))
		return cells.CellAt(0, 0).Style.Fg
	}
	g := m.monitorDashboardLayout()
	panel := m.renderMonitorContextDetail(g.contentWidth, g.meterHeight, colors)
	lines := strings.Split(panel, "\n")
	// Skip the left border cell; the status-line colour starts with its padding.
	if !reflect.DeepEqual(foreground(ansi.Cut(lines[len(lines)-1], 1, 2)), foreground(colors.label().Foreground(colors.primary).Render("x"))) {
		t.Fatal("status line remains subdued")
	}
	for _, notice := range []string{"", "Change requested."} {
		m.monitorPrompt.notice = notice
		prompt := strings.Split(m.renderMonitorPrompt(g.contentWidth, g.meterHeight, colors), "\n")
		want := colors.dimmed().Render("x")
		if notice != "" {
			want = colors.label().Render("x")
		}
		if !reflect.DeepEqual(foreground(prompt[len(prompt)-1]), foreground(want)) {
			t.Fatal("tips must be subdued while notices retain emphasis")
		}
	}
}

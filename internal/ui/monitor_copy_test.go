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

func completedCopyModel() Model {
	m := contextTestModel()
	m.monitorSessionData[0].working = false
	m.monitorSessionData[0].attention = codex.SessionAttentionComplete
	m.monitorSessionData[0].preview.Text = strings.Repeat("reply line\n", 100) + "LAST OUTPUT LINE"
	return m
}

func TestMonitorCopySurfaces(t *testing.T) {
	for _, mode := range []int{contextSplit, contextWide, contextFull} {
		for _, width := range []int{80, 120, 180} {
			m := completedCopyModel()
			m.width = width
			m.setRowContext("root-one", mode)
			if !strings.Contains(ansi.Strip(m.render()), benchmarkDetailCopyLabel) {
				if mode != contextSplit {
					t.Fatal("copy missing from expanded/full detail")
				}
				continue // narrow split boxes deliberately omit the button
			}
			x, y := renderedTextStart(t, m, benchmarkDetailCopyLabel)
			for dx := 0; dx < lipgloss.Width(benchmarkDetailCopyLabel); dx++ {
				if got := m.monitorContextAt(x+dx, y); got != "copy:root-one" {
					t.Fatalf("mode %v width %d click %d = %q", mode, width, dx, got)
				}
			}
			_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if cmd == nil {
				t.Fatal("click did not copy")
			}
			if !reflect.DeepEqual(cmd(), tea.SetClipboard(m.monitorCopyText("root-one"))()) {
				t.Fatal("clipboard command has wrong payload")
			}
			_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
			if cmd == nil {
				t.Fatal("shortcut did not copy")
			}
			if !strings.HasSuffix(m.monitorCopyText("root-one"), "LAST OUTPUT LINE") {
				t.Fatal("offscreen output omitted")
			}
		}
	}
}

func TestMonitorCopyUsesSelectedSession(t *testing.T) {
	m := completedCopyModel()
	m.setRowContext("root-one", contextWide)
	m.monitorSessionData[1].working = false
	m.monitorSessionData[1].attention = codex.SessionAttentionComplete
	m.setRowContext("root-two", contextWide)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd == nil || !reflect.DeepEqual(cmd(), tea.SetClipboard(m.monitorCopyText("root-two"))()) {
		t.Fatal("shortcut copied a different session")
	}
	m.monitorSessionData[1].working = true
	if m.copyMonitorReply("root-two") != nil {
		t.Fatal("resumed session still offers copy")
	}
}

func TestMonitorCopyEligibilityAndComposer(t *testing.T) {
	m := completedCopyModel()
	m.setRowContext("root-one", contextFull)
	for _, state := range []codex.SessionAttention{codex.SessionAttentionInput, codex.SessionAttentionApproval, codex.SessionAttentionCheck} {
		m.monitorSessionData[0].attention = state
		if m.copyMonitorReply("root-one") != nil {
			t.Fatal("non-complete state offered copy")
		}
	}
	m, _ = promptTestModel()
	m.monitorSessionData[0].working = false
	m.monitorSessionData[0].attention = codex.SessionAttentionComplete
	x, y := renderedTextStart(t, m, benchmarkDetailCopyLabel)
	if got := m.monitorContextAt(x, y); got != "copy:root-one" {
		t.Fatalf("composer stole copy: %s", got)
	}
	m, _ = promptKey(m, tea.KeyEnter, "")
	m, _ = promptKey(m, 'c', "c")
	if m.monitorPrompt.input.Value() != "c" {
		t.Fatal("copy stole typed c")
	}
}

func TestMonitorCompletedReplyScroll(t *testing.T) {
	m := completedCopyModel()
	m.setRowContext("root-one", contextFull)
	for _, key := range []rune{tea.KeyDown, tea.KeyPgDown} {
		n, _ := m.Update(tea.KeyPressMsg{Code: key})
		if n.(Model).monitorContextScroll <= m.monitorContextScroll {
			t.Fatal("key did not scroll")
		}
		m = n.(Model)
	}
	g := m.monitorDashboardLayout()
	n, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: g.meterY + 2, Button: tea.MouseWheelDown})
	if n.(Model).monitorContextScroll <= m.monitorContextScroll {
		t.Fatal("wheel did not scroll")
	}
	m = n.(Model)
	for _, key := range []rune{tea.KeyUp, tea.KeyPgUp} {
		n, _ := m.Update(tea.KeyPressMsg{Code: key})
		if n.(Model).monitorContextScroll >= m.monitorContextScroll {
			t.Fatal("key did not scroll back")
		}
		m = n.(Model)
	}
	m.scrollMonitorContext(10000)
	if !strings.Contains(ansi.Strip(m.render()), "LAST OUTPUT LINE") {
		t.Fatal("last output unreachable")
	}
	m.scrollMonitorContext(-10000)
	if m.monitorContextScroll != 0 {
		t.Fatal("cannot return to start")
	}
}

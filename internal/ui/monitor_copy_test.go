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
	raw := "\x1b[31mReply\x1b[0m\x07\n" + strings.Repeat("reply line\n", 100) + "LAST OUTPUT LINE"
	want := codex.SanitizeSessionContext(raw)
	for _, mode := range []int{contextSplit, contextWide, contextFull} {
		for _, width := range []int{80, 120, 180} {
			m := completedCopyModel()
			m.monitorSessionData[0].preview.Text = raw
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
			if !monitorCopyCommandMatches(cmd, want) {
				t.Fatal("clipboard command has wrong payload")
			}
			_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
			if !monitorCopyCommandMatches(cmd, want) {
				t.Fatal("shortcut did not copy the entire sanitized reply")
			}
		}
	}
}

func TestMonitorCopyRequiresVisibleDetail(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		m := completedCopyModel()
		m.setRowContext("root-one", contextGraph)
		m.monitorContextHidden = hidden
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
		if cmd != nil {
			t.Fatal("graph-only/hidden detail unexpectedly copied a reply")
		}
	}
}

func TestMonitorCopyUsesSelectedSession(t *testing.T) {
	m := completedCopyModel()
	m.setRowContext("root-one", contextWide)
	m.monitorSessionData[1].working = false
	m.monitorSessionData[1].attention = codex.SessionAttentionComplete
	m.setRowContext("root-two", contextWide)
	want := codex.SanitizeSessionContext(m.monitorSessionData[1].preview.Text)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if !monitorCopyCommandMatches(cmd, want) {
		t.Fatal("shortcut copied a different session")
	}
	m.monitorSessionData[1].working = true
	if m.copyMonitorReply("root-two") == nil {
		t.Fatal("resumed session should still offer its visible reply")
	}
}

func TestMonitorCopyEligibilityAndComposer(t *testing.T) {
	m := completedCopyModel()
	m.setRowContext("root-one", contextFull)
	for _, kind := range []codex.SessionContextKind{codex.SessionContextNone, codex.SessionContextQuestion, codex.SessionContextApproval} {
		m.monitorSessionData[0].preview.Kind = kind
		if m.copyMonitorReply("root-one") != nil {
			t.Fatal("non-prose context offered copy")
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

func TestMonitorCopyDuringWork(t *testing.T) {
	for _, kind := range []codex.SessionContextKind{codex.SessionContextActivity, codex.SessionContextReply} {
		m := completedCopyModel()
		m.monitorSessionData[0].working = true
		m.monitorSessionData[0].attention = codex.SessionAttentionNone
		m.monitorSessionData[0].preview.Kind = kind
		m.setRowContext("root-one", contextFull)
		want := codex.SanitizeSessionContext(m.monitorSessionData[0].preview.Text)
		x, y := renderedTextStart(t, m, benchmarkDetailCopyLabel)
		_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if !monitorCopyCommandMatches(cmd, want) {
			t.Fatal("working prose was not copied")
		}
		m.monitorSessionData[0].preview.Text = "New activity"
		if !monitorCopyCommandMatches(cmd, want) {
			t.Fatal("clipboard snapshot changed after activation")
		}
	}
	m := approvalTestModel()
	n, _ := m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m = n.(Model)
	n, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd == nil || !n.(Model).monitorApprovalBusy || n.(Model).monitorCopyFlash != "" {
		t.Fatal("copy interfered with approval confirmation")
	}
}

func TestMonitorCopyRecoveredIdleReply(t *testing.T) {
	for _, mode := range []int{contextSplit, contextWide, contextFull} {
		m := completedCopyModel()
		m.monitorSessionData[0].attention = codex.SessionAttentionNone
		m.setRowContext("root-one", mode)
		want := codex.SanitizeSessionContext(m.monitorSessionData[0].preview.Text)
		x, y := renderedTextStart(t, m, benchmarkDetailCopyLabel)
		if got := m.monitorContextAt(x, y); got != "copy:root-one" {
			t.Fatalf("recovered reply copy target = %q", got)
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
		if !monitorCopyCommandMatches(cmd, want) {
			t.Fatal("recovered reply could not be copied without a fresh completion")
		}
	}
}

func monitorCopyCommandMatches(cmd tea.Cmd, text string) bool {
	if cmd == nil {
		return false
	}
	commands, ok := cmd().(tea.BatchMsg)
	return ok && len(commands) == 2 && reflect.DeepEqual(commands[0](), tea.SetClipboard(text)())
}

func TestMonitorCopyHighlight(t *testing.T) {
	m := completedCopyModel()
	m.setRowContext("root-one", contextFull)
	colors := paletteFor(m.theme)
	idle := m.renderMonitorCopy(80, "root-one", colors)
	if idle != colors.label().Foreground(colors.primary).Render(benchmarkDetailCopyLabel) {
		t.Fatal("usable copy should match the panel border")
	}
	m.monitorContextHover = "copy:root-one"
	hover := m.renderMonitorCopy(80, "root-one", colors)
	if hover == idle {
		t.Fatal("hover did not highlight copy")
	}
	m.monitorContextHover = ""
	n, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = n.(Model)
	if cmd == nil || m.renderMonitorCopy(80, "root-one", colors) != hover {
		t.Fatal("copy did not flash")
	}
	n, _ = m.Update(monitorCopyFlashExpiredMsg{sequence: m.monitorCopySequence - 1})
	m = n.(Model)
	if m.monitorCopyFlash == "" {
		t.Fatal("stale expiry cleared flash")
	}
	n, _ = m.Update(monitorCopyFlashExpiredMsg{sequence: m.monitorCopySequence})
	m = n.(Model)
	if m.renderMonitorCopy(80, "root-one", colors) != idle {
		t.Fatal("copy flash did not expire")
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

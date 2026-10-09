package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestLongApprovalDetailFullScreen(t *testing.T) {
	for _, size := range [][2]int{{60, 24}, {80, 30}, {120, 30}, {180, 40}} {
		for _, history := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/history=%t", size[0], size[1], history), func(t *testing.T) {
				m := approvalTestModel()
				m.width, m.height = size[0], size[1]
				c := &m.monitorSessionData[0].preview
				c.CurrentTask = strings.Repeat("Long task details with wrapping. ", 30)
				c.ApprovalContext = strings.Repeat("Prior commentary with wrapping. ", 30)
				c.Text = "Review the full command"
				c.CommandDetails = codex.ApprovalCommandDetails{Justification: strings.Repeat("Long explanation. ", 400) + "END OF JUSTIFICATION", Command: strings.Repeat("echo long-command\n", 400) + "END OF COMMAND", Directory: "/work"}
				if history {
					m.monitorHistory = map[string]monitorTurnHistory{m.monitorContextDetail: {turns: []codex.SessionContext{{ThreadID: m.monitorContextDetail, TurnID: "old", Kind: codex.SessionContextReply, Text: "previous reply", At: time.Now().Add(-time.Minute)}}}}
				}
				for _, offset := range []int{0, 10000} {
					m.scrollMonitorContext(offset)
					plain := ansi.Strip(m.render())
					g := m.monitorDashboardLayout()
					if lipgloss.Height(plain) > m.height || lipgloss.Width(plain) > m.width {
						t.Fatalf("render overflow %dx%d", lipgloss.Width(plain), lipgloss.Height(plain))
					}
					lines := strings.Split(plain, "\n")
					buttons := m.monitorApprovalButtons(g.contentWidth, g.meterHeight)
					if len(buttons) == 0 {
						t.Fatal("missing controls")
					}
					for _, b := range buttons {
						found := false
						for y, line := range lines {
							if x := strings.Index(line, b.label); x >= 0 {
								found = true
								if got := m.monitorContextAt(lipgloss.Width(line[:x]), y); got != b.action {
									t.Errorf("button hit %q expected %q", got, b.action)
								}
							}
						}
						if !found {
							t.Errorf("button clipped: %s", b.label)
						}
					}
					if offset > 0 && !strings.Contains(plain, "END OF COMMAND") {
						t.Fatal("last text row inaccessible")
					}
				}
			})
		}
	}
}

func TestBlockedApprovalReasonStaysVisible(t *testing.T) {
	m := approvalTestModel()
	m.monitorSessionData[0].preview.Text = strings.Repeat("request detail\n", 200)
	m.monitorSessionData[0].preview.ApprovalToken = ""
	m.monitorSessionData[0].preview.ApprovalBlocked = "truncated"
	for _, key := range []string{"home", "end"} {
		m, _, _ = m.updateMonitorContextKey(key)
		plain := ansi.Strip(m.render())
		if !strings.Contains(plain, "REPLY IN CODEX") || !strings.Contains(plain, "Request details exceed the display limit.") {
			t.Fatal("blocked reason scrolled away")
		}
		if key == "end" && m.monitorContextScroll != m.monitorContextScrollLimit() {
			t.Fatal("End did not reach final line")
		}
	}
	m, _, _ = m.updateMonitorContextKey("home")
	if m.monitorContextScroll != 0 {
		t.Fatal("Home did not return to the start")
	}
	// Mouse wheel events must reach the same bottom as End.
	g := m.monitorDashboardLayout()
	for i := 0; i < 100; i++ {
		next, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{X: 10, Y: g.meterY + 2, Button: tea.MouseWheelDown}))
		m = next.(Model)
	}
	if m.monitorContextScroll != m.monitorContextScrollLimit() {
		t.Fatal("wheel cannot reach final line")
	}
}

package ui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func attentionTestModel() Model {
	m := contextTestModel()
	m.monitorSessionData[0].attention = codex.SessionAttentionApproval
	m.monitorSessionData[1].attention = codex.SessionAttentionInput
	for i := range m.monitorSessionData {
		m.monitorSessionData[i].workingDirectory = fmt.Sprintf("/work/project-%d", i+1)
	}
	return m
}

func TestMonitorSummaryCountersAndObservationState(t *testing.T) {
	m := attentionTestModel()
	m.monitorBaseline, m.monitorLatest = 100, 725
	m.monitorStartedAt = time.Now().Add(-time.Minute)
	m.monitorSessionData[0].agentCount = 8
	lines := m.monitorSummaryLines(120, 2, paletteFor(themeHacker))
	values := strings.Fields(ansi.Strip(lines[1]))
	if got := strings.Join(values, ","); got != "625,3,1,1,1,0" {
		t.Fatalf("counts %s", got)
	}
	m.monitorSessionData[2].attention = codex.SessionAttentionCheck
	values = strings.Fields(ansi.Strip(m.monitorSummaryLines(120, 2, paletteFor(themeHacker))[1]))
	if strings.Join(values, ",") != "625,3,0,1,1,1" {
		t.Fatal(values)
	}
	m.monitorDismissed = map[string]monitorSessionDismissal{"root-one": {}}
	values = strings.Fields(ansi.Strip(m.monitorSummaryLines(120, 2, paletteFor(themeHacker))[1]))
	if strings.Join(values, ",") != "625,2,0,0,1,1" {
		t.Fatal("dismissal changed historical tokens or retained visible count", values)
	}
	for _, paused := range []bool{false, true} {
		if paused {
			m.monitorState = monitorPaused
		} else {
			m.monitorError = "lost"
		}
		values = strings.Fields(ansi.Strip(m.monitorSummaryLines(120, 2, paletteFor(themeHacker))[1]))
		if strings.Join(values, ",") != "625,2,—,—,—,—" {
			t.Fatal("stale live counts", values)
		}
		if len(m.monitorAttentionSessions()) != 0 {
			t.Fatal("stale attention buttons")
		}
		m.monitorError = ""
	}
}

func TestMonitorAttentionRenderedHitSurfaces(t *testing.T) {
	for _, size := range [][2]int{{40, 16}, {60, 24}, {80, 24}, {80, 30}, {120, 40}, {180, 55}} {
		for _, full := range []bool{false, true} {
			m := attentionTestModel()
			m.width, m.height = size[0], size[1]
			if full {
				m.setRowContext("root-three", contextFull)
			}
			out := ansi.Strip(m.render())
			if lipgloss.Width(out) > m.width || lipgloss.Height(out) > m.height {
				t.Fatalf("overflow %v %v: %dx%d\n%s", size, full, lipgloss.Width(out), lipgloss.Height(out), out)
			}
			g := m.dashboardLayout()
			a := m.monitorArea(g.contentWidth, g.meterHeight)
			buttons, y := a.attention, g.meterY+a.topHeight
			if full {
				summary, _, b := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
				buttons = b
				y = g.meterY + summary
			}
			lines := strings.Split(out, "\n")
			for _, b := range buttons {
				x, by := 2+b.rect.x, y+b.rect.y
				if !strings.Contains(lines[by], b.label) {
					t.Fatalf("missing rendered button %v %v: %q at %d", size, full, b.label, by)
				}
				for dx := 0; dx < b.rect.width; dx++ {
					if got := m.monitorContextAt(x+dx, by); got != b.action {
						t.Fatalf("hit %v %v got %q want %q", size, full, got, b.action)
					}
				}
				if id, ok := strings.CutPrefix(b.action, "attention:"); ok {
					n, cmd := m.Update(tea.MouseClickMsg{X: x, Y: by, Button: tea.MouseLeft})
					next := n.(Model)
					if cmd != nil || next.monitorSelectedID != id || next.monitorContextDetail != id || next.monitorApprovalConfirm != "" {
						t.Fatal("navigation dispatched or failed to select exact session")
					}
				}
			}
			if size[1] >= 40 && len(buttons) == 0 {
				t.Fatal("tall terminal missing attention")
			}
			if !full && size == [2]int{80, 24} && len(buttons) == 0 {
				t.Fatal("default terminal missing attention shortcuts")
			}
		}
	}
}

func TestMonitorDetailHeaderPriorityAndComposer(t *testing.T) {
	m := attentionTestModel()
	m.setRowContext("root-three", contextFull)
	seen := map[string]bool{}
	for height := 3; height <= 60; height++ {
		summary, rows, _ := m.monitorDetailHeader(116, height)
		if summary > 0 {
			seen["both"] = true
			if rows == 0 {
				t.Fatal("summary evicted attention")
			}
		} else if rows > 0 {
			seen["attention"] = true
		} else {
			seen["neither"] = true
		}
		if (summary+rows) > 0 && height-summary-rows < 10 {
			t.Fatal("chrome crowded out text")
		}
	}
	if len(seen) != 3 {
		t.Fatal("missing responsive tier", seen)
	}
	p, _ := promptTestModel()
	p.width, p.height = 80, 45
	p.monitorSessionData[1].attention = codex.SessionAttentionApproval
	p.focusMonitorPrompt()
	p.monitorPrompt.input.SetValue(strings.Repeat("A long draft that needs room. ", 60))
	g := p.dashboardLayout()
	summary, rows, _ := p.monitorDetailHeader(g.contentWidth, g.meterHeight)
	minimum := max(10, p.layoutDetailControls(g.contentWidth, g.meterHeight).rows+9)
	if summary+rows > 0 && g.meterHeight-summary-rows < minimum {
		t.Fatal("composer crowded by chrome")
	}
	actual := p.monitorDashboardLayout()
	if actual.meterY+actual.meterHeight != g.meterY+g.meterHeight {
		t.Fatal("footer moved")
	}
}

func TestMonitorAttentionOverflowAndSafeLabels(t *testing.T) {
	m := attentionTestModel()
	m.monitorSessionData = nil
	for i := 0; i < 31; i++ {
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{id: strconv.Itoa(i), workingDirectory: "/work/\x1b[31m同じ名前\x1b[0m\npath", displayed: true, attention: codex.SessionAttentionApproval})
	}
	seen := map[string]bool{}
	for page := 0; page < 40; page++ {
		buttons, rows := m.monitorAttentionButtons(56, 2)
		if rows > 2 || len(buttons) == 0 {
			t.Fatal("unbounded/missing flow")
		}
		for _, b := range buttons {
			if b.rect.x < 0 || b.rect.x+b.rect.width > 56 || b.rect.y >= 2 {
				t.Fatal("overflow", b)
			}
			if strings.ContainsAny(b.label, "\x1b\n") {
				t.Fatal("unsafe label", b.label)
			}
			if id, ok := strings.CutPrefix(b.action, "attention:"); ok {
				seen[id] = true
			}
			if next, ok := strings.CutPrefix(b.action, "attention-next:"); ok {
				m.monitorAttentionPage, _ = strconv.Atoi(next)
			}
		}
		if m.monitorAttentionPage == 0 {
			break
		}
	}
	if len(seen) != 31 {
		t.Fatal("overflow navigation omitted sessions", len(seen))
	}
	m.monitorSessionData = m.monitorSessionData[:1]
	m.monitorAttentionPage = 30
	if b, _ := m.monitorAttentionButtons(56, 2); len(b) != 1 {
		t.Fatal("shrunken list retained stale page")
	}
}

func TestMonitorAttentionSwitchClearsApprovalAndDraft(t *testing.T) {
	m := approvalTestModel()
	m.monitorSessionData[1].attention = codex.SessionAttentionInput
	m.monitorApprovalConfirm = "armed"
	m.monitorContextScroll = 10
	m.monitorPrompt.input = newMonitorEditor()
	m.monitorPrompt.input.SetValue("private draft")
	g := m.dashboardLayout()
	summary, _, buttons := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
	for _, b := range buttons {
		if b.action != "attention:root-two" {
			continue
		}
		n, cmd := m.Update(tea.MouseClickMsg{X: 2 + b.rect.x, Y: g.meterY + summary + b.rect.y, Button: tea.MouseLeft})
		next := n.(Model)
		if cmd != nil || next.monitorContextDetail != "root-two" || next.monitorApprovalConfirm != "" || next.monitorPrompt.input.Value() != "" || next.monitorContextScroll != 0 {
			t.Fatal("state leaked between sessions")
		}
		if m.fetcher.(*approvalTestClient).decision != "" {
			t.Fatal("navigation approved command")
		}
		return
	}
	t.Fatal("missing navigation target")
}

func TestMonitorAttentionSwitchFromFocusedComposer(t *testing.T) {
	m, c := promptTestModel()
	m.monitorSessionData[1].attention = codex.SessionAttentionApproval
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("unsent draft")
	g := m.dashboardLayout()
	summary, _, buttons := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
	for _, b := range buttons {
		if b.action != "attention:root-two" {
			continue
		}
		n, cmd := m.Update(tea.MouseClickMsg{X: 2 + b.rect.x, Y: g.meterY + summary + b.rect.y, Button: tea.MouseLeft})
		next := n.(Model)
		if cmd != nil || next.monitorContextDetail != "root-two" || next.monitorPrompt.input.Focused() || next.monitorPrompt.input.Value() != "" || c.answers != nil {
			t.Fatal("focused composer consumed navigation or sent text")
		}
		return
	}
	t.Fatal("missing attention while editing")
}

func TestMonitorSummaryDetailScrollingAndEmptyAttentionContext(t *testing.T) {
	m := attentionTestModel()
	m.setRowContext("root-three", contextFull)
	m.monitorSessionData[2].preview.Text = strings.Repeat("line of detail\n", 100) + "LAST-LINE"
	m.scrollMonitorContext(10000)
	if !strings.Contains(ansi.Strip(m.render()), "LAST-LINE") {
		t.Fatal("header reduced reachable scroll content")
	}
	m.monitorSessionData[0].preview = codex.SessionContext{}
	m.setRowContext("root-one", contextFull)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "root-one") || !strings.Contains(out, "project-1") {
		t.Fatal("empty attention lost session identity")
	}
}

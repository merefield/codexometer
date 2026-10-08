package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func historyContext(turn int) codex.SessionContext {
	return codex.SessionContext{ThreadID: "root-one", TurnID: fmt.Sprint(turn), Kind: codex.SessionContextReply, CurrentTask: fmt.Sprintf("Task %d", turn), Text: fmt.Sprintf("Answer %d\n", turn) + strings.Repeat("More detail\n", 40), At: time.Unix(int64(turn), 0)}
}

func historyTestModel() Model {
	m, _ := promptTestModel()
	for i := 1; i <= 3; i++ {
		m.monitorSessionData[0].preview = historyContext(i)
		m.observeMonitorHistory()
	}
	return m
}

func TestMonitorHistoryNavigationPinningAndCopy(t *testing.T) {
	m := historyTestModel()
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	m = next.(Model)
	c, ok := m.historicalContext()
	if !ok || c.TurnID != "2" || !strings.Contains(strings.Join(m.contextDetailLines(100), "\n"), "Task 2") || m.monitorCopyText("root-one") != c.Text {
		t.Fatal("Previous did not select/copy the previous completed turn")
	}
	m.scrollMonitorContext(4)
	scroll := m.monitorContextScroll
	for i := 4; i <= 18; i++ {
		u := codex.LiveUsageSession{ID: "root-one", Active: true, Context: historyContext(i)}
		m.syncMonitorSessions(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{u}}, time.Now())
	}
	if pinned, _ := m.historicalContext(); pinned != c || m.monitorContextScroll != scroll || len(m.monitorHistory["root-one"].turns) != codex.SessionHistoryLimit {
		t.Fatal("incoming turns replaced pinned answer, scroll, or exceeded cache cap")
	}
	if strings.Contains(strings.Join(m.contextDetailLines(100), "\n"), "Answer 18") {
		t.Fatal("live reply leaked into pinned document")
	}
	m.moveMonitorHistory(1) // evicted pinned answer advances to oldest retained turn
	if c, _ := m.historicalContext(); c.TurnID != "9" {
		t.Fatal("evicted selection cannot advance")
	}
	m.showLiveMonitorHistory()
	if _, ok := m.historicalContext(); ok || !strings.Contains(m.monitorCopyText("root-one"), "Answer 18") {
		t.Fatal("Live did not restore current reply")
	}
	// Cache and selected excerpt belong to the session, not the detail viewport.
	m.moveMonitorHistory(-1)
	m.setRowContext("root-two", contextFull)
	if _, ok := m.historicalContext(); ok {
		t.Fatal("selection leaked to another session")
	}
	m.setRowContext("root-one", contextFull)
	if c, ok := m.historicalContext(); !ok || c.TurnID != "17" {
		t.Fatal("selection lost across navigation")
	}
}

func TestMonitorHistoryRejectsStreamingAndRemovesCapabilities(t *testing.T) {
	m := historyTestModel()
	c := historyContext(4)
	c.Streaming = true
	m.monitorSessionData[0].preview = c
	m.observeMonitorHistory()
	if len(m.monitorHistory["root-one"].turns) != 3 {
		t.Fatal("saved incomplete stream")
	}
	c.Streaming = false
	c.ApprovalToken = "never-retain"
	c.InputToken = "never-retain"
	c.Text = "\x1b[31manswer\x00"
	m.monitorSessionData[0].preview = c
	m.observeMonitorHistory()
	h := m.monitorHistory["root-one"]
	got := h.turns[len(h.turns)-1]
	if got.Text != "answer" || got.ApprovalToken != "" || got.InputToken != "" {
		t.Fatal("history retained controls or unsafe text")
	}
	m.observeMonitorHistory()
	if len(m.monitorHistory["root-one"].turns) != 4 {
		t.Fatal("duplicate turn retained")
	}
}

func TestMonitorHistoryRenderedControlsAndLiveApproval(t *testing.T) {
	for _, width := range []int{28, 40, 80, 160} {
		m := historyTestModel()
		m.width = width
		m.height = 45
		m.moveMonitorHistory(-1)
		g := m.monitorDashboardLayout()
		out := m.renderMonitorContextDetail(g.contentWidth, g.meterHeight, paletteFor(m.theme))
		if lipgloss.Width(out) > g.contentWidth || lipgloss.Height(out) > g.meterHeight {
			t.Fatal("history overflows detail")
		}
		lines := strings.Split(ansi.Strip(out), "\n")
		for _, b := range m.historyNavigationButtons(g.contentWidth, g.meterHeight) {
			if got := ansi.Cut(lines[b.rect.y], b.rect.x, b.rect.x+b.rect.width); got != b.label {
				t.Fatalf("render/hit mismatch: %q vs %q", got, b.label)
			}
			for x := b.rect.x; x < b.rect.x+b.rect.width; x++ {
				want := b.action
				if !b.enabled {
					want = "navigation-disabled"
				}
				if got := m.monitorContextAt(x+2, g.meterY+b.rect.y); got != want {
					t.Fatalf("history click %d,%d: %q != %q", x, b.rect.y, got, want)
				}
			}
			if b.enabled {
				m.monitorContextHover = b.action
				hover := m.renderMonitorContextDetail(g.contentWidth, g.meterHeight, paletteFor(m.theme))
				if hover == out || ansi.Strip(hover) != ansi.Strip(out) {
					t.Fatal("history hover altered layout or was missing")
				}
				m.monitorContextHover = ""
			}
		}
	}
	m := historyTestModel()
	m.moveMonitorHistory(-1)
	live := approvalTestModel()
	approval := live.monitorSessionData[0].preview
	m.fetcher = live.fetcher
	m.monitorSessionData[0].preview = approval
	doc := strings.Join(m.contextDetailLines(100), "\n")
	if !strings.Contains(doc, i18n.Text("LIVE SESSION")) || !strings.Contains(doc, "Answer 2") || !strings.Contains(doc, approval.CommandDetails.Command) {
		t.Fatal("live request missing or history replaced")
	}
	if m.monitorApprovalToken() != approval.ApprovalToken {
		t.Fatal("historical turn replaced live capability")
	}
	m, cmd, _ := m.monitorApprovalAction("decision:0")
	if _, historical := m.historicalContext(); historical || cmd != nil || m.monitorApprovalConfirm == "" {
		t.Fatal("first approval choice must show live request and still require confirmation")
	}
	m.moveMonitorHistory(-1)
	m.monitorSessionData[0].attention = codex.SessionAttentionApproval
	m.openMonitorAttention("attention:root-one")
	if _, historical := m.historicalContext(); historical {
		t.Fatal("approval pill reopened historical reply instead of current request")
	}
}

func TestMonitorHistoryShortLayoutsAndDrafts(t *testing.T) {
	for _, size := range [][2]int{{28, 12}, {40, 16}, {80, 20}, {160, 40}} {
		m := historyTestModel()
		m.width, m.height = size[0], size[1]
		m.monitorPrompt.input = newMonitorEditor()
		m.focusMonitorPrompt()
		m.monitorPrompt.input.SetValue("Keep this unsent draft")
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
		m = next.(Model)
		if m.monitorPrompt.input.Value() != "Keep this unsent draft" || m.monitorPrompt.input.Focused() {
			t.Fatal("history navigation changed draft or retained focus")
		}
		g := m.monitorDashboardLayout()
		out := m.renderMonitorContextDetail(g.contentWidth, g.meterHeight, paletteFor(m.theme))
		if lipgloss.Height(out) > g.meterHeight || lipgloss.Width(out) > g.contentWidth {
			t.Fatalf("history overflow at %v", size)
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = next.(Model)
		if m.monitorContextScroll == 0 {
			t.Fatal("Page Down no longer scrolls historical reply")
		}
		m.focusMonitorPrompt()
		if _, historical := m.historicalContext(); historical || m.monitorPrompt.input.Value() != "Keep this unsent draft" {
			t.Fatal("return to live discarded draft")
		}
	}
}

type historyTestClient struct {
	*promptTestClient
	turns []codex.SessionContext
	err   error
}

func (c *historyTestClient) SessionHistory(context.Context, string) ([]codex.SessionContext, error) {
	return c.turns, c.err
}

func TestMonitorHistoryAsyncRecoveryAndFallback(t *testing.T) {
	m := historyTestModel()
	_, promptClient := promptTestModel()
	client := &historyTestClient{promptTestClient: promptClient, turns: []codex.SessionContext{historyContext(1), historyContext(2), historyContext(3), historyContext(4)}}
	m.fetcher = client
	now := time.Now()
	cmd := m.pollMonitorHistory(now)
	if cmd == nil || m.pollMonitorHistory(now) != nil {
		t.Fatal("missing fetch or overlapping fetch")
	}
	m.moveMonitorHistory(-1)
	next, _, handled := m.updateMonitorHistory(cmd())
	if !handled || len(next.monitorHistory["root-one"].turns) != 4 {
		t.Fatal("server did not recover missed completion")
	}
	m = next
	if c, _ := m.historicalContext(); c.TurnID != "2" {
		t.Fatal("async completion stole pinned view")
	}
	if m.pollMonitorHistory(now.Add(time.Second)) != nil {
		t.Fatal("history polled on every paint")
	}
	client.err = codex.ErrSessionHistory
	cmd = m.pollMonitorHistory(now.Add(6 * time.Second))
	m, _, _ = m.updateMonitorHistory(cmd())
	if len(m.monitorHistory["root-one"].turns) != 4 {
		t.Fatal("unsupported server discarded observed history")
	}
	stale := monitorHistoryResult{id: "root-one", request: 1, turns: []codex.SessionContext{historyContext(99)}}
	m, _, _ = m.updateMonitorHistory(stale)
	if len(m.monitorHistory["root-one"].turns) != 4 {
		t.Fatal("stale result replaced history")
	}
	m.focusMonitorPrompt()
	if _, ok := m.historicalContext(); ok {
		t.Fatal("composer focus did not return to live session")
	}
}

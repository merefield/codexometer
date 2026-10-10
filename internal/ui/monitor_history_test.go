package ui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
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

func TestMonitorHistoryRefreshRejectsStalePreview(t *testing.T) {
	m := historyTestModel()
	h := m.monitorHistory["root-one"]
	h.request = 1
	h.requestedAt = time.Now()
	m.monitorHistory["root-one"] = h
	var turns []codex.SessionContext
	for i := 4; i <= 13; i++ {
		turns = append(turns, historyContext(i))
	}
	m, _, _ = m.updateMonitorHistory(monitorHistoryResult{id: "root-one", request: 1, turns: turns})
	// Telemetry still displays turn 3, which is outside the server's page.
	// Observing it repeatedly (including during navigation) must not evict
	// turn 4 or append turn 3 as the newest answer.
	m.observeMonitorHistory()
	m.moveMonitorHistory(-1)
	got := m.monitorHistory["root-one"].turns
	if len(got) != len(turns) || got[0].TurnID != "4" || got[len(got)-1].TurnID != "13" {
		t.Fatalf("stale preview reordered history: first=%s last=%s", got[0].TurnID, got[len(got)-1].TurnID)
	}
	if c, ok := m.historicalContext(); !ok || c.TurnID != "13" {
		t.Fatal("Previous did not select the newest recovered answer")
	}
	// Existing entries can still gain observed guidance without reordering.
	c := historyContext(7)
	c.LatestGuidance = "Preserve this guidance"
	m.monitorSessionData[0].preview = c
	m.observeMonitorHistory()
	if got := m.monitorHistory["root-one"].turns[3]; got.TurnID != "7" || got.LatestGuidance != c.LatestGuidance {
		t.Fatal("stale-preview guard prevented an existing entry update")
	}
	// New completions remain observable even if later history reads fail.
	c = historyContext(14)
	c.At = h.requestedAt.Add(time.Second)
	m.monitorSessionData[0].preview = c
	m.observeMonitorHistory()
	got = m.monitorHistory["root-one"].turns
	if len(got) != codex.SessionHistoryLimit || got[0].TurnID != "5" || got[len(got)-1].TurnID != "14" {
		t.Fatal("new completion was lost after a successful server refresh")
	}
	if c, ok := m.historicalContext(); !ok || c.TurnID != "13" {
		t.Fatal("new completion replaced the pinned answer")
	}
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

// Decode the bytes sent by the default macOS terminal keymaps, rather than
// constructing an already-normalised arrow event that bypasses the bug.
func historyTerminalKey(t *testing.T, sequence string) tea.KeyPressMsg {
	t.Helper()
	decoder := uv.EventDecoder{}
	n, event := decoder.Decode([]byte(sequence))
	key, ok := event.(uv.KeyPressEvent)
	if !ok || n != len(sequence) {
		t.Fatalf("terminal sequence %q did not decode as one key: %T", sequence, event)
	}
	return tea.KeyPressMsg(key)
}

func TestMonitorHistoryTerminalArrowEncodings(t *testing.T) {
	for _, keys := range []struct{ name, previous, next string }{
		{"native", "\x1b[1;3D", "\x1b[1;3C"},
		{"macOS defaults", "\x1bb", "\x1bf"},
	} {
		t.Run(keys.name, func(t *testing.T) {
			m := historyTestModel()
			next, _ := m.Update(historyTerminalKey(t, keys.previous))
			m = next.(Model)
			c, selected := m.historicalContext()
			if keys.name == "macOS defaults" && runtime.GOOS != "darwin" {
				if selected {
					t.Fatal("macOS fallback changed another platform's navigation")
				}
				return
			}
			if !selected || c.TurnID != "2" || m.monitorContextDetail != "root-one" {
				t.Fatal("terminal key did not open previous turn")
			}
			next, _ = m.Update(historyTerminalKey(t, keys.next))
			m = next.(Model)
			if c, ok := m.historicalContext(); !ok || c.TurnID != "3" {
				t.Fatal("terminal key did not advance history")
			}
			next, _ = m.Update(historyTerminalKey(t, keys.next))
			if _, selected := next.(Model).historicalContext(); selected {
				t.Fatal("next did not return to live detail")
			}
		})
	}
}

func TestMonitorHistoryMacArrowEncodingsKeepEditorWordMovement(t *testing.T) {
	for _, sequence := range []string{"\x1bb", "\x1bf"} {
		m := historyTestModel()
		m.focusMonitorPrompt()
		m.monitorPrompt.input.SetValue("one two three")
		m.monitorPrompt.input.CursorEnd()
		// Put the cursor at the start of the final word for forward movement.
		if sequence == "\x1bf" {
			next, _ := m.Update(historyTerminalKey(t, "\x1bb"))
			m = next.(Model)
		}
		next, _ := m.Update(historyTerminalKey(t, sequence))
		m = next.(Model)
		if _, historical := m.historicalContext(); historical || !m.monitorPrompt.input.Focused() {
			t.Fatal("word movement selected history or left the editor")
		}
		m, _ = promptKey(m, 'X', "X")
		want := "one two Xthree"
		if sequence == "\x1bf" {
			want = "one two threeX"
		}
		if m.monitorPrompt.input.Value() != want {
			t.Fatalf("word movement/edit changed: %q, want %q", m.monitorPrompt.input.Value(), want)
		}
	}
}

func TestMonitorHistoryMacArrowEncodingsRespectOtherViewsAndEditors(t *testing.T) {
	for _, configure := range []func(*Model){
		func(m *Model) { m.meterView = viewBars },
		func(m *Model) { m.setRowContext("root-one", contextWide) },
		func(m *Model) { m.monitorCommands.open = true },
		func(m *Model) { m.monitorQueue.open = true },
		func(m *Model) { m.scheduleUI.open = true },
	} {
		for _, sequence := range []string{"\x1bb", "\x1bf"} {
			m := historyTestModel()
			configure(&m)
			if n, _, handled := m.updateMonitorHistory(historyTerminalKey(t, sequence)); handled || n.monitorHistory["root-one"].selected != nil {
				t.Fatal("macOS history alias intercepted another view/editor")
			}
		}
	}
}

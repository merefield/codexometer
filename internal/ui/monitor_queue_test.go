package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
)

type queueTestClient struct {
	*turnTestClient
	items                 []codex.SessionQueuedMessage
	revision              uint64
	err                   error
	changed, thread, text string
	removed               bool
}

func (c *queueTestClient) SessionQueue(context.Context, string) ([]codex.SessionQueuedMessage, error) {
	return c.items, c.err
}
func (c *queueTestClient) SessionQueueRevision() uint64 { return c.revision }
func (c *queueTestClient) ChangeSessionQueue(_ context.Context, thread string, item codex.SessionQueuedMessage, text string, remove bool) error {
	c.thread = thread
	c.changed = item.ID
	c.text = text
	c.removed = remove
	return c.err
}
func queueTestModel(inline bool) (Model, *queueTestClient) {
	m, c := turnTestModel()
	if inline {
		m, c = inlineTurnTestModel()
	}
	m.width, m.height = 160, 55
	client := &queueTestClient{turnTestClient: c, items: []codex.SessionQueuedMessage{{ID: "one", Text: "Check the tests", Token: "token-one", Editable: true}, {ID: "two", Text: "Update documentation", Token: "token-two", Editable: true}}}
	m.fetcher = client
	cmd := m.pollMonitorQueue()
	m, _, _ = m.updateMonitorQueue(cmd())
	return m, client
}

func TestQueuedSendShowsReceiptAndReconcilesImmediately(t *testing.T) {
	m, c := queueTestModel(false)
	m.monitorQueue.items = nil
	m.monitorQueue.loading = true // an older empty read is still in flight
	oldRequest := m.monitorQueue.request
	m.monitorPrompt.input.SetValue("Next task")
	m, send, _ := m.submitMonitorTurn("queue")
	m, refresh, _ := m.updateMonitorPrompt(send())
	if refresh == nil || !strings.Contains(ansi.Strip(m.render()), "ACCEPTED // CHECKING QUEUE") || !strings.Contains(ansi.Strip(m.render()), "Next task") {
		t.Fatal("successful queue submission not immediately visible/refreshed")
	}
	entries := m.followupEntries()
	if len(entries) != 1 || entries[0].editable || entries[0].removable || m.followupAction(0, "delete") != nil {
		t.Fatal("receipt allowed mutation")
	}
	m, _, _ = m.updateMonitorQueue(monitorQueueResult{session: m.monitorQueue.session, request: oldRequest})
	if len(m.monitorQueue.submitted) != 1 {
		t.Fatal("pre-send read erased receipt")
	}
	c.items = nil // already dispatched by the time the post-send list arrives
	m, _, _ = m.updateMonitorQueue(refresh())
	if len(m.followupEntries()) != 0 {
		t.Fatal("dispatched message left a phantom queue entry")
	}
}

func TestEmptyQueueFailureAndCrowdedRowRemainVisible(t *testing.T) {
	m, _ := queueTestModel(true)
	m.monitorSessionData[0].preview.Text = strings.Repeat("Long context\n", 100)
	if !strings.Contains(ansi.Strip(m.render()), "FOLLOW-UPS // 2") {
		t.Fatal("long context hid queue")
	}
	m.monitorQueue.items = nil
	m, _, _ = m.updateMonitorQueue(monitorQueueResult{session: m.monitorQueue.session, request: m.monitorQueue.request, err: errors.New("offline")})
	if !strings.Contains(ansi.Strip(m.render()), "FOLLOW-UPS // UNAVAILABLE") {
		t.Fatal("initial queue failure looked empty")
	}
}

func TestComposerDraftSurvivesDetailNavigation(t *testing.T) {
	for _, working := range []bool{false, true} {
		m, _ := promptTestModel()
		if working {
			m, _ = turnTestModel()
		} else {
			m.focusMonitorPrompt()
		}
		const draft = "Keep this unsent draft"
		m.monitorPrompt.input.SetValue(draft)
		for _, mode := range []int{contextWide, contextFull, contextWide, contextFull} {
			m.setRowContext("root-one", mode)
			if m.monitorPrompt.input.Value() != draft || m.monitorPrompt.input.Focused() {
				t.Fatal("navigation lost draft or retained typing focus")
			}
			if !strings.Contains(ansi.Strip(m.renderMonitorPrompt(100, 30, paletteFor(m.theme))), draft) {
				t.Fatal("unfocused draft replaced by placeholder")
			}
			m.focusMonitorPrompt()
			if m.monitorPrompt.input.Value() != draft {
				t.Fatal("refocusing lost draft")
			}
		}
		m.setRowContext("another-session", contextFull)
		if m.monitorPrompt.input.Value() != "" {
			t.Fatal("draft transferred to another session")
		}
	}
}

func TestComposerDraftsAreSessionScopedAndMemoryOnly(t *testing.T) {
	m, c := promptTestModel()
	other := m.monitorSessionData[0]
	other.id = "root-two"
	m.monitorSessionData = append(m.monitorSessionData, other)
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("First session draft")
	m.setRowContext("root-two", contextFull)
	c.offer.ThreadID, c.offer.Token = "root-two", "second-capability"
	m.focusMonitorPrompt()
	if m.monitorPrompt.input.Value() != "" {
		t.Fatal("draft leaked to second session")
	}
	m.monitorPrompt.input.SetValue("Second session draft")
	c.offer.ThreadID, c.offer.Token = "root-one", "fresh-first-capability"
	m.setRowContext("root-one", contextFull)
	if m.monitorPrompt.input.Value() != "First session draft" || m.monitorPrompt.input.Focused() || m.monitorPrompt.offer.Token != c.offer.Token {
		t.Fatal("first draft not restored with fresh capability and no focus")
	}
	c.offer.ThreadID, c.offer.Token = "root-two", "fresh-second-capability"
	m.setRowContext("root-two", contextFull)
	if m.monitorPrompt.input.Value() != "Second session draft" {
		t.Fatal("second draft not restored")
	}
	m.monitorPrompt.input.Reset() // clear/send must not resurrect previous content
	c.offer.ThreadID, c.offer.Token = "root-one", "first-again"
	m.setRowContext("root-one", contextFull)
	c.offer.ThreadID, c.offer.Token = "root-two", "second-again"
	m.setRowContext("root-two", contextFull)
	m.focusMonitorPrompt()
	if m.monitorPrompt.input.Value() != "" {
		t.Fatal("cleared draft resurrected")
	}
	fresh, _ := promptTestModel()
	if len(fresh.monitorDrafts) != 0 {
		t.Fatal("draft survived new application model")
	}
}

func TestComposerDoesNotStoreInFlightOrQuestionDrafts(t *testing.T) {
	for _, kind := range []string{"busy", "question", "secret"} {
		m, _ := promptTestModel()
		m.focusMonitorPrompt()
		m.monitorPrompt.input.SetValue("Do not retain this")
		switch kind {
		case "busy":
			m.monitorPrompt.busy = true
		case "question":
			m.monitorPrompt.offer.Questions = []codex.PromptQuestion{{}}
		case "secret":
			m.monitorPrompt.input.secret = true
		}
		m.setRowContext("another", contextFull)
		if len(m.monitorDrafts) != 0 {
			t.Fatalf("retained %s draft", kind)
		}
	}
}

func TestFollowupRowsWithoutIndent(t *testing.T) {
	m, _ := queueTestModel(false)
	colors := paletteFor(m.theme)
	lines := strings.Split(ansi.Strip(m.renderMonitorQueue(120, 3, colors)), "\n")
	if !strings.HasPrefix(lines[1], "QUEUED // Check the tests") || !strings.HasPrefix(lines[2], "QUEUED // Update documentation") {
		t.Fatalf("queue rows have unnecessary indentation: %q", lines)
	}
	buttonX := ansi.StringWidth(lines[1][:strings.Index(lines[1], "[EDIT]")])
	m.monitorQueue.focused = true
	selected := strings.Split(ansi.Strip(m.renderMonitorQueue(120, 3, colors)), "\n")
	if !strings.HasPrefix(selected[1], "› QUEUED // Check the tests") || ansi.StringWidth(selected[1][:strings.Index(selected[1], "[EDIT]")]) != buttonX {
		t.Fatal("selection marker was lost or moved the queue controls")
	}
}

func TestNativeQueuePlacementAndClickSurfaces(t *testing.T) {
	for _, inline := range []bool{false, true} {
		m, _ := queueTestModel(inline)
		lines := strings.Split(ansi.Strip(m.render()), "\n")
		queueY, composerY := -1, -1
		for y, line := range lines {
			if strings.Contains(line, "FOLLOW-UPS // 2") {
				queueY = y
			}
			if strings.Contains(line, "Click here or press Enter to write") || strings.Contains(line, "Keep this draft") {
				composerY = y
			}
			for label, action := range map[string]string{"[EDIT]": "queue:edit:0", "[×]": "queue:delete:0"} {
				if x := strings.Index(line, label); x >= 0 && strings.Contains(line, "Check the tests") {
					x = ansi.StringWidth(line[:x])
					for i := 0; i < ansi.StringWidth(label); i++ {
						if got := m.monitorContextAt(x+i, y); got != action {
							t.Fatalf("inline %v %s at %d,%d = %q", inline, label, x+i, y, got)
						}
					}
				}
			}
		}
		if queueY < 0 || composerY <= queueY {
			t.Fatalf("queue not above composer: inline %v", inline)
		}
		if inline && !strings.Contains(lines[queueY], "1–1") {
			t.Fatal("wide row didn't collapse queue")
		}
		// Composer remains anchored at its existing position.
		w, h := m.monitorPromptSize()
		rows := m.monitorPromptRows(w, h)
		if rows != 3 && !m.monitorPrompt.input.Focused() {
			t.Fatal("queue changed composer size")
		}
	}
}

func TestNativeQueueSurvivesApprovalAndOtherClientChanges(t *testing.T) {
	m, c := queueTestModel(false)
	m.monitorPrompt.input.Blur()
	m.monitorSessionData[0].preview.Kind = codex.SessionContextApproval
	m.monitorSessionData[0].preview.Text = "Approve this command"
	if !strings.Contains(ansi.Strip(m.render()), "FOLLOW-UPS // 2") {
		t.Fatal("approval hid queue")
	}
	if m.monitorPromptOffer().Token != "" {
		t.Fatal("approval exposed ordinary composer")
	}
	c.items = c.items[:1]
	c.revision++
	cmd := m.pollMonitorQueue()
	if cmd == nil {
		t.Fatal("queue notification did not invalidate refresh delay")
	}
	m, _, _ = m.updateMonitorQueue(cmd())
	if len(m.monitorQueue.items) != 1 {
		t.Fatal("external queue change not reflected")
	}
	c.err = errors.New("offline")
	c.revision++
	cmd = m.pollMonitorQueue()
	m, _, _ = m.updateMonitorQueue(cmd())
	if !m.monitorQueue.stale || len(m.monitorQueue.items) != 1 {
		t.Fatal("failed refresh erased pending context")
	}
	m.openQueueItem(0, false)
	if m.monitorQueue.open {
		t.Fatal("stale queue permitted edits")
	}
}

func TestNativeQueueEditDeleteAndIsolation(t *testing.T) {
	for _, remove := range []bool{false, true} {
		m, c := queueTestModel(false)
		m.openQueueItem(0, remove)
		if !m.monitorQueue.open || m.monitorPrompt.input.Focused() {
			t.Fatal("queue editor did not take exclusive focus")
		}
		m.monitorQueue.input.SetValue("New queue text")
		m, cmd, _ := m.updateMonitorQueue(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
		if cmd == nil || c.changed != "" {
			t.Fatal("mutation executed before command")
		}
		m, _, _ = m.updateMonitorQueue(cmd())
		if c.changed != "one" || c.thread != "root-one" || c.removed != remove || m.monitorQueue.open {
			t.Fatal("wrong mutation target")
		}
	}
	m, _ := queueTestModel(false)
	m.monitorSelectedID = "elsewhere"
	m.monitorContextDetail = "elsewhere"
	m.openQueueItem(0, true)
	if m.monitorQueue.open {
		t.Fatal("old selection could delete queue")
	}
}

func TestNativeQueueFocusScrollAndEmpty(t *testing.T) {
	m, _ := queueTestModel(false)
	m, _, _ = m.updateMonitorQueue(tea.KeyPressMsg(tea.Key{Code: 'q', Mod: tea.ModAlt}))
	if !m.monitorQueue.focused || m.monitorPrompt.input.Focused() {
		t.Fatal("queue keyboard focus missing")
	}
	m, _, _ = m.updateMonitorQueue(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	m, _, _ = m.updateMonitorQueue(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if !m.monitorQueue.open || m.monitorQueue.item.ID != "two" {
		t.Fatal("keyboard selection targeted wrong item")
	}
	m, _, _ = m.updateMonitorQueue(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.monitorQueue.open {
		t.Fatal("escape did not cancel")
	}
	m.monitorQueue.items = nil
	if m.monitorQueueRows(100, 40, 3) != 0 {
		t.Fatal("empty queue reserved space")
	}
	m.monitorQueue.next = time.Now().Add(time.Hour)
}

func TestUnifiedFollowupsPreserveBackendAndScheduledControls(t *testing.T) {
	m, c := queueTestModel(false)
	m.scheduleUI.queue = schedule.New()
	if err := m.scheduleUI.queue.Save(schedule.Job{Session: "root-one", Text: "Scheduled task", Trigger: "quota"}, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	entries := m.followupEntries()
	if len(entries) != 3 || entries[0].label != "QUEUED" || entries[1].label != "QUEUED" || entries[2].label != "SCHEDULED · AFTER QUOTA REFRESH" {
		t.Fatal(entries)
	}
	view := ansi.Strip(m.renderMonitorQueue(120, 4, paletteFor(m.theme)))
	if strings.Count(view, "FOLLOW-UPS") != 1 || !strings.Contains(view, "[NOW]") || strings.Count(view, "Scheduled task") != 1 {
		t.Fatal(view)
	}
	m.followupAction(2, "edit")
	if !m.scheduleUI.open || m.scheduleUI.input.Value() != "Scheduled task" || m.monitorQueue.open || c.changed != "" {
		t.Fatal("scheduled edit used native queue")
	}
	m.scheduleUI.open = false
	m.followupAction(2, "delete")
	if m.hasSchedule("root-one") || len(m.monitorQueue.items) != 2 || c.changed != "" {
		t.Fatal("scheduled deletion affected native queue")
	}
	m.followupAction(0, "edit")
	if !m.monitorQueue.open || m.scheduleUI.open || m.monitorQueue.item.ID != "one" {
		t.Fatal("native edit used scheduler")
	}
}

func TestUnifiedScheduledButtonsMatchRenderedCells(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, width := range []int{70, 160} {
			m, _ := scheduledTestModel(t)
			m.width, m.height = width, 55
			if inline {
				m.monitorSessionData = m.monitorSessionData[:1]
				m.setRowContext("root-one", contextWide)
			}
			view := strings.Split(ansi.Strip(m.render()), "\n")
			hits := 0
			for y, line := range view {
				if !strings.Contains(line, "[NOW]") {
					continue
				}
				for label, action := range map[string]string{"[NOW]": "queue:now:0", "[EDIT]": "queue:edit:0", "[×]": "queue:delete:0"} {
					x := strings.Index(line, label)
					if x < 0 {
						t.Fatal("missing button", label)
					}
					x = ansi.StringWidth(line[:x])
					for dx := 0; dx < ansi.StringWidth(label); dx++ {
						if got := m.monitorContextAt(x+dx, y); got != action {
							t.Fatalf("inline %v width %d %s got %q at %d,%d", inline, width, label, got, x+dx, y)
						}
					}
					n, _ := m.Update(tea.MouseMotionMsg{X: x, Y: y})
					if n.(Model).monitorContextHover != action {
						t.Fatal("hover mismatch")
					}
					hits++
				}
			}
			if hits != 3 {
				t.Fatal("scheduled controls missing", inline, width)
			}
		}
	}
}

func TestQueueEditorButtonsAndKeyboardFocus(t *testing.T) {
	for _, remove := range []bool{false, true} {
		m, c := queueTestModel(false)
		m.openQueueItem(0, remove)
		lines := strings.Split(ansi.Strip(m.render()), "\n")
		for _, b := range m.queueEditorButtons() {
			if b.y >= len(lines) || !strings.Contains(lines[b.y], b.text) {
				t.Fatal("button not on clickable row", b.y, b.text, lines)
			}
			pos := strings.Index(lines[b.y], b.text)
			if pos < 0 || ansi.StringWidth(lines[b.y][:pos]) != b.x {
				t.Fatal("button x mismatch", b)
			}
			n, _, _ := m.updateMonitorQueue(tea.MouseMotionMsg{X: b.x, Y: b.y})
			if n.monitorQueue.hover != b.key {
				t.Fatal("button hover missing")
			}
		}
		// Letter hotkeys remain text in an edit; no dashboard/queue action leaks.
		if !remove {
			m, _, _ = m.updateMonitorQueue(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
			if c.changed != "" || m.monitorQueue.deleting {
				t.Fatal("typing x deleted a message")
			}
		}
	}
}

func TestFollowupSelectionSurvivesQueueRefresh(t *testing.T) {
	m, c := queueTestModel(false)
	m.scheduleUI.queue = schedule.New()
	if err := m.scheduleUI.queue.Save(schedule.Job{Session: "root-one", Text: "Later", Trigger: "quota"}, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	m.monitorQueue.focused = true
	m.monitorQueue.focusSession = "root-one"
	m.monitorQueue.offset = 2
	c.items = c.items[:1]
	c.revision++
	cmd := m.pollMonitorQueue()
	m, _, _ = m.updateMonitorQueue(cmd())
	if !m.monitorQueue.focused || m.monitorQueue.offset != 1 || m.followupEntries()[1].trigger == nil {
		t.Fatal("refresh moved selection to different follow-up")
	}
}

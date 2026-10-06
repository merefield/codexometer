package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type turnTestClient struct {
	*promptTestClient
	active       codex.SessionPromptOffer
	action, text string
}

func inlineTurnTestModel() (Model, *turnTestClient) {
	m, idle := inlinePromptTestModel()
	idle.offer = codex.SessionPromptOffer{}
	c := &turnTestClient{promptTestClient: idle, active: codex.SessionPromptOffer{Token: "working", ThreadID: "root-one", TurnID: "turn"}}
	m.fetcher = c
	return m, c
}

func TestInlineWorkingComposerKeysAndClicks(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		for _, tc := range []struct {
			key    rune
			action string
		}{{tea.KeyEnter, "steer"}, {tea.KeyTab, "queue"}, {tea.KeyEscape, "interrupt"}} {
			m, c := inlineTurnTestModel()
			m.width = width
			w, h := m.monitorPromptSize()
			rows := m.monitorPromptRows(w, h)
			if rows != 3 || !strings.Contains(ansi.Strip(m.render()), "Tab: queue") {
				t.Fatalf("missing working composer at width %d", width)
			}
			g := m.dashboardLayout()
			a := m.monitorArea(g.contentWidth, g.meterHeight)
			mw, _, _ := monitorSessionColumnWidths(a.width)
			_, _, cy := monitorContextBodyLayout(h, rows)
			y := g.meterY + a.topHeight + a.gap - 1 + cy + 1
			for x := mw + 5; x < mw+3+w-2; x++ {
				if got := m.monitorContextAt(x, y); got != "prompt" {
					t.Fatalf("click %d,%d = %q", x, y, got)
				}
			}
			next, _ := m.Update(tea.MouseClickMsg{X: mw + 5, Y: y, Button: tea.MouseLeft})
			m = next.(Model)
			if !m.monitorPrompt.input.Focused() || m.monitorContextDetail != "" {
				t.Fatal("inline click navigated instead of focusing")
			}
			m.monitorPrompt.input.SetValue("Inline task")
			m, cmd := promptKey(m, tc.key, "")
			if cmd == nil {
				t.Fatal("missing turn command")
			}
			m, _, _ = m.updateMonitorPrompt(cmd())
			if c.action != tc.action || c.text != "Inline task" {
				t.Fatal(c.action, c.text)
			}
			if tc.action == "interrupt" && m.monitorPrompt.input.Value() != "Inline task" {
				t.Fatal("interrupt discarded draft")
			}
		}
	}
}

func TestInlineWorkingComposerSpaceAndSelection(t *testing.T) {
	m, c := inlineTurnTestModel()
	w, h := m.monitorPromptSize()
	m.monitorSessionData[0].preview.Text = strings.Repeat("context\n", 100)
	if m.monitorPromptRows(w, h) != 0 {
		t.Fatal("composer displaced context")
	}
	m.monitorSessionData[0].preview.Text = "Working."
	m.monitorSessionData[0].preview.Kind = codex.SessionContextApproval
	if m.monitorPromptRows(w, h) != 0 {
		t.Fatal("composer displaced approval")
	}
	m.monitorSessionData[0].preview.Kind = codex.SessionContextActivity
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("Private draft")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	m = next.(Model)
	w, h = m.monitorPromptSize()
	if m.monitorPromptRows(w, h) != 0 || m.monitorPrompt.input.Focused() {
		t.Fatal("hidden composer retained focus")
	}
	if c.action != "" {
		t.Fatal("resize sent input")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	m.focusMonitorPrompt()
	if m.monitorPrompt.input.Value() != "Private draft" {
		t.Fatal("resize lost draft")
	}
	m.monitorSelectedID = "another"
	m, _, _ = m.updateMonitorPrompt(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.monitorPrompt.input.Value() != "" || m.monitorPrompt.input.Focused() || c.action != "" {
		t.Fatal("draft or action leaked to new selection")
	}
}

func TestInlineWorkingComposerSingleOwner(t *testing.T) {
	m, idle := promptTestModel()
	idle.offer = codex.SessionPromptOffer{}
	m.width, m.height = 180, 65
	m.monitorSessionData = m.monitorSessionData[:2]
	for i := range m.monitorSessionData {
		m.monitorSessionData[i].preview.Text = "Working."
		m.setRowContext(m.monitorSessionData[i].id, contextWide)
	}
	id := m.monitorSessionData[1].id
	c := &turnTestClient{promptTestClient: idle, active: codex.SessionPromptOffer{Token: "second-turn", ThreadID: id, TurnID: "turn-two"}}
	m.fetcher = c
	if got := strings.Count(ansi.Strip(m.render()), "Tab: queue"); got != 1 {
		t.Fatalf("rendered %d working composers, want one", got)
	}
	m.focusMonitorPrompt()
	if m.monitorPrompt.session != id {
		t.Fatal("wrong session owns composer")
	}
	m.monitorPrompt.input.SetValue("Only second session")
	m, cmd := promptKey(m, tea.KeyTab, "")
	if cmd == nil {
		t.Fatal("missing queue command")
	}
	m, _, _ = m.updateMonitorPrompt(cmd())
	if c.action != "queue" || c.text != "Only second session" {
		t.Fatal("queue targeted wrong session")
	}
	m.selectMonitorSession(-1)
	if m.monitorPromptOffer().Token != "" || m.monitorPrompt.input.Focused() {
		t.Fatal("selection change retained previous working composer")
	}
}

func (c *turnTestClient) SessionTurn(id string) codex.SessionPromptOffer {
	if id == c.active.ThreadID {
		return c.active
	}
	return codex.SessionPromptOffer{}
}
func (c *turnTestClient) SendSessionTurn(_ context.Context, o codex.SessionPromptOffer, action, text string) error {
	if o.Token != c.active.Token {
		return errors.New("expired")
	}
	c.action, c.text = action, text
	return c.err
}
func turnTestModel() (Model, *turnTestClient) {
	m, idle := promptTestModel()
	idle.offer = codex.SessionPromptOffer{}
	c := &turnTestClient{promptTestClient: idle, active: codex.SessionPromptOffer{Token: "working", ThreadID: "root-one", TurnID: "turn"}}
	m.fetcher = c
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("Keep this draft")
	return m, c
}
func TestWorkingComposerKeys(t *testing.T) {
	for _, tc := range []struct {
		key    rune
		action string
	}{{tea.KeyEnter, "steer"}, {tea.KeyTab, "queue"}, {tea.KeyEscape, "interrupt"}} {
		t.Run(tc.action, func(t *testing.T) {
			m, c := turnTestModel()
			m, cmd := promptKey(m, tc.key, "")
			if cmd == nil {
				t.Fatal("missing command")
			}
			m, _, _ = m.updateMonitorPrompt(cmd())
			if c.action != tc.action || c.text != "Keep this draft" {
				t.Fatal(c.action, c.text)
			}
			if tc.action == "interrupt" && m.monitorPrompt.input.Value() != "Keep this draft" {
				t.Fatal("draft lost")
			}
			if tc.action != "interrupt" && m.monitorPrompt.input.Value() != "" {
				t.Fatal("sent text remained")
			}
		})
	}
}
func TestWorkingComposerTransitionKeepsDraftWithoutSending(t *testing.T) {
	m, c := turnTestModel()
	c.active = codex.SessionPromptOffer{}
	c.offer = codex.SessionPromptOffer{Token: "idle", ThreadID: "root-one"}
	m, cmd := promptKey(m, tea.KeyEnter, "")
	if cmd != nil || len(c.answers) != 0 || m.monitorPrompt.input.Value() != "Keep this draft" {
		t.Fatal("transition sent or lost draft")
	}
	m, cmd = promptKey(m, tea.KeyEnter, "")
	if cmd == nil {
		t.Fatal("idle send unavailable")
	}
	cmd()
	if len(c.answers) != 1 || c.answers[0] != "Keep this draft" {
		t.Fatal(c.answers)
	}
}
func TestWorkingComposerErrorAndScheduleIsolation(t *testing.T) {
	m, c := turnTestModel()
	m.openSchedule()
	if m.scheduleUI.open {
		t.Fatal("working offer enabled scheduler")
	}
	c.err = errors.New("unsupported")
	m, cmd := promptKey(m, tea.KeyTab, "")
	m, _, _ = m.updateMonitorPrompt(cmd())
	if m.monitorPrompt.input.Value() != "Keep this draft" || !strings.Contains(m.monitorPrompt.notice, "unconfirmed") {
		t.Fatal("failed queue lost draft or hid error")
	}
	view := m.renderMonitorPrompt(120, 30, paletteFor(themeHacker))
	if !strings.Contains(view, monitorDotWave(m.phase)) || !strings.Contains(view, "WORKING") {
		t.Fatal("missing progress above composer")
	}
}

func TestWorkingComposerBusyKeysDoNotReachDashboard(t *testing.T) {
	m, _ := turnTestModel()
	m, _ = promptKey(m, tea.KeyEnter, "")
	for _, text := range []string{"q", "t", "r"} {
		var cmd tea.Cmd
		m, cmd = promptKey(m, []rune(text)[0], text)
		if cmd != nil || m.monitorPrompt.input.Value() != "Keep this draft" {
			t.Fatal("busy editor leaked dashboard key or modified in-flight input")
		}
	}
}

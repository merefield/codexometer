package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type promptTestClient struct {
	offer   codex.SessionPromptOffer
	answers []string
	err     error
}

func (c *promptTestClient) Fetch(context.Context) (codex.Snapshot, error) {
	return codex.DemoSnapshot(), nil
}
func (c *promptTestClient) SessionPrompt(thread string) codex.SessionPromptOffer {
	if thread == c.offer.ThreadID {
		return c.offer
	}
	return codex.SessionPromptOffer{}
}
func (c *promptTestClient) SendSessionPrompt(_ context.Context, token string, answers []string) error {
	if token != c.offer.Token {
		return errors.New("expired")
	}
	c.answers = answers
	c.offer.Token = ""
	return c.err
}
func promptTestModel() (Model, *promptTestClient) {
	m := contextTestModel()
	c := &promptTestClient{offer: codex.SessionPromptOffer{Token: "prompt", ThreadID: "root-one"}}
	m.fetcher = c
	m.openMonitorContext("root-one")
	return m, c
}
func promptKey(m Model, code rune, text string) (Model, tea.Cmd) {
	n, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: code, Text: text}))
	return n.(Model), cmd
}

func TestMonitorPromptEnterAndHotkeyIsolation(t *testing.T) {
	m, c := promptTestModel()
	m, _ = promptKey(m, tea.KeyEnter, "")
	if !m.monitorPrompt.input.Focused() {
		t.Fatal("Enter did not focus")
	}
	for _, text := range []string{"Q", "s", "t", "h", "i", "X", "Hello 世界"} {
		m, _ = promptKey(m, []rune(text)[0], text)
	}
	if m.monitorContextHidden || m.monitorContextDetail != "root-one" {
		t.Fatal("typing triggered dashboard action")
	}
	want := "QsthiXHello 世界"
	if m.monitorPrompt.input.Value() != want {
		t.Fatalf("case/input lost: %q", m.monitorPrompt.input.Value())
	}
	m, cmd := promptKey(m, tea.KeyEnter, "")
	if cmd == nil || !m.monitorPrompt.busy || c.answers != nil {
		t.Fatal("send not queued exclusively")
	}
	n, _ := m.Update(cmd())
	m = n.(Model)
	if len(c.answers) != 1 || c.answers[0] != want || m.monitorPrompt.busy {
		t.Fatal("wrong submission", c.answers)
	}
	if m.monitorPrompt.input.Value() != "" {
		t.Fatal("submitted draft retained")
	}
}

func TestMonitorPromptQuestionsAndSecret(t *testing.T) {
	m, c := promptTestModel()
	c.offer.Questions = []codex.PromptQuestion{{ID: "a", Text: "Pick one", Options: []string{"First", "Second"}}, {ID: "b", Text: "Why?", FreeText: true, Secret: true}}
	m.monitorSessionData[0].preview.Kind = codex.SessionContextQuestion
	m.monitorSessionData[0].preview.InputToken = c.offer.Token
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("not offered")
	m, cmd, _ := m.submitMonitorPrompt()
	if cmd != nil || m.monitorPrompt.question != 0 {
		t.Fatal("invalid choice sent")
	}
	m, _ = promptKey(m, tea.KeyDown, "")
	m, cmd = promptKey(m, tea.KeyEnter, "")
	if cmd != nil || m.monitorPrompt.question != 1 || c.answers != nil {
		t.Fatal("partial answers sent")
	}
	m.monitorPrompt.input.SetValue("supersecret")
	if strings.Contains(ansi.Strip(m.render()), "supersecret") {
		t.Fatal("secret displayed")
	}
	m, cmd = promptKey(m, tea.KeyEnter, "")
	if cmd == nil {
		t.Fatal("missing complete reply")
	}
	cmd()
	if strings.Join(c.answers, "/") != "First/supersecret" {
		t.Fatal(c.answers)
	}
}

func TestMonitorPromptStaleFocusPasteAndEsc(t *testing.T) {
	m, c := promptTestModel()
	m.focusMonitorPrompt()
	n, cmd := m.Update(tea.PasteMsg{Content: "Hello\nWORLD"})
	m = n.(Model)
	if m.monitorPrompt.busy || c.answers != nil || !strings.Contains(m.monitorPrompt.input.Value(), "WORLD") {
		t.Fatal("paste sent or lost")
	}
	m, _ = promptKey(m, tea.KeyEscape, "")
	if m.monitorPrompt.input.Focused() || m.monitorContextDetail == "" {
		t.Fatal("Esc should only blur first")
	}
	m.focusMonitorPrompt()
	c.offer.Token = "new-request"
	m, cmd = promptKey(m, 'q', "q")
	if cmd != nil || m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "" {
		t.Fatal("stale editor fell through or retained draft")
	}
	m.focusMonitorPrompt()
	m.stepBackMonitorContext()
	if m.monitorPrompt.input.Value() != "" || m.monitorPrompt.input.Focused() {
		t.Fatal("draft leaked to another mode")
	}
}

func TestMonitorPromptResponsiveClickTargets(t *testing.T) {
	lang := i18n.Code()
	for _, size := range [][2]int{{24, 12}, {40, 16}, {80, 24}, {120, 40}, {180, 50}} {
		m, _ := promptTestModel()
		m.width, m.height = size[0], size[1]
		out := m.render()
		if lipgloss.Width(out) > m.width || lipgloss.Height(out) > m.height {
			t.Fatalf("overflow %s %v", lang, size)
		}
		g := m.dashboardLayout()
		rows := m.monitorPromptRows(g.contentWidth, g.meterHeight)
		if rows == 0 {
			continue
		}
		_, _, y := monitorContextBodyLayout(g.meterHeight, rows)
		y += g.meterY + 1
		for x := 4; x < g.contentWidth; x++ {
			if m.monitorContextAt(x, y) != "prompt" {
				t.Fatalf("miss %s %v %d,%d", lang, size, x, y)
			}
		}
		n, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 4, Y: y, Button: tea.MouseLeft}))
		if !n.(Model).monitorPrompt.input.Focused() {
			t.Fatal("click did not focus")
		}
	}
}

func TestMonitorPromptSourceAndChangedRequest(t *testing.T) {
	m, c := promptTestModel()
	m.monitorSessionData[0].preview.ThreadID = "child"
	if got := m.monitorPromptOffer(); got.ThreadID != "root-one" {
		t.Fatal("follow-up targeted child", got)
	}
	c.offer.ThreadID = "child"
	c.offer.Questions = []codex.PromptQuestion{{ID: "q", Text: "Question", FreeText: true}}
	m.monitorSessionData[0].preview.Kind = codex.SessionContextQuestion
	if m.monitorPromptOffer().Token != "" {
		t.Fatal("unseen question actionable")
	}
	m.monitorSessionData[0].preview.InputToken = c.offer.Token
	if got := m.monitorPromptOffer(); got.ThreadID != "child" {
		t.Fatal("question did not target child", got)
	}
	m.focusMonitorPrompt()
	c.offer.Token = "replacement"
	if m.monitorPromptOffer().Token != "" {
		t.Fatal("new question answered with old displayed context")
	}
	m.monitorSessionData[0].preview.Kind = codex.SessionContextApproval
	m.monitorPrompt.notice = "previous outcome"
	g := m.dashboardLayout()
	if m.monitorPromptRows(g.contentWidth, g.meterHeight) != 0 {
		t.Fatal("prompt notice overlaps approval footer")
	}
}

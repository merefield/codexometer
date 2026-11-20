package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
)

type newSessionProbe struct {
	resumeFinderProbe
	prompts []string
}

func (p *newSessionProbe) StartSession(_ context.Context, _, _, prompt string) (string, error) {
	p.prompts = append(p.prompts, prompt)
	return "created", nil
}

func TestNewSessionClickAndCancelResumePreservesDraft(t *testing.T) {
	m := contextTestModel()
	probe := &newSessionProbe{}
	m.fetcher = probe
	m.finder.setScope("account")
	g := m.dashboardLayout()
	area := m.monitorArea(g.contentWidth, g.meterHeight)
	if area.newRect.x <= area.closeAllRect.x || area.newRect.x+area.newRect.width != g.contentWidth {
		t.Fatal("New session should fit next to Close all", area.newRect)
	}
	click := func(x, y int) tea.MouseClickMsg {
		return tea.MouseClickMsg{Button: tea.MouseLeft, X: x + 2, Y: y + g.meterY}
	}
	next, _ := m.Update(click(area.newRect.x, 1))
	m = next.(Model)
	if !m.newSession.open() || len(probe.prompts) != 0 {
		t.Fatal("expected a local draft without creating a thread")
	}
	m.newSession.prompt.SetValue("Unsent draft")
	saved := layoutNewSession(g.contentWidth, g.meterHeight, false, false).buttons[1].rect
	next, _ = m.Update(click(saved.x, saved.y))
	m = next.(Model)
	if !m.finder.include {
		t.Fatal("saved sessions button did not open the picker")
	}
	next, _ = m.Update(specialKey(tea.KeyEscape))
	m = next.(Model)
	if m.finder.include || m.newSession.prompt.Value() != "Unsent draft" || len(probe.prompts) != 0 {
		t.Fatal("closing the picker should restore the unsent draft")
	}
	next, _ = m.Update(specialKey(tea.KeyEscape))
	if next.(Model).newSession.open() || len(probe.prompts) != 0 {
		t.Fatal("closing the draft should discard it without creating a thread")
	}
}

func TestNewSessionFirstSendUsesStrictReview(t *testing.T) {
	m := contextTestModel()
	probe := &newSessionProbe{}
	m.fetcher = probe
	m.confirmations.Mode = confirmation.Require
	m.finder.setScope("account")
	m.openNewSession()
	m.newSession.prompt.SetValue("/resume")
	next, _ := m.Update(specialKey(tea.KeyEnter))
	m = next.(Model)
	if !m.finder.include || len(probe.prompts) != 0 {
		t.Fatal("/resume should just open the picker")
	}
	m.finder.clearHistory()
	m.newSession.prompt.SetValue("First message")
	next, cmd := m.Update(specialKey(tea.KeyEnter))
	m = next.(Model)
	if m.actionReview == nil || cmd != nil {
		t.Fatal("strict mode must review the exact first message before sending")
	}
	next, cmd = m.Update(key('c'))
	m = next.(Model)
	if !m.newSession.sending || cmd == nil {
		t.Fatal("confirmation did not send")
	}
	result := cmd().(newSessionResult)
	if result.id != "created" || len(probe.prompts) != 1 || probe.prompts[0] != "First message" {
		t.Fatal(result, probe.prompts)
	}
}

func TestNewSessionLateObservationKeepsAcknowledgedID(t *testing.T) {
	m := contextTestModel()
	m.fetcher = &newSessionProbe{}
	m.finder.setScope("account")
	m.openNewSession()
	m.newSession.prompt.SetValue("hello")
	generation := m.newSession.generation
	m, _, _ = m.updateNewSession(newSessionResult{generation: generation, id: "created"})
	m, _, _ = m.updateNewSession(newSessionDeadline(generation))
	if m.newSession.pendingID != "created" || !strings.Contains(m.newSession.notice, "still waiting") || m.startDraftSession() != nil {
		t.Fatal("waiting for telemetry must not discard the acknowledged ID or allow a resend")
	}
	next, _, _ := m.applyMonitorFetch(monitorFetchedMsg{kind: monitorFetchSample, at: time.Now(), usage: codex.LiveUsageSnapshot{
		SavedScope: "account", Sessions: []codex.LiveUsageSession{{ID: "created", Active: true, Working: true}},
	}})
	m = next.(Model)
	if m.newSession.open() || m.monitorContextDetail != "created" {
		t.Fatal("late observation did not open acknowledged session")
	}
}

func TestNewSessionFitsShortTerminalsAndSharesHitTargets(t *testing.T) {
	m := contextTestModel()
	m.fetcher = &newSessionProbe{}
	m.finder.setScope("account")
	m.openNewSession()
	m.newSession.prompt.SetValue(strings.Repeat("line\n", 20))
	for _, width := range []int{29, 42, 58, 120} {
		m.width = width
		for h := 10; h <= 24; h++ {
			m.height = h
			d := m.dashboardLayout()
			g := layoutNewSession(d.contentWidth, d.meterHeight, false, false)
			rows := strings.Split(m.renderNewSession(d.contentWidth, d.meterHeight, paletteFor(m.theme)), "\n")
			if len(rows) != d.meterHeight {
				t.Fatalf("%dx%d: rendered %d rows, expected %d", width, h, len(rows), d.meterHeight)
			}
			if g.prompt < 0 || g.prompt >= len(rows) || !strings.Contains(ansi.Strip(rows[g.prompt]), ">") {
				t.Fatalf("%dx%d: composer is not visible", width, h)
			}
			for _, button := range g.buttons {
				row := ansi.Strip(rows[button.rect.y])
				if i := strings.Index(row, button.label); i < 0 || ansi.StringWidth(row[:i]) != button.rect.x {
					t.Fatalf("%dx%d: button %q doesn't match its hit target: %q", width, h, button.label, row)
				}
			}
		}
	}
}

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
	"github.com/merefield/codexometer/internal/schedule"
)

func compactClick(t *testing.T, m Model, label string) tea.MouseClickMsg {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.render()), "\n") {
		if x := strings.Index(line, label); x >= 0 {
			return tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft}
		}
	}
	t.Fatalf("no %q in compact render", label)
	return tea.MouseClickMsg{}
}

func compactEvent(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestCompactFollowupsBadgeAndDismissal(t *testing.T) {
	m, _ := queueTestModel(false)
	m.width, m.height = 88, 21
	m.setRowContext("root-one", contextGraph)
	now := time.Now()
	m.scheduleUI.queue = schedule.New()
	if err := m.scheduleUI.queue.Save(schedule.Job{Session: "root-one", Trigger: "quota", Text: "Saved prompt"}, "a", now); err != nil {
		t.Fatal(err)
	}
	m.monitorSteers = []monitorSteerReceipt{{session: "root-one", text: "Already sent"}}
	m.monitorQueue.submitted = []string{"Waiting for queue receipt"}
	m.monitorPrompt.input.SetValue("Keep my draft")
	const label = "[ FOLLOW-UPS · 3 ]"
	click := compactClick(t, m, label)
	if hit := m.monitorContextAt(click.X, click.Y); hit != "followups:root-one" {
		t.Fatalf("badge hit = %q", hit)
	}
	m = compactEvent(m, click)
	if m.monitorQueue.compact != "root-one" || m.monitorContextDetail != "" {
		t.Fatal("badge did not open a list for the correct session")
	}
	view := ansi.Strip(m.render())
	for _, want := range []string{"Check the tests", "Update documentation", "Saved prompt", "[ CLOSE ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("list missing %q", want)
		}
	}
	if strings.Contains(view, "Already sent") || strings.Contains(view, "Waiting for queue receipt") {
		t.Fatal("compact list includes receipts")
	}
	for _, close := range []string{"Esc", "[ CLOSE ]", label} {
		if close == "Esc" {
			m = compactEvent(m, tea.KeyPressMsg{Code: tea.KeyEscape})
		} else {
			m = compactEvent(m, compactClick(t, m, close))
		}
		if m.monitorQueue.compact != "" || m.monitorQueue.compactFocus != "root-one" ||
			m.monitorPrompt.input.Value() != "Keep my draft" || m.compactFollowupCount("root-one") != 3 {
			t.Fatalf("%s did not return to the badge with draft and queue intact", close)
		}
		m = compactEvent(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.monitorQueue.compact != "root-one" {
			t.Fatal("Enter did not reopen the focused badge")
		}
	}
	m = compactEvent(m, tea.WindowSizeMsg{Width: 150, Height: 45})
	if m.monitorQueue.compact != "" || strings.Contains(ansi.Strip(m.render()), label) {
		t.Fatal("compact UI survived a resize to a roomy terminal")
	}
}

type compactGoalFixture struct{}

func (compactGoalFixture) PrepareGoalResume(context.Context, string, string, string, string) (codex.GoalResumeTarget, error) {
	return codex.NewGoalResumeTarget(&codex.SessionGoal{ThreadID: "root-one", Status: "active", Objective: "Goal example"}), nil
}

func (compactGoalFixture) ObserveScheduledGoal(context.Context, string) (*codex.SessionGoal, error) {
	return nil, codex.ErrScheduledGoalNotReady
}

func (compactGoalFixture) ResumeScheduledGoal(context.Context, string, string, codex.GoalResumeTarget) error {
	return codex.ErrScheduledGoalNotReady
}

func TestCompactFollowupsCanScrollAndCancelGoal(t *testing.T) {
	m, _ := queueTestModel(false)
	m.width, m.height = 52, 17
	m.setRowContext("root-one", contextFull)
	m.monitorPrompt.input.Blur()
	m.monitorQueue.items = nil
	for range 14 {
		m.monitorQueue.items = append(m.monitorQueue.items, codex.SessionQueuedMessage{Text: "Queued work", Editable: true})
	}
	now := time.Now()
	m.snapshot.AccountFingerprint, m.snapshot.FetchedAt = "test-account", now
	window := codex.Window{UsedPercent: 80}
	m.snapshot.RateLimits.Primary = &window
	m.scheduleUI.queue = schedule.New()
	if err := m.scheduleUI.queue.ArmGoalResume(context.Background(), compactGoalFixture{}, m.snapshot, now, "root-one", "revision", "choice"); err != nil {
		t.Fatal(err)
	}
	m = compactEvent(m, tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
	if m.monitorQueue.compact != "root-one" {
		t.Fatal("Alt+Q did not open narrow detail")
	}
	if strings.Contains(ansi.Strip(m.render()), "GOAL RESUME") {
		t.Fatal("all items unexpectedly fit without scrolling")
	}
	for range 4 {
		m = compactEvent(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !strings.Contains(ansi.Strip(m.render()), "GOAL RESUME") {
		t.Fatal("goal entry is inaccessible at the end of the list")
	}
	m = compactEvent(m, tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.hasSchedule("root-one") || len(m.monitorQueue.items) != 14 {
		t.Fatal("delete did not cancel only the selected goal trigger")
	}
}

func TestCompactFollowupsStrictReviewMouseSurface(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.width, m.height = 51, 47
	m.confirmations.Mode = confirmation.Require
	m = compactEvent(m, tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
	m = compactEvent(m, tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.actionReview == nil || !m.hasSchedule("root-one") {
		t.Fatal("strict removal was not reviewed")
	}
	m = compactEvent(m, compactClick(t, m, "[ C CONFIRM ]"))
	if m.actionReview != nil || m.hasSchedule("root-one") || m.monitorQueue.compact != "root-one" {
		t.Fatal("mouse confirmation did not cancel the exact trigger and return to the compact list")
	}
}

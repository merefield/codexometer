package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
)

func TestConfirmationModesBindApprovalScope(t *testing.T) {
	for _, mode := range []confirmation.Mode{confirmation.Balanced, confirmation.None, confirmation.Require} {
		for index, kind := range []string{"accept", "decline", "cancel", "acceptForSession", "acceptWithExecpolicyAmendment"} {
			m := approvalTestModel()
			m.SetConfirmationMode(mode)
			action := m.monitorApprovalButtons(180, 40)[index].action
			m, cmd, _ := m.monitorApprovalAction(action)
			guarded := (confirmation.Policy{Mode: mode}).Required(confirmation.Approval(kind))
			if guarded {
				if cmd != nil || m.monitorApprovalConfirm == "" {
					t.Fatal("missing confirmation", mode, kind)
				}
				m, cmd, _ = m.updateMonitorApprovalKey("c")
			}
			if cmd == nil || !m.monitorApprovalBusy {
				t.Fatal("explicit decision was not queued", mode, kind)
			}
			cmd()
			if m.fetcher.(*approvalTestClient).decision != m.monitorSessionData[0].preview.ApprovalOptions[index].Value {
				t.Fatal("wrong decision sent")
			}
		}
	}
}

func TestBalancedCloseAllIsImmediate(t *testing.T) {
	m := contextTestModel()
	m.requestMonitorCloseAll()
	if m.visibleMonitorSessionCount() != 0 || m.monitorCloseAllArmed() {
		t.Fatal("balanced Close All still needs confirmation")
	}
}

func TestStrictReversibleCommandRequiresReview(t *testing.T) {
	m, client := commandTestModel(t)
	m.SetConfirmationMode(confirmation.Require)
	m.monitorCommands.menu.Choices[0].Reversible = true
	m, cmd, _ := m.chooseMonitorCommand()
	if cmd != nil || client.calls != 0 || !m.monitorCommands.detail {
		t.Fatal("strict setting change bypassed review")
	}
	_, cmd, _ = m.confirmMonitorCommand()
	if cmd == nil {
		t.Fatal("reviewed setting could not be confirmed")
	}
	cmd()
	if client.calls != 1 {
		t.Fatal("setting not applied exactly once")
	}
}

func TestStrictReplyReviewBindsPayloadAndCapability(t *testing.T) {
	for _, change := range []string{"none", "payload", "capability", "target", "expiry", "cancel", "repeat"} {
		t.Run(change, func(t *testing.T) {
			m, client := promptTestModel()
			m.SetConfirmationMode(confirmation.Require)
			m.focusMonitorPrompt()
			m.monitorPrompt.input.SetValue("Review this exact draft")
			m, cmd, _ := m.submitMonitorPrompt()
			if cmd != nil || m.actionReview == nil || client.answers != nil {
				t.Fatal("reply bypassed strict review")
			}
			if rendered := ansi.Strip(m.render()); !strings.Contains(rendered, "Review this exact draft") || !strings.Contains(rendered, "CONFIRM") {
				t.Fatal("review omitted payload or confirmation")
			}
			k := key('c')
			switch change {
			case "payload":
				m.monitorPrompt.input.SetValue("Different draft")
			case "capability":
				client.offer.Token = "replacement"
			case "target":
				m.setRowContext("root-two", contextFull)
			case "expiry":
				m.actionReview.until = time.Now().Add(-time.Second)
			case "cancel":
				k = specialKey(tea.KeyEscape)
			case "repeat":
				k.IsRepeat = true
			}
			next, cmd := m.Update(k)
			m = next.(Model)
			if change != "none" {
				if cmd != nil || client.answers != nil {
					t.Fatal("unreviewed or cancelled payload submitted")
				}
				return
			}
			if cmd == nil || m.actionReview != nil {
				t.Fatal("fresh explicit confirmation did not submit")
			}
			cmd()
			if len(client.answers) != 1 || client.answers[0] != "Review this exact draft" {
				t.Fatal("wrong reply submitted")
			}
		})
	}
}

func TestStrictTurnActionsRequireExplicitReview(t *testing.T) {
	for _, action := range []string{"steer", "queue", "interrupt"} {
		m, client := turnTestModel()
		m.SetConfirmationMode(confirmation.Require)
		m, cmd, _ := m.submitMonitorTurn(action)
		if cmd != nil || m.actionReview == nil {
			t.Fatal("turn action bypassed review", action)
		}
		next, cmd := m.Update(key('c'))
		if cmd == nil || next.(Model).actionReview != nil {
			t.Fatal("turn action could not be confirmed", action)
		}
		cmd()
		if client.action != action {
			t.Fatal("wrong active-turn action", client.action)
		}
	}
}

func TestStrictReviewMasksAllSecretAnswers(t *testing.T) {
	m, client := promptTestModel()
	m.monitorSessionData[0].preview.Kind = codex.SessionContextQuestion
	m.monitorSessionData[0].preview.InputToken = client.offer.Token
	client.offer.Questions = []codex.PromptQuestion{{Text: "Secret", Secret: true, FreeText: true}, {Text: "Public", FreeText: true}}
	m.SetConfirmationMode(confirmation.Require)
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("Never reveal this secret")
	m, _, _ = m.submitMonitorPrompt()
	m.monitorPrompt.input.SetValue("Public answer")
	m, cmd, _ := m.submitMonitorPrompt()
	if cmd != nil || m.actionReview == nil {
		t.Fatal("missing question review")
	}
	if strings.Contains(ansi.Strip(m.render()), "Never reveal this secret") || !strings.Contains(ansi.Strip(m.render()), "Public answer") {
		t.Fatal("question review revealed secret or hid public answer")
	}
	next, cmd := m.Update(key('c'))
	if cmd == nil || next.(Model).actionReview != nil {
		t.Fatal("question answers could not be confirmed")
	}
	cmd()
	if client.answers[0] != "Never reveal this secret" {
		t.Fatal("masking altered submitted answer")
	}
}

func TestStrictQueueEditUsesReviewedItem(t *testing.T) {
	m, client := queueTestModel(false)
	m.SetConfirmationMode(confirmation.Require)
	m.openQueueItem(0, false)
	m.monitorQueue.input.SetValue("Reviewed edit")
	m, cmd, _ := m.changeQueueItem()
	if cmd != nil || m.actionReview == nil {
		t.Fatal("queue edit bypassed review")
	}
	next, cmd := m.Update(key('c'))
	if cmd == nil || next.(Model).actionReview != nil {
		t.Fatal("queue edit could not be confirmed")
	}
	cmd()
	if client.changed != "one" || client.text != "Reviewed edit" {
		t.Fatal("wrong queue edit")
	}
}

func TestStrictReviewKeepsMouseControlsVisibleWithLongDrafts(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, height := range []int{18, 24, 40} {
			m, client := promptTestModel()
			m.width, m.height = width, height
			m.SetConfirmationMode(confirmation.Require)
			m.focusMonitorPrompt()
			m.monitorPrompt.input.SetValue(strings.Repeat("Long reviewed draft\n", 100))
			m, _, _ = m.submitMonitorPrompt()
			buttons := m.actionReviewButtons()
			if len(buttons) != 2 {
				t.Fatal("review controls hidden at supported size", width, height)
			}
			g := m.monitorDashboardLayout()
			lines := strings.Split(ansi.Strip(m.render()), "\n")
			for _, button := range buttons {
				y := g.meterY + button.rect.y
				x := 2 + button.rect.x
				if y >= len(lines) || ansi.Cut(lines[y], x, x+button.rect.width) != button.label {
					t.Fatal("review control rendering/hit geometry mismatch", width, height, button.label)
				}
			}
			next, cmd := m.Update(tea.MouseClickMsg{X: 2 + buttons[0].rect.x, Y: g.meterY + buttons[0].rect.y, Button: tea.MouseLeft})
			if cmd == nil || next.(Model).actionReview != nil {
				t.Fatal("visible review confirmation was not clickable")
			}
			cmd()
			if len(client.answers) != 1 {
				t.Fatal("reviewed draft was not submitted")
			}
		}
	}
}

func TestStrictTriggerDeletionCanBeCancelled(t *testing.T) {
	m, _ := scheduledTestModel(t)
	m.SetConfirmationMode(confirmation.Require)
	m.triggerAction("ctrl+d")
	if m.actionReview == nil || len(m.scheduleUI.queue.List("root-one")) != 1 {
		t.Fatal("trigger deletion bypassed review")
	}
	next, _ := m.Update(specialKey(tea.KeyEscape))
	m = next.(Model)
	if m.actionReview != nil || len(m.scheduleUI.queue.List("root-one")) != 1 {
		t.Fatal("cancel deleted trigger")
	}
	m.triggerAction("ctrl+d")
	next, _ = m.Update(key('c'))
	m = next.(Model)
	if m.actionReview != nil || len(m.scheduleUI.queue.List("root-one")) != 0 {
		t.Fatal("confirmed trigger deletion failed")
	}
}

func TestBalancedOneTimeApprovalDoesNotReserveStrictButtonWidths(t *testing.T) {
	m := approvalTestModel()
	m.SetConfirmationMode(confirmation.Balanced)
	options := m.monitorSessionData[0].preview.ApprovalOptions
	m.monitorSessionData[0].preview.ApprovalOptions = [8]codex.ApprovalOption{options[0], options[2]}
	if len(m.monitorApprovalButtons(36, 14)) != 2 {
		t.Fatal("immediate approval buttons were hidden by unused confirmation widths")
	}
}

func TestStrictBenchmarkScopeShowsConfirmationAtEveryLabelSize(t *testing.T) {
	plan := codex.BenchmarkPlan{Models: []codex.BenchmarkModelOption{{Model: "model", DisplayName: "Model", Efforts: []string{"high"}}}, Efforts: []string{"high"}, Games: []string{"P-1"}}
	client := codex.Client{DigBenchToken: "synthetic", DigBenchGames: plan.Games}
	for _, suite := range []int{0, 1, 2} {
		m := Model{width: 160, height: 40, meterView: viewBenchmark, benchmarkRunner: client, benchmarkScopedRunner: client, benchmarkPlan: plan, benchmarkScope: plan.AllScope(), benchmarkCombinations: 1, benchmarkSelectedSuite: suite}
		m.SetConfirmationMode(confirmation.Require)
		next, cmd := m.Update(key('b'))
		m = next.(Model)
		if cmd == nil || !m.benchmarkSelectedArmed || m.benchmarkState == benchmarkRunning {
			t.Fatal("first press did not exclusively arm review", suite)
		}
		for _, width := range []int{200, 36, 18} {
			found := false
			for _, row := range m.benchmarkControlLines(width) {
				for _, segment := range row {
					if segment.button != footerButtonBenchmarkSelected {
						continue
					}
					found = true
					if !strings.Contains(segment.text, "CONFIRM") && !strings.Contains(segment.text, "?") {
						t.Fatal("armed scope still looks like immediate launch", suite, width, segment.text)
					}
					if width == 200 {
						unit := "TURNS"
						if suite == 2 {
							unit = "REMOTE SESSIONS"
						}
						if !strings.Contains(segment.text, unit) {
							t.Fatal("wrong launch unit", segment.text)
						}
					}
				}
			}
			if !found {
				t.Fatal("confirmation control disappeared", suite, width)
			}
		}
		next, cmd = m.Update(key('b'))
		if cmd == nil || next.(Model).benchmarkSelectedArmed || next.(Model).benchmarkState != benchmarkRunning {
			t.Fatal("explicit second press did not launch", suite)
		}
	}
}

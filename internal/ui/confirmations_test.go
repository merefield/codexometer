package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

func TestNoConfirmQueueDeletionStillTargetsTheSelectedItem(t *testing.T) {
	m, client := queueTestModel(false)
	m.SetNoConfirm(true)
	cmd := m.openQueueItem(0, true)
	if cmd == nil || client.changed != "" {
		t.Fatal("deletion was not an explicit one-shot command")
	}
	m, _, _ = m.updateMonitorQueue(cmd())
	if !client.removed || client.changed != "one" || client.thread != "root-one" || m.monitorQueue.open {
		t.Fatal("wrong queue deletion target")
	}
}

func TestNoConfirmProfileSelectionDoesNotApplyOtherSessions(t *testing.T) {
	m, client := profileTestModel(t)
	m.SetNoConfirm(true)
	m, cmd, _ := m.updateProfileKey("1")
	if cmd == nil || client.updates != 0 {
		t.Fatal("explicit profile action was not queued exactly once")
	}
	n, _ := m.Update(cmd())
	m = n.(Model)
	if client.updates != 1 || len(client.targets) != 1 || client.targets[0].ID != "one" {
		t.Fatal("no-confirm applied an unselected profile")
	}
}

func TestNoConfirmSendNowRetainsDispatchGuards(t *testing.T) {
	m, client := scheduledTestModel(t)
	m.SetNoConfirm(true)
	cmd := m.triggerAction("ctrl+n")
	if cmd == nil {
		t.Fatal("Send now still required a second confirmation")
	}
	cmd()
	if len(client.answers) != 1 || client.answers[0] != "The saved trigger prompt" {
		t.Fatal("wrong scheduled prompt", client.answers)
	}
}

func TestNoConfirmRunAllRequiresAnExplicitLaunch(t *testing.T) {
	tasks := make(chan []codex.BenchmarkTaskID, 1)
	m := New(benchmarkCaptureFetcher{stubFetcher: stubFetcher{snapshot: codex.DemoSnapshot()}, tasks: tasks}, time.Minute)
	m.SetNoConfirm(true)
	m.snapshot, m.loading, m.meterView, m.benchmarkCombinations = codex.DemoSnapshot(), false, viewBenchmark, 1
	m, cmd := m.activateFooterButton(footerButtonBenchmarkAll)
	if cmd == nil || m.benchmarkState != benchmarkRunning || m.benchmarkAllArmed {
		t.Fatal("Run All still required confirmation")
	}
	select {
	case <-tasks:
		t.Fatal("benchmark ran without explicit command dispatch")
	default:
	}
}

func TestNoConfirmDoesNotTurnComposerTypingIntoAnApproval(t *testing.T) {
	m := approvalTestModel()
	m.SetNoConfirm(true)
	m.monitorPrompt = monitorPromptState{session: "root-one", offer: codex.SessionPromptOffer{Token: "previous", TurnID: "turn"}, input: newMonitorEditor()}
	m.monitorPrompt.input.SetValue("Keep my draft")
	m.monitorPrompt.input.Focus()
	n, cmd := m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if cmd != nil || n.(Model).monitorApprovalBusy || m.fetcher.(*approvalTestClient).decision != "" {
		t.Fatal("typing during a composer/approval transition granted permission")
	}
}

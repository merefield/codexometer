package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorSteerReceipt(t *testing.T) {
	for _, inline := range []bool{false, true} {
		m, _ := turnTestModel()
		if inline {
			m, _ = inlineTurnTestModel()
			m.focusMonitorPrompt()
		}
		m.monitorPrompt.input.SetValue("Use Go please")
		m, cmd, _ := m.submitMonitorTurn("steer")
		m, _, _ = m.updateMonitorPrompt(cmd())
		entries := m.followupEntries()
		if len(entries) != 1 || entries[0].text != "Use Go please" || !entries[0].readOnly || m.monitorPrompt.input.Value() != "" {
			t.Fatalf("missing steer receipt: %+v", entries)
		}
		if len(followupButtons(entries[0], 100)) != 0 {
			t.Fatal("sent steer offered mutation controls")
		}
		for _, action := range []string{"edit", "delete", "now"} {
			if m.followupAction(0, action) != nil || m.monitorQueue.open {
				t.Fatal("sent steer was actionable")
			}
		}
		for _, width := range []int{40, 100, 180} {
			text := ansi.Strip(m.renderMonitorQueue(width, 2, paletteFor(m.theme)))
			if !strings.Contains(text, "STEER SENT") || strings.Contains(text, "[EDIT]") || strings.Contains(text, "[×]") {
				t.Fatalf("bad read-only rendering: %q", text)
			}
		}
		m.monitorContextDetail = "another-session"
		if len(m.followupEntries()) != 0 || len(m.monitorSteers) != 1 {
			t.Fatal("receipt leaked or was lost on navigation")
		}
	}
}

func TestMonitorSteerReconciliation(t *testing.T) {
	m, _ := turnTestModel()
	r := monitorSteerReceipt{session: "root-one", thread: "root-one", turn: "turn", text: "Use Go", baselineID: "old", baselineText: "Use Go"}
	m.recordMonitorSteer(r)
	for _, c := range []codex.SessionContext{
		{},
		{ThreadID: "child", TurnID: "turn", LatestGuidance: "Use Go", LatestGuidanceID: "new"},
		{ThreadID: "root-one", TurnID: "other-turn", LatestGuidance: "Use Go", LatestGuidanceID: "new"},
		{ThreadID: "root-one", TurnID: "turn", LatestGuidance: "Use Go", LatestGuidanceID: "old"},
	} {
		m.monitorSessionData[0].preview = c
		m.reconcileMonitorSteers()
		if len(m.monitorSteers) != 1 {
			t.Fatalf("unrelated or old observation cleared steer: %+v", c)
		}
	}
	m.monitorSessionData[0].preview.LatestGuidanceID = "new"
	m.reconcileMonitorSteers()
	if len(m.monitorSteers) != 0 {
		t.Fatal("new identical guidance did not reconcile")
	}
	// Guidance may arrive before the send acknowledgement.
	m.recordMonitorSteer(r)
	if len(m.monitorSteers) != 0 {
		t.Fatal("already visible guidance created a duplicate receipt")
	}
}

func TestMonitorSteerAckAfterNavigationAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		m, c := turnTestModel()
		if fail {
			c.err = errors.New("unconfirmed")
		}
		m, cmd, _ := m.submitMonitorTurn("steer")
		m.monitorPrompt.session = "other-session"
		m.monitorPrompt.input.SetValue("Other draft")
		m, _, _ = m.updateMonitorPrompt(cmd())
		if m.monitorPrompt.input.Value() != "Other draft" || (len(m.monitorSteers) == 1) == fail {
			t.Fatal("late acknowledgement lost receipt or changed another draft")
		}
	}
}

func TestMonitorSteersSurviveQueueReadsAndReconcileFromTelemetry(t *testing.T) {
	m, c := queueTestModel(false)
	m.recordMonitorSteer(monitorSteerReceipt{session: "root-one", thread: "root-one", turn: "turn", text: "First steer"})
	m.recordMonitorSteer(monitorSteerReceipt{session: "root-one", thread: "root-one", turn: "turn", text: "Latest steer"})
	m.recordMonitorSteer(monitorSteerReceipt{session: "root-two", thread: "root-two", turn: "other", text: "Other session"})
	c.items = nil
	m.monitorQueue.next = time.Time{}
	refresh := m.pollMonitorQueue()
	m, _, _ = m.updateMonitorQueue(refresh())
	if len(m.followupEntries()) != 2 || len(m.monitorSteers) != 3 {
		t.Fatal("empty native queue erased sent steers")
	}
	old := m // model updates must not mutate previous receipt slices
	updated, _, accepted := m.applyMonitorFetch(monitorFetchedMsg{kind: monitorFetchSample, at: time.Now(), usage: codex.LiveUsageSnapshot{
		TotalTokens: m.monitorLatest,
		Sessions: []codex.LiveUsageSession{{ID: "root-one", Active: true, Context: codex.SessionContext{
			ThreadID: "root-one", TurnID: "turn", LatestGuidance: "Latest steer", LatestGuidanceID: "latest",
		}}},
	}})
	m = updated.(Model)
	if !accepted || len(m.monitorSteers) != 1 || m.monitorSteers[0].session != "root-two" || len(old.monitorSteers) != 3 {
		t.Fatal("telemetry failed to retire superseded steers without affecting another session")
	}
}

func TestMonitorSteerRetiresOnlyForPositivelyNewerTurn(t *testing.T) {
	m, _ := turnTestModel()
	now := time.Now()
	m.recordMonitorSteer(monitorSteerReceipt{session: "root-one", thread: "root-one", turn: "turn", text: "Direction", sentAt: now})
	m.monitorSessionData[0].preview = codex.SessionContext{ThreadID: "root-one", TurnID: "previous", At: now.Add(-time.Second)}
	m.reconcileMonitorSteers()
	if len(m.monitorSteers) != 1 {
		t.Fatal("stale turn observation erased sent steer")
	}
	m.monitorSessionData[0].preview = codex.SessionContext{ThreadID: "root-one", TurnID: "next", At: now.Add(time.Second)}
	m.reconcileMonitorSteers()
	if len(m.monitorSteers) != 0 {
		t.Fatal("new turn retained obsolete steering receipt")
	}
}

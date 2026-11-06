package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

type readTestClient struct {
	approvalTestClient
	reads []string
}

func (c *readTestClient) AcknowledgeSessionResult(_ context.Context, id, turn, revision string) (codex.SessionReadState, error) {
	c.reads = append(c.reads, id+"/"+turn+"/"+revision)
	return codex.SessionReadState{Revision: revision + "-read"}, nil
}
func unreadTestModel() (Model, *readTestClient) {
	m := contextTestModel()
	client := &readTestClient{}
	m.fetcher = client
	m.monitorSelectedID = "root-one"
	m.setRowContext("root-one", contextGraph)
	row := &m.monitorSessionData[0]
	row.working = false
	row.attention = codex.SessionAttentionComplete
	row.preview.TurnID = "t1"
	row.readState = codex.SessionReadState{Revision: "v1", Unread: true}
	return m, client
}

func TestUnreadPartialDetailAcknowledgesOnlyExplicitReview(t *testing.T) {
	m, c := unreadTestModel()
	m.render()
	if len(c.reads) != 0 {
		t.Fatal("render marked result read")
	}
	// Selection alone, automatic arrival and geometry restoration do not review.
	m.setRowContext("root-one", contextSplit)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	if m.monitorReadBusy || len(c.reads) != 0 {
		t.Fatal("restored layout marked result read")
	}
	m.setRowContext("root-one", contextGraph)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = next.(Model)
	if cmd == nil || m.rowContextMode("root-one") != contextSplit {
		t.Fatal("opening partial detail did not stage receipt")
	}
	if len(c.reads) != 0 {
		t.Fatal("receipt sent before command dispatch")
	}
	result := cmd().(monitorReadResult)
	if len(c.reads) != 1 || c.reads[0] != "root-one/t1/v1" {
		t.Fatal("wrong viewed result", c.reads)
	}
	next, _ = m.Update(result)
	m = next.(Model)
	if m.monitorSessionData[0].readState.Unread {
		t.Fatal("successful review still unread")
	}
	for _, item := range m.monitorAttentionSessions() {
		if item.id == "root-one" {
			t.Fatal("read completion retained its attention pill")
		}
	}
	if !strings.Contains(m.renderMonitorSessionBadge(m.monitorSessionData[0], 70, paletteFor(themeHacker)), "TURN COMPLETE") {
		t.Fatal("read acknowledgement erased completion status")
	}
	// Another client's new receipt must survive a late acknowledgement response.
	m.monitorSessionData[0].readState = codex.SessionReadState{Revision: "v2", Unread: true}
	next, _ = m.Update(result)
	if next.(Model).monitorSessionData[0].readState.Revision != "v2" {
		t.Fatal("late response cleared newer result")
	}
}

func TestUnreadHistoryAndUnavailableContextCannotAcknowledgeLatest(t *testing.T) {
	for _, mode := range []string{"older-history", "streaming", "child", "no-text", "stale", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			m, c := unreadTestModel()
			m.setRowContext("root-one", contextFull)
			switch mode {
			case "older-history":
				old := m.monitorSessionData[0].preview
				old.TurnID = "older"
				m.saveMonitorHistory("root-one", monitorTurnHistory{selected: &old})
			case "streaming":
				m.monitorSessionData[0].preview.Streaming = true
			case "child":
				m.monitorSessionData[0].preview.ThreadID = "child"
			case "no-text":
				m.monitorSessionData[0].preview.Text = ""
			case "stale":
				m.monitorError = "disconnected"
			case "unsupported":
				m.monitorSessionData[0].readState = codex.SessionReadState{}
			}
			m.monitorReadIntent = "root-one"
			if cmd := m.acknowledgeMonitorResult(); cmd != nil || len(c.reads) != 0 {
				t.Fatal("unviewed result acknowledged")
			}
		})
	}
}

func TestReadReceiptSurvivesMonitorStartAndSessionUpdates(t *testing.T) {
	m, _ := unreadTestModel()
	observed := codex.LiveUsageSession{ID: "root-one", Active: true, ReadState: codex.SessionReadState{Revision: "already-read"}, Attention: codex.SessionAttentionComplete}
	usage := codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{observed}}
	m.startMonitorSessions(usage, time.Now())
	if m.monitorSessionData[0].readState != observed.ReadState {
		t.Fatal("startup discarded saved read receipt")
	}
	observed.ReadState = codex.SessionReadState{Revision: "new", Unread: true}
	usage.Sessions[0] = observed
	m.syncMonitorSessions(usage, time.Now())
	if m.monitorSessionData[0].readState != observed.ReadState {
		t.Fatal("later activity did not restore unread result")
	}
}

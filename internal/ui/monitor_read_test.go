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

func TestUnreadLiveReviewGestures(t *testing.T) {
	for _, action := range []string{"badge:root-one", "history:live"} {
		t.Run(action, func(t *testing.T) {
			m, client := unreadTestModel()
			if action == "history:live" {
				m.setRowContext("root-one", contextFull)
				old := m.monitorSessionData[0].preview
				old.TurnID = "older"
				m.saveMonitorHistory("root-one", monitorTurnHistory{selected: &old})
			}
			for y := 0; y < m.height; y++ {
				for x := 0; x < m.width; x++ {
					if m.monitorContextAt(x, y) != action {
						continue
					}
					next, _, handled := m.updateMonitorContextMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
					if !handled {
						t.Fatal("review gesture not handled")
					}
					cmd := next.acknowledgeMonitorResult()
					if cmd == nil {
						t.Fatal("explicit review did not stage receipt")
					}
					cmd()
					if len(client.reads) != 1 {
						t.Fatal("review did not acknowledge result")
					}
					return
				}
			}
			t.Fatal("review control has no click target")
		})
	}
	m, _ := unreadTestModel()
	m.setRowContext("root-one", contextFull)
	m.showLiveMonitorHistory()
	if m.monitorReadIntent != "" {
		t.Fatal("automatic Live restoration staged a receipt")
	}
}

func TestUnreadIndependentPartialPaneWithHiddenGlobalLayout(t *testing.T) {
	m, client := unreadTestModel()
	m.monitorContextHidden = true
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = next.(Model)
	if !m.contextTargetHidden() || m.rowContextMode("root-one") != contextSplit || cmd == nil || !m.monitorReadBusy {
		t.Fatal("opening independent partial pane did not acknowledge visible result")
	}
	cmd()
	if len(client.reads) != 1 {
		t.Fatal("visible partial pane was not acknowledged")
	}
	m.monitorReadBusy = false
	rect, ok := m.monitorResultRect("root-one")
	if !ok {
		t.Fatal("partial result has no visible body")
	}
	next, cmd = m.Update(tea.MouseWheelMsg{X: rect.x, Y: rect.y, Button: tea.MouseWheelDown})
	if !next.(Model).monitorReadBusy || cmd == nil {
		t.Fatal("wheel over independent result did not acknowledge it")
	}
}

func TestUnreadCompactPartialResultBounds(t *testing.T) {
	for _, mode := range []int{contextSplit, contextWide} {
		for _, height := range []int{3, 4} {
			found := false
			for terminalHeight := 12; terminalHeight < 70; terminalHeight++ {
				m, _ := unreadTestModel()
				m.height = terminalHeight
				m.setRowContext("root-one", mode)
				for _, pane := range m.monitorPartialContextPanes() {
					if pane.session.id != "root-one" || pane.frame.height != height || pane.frame.width < 5 {
						continue
					}
					rect, ok := m.monitorResultRect("root-one")
					if !ok || rect.y != m.monitorDashboardLayout().meterY+pane.frame.y+1 || rect.height != height-2 {
						t.Fatalf("mode %v height %d: visible compact reply has incorrect bounds: %+v", mode, height, rect)
					}
					next, cmd := m.Update(tea.MouseWheelMsg{X: rect.x, Y: rect.y, Button: tea.MouseWheelUp})
					if cmd == nil || !next.(Model).monitorReadBusy {
						t.Fatalf("visible compact reply could not be reviewed: mode=%d paneHeight=%d terminalHeight=%d rect=%+v model=%+v", mode, height, terminalHeight, rect, next.(Model).monitorError)
					}
					found = true
					break
				}
				if found {
					break
				}
			}
			if !found {
				t.Fatalf("no %d-row pane in mode %v", height, mode)
			}
		}
	}
}

func TestUnreadFullResultBoundsExcludeControls(t *testing.T) {
	for _, history := range []bool{false, true} {
		for _, height := range []int{18, 40} {
			m, _ := promptTestModel()
			m.height = height
			if history {
				old := m.monitorSessionData[0].preview
				old.TurnID = "older"
				m.saveMonitorHistory("root-one", monitorTurnHistory{turns: []codex.SessionContext{old}})
			}
			g := m.monitorDashboardLayout()
			controls := m.layoutDetailControls(g.contentWidth, g.meterHeight)
			if controls.rows == 0 {
				t.Fatal("fixture has no composer controls")
			}
			rect, ok := m.monitorResultRect("root-one")
			_, _, controlY := m.detailBodyLayout(g.contentWidth, g.meterHeight, controls.rows)
			firstBody := g.meterY + 1 + m.historyNavigationRows(g.contentWidth, g.meterHeight)
			if !ok || rect.y != firstBody || !rect.contains(rect.x, firstBody) || rect.contains(rect.x, g.meterY+controlY) {
				t.Fatalf("history %v height %d: bounds include controls or miss first body row: %+v controlY=%d", history, height, rect, controlY)
			}
		}
	}
}

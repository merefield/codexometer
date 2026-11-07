package ui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

type monitorReadResult struct {
	id, revision string
	state        codex.SessionReadState
	err          error
}

// Only deliberate input may acknowledge a result. Rendering, refreshes,
// restored layouts, hover and automatic history hydration never do so.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if result, ok := message.(monitorReadResult); ok {
		m.monitorReadBusy = false
		if result.err == nil {
			m.monitorSessionData = slices.Clone(m.monitorSessionData)
			for i := range m.monitorSessionData {
				row := &m.monitorSessionData[i]
				if row.id == result.id && row.readState.Revision == result.revision {
					row.readState = result.state
				}
			}
		}
		return m, nil
	}
	m.monitorReadIntent = ""
	next, command := m.update(message)
	updated := next.(Model)
	deliberate := false
	switch msg := message.(type) {
	case tea.KeyPressMsg:
		deliberate = true
	case tea.MouseClickMsg:
		deliberate = msg.Button == tea.MouseLeft
	case tea.MouseWheelMsg:
		deliberate = true
		updated.monitorReadIntent = ""
		mouse := msg.Mouse()
		for _, row := range m.monitorSessionData {
			if rect, ok := m.monitorResultRect(row.id); ok && rect.contains(mouse.X, mouse.Y) {
				updated.monitorReadIntent = row.id
				break
			}
		}

	}
	if !deliberate {
		updated.monitorReadIntent = ""
	}
	receipt := updated.acknowledgeMonitorResult()
	return updated, tea.Batch(command, receipt)
}

func (m *Model) acknowledgeMonitorResult() tea.Cmd {
	id := m.monitorReadIntent
	m.monitorReadIntent = ""
	client, ok := m.fetcher.(codex.SessionReadStateClient)
	if !ok || id == "" || m.monitorReadBusy || m.meterView != viewMonitor || m.monitorState != monitorRunning || m.monitorError != "" || m.monitorCommands.open || m.monitorQueue.open || m.scheduleUI.open || m.rowContextMode(id) == contextGraph {
		return nil
	}
	for _, row := range m.monitorSessionData {
		c := row.preview
		if row.id != id || m.hasSessionProfile(row) || !m.monitorSessionVisible(row) || !row.readState.Unread || !row.readState.Known() || c.Kind != codex.SessionContextReply || c.Streaming || c.Text == "" || c.ThreadID != id || c.TurnID == "" {
			continue
		}
		if _, visible := m.monitorResultRect(id); !visible {
			return nil
		}
		if h := m.monitorHistory[id]; h.selected != nil && !sameHistoryTurn(*h.selected, c) {
			return nil
		}
		revision, turn := row.readState.Revision, c.TurnID
		m.monitorReadBusy = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			state, err := client.AcknowledgeSessionResult(ctx, id, turn, revision)
			return monitorReadResult{id: id, revision: revision, state: state, err: err}
		}
	}
	return nil
}

// Match the actual displayed context pane, including narrow split layouts
// which render only a graph. Graphs, controls and off-page rows are not reviews.
func (m Model) monitorResultRect(id string) (monitorRect, bool) {
	g := m.monitorDashboardLayout()
	if m.monitorContextDetail != "" {
		if m.monitorContextDetail != id || m.contextTargetHidden() {
			return monitorRect{}, false
		}
		controls := m.layoutDetailControls(g.contentWidth, g.meterHeight)
		rows, _, _ := m.detailBodyLayout(g.contentWidth, g.meterHeight, controls.rows)
		rect := monitorRect{x: 4, y: g.meterY + 2, width: g.contentWidth - 4, height: rows}
		return rect, rect.width > 0 && rect.height > 0
	}
	for _, pane := range m.monitorPartialContextPanes() {
		if pane.session.id == id {
			r := pane.frame
			rect := monitorRect{x: r.x + 4, y: g.meterY + r.y + 2, width: r.width - 4, height: r.height - 4}
			return rect, rect.width > 0 && rect.height > 0
		}
	}

	return monitorRect{}, false
}

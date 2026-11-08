package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type monitorReadResult struct {
	id, revision string
	state        codex.SessionReadState
	err          error
}

const monitorReadDelay = 3 * time.Second

type monitorReadDwell struct {
	id, turn, revision string
	until              time.Time
}

type monitorReadDwellElapsed monitorReadDwell

// Only deliberate selection/expansion starts the wide-row dwell. Full detail
// reviews immediately; rendering, restored layouts and result arrival do not.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if elapsed, ok := message.(monitorReadDwellElapsed); ok {
		command := m.finishMonitorReadDwell(monitorReadDwell(elapsed), time.Now())
		return m, command
	}
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
	if d := m.monitorReadDwell; d.id != "" && !m.validMonitorReadDwell(d) {
		m.monitorReadDwell = monitorReadDwell{}
	}
	m.monitorReadIntent = ""
	next, command := m.update(message)
	updated := next.(Model)
	deliberate := false
	switch msg := message.(type) {
	case tea.KeyPressMsg:
		deliberate = !msg.IsRepeat
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
	if d := updated.monitorReadDwell; d.id != "" && !updated.validMonitorReadDwell(d) {
		updated.monitorReadDwell = monitorReadDwell{}
	}
	id := updated.monitorReadIntent
	updated.monitorReadIntent = ""
	var receipt tea.Cmd
	if id != "" && updated.monitorContextDetail == id {
		updated.monitorReadDwell = monitorReadDwell{}
		updated.monitorReadIntent = id
		receipt = updated.acknowledgeMonitorResult()
	} else if id != "" && updated.monitorReadDwell.id == "" {
		row, eligible := updated.monitorWideReadResult(id)
		if eligible {
			d := monitorReadDwell{id: id, turn: row.preview.TurnID, revision: row.readState.Revision, until: time.Now().Add(monitorReadDelay)}
			updated.monitorReadDwell = d
			receipt = tea.Tick(monitorReadDelay, func(time.Time) tea.Msg { return monitorReadDwellElapsed(d) })
		}
	}
	return updated, tea.Batch(command, receipt)
}

func (m Model) monitorUnreadResult(id string) (monitorSession, bool) {
	if _, ok := m.fetcher.(codex.SessionReadStateClient); !ok || id == "" || m.width <= 0 || m.height <= 0 || m.actionReview != nil || m.monitorReadBusy || m.meterView != viewMonitor || m.monitorState != monitorRunning || m.monitorError != "" || m.monitorCommands.open || m.monitorQueue.open || m.scheduleUI.open {
		return monitorSession{}, false
	}
	for _, row := range m.monitorSessionData {
		c := row.preview
		if row.id != id || m.hasSessionProfile(row) || !m.monitorSessionVisible(row) || !row.readState.Unread || !row.readState.Known() || c.Kind != codex.SessionContextReply || c.Streaming || c.Text == "" || c.ThreadID != id || c.TurnID == "" {
			continue
		}
		rect, visible := m.monitorResultRect(id)
		if !visible {
			return monitorSession{}, false
		}
		if m.monitorContextDetail == id {
			document := m.contextDetailDocument(rect.width)
			start, end := m.detailVisibleRange(len(document), rect.height)
			if !slices.ContainsFunc(document[start:end], func(line detailLine) bool {
				return line.kind == "reply" && strings.TrimSpace(ansi.Strip(line.text)) != ""
			}) {
				return monitorSession{}, false
			}
		}
		if h := m.monitorHistory[id]; h.selected != nil && !sameHistoryTurn(*h.selected, c) {
			return monitorSession{}, false
		}
		return row, true
	}
	return monitorSession{}, false
}

func (m Model) monitorWideReadResult(id string) (monitorSession, bool) {
	if m.monitorContextDetail != "" || m.monitorSelectedID != id || m.rowContextMode(id) != contextWide || m.monitorSummaryFocused || m.monitorAttentionFocused || m.monitorPrompt.input.Focused() {
		return monitorSession{}, false
	}
	row, ok := m.monitorUnreadResult(id)
	if !ok {
		return monitorSession{}, false
	}
	rect, _ := m.monitorResultRect(id)
	header := terminalLabel(row.preview.ThreadID) + " // " + terminalLabel(row.preview.Source)
	if rect.height <= len(sessionWebTextLines(header, max(rect.width, 1))) {
		return monitorSession{}, false // Source metadata alone is not a visible reply.
	}
	return row, true
}

func (m Model) validMonitorReadDwell(d monitorReadDwell) bool {
	row, ok := m.monitorWideReadResult(d.id)
	return ok && row.preview.TurnID == d.turn && row.readState.Revision == d.revision
}

func (m *Model) finishMonitorReadDwell(d monitorReadDwell, now time.Time) tea.Cmd {
	if d.id == "" || m.monitorReadDwell != d || now.Before(d.until) {
		return nil
	}
	m.monitorReadDwell = monitorReadDwell{}
	if !m.validMonitorReadDwell(d) {
		return nil
	}
	m.monitorReadIntent = d.id
	return m.acknowledgeMonitorResult()
}

func (m *Model) acknowledgeMonitorResult() tea.Cmd {
	id := m.monitorReadIntent
	m.monitorReadIntent = ""
	if m.monitorContextDetail != id && (m.monitorSelectedID != id || m.rowContextMode(id) != contextWide) {
		return nil
	}
	row, ok := m.monitorUnreadResult(id)
	if !ok {
		return nil
	}
	client := m.fetcher.(codex.SessionReadStateClient)
	revision, turn := row.readState.Revision, row.preview.TurnID
	m.monitorReadBusy = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		state, err := client.AcknowledgeSessionResult(ctx, id, turn, revision)
		return monitorReadResult{id: id, revision: revision, state: state, err: err}
	}
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
		rect := monitorRect{x: 4, y: g.meterY + 1 + m.historyNavigationRows(g.contentWidth, g.meterHeight), width: g.contentWidth - 4, height: rows}
		return rect, rect.width > 0 && rect.height > 0
	}
	for _, pane := range m.monitorPartialContextPanes() {
		if pane.session.id == id {
			r := pane.frame
			rows, _, _ := monitorContextBodyLayout(r.height, 0)
			if m.rowContextMode(id) == contextWide {
				controls := m.expandedControlRows(r.width, r.height, pane.session)
				if id == m.monitorContextTarget() {
					controls += m.monitorQueueRows(r.width, r.height, controls)
				}
				rows, _, _ = monitorContextBodyLayout(r.height, controls)
			} else {
				rows = min(rows, 2) // split previews display at most two reply rows
			}
			rect := monitorRect{x: r.x + 4, y: g.meterY + r.y + 1, width: r.width - 4, height: rows}
			return rect, rect.width > 0 && rect.height > 0
		}
	}

	return monitorRect{}, false
}

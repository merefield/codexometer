package ui

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type monitorTurnHistory struct {
	turns []codex.SessionContext
	// Pin an immutable excerpt rather than an index: incoming turns and cache
	// eviction must never change the answer somebody is currently reading.
	selected    *codex.SessionContext
	requestedAt time.Time
	// Successful server reads own the order of replies observed before the
	// request. Lagging telemetry may update those entries, but not reappend
	// an older reply that the bounded server page has already evicted.
	observedThrough time.Time
	busy            bool
	request         uint64
}

type monitorHistoryResult struct {
	id      string
	request uint64
	turns   []codex.SessionContext
	err     error
}

func sameHistoryTurn(a, b codex.SessionContext) bool {
	if a.ThreadID != b.ThreadID {
		return false
	}
	if a.TurnID != "" && b.TurnID != "" {
		return a.TurnID == b.TurnID
	}
	return a.ItemID == b.ItemID && a.At.Equal(b.At) && a.Text == b.Text
}

func (m *Model) saveMonitorHistory(id string, h monitorTurnHistory) {
	m.monitorHistory = maps.Clone(m.monitorHistory)
	if m.monitorHistory == nil {
		m.monitorHistory = map[string]monitorTurnHistory{}
	}
	m.monitorHistory[id] = h
}

func addHistoryTurn(h monitorTurnHistory, c codex.SessionContext) monitorTurnHistory {
	if c.Kind != codex.SessionContextReply || c.Streaming || strings.TrimSpace(c.Text) == "" {
		return h
	}
	c = codex.HistoryExcerpt(c)
	for i, old := range h.turns {
		if sameHistoryTurn(old, c) {
			if old == c {
				return h
			}
			h.turns = slices.Clone(h.turns)
			h.turns[i] = c
			return h
		}
	}
	h.turns = slices.Clone(h.turns)
	h.turns = append(h.turns, c)
	if len(h.turns) > codex.SessionHistoryLimit {
		h.turns = slices.Clone(h.turns[len(h.turns)-codex.SessionHistoryLimit:])
	}
	return h
}

func (m *Model) observeMonitorHistory() {
	cloned := false
	for _, s := range m.monitorSessionData {
		if s.preview.Kind == codex.SessionContextReply && !s.preview.Streaming && s.preview.Text != "" {
			h := m.monitorHistory[s.id]
			if !h.observedThrough.IsZero() && !s.preview.At.After(h.observedThrough) &&
				!slices.ContainsFunc(h.turns, func(c codex.SessionContext) bool { return sameHistoryTurn(c, s.preview) }) {
				continue
			}
			next := addHistoryTurn(h, s.preview)
			if slices.Equal(h.turns, next.turns) {
				continue
			}
			if !cloned {
				m.monitorHistory = maps.Clone(m.monitorHistory)
				if m.monitorHistory == nil {
					m.monitorHistory = map[string]monitorTurnHistory{}
				}
				cloned = true
			}
			m.monitorHistory[s.id] = next
		}
	}
}

func (m Model) historicalContext() (codex.SessionContext, bool) {
	if m.monitorContextDetail == "" || m.contextTargetHidden() {
		return codex.SessionContext{}, false
	}
	h := m.monitorHistory[m.monitorContextDetail]
	if h.selected == nil {
		return codex.SessionContext{}, false
	}
	return *h.selected, true
}

func (m *Model) moveMonitorHistory(delta int) {
	id := m.monitorContextDetail
	if id == "" {
		return
	}
	m.observeMonitorHistory()
	h := m.monitorHistory[id]
	index := len(h.turns)
	if h.selected != nil {
		index = -1 // pinned entry may already have fallen outside the cache
		for i, c := range h.turns {
			if sameHistoryTurn(c, *h.selected) {
				index = i
				break
			}
		}
	} else if s, ok := m.contextDetailSession(); ok && len(h.turns) > 0 && sameHistoryTurn(h.turns[len(h.turns)-1], s.preview) {
		// Live already displays this completed answer; Previous means the
		// answer before it, not a duplicate of the current screen.
		index--
	}
	index += delta
	if index < 0 {
		return
	}
	if index >= len(h.turns) {
		h.selected = nil
	} else {
		c := h.turns[index]
		h.selected = &c
	}
	m.saveMonitorHistory(id, h)
	m.monitorContextScroll = 0
	m.monitorPrompt.input.Blur()
	m.monitorApprovalConfirm = ""
	m.monitorApprovalNumberReleased = false
}

func (m *Model) pollMonitorHistory(now time.Time) tea.Cmd {
	id := m.monitorContextDetail
	if m.meterView != viewMonitor || id == "" || m.contextTargetHidden() || m.scheduleUI.open || m.monitorCommands.open || m.monitorQueue.open {
		return nil
	}
	c, ok := m.fetcher.(codex.SessionHistoryClient)
	if !ok {
		return nil
	}
	h := m.monitorHistory[id]
	if h.busy || !h.requestedAt.IsZero() && now.Sub(h.requestedAt) < 5*time.Second {
		return nil
	}
	h.busy = true
	h.requestedAt = now
	m.monitorHistorySequence++
	h.request = m.monitorHistorySequence
	m.saveMonitorHistory(id, h)
	request := h.request
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		turns, err := c.SessionHistory(ctx, id)
		return monitorHistoryResult{id, request, turns, err}
	}
}

func (m Model) updateMonitorHistory(msg tea.Msg) (Model, tea.Cmd, bool) {
	if result, ok := msg.(monitorHistoryResult); ok {
		h, exists := m.monitorHistory[result.id]
		if !exists || h.request != result.request {
			return m, nil, true
		}
		h.busy = false
		if result.err == nil && len(result.turns) > 0 {
			h.observedThrough = h.requestedAt
			// Server order is authoritative. Merge only observed turns not in
			// that page (e.g. a completion newer than the asynchronous read).
			old := h.turns
			h.turns = nil
			for _, c := range result.turns {
				for _, previous := range old {
					if sameHistoryTurn(c, previous) && c.LatestGuidance == "" {
						c.LatestGuidance = previous.LatestGuidance
						break
					}
				}
				h = addHistoryTurn(h, c)
			}
			for _, c := range old {
				found := false
				for _, v := range h.turns {
					if sameHistoryTurn(c, v) {
						found = true
						break
					}
				}
				if !found && c.At.After(h.requestedAt) {
					h = addHistoryTurn(h, c)
				}
			}
		}
		m.saveMonitorHistory(result.id, h)
		return m, nil, true
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && (key.String() == "alt+left" || key.String() == "alt+right") &&
		m.meterView == viewMonitor && m.monitorContextDetail != "" && !m.contextTargetHidden() && !m.monitorCommands.open && !m.scheduleUI.open && !m.monitorQueue.open {
		delta := -1
		if key.String() == "alt+right" {
			delta = 1
		}
		m.moveMonitorHistory(delta)
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) historyNavigationButtons(width, height int) []monitorNavigationButton {
	textRows, _, _ := monitorContextBodyLayout(height, m.layoutDetailControls(width, height).rows)
	if textRows < 2 {
		return nil
	}
	h := m.monitorHistory[m.monitorContextDetail]
	if len(h.turns) == 0 && h.selected == nil {
		return nil
	}
	older := len(h.turns) > 0
	if h.selected != nil {
		older = false
		for i, c := range h.turns {
			if sameHistoryTurn(c, *h.selected) {
				older = i > 0
				break
			}
		}
	} else if s, ok := m.contextDetailSession(); ok && len(h.turns) == 1 && sameHistoryTurn(h.turns[0], s.preview) {
		older = false
	}
	if !older && h.selected == nil {
		return nil
	}
	buttons := []monitorNavigationButton{
		{label: i18n.Text("[ Alt+←: PREVIOUS ]"), action: "history:previous", enabled: older},
		{label: i18n.Text("[ Alt+→: NEXT ]"), action: "history:next", enabled: h.selected != nil},
		{label: i18n.Text("[ LIVE ]"), action: "history:live", enabled: h.selected != nil},
	}
	x := 2
	for i := range buttons {
		buttons[i].rect = monitorRect{x: x, y: 1, width: lipgloss.Width(buttons[i].label), height: 1}
		x += buttons[i].rect.width + 1
	}
	if x > width-2 {
		for i, label := range []string{"[Alt+←]", "[Alt+→]", i18n.Text("[ LIVE ]")} {
			buttons[i].label = label
		}
		x = 2
		for i := range buttons {
			buttons[i].rect = monitorRect{x: x, y: 1, width: lipgloss.Width(buttons[i].label), height: 1}
			x += buttons[i].rect.width + 1
		}
	}
	if x > width-2 {
		return nil
	}
	return buttons
}

func (m Model) historyNavigationRows(width, height int) int {
	if len(m.historyNavigationButtons(width, height)) > 0 {
		return 1
	}
	return 0
}

func (m Model) detailBodyLayout(width, height, controls int) (textRows, gap, controlY int) {
	textRows, gap, controlY = monitorContextBodyLayout(height, controls)
	textRows = max(textRows-m.historyNavigationRows(width, height), 0)
	return
}

func (m *Model) showLiveMonitorHistory() {
	id := m.monitorContextDetail
	h := m.monitorHistory[id]
	if h.selected != nil {
		h.selected = nil
		m.saveMonitorHistory(id, h)
		m.monitorContextScroll = 0
	}
}

func (m Model) historyDocument(width int, s monitorSession, c codex.SessionContext) []detailLine {
	h := m.monitorHistory[s.id]
	heading := i18n.Text("PREVIOUS TURN")
	for i, v := range h.turns {
		if sameHistoryTurn(c, v) {
			heading += i18n.Format(" // %d OF %d", i+1, len(h.turns))
			break
		}
	}
	lines := []detailLine{{ansi.Truncate(heading, width, "…"), "heading"}}
	appendSection := func(title, text string) {
		if text == "" {
			return
		}
		lines = append(lines, detailLine{}, detailLine{ansi.Truncate(title, width, "…"), "heading"})
		for _, line := range sessionWebTextLines(codex.SanitizeSessionContext(text), width) {
			lines = append(lines, detailLine{ansi.Truncate(line, width, ""), "body"})
		}
	}
	appendSection(i18n.Text("TASK"), c.CurrentTask)
	appendSection(i18n.Text("LATEST GUIDANCE"), c.LatestGuidance)
	appendSection(i18n.Text("LAST REPLY"), c.Text)
	// Current requests are still live; never render controls beside a saved
	// approval or present historical text as the justification for a new one.
	if s.preview.Kind == codex.SessionContextApproval || s.preview.Kind == codex.SessionContextQuestion || m.hasSessionProfile(s) {
		lines = append(lines, detailLine{}, detailLine{ansi.Truncate(i18n.Text("LIVE SESSION"), width, "…"), "warning"})
		live := m
		live.monitorHistory = nil
		lines = append(lines, live.contextDetailDocument(width)...)
	}
	return lines
}

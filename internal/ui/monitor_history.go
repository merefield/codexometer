package ui

import (
	"context"
	"maps"
	"runtime"
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

func (m *Model) saveMonitorHistory(id string, h monitorTurnHistory) {
	m.monitorHistory = maps.Clone(m.monitorHistory)
	if m.monitorHistory == nil {
		m.monitorHistory = map[string]monitorTurnHistory{}
	}
	m.monitorHistory[id] = h
}

func (h *monitorTurnHistory) protectPinned(c codex.SessionContext) {
	if h.selected != nil && c.ReplyRestricted {
		pinned := projectMonitorReply(c, *h.selected)
		if pinned != *h.selected {
			h.selected = &pinned
		}
	}
}

func addHistoryTurn(h monitorTurnHistory, c codex.SessionContext) monitorTurnHistory {
	if c.Kind != codex.SessionContextReply || c.Streaming || !c.ReplyRestricted && strings.TrimSpace(c.Text) == "" {
		return h
	}
	c = projectMonitorReply(codex.SessionContext{}, codex.HistoryExcerpt(c))
	h.protectPinned(c)
	for i, old := range h.turns {
		if old.SameHistoryTurn(c) {
			c = projectMonitorReply(old, c)
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
		if s.preview.Kind == codex.SessionContextReply && !s.preview.Streaming && s.preview.HasDisplayText() {
			h := m.monitorHistory[s.id]
			next := h
			if !h.observedThrough.IsZero() && !s.preview.At.After(h.observedThrough) &&
				!slices.ContainsFunc(h.turns, func(c codex.SessionContext) bool { return c.SameHistoryTurn(s.preview) }) {
				// The selected excerpt can outlive the page it came from.
				// Revoke it without putting a stale turn back on that page.
				next.protectPinned(s.preview)
			} else {
				next = addHistoryTurn(h, s.preview)
			}
			if slices.Equal(h.turns, next.turns) && h.selected == next.selected {
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
			if c.SameHistoryTurn(*h.selected) {
				index = i
				break
			}
		}
	} else if s, ok := m.contextDetailSession(); ok && len(h.turns) > 0 && h.turns[len(h.turns)-1].SameHistoryTurn(s.preview) {
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
	m.monitorReadIntent = id
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
					if c.SameHistoryTurn(previous) {
						previous.ProtectReply(&c)
						if c.LatestGuidance == "" {
							c.LatestGuidance = previous.LatestGuidance
						}
						break
					}
				}
				if c.ReplyRestricted {
					if i := m.monitorSessionIndex(result.id); i >= 0 {
						m.monitorSessionData = slices.Clone(m.monitorSessionData)
						row := &m.monitorSessionData[i]
						row.preview = projectMonitorReply(c, row.preview)
					}
				}
				h = addHistoryTurn(h, c)
			}
			for _, c := range old {
				found := false
				for _, v := range h.turns {
					if c.SameHistoryTurn(v) {
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
	if key, ok := msg.(tea.KeyPressMsg); ok &&
		m.meterView == viewMonitor && m.monitorContextDetail != "" && !m.contextTargetHidden() && !m.monitorCommands.open && !m.scheduleUI.open && !m.monitorQueue.open {
		delta := 0
		switch key.String() {
		case "alt+left":
			delta = -1
		case "alt+right":
			delta = 1
		case "alt+b", "alt+f":
			// Terminal.app and Ghostty's macOS defaults encode Option+arrows
			// as word movement. Keep those keys with a focused reply editor.
			if runtime.GOOS == "darwin" && !m.monitorPrompt.input.Focused() {
				delta = -1
				if key.String() == "alt+f" {
					delta = 1
				}
			}
		}
		if delta != 0 {
			m.moveMonitorHistory(delta)
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m Model) historyNavigationButtons(width, height int) []monitorNavigationButton {
	buttons, _, _ := m.detailBodyLayout(width, height, m.layoutDetailControls(width, height).rows)
	return buttons
}

func (m Model) historyButtonsForRows(width, textRows int) []monitorNavigationButton {
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
			if c.SameHistoryTurn(*h.selected) {
				older = i > 0
				break
			}
		}
	} else if s, ok := m.contextDetailSession(); ok && len(h.turns) == 1 && h.turns[0].SameHistoryTurn(s.preview) {
		older = false
	}
	if !older && h.selected == nil {
		return nil
	}
	modifier := "Alt"
	if runtime.GOOS == "darwin" {
		modifier = "Opt"
	}
	buttons := []monitorNavigationButton{
		{label: strings.Replace(i18n.Text("[ Alt+←: PREVIOUS ]"), "Alt+", modifier+"+", 1), action: "history:previous", enabled: older},
		{label: strings.Replace(i18n.Text("[ Alt+→: NEXT ]"), "Alt+", modifier+"+", 1), action: "history:next", enabled: h.selected != nil},
		{label: i18n.Text("[ LIVE ]"), action: "history:live", enabled: h.selected != nil},
	}
	if layoutNavigationButtons(buttons, 2, 1) <= width-2 {
		return buttons
	}
	buttons[0].label = "[" + modifier + "+←]"
	buttons[1].label = "[" + modifier + "+→]"
	if layoutNavigationButtons(buttons, 2, 1) > width-2 {
		return nil
	}
	return buttons
}

func (m Model) detailBodyLayout(width, height, controls int) (buttons []monitorNavigationButton, textRows, controlY int) {
	textRows, _, controlY = monitorContextBodyLayout(height, controls)
	buttons = m.historyButtonsForRows(width, textRows)
	if len(buttons) > 0 {
		textRows--
	}
	return
}

// Use the observation's timestamp, not the duration of the turn or command.
func observationLines(c codex.SessionContext, width int) []detailLine {
	age := "--"
	if !c.At.IsZero() {
		age = i18n.Format("%s ago", strings.ToLower(formatMonitorAge(time.Since(c.At))))
	}
	var fields []string
	for _, s := range []string{c.Source, c.ThreadID} {
		if label := terminalLabel(s); label != "" {
			fields = append(fields, label)
		}
	}
	var lines []detailLine
	for _, s := range strings.Split(ansi.Hardwrap(strings.Join(append(fields, age), " // "), width, true), "\n") {
		lines = append(lines, detailLine{ansi.Truncate(s, width, ""), "observation"})
	}
	return lines
}

type monitorDetailPage struct {
	document []detailLine
	buttons  []monitorNavigationButton
	right    string // leading padding plus metadata; never part of a button
	rows     int
}

// Rendering, scrolling and read receipts must agree about which observations
// moved out of the document and how many rows are available to show replies.
func (m Model) detailPage(width, height, controls int) monitorDetailPage {
	var p monitorDetailPage
	p.buttons, p.rows, _ = m.detailBodyLayout(width, height, controls)
	inner := max(1, width-4)
	doc := m.contextDetailDocument(inner)
	n := 0
	for n < len(doc) && doc[n].kind == "observation" {
		n++
	}
	if n == 1 && len(p.buttons) > 0 {
		first, last := p.buttons[0].rect, p.buttons[len(p.buttons)-1].rect
		gap := inner - (last.x + last.width - first.x) - lipgloss.Width(doc[0].text)
		if gap >= 1 {
			p.right = strings.Repeat(" ", gap) + doc[0].text
			doc, n = doc[1:], 0
		}
	}
	// Keep a blank before the first heading if it fits. For an already
	// scrolling document, keep it on roomy viewports but not on short ones.
	// A short viewport with a slash popup uses that row for reply text.
	compactPopup := p.rows < 8 && (m.monitorCommands.open || m.suggestionsActive())
	if n < len(doc) && doc[n] == (detailLine{}) &&
		(compactPopup || len(doc) > p.rows && (p.rows < 8 || len(doc)-1 == p.rows)) {
		doc = append(doc[:n], doc[n+1:]...)
	}
	p.document = spaceCommandReview(doc, p.rows, false)
	return p
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
		if c.SameHistoryTurn(v) {
			heading += i18n.Format(" // %d OF %d", i+1, len(h.turns))
			break
		}
	}
	lines := append(observationLines(c, width), detailLine{}, detailLine{ansi.Truncate(heading, width, "…"), "heading"})
	appendSection := func(title, text, kind string) {
		if text == "" {
			return
		}
		lines = append(lines, detailLine{}, detailLine{ansi.Truncate(title, width, "…"), "heading"})
		wrap := sessionWebTextLines
		if kind == "prose" || kind == "reply" {
			wrap = m.proseLines
		}
		for _, line := range wrap(codex.SanitizeSessionContext(text), width) {
			lines = append(lines, detailLine{ansi.Truncate(line, width, ""), kind})
		}
	}
	appendSection(i18n.Text("TASK"), c.CurrentTask, "body")
	appendSection(i18n.Text("LATEST GUIDANCE"), c.LatestGuidance, "body")
	replyKind := "prose"
	if c.Kind == codex.SessionContextReply && c.SameHistoryTurn(s.preview) && c.Text == s.preview.Text {
		replyKind = "reply"
	}
	appendSection(i18n.Text("LAST REPLY"), c.Text, replyKind)
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

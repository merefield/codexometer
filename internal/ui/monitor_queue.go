package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
)

type followupEntry struct {
	label, text              string
	native                   *codex.SessionQueuedMessage
	trigger                  *schedule.Job
	editable, removable, now bool
}

func (entry followupEntry) key() string {
	if entry.native != nil {
		return "native:" + entry.native.ID
	}
	return "scheduled:" + entry.trigger.ID
}

func (m Model) followupEntries() []followupEntry {
	var entries []followupEntry
	if m.monitorQueue.session == m.monitorContextTarget() {
		for i, item := range m.monitorQueue.items {
			label := "NEXT TURN"
			if i > 0 {
				label = "THEN"
			}
			entries = append(entries, followupEntry{label: label, text: item.Text, native: &item, editable: item.Editable && !m.monitorQueue.stale, removable: !m.monitorQueue.stale})
		}
	}
	if j, ok := m.pendingTrigger(); ok {
		label := "AFTER QUOTA REFRESH"
		if j.Trigger == "at" {
			label = "AT " + j.At.Local().Format("02 Jan 15:04 MST")
		}
		if j.Status != "pending" {
			label += " // " + strings.ToUpper(j.Status)
		}
		entries = append(entries, followupEntry{label: label, text: j.Text, trigger: &j, editable: j.Status == "pending", removable: j.Status != "sending", now: m.triggerReady(j)})
	}
	return entries
}

func followupButtons(entry followupEntry, width int) []triggerButton {
	var buttons []triggerButton
	if entry.trigger != nil {
		buttons = append(buttons, triggerButton{text: "[NOW]", key: "now", enabled: entry.now})
	}
	buttons = append(buttons, triggerButton{text: "[EDIT]", key: "edit", enabled: entry.editable}, triggerButton{text: "[×]", key: "delete", enabled: entry.removable})
	total := len(buttons) - 1
	for _, b := range buttons {
		total += ansi.StringWidth(b.text)
	}
	x := max(width-4-total, 0)
	for i := range buttons {
		buttons[i].x = x
		x += ansi.StringWidth(buttons[i].text) + 1
	}
	return buttons
}

type monitorQueueState struct {
	hover                                         string
	focusSession                                  string
	session                                       string
	items                                         []codex.SessionQueuedMessage
	loading, busy, open, deleting, focused, stale bool
	request                                       uint64
	revision                                      uint64
	next                                          time.Time
	offset                                        int
	item                                          codex.SessionQueuedMessage
	input                                         monitorEditor
	notice                                        string
}
type monitorQueueResult struct {
	session           string
	request, revision uint64
	items             []codex.SessionQueuedMessage
	err               error
}
type monitorQueueChanged struct{ err error }

func (m *Model) pollMonitorQueue() tea.Cmd {
	q := &m.monitorQueue
	c, ok := m.fetcher.(codex.SessionQueueClient)
	id := m.monitorContextTarget()
	if !ok || m.meterView != viewMonitor || id == "" || m.contextTargetHidden() || q.open || q.busy || q.loading || m.scheduleUI.open {
		return nil
	}
	if m.monitorContextDetail == "" && (m.monitorSelectedID != id || m.rowContextMode(id) != contextWide) {
		return nil
	}
	revision := c.SessionQueueRevision()
	if q.session == id && revision == q.revision && time.Now().Before(q.next) {
		return nil
	}
	if q.session != id {
		q.items = nil
		q.offset = 0
		q.focused = false
	}
	q.session = id
	q.loading = true
	q.request++
	request := q.request
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		items, err := c.SessionQueue(ctx, id)
		return monitorQueueResult{id, request, revision, items, err}
	}
}

// Controls remain anchored at the bottom. The queue occupies only spare space
// above them, so approval/composer click targets don't move.
func (m Model) monitorQueueRows(width, height, controlRows int) int {
	id := m.monitorContextTarget()
	entries := m.followupEntries()
	if m.meterView != viewMonitor || m.contextTargetHidden() || len(entries) == 0 || width < 34 {
		return 0
	}
	if s, ok := m.contextDetailSession(); !ok || m.hasSessionProfile(s) {
		return 0
	}
	if m.monitorContextDetail == "" && (m.monitorSelectedID != id || m.rowContextMode(id) != contextWide) {
		return 0
	}
	available := height - controlRows - 7 // preserve borders, gap and at least four context rows
	limit := 6
	if m.monitorContextDetail == "" {
		s, _ := m.contextDetailSession()
		textRows, _, _ := monitorContextBodyLayout(height, controlRows)
		available = min(available, textRows-len(expandedContextLines(width, s)))
		limit = 2
	}
	if available < 1 {
		return 0
	}
	return min(available, limit, len(entries)+1)
}

func (m Model) renderMonitorQueue(width, rows int, colors palette) string {
	if rows == 0 {
		return ""
	}
	q := m.monitorQueue
	entries := m.followupEntries()
	start := min(q.offset, max(len(entries)-(rows-1), 0))
	header := fmt.Sprintf("FOLLOW-UPS // %d", len(entries))
	if q.stale && len(q.items) > 0 && q.session == m.monitorContextTarget() {
		header += " // UNAVAILABLE"
	}
	if q.focused {
		header += " // ↑↓ · Enter: edit · X: delete · Esc"
	} else {
		header += " // Alt+Q"
	}
	if rows == 1 {
		header += " // open follow-ups"
	} else if len(entries) > rows-1 {
		header += fmt.Sprintf(" // %d–%d // scroll", start+1, min(start+rows-1, len(entries)))
	}
	lines := []string{colors.label().Render(ansi.Truncate(header, max(width-4, 1), "…"))}
	for i := start; i < min(start+rows-1, len(entries)); i++ {
		entry := entries[i]
		buttons := followupButtons(entry, width)
		text := entry.label + " // " + strings.Join(strings.Fields(entry.text), " ")
		space := max(buttons[0].x-1, 1)
		text = ansi.Truncate(text, space, "…")
		text += strings.Repeat(" ", max(space-ansi.StringWidth(text), 0))
		prefix := colors.label()
		if q.focused && i == q.offset {
			prefix = prefix.Underline(true)
		}
		line := prefix.Render(text)
		for _, b := range buttons {
			style := colors.label()
			if !b.enabled {
				style = colors.dimmed()
			} else if m.monitorContextHover == "queue:"+b.key+":"+strconv.Itoa(i) {
				style = style.Foreground(colors.background).Background(colors.primary)
			}
			line += " " + style.Render(b.text)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) monitorQueueAction(width, height, controls, x, y int) string {
	rows := m.monitorQueueRows(width, height, controls)
	if rows == 0 {
		return ""
	}
	_, _, top := monitorContextBodyLayout(height, controls+rows)
	if x < 2 || x >= width-2 || y < top || y >= top+rows {
		return ""
	}
	if y == top {
		return "queue:focus"
	}
	entries := m.followupEntries()
	start := min(m.monitorQueue.offset, max(len(entries)-(rows-1), 0))
	i := start + y - top - 1
	if i >= len(entries) {
		return "queue:focus"
	}
	for _, b := range followupButtons(entries[i], width) {
		if b.enabled && x >= 2+b.x && x < 2+b.x+ansi.StringWidth(b.text) {
			return "queue:" + b.key + ":" + strconv.Itoa(i)
		}
	}
	return "queue:focus"
}

func (m Model) monitorQueueAt(x, y int) string {
	g := m.monitorDashboardLayout()
	x -= 2
	y -= g.meterY
	if m.monitorContextDetail != "" {
		base := m.layoutDetailControlsBase(g.contentWidth, g.meterHeight)
		return m.monitorQueueAction(g.contentWidth, g.meterHeight, base.rows, x, y)
	}
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	rowY := a.topHeight + a.gap - 1
	mw, cw, _ := monitorSessionColumnWidths(a.width)
	for i, s := range sessions {
		if s.id == m.monitorSelectedID && m.rowContextMode(s.id) == contextWide {
			return m.monitorQueueAction(cw, heights[i], m.expandedControlRows(cw, heights[i], s), x-mw-1, y-rowY)
		}
		rowY += heights[i]
	}
	return ""
}

func (m *Model) openQueueItem(index int, remove bool) tea.Cmd {
	q := &m.monitorQueue
	if q.busy || q.loading || q.stale || q.session != m.monitorContextTarget() || index < 0 || index >= len(q.items) {
		return nil
	}
	item := q.items[index]
	if !remove && !item.Editable {
		return nil
	}
	q.item = item
	q.open = true
	q.deleting = remove
	q.focused = false
	q.notice = ""
	q.input = newMonitorEditor()
	q.input.SetValue(item.Text)
	m.monitorPrompt.input.Blur()
	return q.input.Focus()
}

func (m *Model) followupAction(index int, action string) tea.Cmd {
	m.monitorQueue.focused = false
	entries := m.followupEntries()
	if index < 0 || index >= len(entries) {
		return nil
	}
	entry := entries[index]
	if entry.trigger != nil {
		switch action {
		case "edit":
			if !entry.editable {
				return nil
			}
			m.setRowContext(entry.trigger.Session, contextFull)
			return m.openSchedule()
		case "delete":
			if entry.removable {
				return m.triggerAction("ctrl+d")
			}
		case "now":
			if entry.now {
				m.setRowContext(entry.trigger.Session, contextFull)
				return m.triggerAction("ctrl+n")
			}
		}
		return nil
	}
	return m.openQueueItem(index, action == "delete")
}

func (m Model) updateMonitorQueue(msg tea.Msg) (Model, tea.Cmd, bool) {
	q := &m.monitorQueue
	switch v := msg.(type) {
	case monitorQueueResult:
		if v.request == q.request {
			selected := ""
			before := m.followupEntries()
			if q.focused && q.offset < len(before) {
				selected = before[q.offset].key()
			}
			q.loading = false
			q.revision = v.revision
			q.next = time.Now().Add(2 * time.Second)
			q.stale = v.err != nil
			if v.err != nil {
				q.next = time.Now().Add(10 * time.Second)
			} else {
				q.items = v.items
			}
			after := m.followupEntries()
			q.offset = min(q.offset, max(len(after)-1, 0))
			if selected != "" {
				q.focused = false
				for i, entry := range after {
					if entry.key() == selected {
						q.offset = i
						q.focused = true
						break
					}
				}
			}
		}
		return m, nil, true
	case monitorQueueChanged:
		q.busy = false
		if v.err != nil {
			q.notice = "Change unconfirmed or message already started. Check Codex; no automatic retry."
		} else {
			q.open = false
			q.input.Blur()
			q.items = nil
			q.next = time.Time{}
		}
		return m, nil, true
	}
	if m.scheduleUI.open {
		q.focused = false
		return m, nil, false
	}
	if q.open {
		q.input.configure(max(m.width-4, 24), max(m.height-10, 8))
		if key, ok := msg.(tea.KeyPressMsg); ok {
			if q.busy {
				return m, nil, true
			}
			switch key.String() {
			case "esc", "ctrl+c":
				q.open = false
				q.input.Blur()
				q.next = time.Time{}
				return m, nil, true
			case "enter":
				return m.changeQueueItem()
			}
		}
		if mouse, ok := msg.(tea.MouseMsg); ok {
			point := mouse.Mouse()
			q.hover = ""
			for _, b := range m.queueEditorButtons() {
				if b.enabled && point.Y == b.y && point.X >= b.x && point.X < b.x+ansi.StringWidth(b.text) {
					q.hover = b.key
				}
			}
			if _, click := msg.(tea.MouseClickMsg); click && point.Button == tea.MouseLeft {
				if q.hover == "save" {
					return m.changeQueueItem()
				}
				if q.hover == "cancel" {
					q.open = false
					q.next = time.Time{}
				}
			}
			return m, nil, true
		}
		if !q.deleting && !q.busy {
			var cmd tea.Cmd
			q.input, cmd = q.input.Update(msg)
			if cmd != nil {
				return m, cmd, true
			}
		}
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg, tea.MouseWheelMsg:
			return m, nil, true
		}
		return m, nil, false
	}
	if m.meterView != viewMonitor || m.monitorContextTarget() == "" || m.contextTargetHidden() {
		q.focused = false
		return m, nil, false
	}
	if q.focused && q.focusSession != m.monitorContextTarget() {
		q.focused = false
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if key.String() == "alt+q" && len(m.followupEntries()) > 0 {
			q.focused = true
			q.focusSession = m.monitorContextTarget()
			m.monitorPrompt.input.Blur()
			return m, nil, true
		}
		if q.focused {
			switch key.String() {
			case "esc", "tab":
				q.focused = false
			case "up":
				q.offset = max(q.offset-1, 0)
			case "down":
				q.offset = min(q.offset+1, max(len(m.followupEntries())-1, 0))
			case "pgup":
				q.offset = max(q.offset-5, 0)
			case "pgdown":
				q.offset = min(q.offset+5, max(len(m.followupEntries())-1, 0))
			case "enter", "e":
				cmd := m.followupAction(q.offset, "edit")
				return m, cmd, true
			case "delete", "x":
				cmd := m.followupAction(q.offset, "delete")
				return m, cmd, true
			}
			return m, nil, true
		}
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		point := mouse.Mouse()
		hit := m.monitorQueueAt(point.X, point.Y)
		if hit != "" {
			if _, wheel := msg.(tea.MouseWheelMsg); wheel {
				if point.Button == tea.MouseWheelUp {
					q.offset = max(q.offset-1, 0)
				} else if point.Button == tea.MouseWheelDown {
					q.offset = min(q.offset+1, max(len(m.followupEntries())-1, 0))
				}
				return m, nil, true
			}
			if _, click := msg.(tea.MouseClickMsg); click && point.Button == tea.MouseLeft {
				parts := strings.Split(hit, ":")
				if len(parts) == 3 {
					i, _ := strconv.Atoi(parts[2])
					cmd := m.followupAction(i, parts[1])
					return m, cmd, true
				}
				q.focused = true
				q.focusSession = m.monitorContextTarget()
				m.monitorPrompt.input.Blur()
				return m, nil, true
			}
		} else if _, click := msg.(tea.MouseClickMsg); click {
			q.focused = false
		}
	}
	return m, nil, false
}

func (m Model) changeQueueItem() (Model, tea.Cmd, bool) {
	q := &m.monitorQueue
	c, ok := m.fetcher.(codex.SessionQueueClient)
	text := strings.TrimSpace(q.input.Value())
	if !ok || q.busy || !q.deleting && text == "" {
		return m, nil, true
	}
	if m.width < 40 || m.height < 14 {
		q.notice = "Enlarge the terminal before confirming."
		return m, nil, true
	}
	q.busy = true
	id, item, remove := q.session, q.item, q.deleting
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		return monitorQueueChanged{c.ChangeSessionQueue(ctx, id, item, text, remove)}
	}, true
}

func (m Model) renderQueueEditor() string {
	q := m.monitorQueue
	colors := paletteFor(m.theme)
	if m.width < 40 || m.height < 14 {
		return "Enlarge the terminal to edit the queue. Esc: cancel."
	}
	w, h := max(m.width-4, 24), max(m.height-2, 10)
	q.input.configure(w, max(h-8, 8))
	title := "EDIT QUEUED MESSAGE // " + terminalLabel(q.session)
	body := q.input.View(colors)
	if q.deleting {
		title = "DELETE QUEUED MESSAGE // " + terminalLabel(q.session)
		body = ansi.Hardwrap(q.item.Text, max(w-4, 1), true)
	}
	lines := []string{"Codex may start this message while you review it. Esc: cancel.", ""}
	lines = append(lines, strings.Split(body, "\n")...)
	for len(lines) < h-5 {
		lines = append(lines, "")
	}
	lines = lines[:h-5]
	buttons := ""
	for _, b := range m.queueEditorButtons() {
		buttons += strings.Repeat(" ", max(b.x-2-ansi.StringWidth(buttons), 0))
		style := colors.label()
		if !b.enabled {
			style = colors.dimmed()
		} else if q.hover == b.key {
			style = style.Foreground(colors.background).Background(colors.primary)
		}
		buttons += style.Render(b.text)
	}
	lines = append(lines, q.notice, buttons)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(w-4, 1), "…")
	}
	return "\n " + frameSized(w, h-2, title, strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) queueEditorButtons() []triggerButton {
	if m.width < 40 || m.height < 14 {
		return nil
	}
	q := m.monitorQueue
	label := "[ SAVE CHANGES ]"
	if q.deleting {
		label = "[ CONFIRM DELETE ]"
	}
	if q.busy {
		label = "[ WAIT… ]"
	}
	return []triggerButton{{text: label, key: "save", x: 2, y: m.height - 4, enabled: !q.busy && (q.deleting || strings.TrimSpace(q.input.Value()) != "")}, {text: "[ CANCEL ]", key: "cancel", x: 24, y: m.height - 4, enabled: !q.busy}}
}

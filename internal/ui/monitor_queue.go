package ui

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
	"github.com/merefield/codexometer/internal/schedule"
)

type followupEntry struct {
	label, text              string
	receipt                  string
	readOnly                 bool
	native                   *codex.SessionQueuedMessage
	trigger                  *schedule.Job
	editable, removable, now bool
}

func (entry followupEntry) key() string {
	if entry.receipt != "" {
		return entry.receipt
	}
	if entry.native != nil {
		return "native:" + entry.native.ID
	}
	return "scheduled:" + entry.trigger.ID
}

func (m Model) followupEntries() []followupEntry {
	var entries []followupEntry
	if m.monitorQueue.session == m.monitorContextTarget() {
		for _, item := range m.monitorQueue.items {
			entries = append(entries, followupEntry{label: i18n.Text("QUEUED"), text: item.Text, native: &item, editable: item.Editable && !m.monitorQueue.stale, removable: !m.monitorQueue.stale})
		}
		for i, text := range m.monitorQueue.submitted {
			entries = append(entries, followupEntry{label: i18n.Text("ACCEPTED // CHECKING QUEUE"), text: text, receipt: "receipt:" + strconv.Itoa(i)})
		}
	}
	for _, steer := range m.monitorSteers {
		if steer.session == m.monitorContextTarget() {
			entries = append(entries, followupEntry{label: i18n.Text("STEER SENT"), text: steer.text, receipt: "steer:" + strconv.FormatUint(steer.id, 10), readOnly: true})
		}
	}
	if j, ok := m.pendingTrigger(); ok {
		label := i18n.Text("AFTER QUOTA REFRESH")
		if j.Trigger == "at" {
			label = i18n.Text("AT ") + scheduleDate(j.At.Local())
		}
		if j.Status != "pending" {
			label += " // " + strings.ToUpper(i18n.Text(j.Status))
		}
		label = i18n.Text("SCHEDULED · ") + label
		entries = append(entries, followupEntry{label: label, text: j.Text, trigger: &j, editable: j.Status == "pending", removable: j.Status != "sending", now: m.triggerReady(j)})
	}
	return entries
}

func followupButtons(entry followupEntry, width int) []triggerButton {
	if entry.readOnly {
		return nil
	}
	var buttons []triggerButton
	if entry.trigger != nil {
		buttons = append(buttons, triggerButton{text: i18n.Text("[NOW]"), key: "now", enabled: entry.now})
	}
	buttons = append(buttons, triggerButton{text: i18n.Text("[EDIT]"), key: "edit", enabled: entry.editable}, triggerButton{text: "[×]", key: "delete", enabled: entry.removable})
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
	submitted                                     []string // acknowledged sends, not yet reconciled with a fresh list
	loading, busy, open, deleting, focused, stale bool
	request                                       uint64
	revision                                      uint64
	next                                          time.Time
	offset                                        int
	item                                          codex.SessionQueuedMessage
	input                                         monitorEditor
	notice                                        string
	composerRows                                  int
	editorFocus                                   int
	drafts                                        map[string]queueEditDraft
}

type queueEditDraft struct{ original, text string }
type monitorQueueResult struct {
	session           string
	request, revision uint64
	items             []codex.SessionQueuedMessage
	err               error
}
type monitorQueueChanged struct{ err error }

func (m *Model) recordQueuedSubmission(session, text string) tea.Cmd {
	q := &m.monitorQueue
	if q.session != session {
		q.items, q.submitted = nil, nil
		q.offset, q.focused = 0, false
	}
	q.session = session
	q.submitted = append(q.submitted, codex.SanitizeSessionContext(text))
	// A list started before this acknowledgement cannot resolve this receipt.
	q.request++
	q.loading = false
	q.next = time.Time{}
	return m.pollMonitorQueue()
}

func (m Model) queueUnavailable() bool {
	return m.monitorQueue.session == m.monitorContextTarget() && m.monitorQueue.stale
}

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
		q.submitted = nil
		q.stale = false
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
	if m.meterView != viewMonitor || m.contextTargetHidden() || len(entries) == 0 && !m.queueUnavailable() || width < 34 {
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
		// Even long commentary leaves a compact queue/status line. Never
		// take space from approval buttons or the composer.
		available = min(available, max(1, textRows-len(expandedContextLines(width, s))))
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
	header := i18n.Format("FOLLOW-UPS // %d", len(entries))
	if m.queueUnavailable() {
		header = i18n.Text("FOLLOW-UPS // UNAVAILABLE")
	}
	if q.focused && q.offset < len(entries) && entries[q.offset].readOnly {
		header += " // ↑↓ · Esc"
	} else if q.focused {
		header += i18n.Text(" // ↑↓ · Enter: edit · X: delete · Esc")
	} else {
		header += " // Alt+Q"
	}
	if rows == 1 {
		header += i18n.Text(" // open follow-ups")
	} else if len(entries) > rows-1 {
		header += i18n.Format(" // %d–%d // scroll", start+1, min(start+rows-1, len(entries)))
	}
	inner := max(width-4, 1)
	tint := monitorTintBackground(colors, 6)
	header = ansi.Truncate("─ "+header, inner, "…")
	if spare := inner - ansi.StringWidth(header); spare > 1 {
		header += " " + strings.Repeat("─", spare-1)
	}
	lines := []string{colors.label().Bold(true).Foreground(colors.primary).Background(tint).Width(inner).Render(header)}
	for i := start; i < min(start+rows-1, len(entries)); i++ {
		entry := entries[i]
		buttons := followupButtons(entry, width)
		selected := q.focused && i == q.offset
		marker := ""
		background := tint
		if selected {
			marker = "› "
			background = monitorTintBackground(colors, 18)
		}
		text := marker + entry.label + " // " + strings.Join(strings.Fields(entry.text), " ")
		space := inner
		if len(buttons) > 0 {
			space = max(buttons[0].x-1, 1)
		}
		text = ansi.Truncate(text, space, "…")
		text += strings.Repeat(" ", max(space-ansi.StringWidth(text), 0))
		prefix := colors.label().Background(background)
		if selected {
			prefix = prefix.Foreground(colors.primary).Bold(true)
		}
		line := prefix.Render(text)
		for _, b := range buttons {
			style := colors.label().Background(background)
			if !b.enabled {
				style = colors.dimmed().Background(background)
			} else if m.monitorContextHover == "queue:"+b.key+":"+strconv.Itoa(i) {
				style = style.Foreground(colors.background).Background(colors.primary)
			}
			line += colors.label().Background(background).Render(" ") + style.Render(b.text)
		}
		lines = append(lines, colors.label().Background(background).Width(inner).Render(line))
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
	if m.meterView != viewMonitor || m.contextTargetHidden() || len(m.followupEntries()) == 0 && !m.queueUnavailable() {
		return ""
	}
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
	q.editorFocus = 0
	if remove {
		q.editorFocus = 1
	}
	q.notice = ""
	q.input = newMonitorEditor()
	q.input.SetValue(item.Text)
	key := q.session + "\x00" + item.ID
	if draft, ok := q.drafts[key]; ok && draft.original == item.Text && !remove {
		q.input.SetValue(draft.text)
	}
	q.drafts = maps.Clone(q.drafts)
	delete(q.drafts, key)
	m.setRowContext(q.session, contextFull)
	g := m.monitorDashboardLayout()
	q.composerRows = max(3, m.monitorPromptRows(g.contentWidth, g.meterHeight))
	m.monitorPrompt.input.Blur()
	if remove {
		return nil
	}
	return q.input.Focus()
}

func (m *Model) followupAction(index int, action string) tea.Cmd {
	m.monitorQueue.focused = false
	entries := m.followupEntries()
	if index < 0 || index >= len(entries) {
		return nil
	}
	entry := entries[index]
	if entry.receipt != "" {
		return nil
	}
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
				q.submitted = nil
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
			q.notice = i18n.Text("Decision unconfirmed; check Codex. Do not retry here.")
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
	if !q.open && !q.focused {
		switch msg.(type) {
		case tea.MouseMsg, tea.KeyPressMsg:
		default:
			return m, nil, false
		}
	}
	if q.open {
		g := m.monitorDashboardLayout()
		q.input.configure(g.contentWidth, max(q.composerRows, 3)+6)
		if mouse, ok := msg.(tea.MouseMsg); ok {
			point := mouse.Mouse()
			if m.editorNavigationAt(point.X, point.Y) && !q.busy {
				if _, click := msg.(tea.MouseClickMsg); click && point.Button == tea.MouseLeft {
					if !q.deleting {
						q.drafts = maps.Clone(q.drafts)
						if q.drafts == nil {
							q.drafts = map[string]queueEditDraft{}
						}
						q.drafts[q.session+"\x00"+q.item.ID] = queueEditDraft{original: q.item.Text, text: q.input.Value()}
					}
					q.open = false
					q.input.Blur()
				}
				return m, nil, false
			}
			m.hoveredButton = m.footerButtonAt(point.X, point.Y)
			if _, click := msg.(tea.MouseClickMsg); click && point.Button == tea.MouseLeft && m.hoveredButton != footerButtonNone {
				next, cmd := m.pressFooterButton(m.hoveredButton)
				return next.(Model), cmd, true
			}
		}
		if key, ok := msg.(tea.KeyPressMsg); ok {
			if key.String() == "ctrl+c" || key.String() == "q" && q.editorFocus != 0 {
				return m, tea.Quit, true
			}
			if q.busy {
				return m, nil, true
			}
			switch key.String() {
			case "esc":
				q.open = false
				q.input.Blur()
				q.next = time.Time{}
				return m, nil, true
			case "enter":
				if q.editorFocus == 2 {
					q.open = false
					q.input.Blur()
					q.next = time.Time{}
					return m, nil, true
				}
				return m.changeQueueItem()
			case "tab", "shift+tab":
				delta := 1
				if key.String() == "shift+tab" {
					delta = -1
				}
				q.editorFocus = (q.editorFocus + delta + 3) % 3
				if q.deleting && q.editorFocus == 0 {
					q.editorFocus = (q.editorFocus + delta + 3) % 3
				}
				q.input.Blur()
				if q.editorFocus == 0 {
					return m, q.input.Focus(), true
				}
				return m, nil, true
			case "t", "r":
				if q.editorFocus != 0 {
					button := footerButtonTheme
					if key.String() == "r" {
						button = footerButtonRefresh
					}
					next, cmd := m.pressFooterButton(button)
					return next.(Model), cmd, true
				}
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
					q.input.Blur()
					q.next = time.Time{}
				}
				_, _, editorY := monitorContextBodyLayout(g.meterHeight, min(max(q.composerRows, 3), max(g.meterHeight-5, 1)))
				if q.hover == "" && !q.busy && !q.deleting && point.X >= 4 && point.X < g.contentWidth && point.Y >= g.meterY+editorY && point.Y < g.meterY+g.meterHeight-3 {
					q.editorFocus = 0
					return m, q.input.Focus(), true
				}
			}
			return m, nil, true
		}
		if !q.deleting && !q.busy && q.editorFocus == 0 {
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
			if m.monitorContextDetail == "" {
				m.setRowContext(m.monitorContextTarget(), contextFull)
			}
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
				if m.monitorContextDetail == "" {
					m.setRowContext(m.monitorContextTarget(), contextFull)
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
	g := m.monitorDashboardLayout()
	if g.contentWidth < 40 || g.meterHeight < 7 {
		q.notice = i18n.Text("Enlarge the terminal before confirming.")
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
	g := m.monitorDashboardLayout()
	w, h := g.contentWidth, g.meterHeight
	rows := min(max(q.composerRows, 3), max(h-5, 1))
	_, _, editorY := monitorContextBodyLayout(h, rows)
	q.input.configure(w, rows+6)
	title := i18n.Text("EDIT QUEUED MESSAGE")
	if q.deleting {
		title = i18n.Text("DELETE QUEUED MESSAGE")
	}
	identity := shortSessionID(q.session)
	if s, ok := m.contextDetailSession(); ok {
		identity = monitorSessionIdentity(s)
	}
	lines := make([]string, max(h-2, 1))
	lines[0] = colors.header().Render(title + " // " + identity)
	if len(lines) > 1 {
		lines[1] = i18n.Text("Codex may start this message while you review it. Esc: cancel.")
	}
	body := q.input.View(colors)
	if q.deleting {
		body = ansi.Hardwrap(q.item.Text, max(w-4, 1), true)
	}
	for i, line := range strings.Split(body, "\n") {
		y := editorY - 1 + i
		if y >= len(lines)-2 {
			break
		}
		if y >= 0 {
			lines[y] = line
		}
	}
	if len(lines) > 1 {
		lines[len(lines)-2] = q.notice
	}
	buttons := ""
	for _, b := range m.queueEditorButtons() {
		buttons += strings.Repeat(" ", max(b.x-4-ansi.StringWidth(buttons), 0))
		style := colors.label()
		if !b.enabled {
			style = colors.dimmed()
		} else if q.hover == b.key || q.editorFocus == 1 && b.key == "save" || q.editorFocus == 2 && b.key == "cancel" {
			style = style.Foreground(colors.background).Background(colors.primary)
		}
		buttons += style.Render(b.text)
	}
	lines[len(lines)-1] = buttons
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(w-4, 1), "…")
	}
	return frameSized(w, max(h-2, 1), m.monitorDetailTitle(w, colors), strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) queueEditorButtons() []triggerButton {
	g := m.monitorDashboardLayout()
	if g.contentWidth < 40 || g.meterHeight < 7 {
		return nil
	}
	q := m.monitorQueue
	label := i18n.Text("[ SAVE CHANGES ]")
	if q.deleting {
		label = i18n.Text("[ CONFIRM DELETE ]")
	}
	if q.busy {
		label = i18n.Text("[ WAIT… ]")
	}
	cancel := i18n.Text("[ CANCEL ]")
	label = ansi.Truncate(label, max(g.contentWidth-6-ansi.StringWidth(cancel), 1), "…")
	y := g.meterY + g.meterHeight - 2
	return []triggerButton{
		{text: label, key: "save", x: 4, y: y, enabled: !q.busy && (q.deleting || strings.TrimSpace(q.input.Value()) != "")},
		{text: cancel, key: "cancel", x: 4 + ansi.StringWidth(label) + 2, y: y, enabled: !q.busy},
	}
}

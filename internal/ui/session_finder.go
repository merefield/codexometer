package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

const (
	findNone = iota
	findText
	findHistory
	findResults
)

type savedRow struct {
	codex.SavedSession
	cursor string
}
type sessionFinder struct {
	input                          *textinput.Model
	open, include, loading, failed bool
	scope, next, detail            string
	epoch                          uint64
	focus, index, scroll           int
	rows                           []savedRow
	turns                          []codex.SavedTurn
}

func finderLabel() string { return i18n.Text("FILTER") }
func (f sessionFinder) query() string {
	if f.input == nil {
		return ""
	}
	return f.input.Value()
}
func (f *sessionFinder) clearQuery() {
	if f.input != nil {
		f.editInput().SetValue("")
	}
	f.index = 0
}
func (f *sessionFinder) clearHistory() {
	f.epoch++
	f.include, f.loading, f.failed = false, false, false
	f.detail, f.next = "", ""
	f.rows, f.turns = nil, nil
	f.index, f.scroll = 0, 0
}
func (f *sessionFinder) setScope(scope string) {
	if f.scope != scope {
		f.clearHistory()
		f.scope = scope
	}
}
func (m *Model) focusFinder() tea.Cmd {
	m.focusMonitorSummary()
	m.monitorSummaryFocused = false
	f := &m.finder
	f.open = true
	f.closeDetail()
	f.focus = findText
	return f.editInput().Focus()
}

// Keep the inactive finder cheap in Bubble Tea's frequently copied model.
func (f *sessionFinder) editInput() *textinput.Model {
	var input textinput.Model
	if f.input == nil {
		input = textinput.New()
		input.CharLimit = 160
		input.Prompt = ""
		input.Placeholder = finderLabel()
	} else {
		input = *f.input
	}
	f.input = &input
	return f.input
}
func (m Model) finderHeight(available int) int {
	f := m.finder
	if !f.open {
		return 0
	}
	if f.include && m.monitorFilter == monitorFilterAll {
		return min(9, max(available/2, 1))
	}
	return min(1, max(available-1, 0))
}

type savedResult struct {
	epoch             uint64
	scope, cursor, id string
	page              codex.SavedPage
	err               error
}

func (m *Model) requestSaved(cursor, id string) tea.Cmd {
	f := &m.finder
	client, ok := m.fetcher.(codex.SavedSessionClient)
	if !ok || f.scope == "" || f.loading {
		return nil
	}
	f.loading, f.failed = true, false
	f.epoch++
	msg := savedResult{epoch: f.epoch, scope: f.scope, cursor: cursor, id: id}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		msg.page, msg.err = client.BrowseSavedSessions(ctx, msg.scope, msg.cursor, msg.id)
		return msg
	}
}
func (f *sessionFinder) accept(r savedResult) {
	if !f.include || r.epoch != f.epoch || r.scope != f.scope {
		return
	}
	f.loading, f.failed = false, r.err != nil
	if f.failed {
		f.rows, f.turns, f.next = nil, nil, ""
		return
	}
	if r.id != "" {
		f.turns = r.page.Turns
		return
	}
	seen := make(map[string]bool, len(f.rows))
	for _, row := range f.rows {
		seen[row.ID] = true
	}
	for _, row := range r.page.Sessions {
		if !seen[row.ID] {
			f.rows = append(f.rows, savedRow{row, r.cursor})
			seen[row.ID] = true
		}
	}
	f.next = r.page.Next
	if f.next == r.cursor {
		f.next = ""
	}
}
func (m Model) savedMatches() []savedRow {
	if !m.finder.include || m.monitorFilter != monitorFilterAll {
		return nil
	}
	live := make(map[string]bool, len(m.monitorSessionData))
	for _, s := range m.monitorSessionData {
		if m.monitorSessionVisible(s) {
			live[s.id] = true
		}
	}
	var out []savedRow
	for _, r := range m.finder.rows {
		if !live[r.ID] && codex.SessionMatches(m.finder.query(), r.ID, r.Name, r.Directory) {
			out = append(out, r)
		}
	}
	return out
}
func (m *Model) historyToggle() tea.Cmd {
	f := &m.finder
	wasIncluded := f.include
	f.clearHistory()
	if !wasIncluded && f.scope != "" {
		f.include = true
		return m.requestSaved("", "")
	}
	return nil
}
func (m *Model) activateSaved() tea.Cmd {
	f := &m.finder
	if f.loading {
		return nil
	}
	rows := m.savedMatches()
	if f.index >= 0 && f.index < len(rows) {
		r := rows[f.index]
		f.detail = r.ID
		f.scroll = 0
		f.turns = nil
		return m.requestSaved(r.cursor, r.ID)
	}
	if f.index == len(rows) && f.next != "" {
		return m.requestSaved(f.next, "")
	}
	return nil
}
func (m Model) savedLines(width int) []string {
	f := m.finder
	if f.failed {
		return []string{i18n.Text("UNAVAILABLE")}
	}
	if f.loading {
		return []string{"…"}
	}
	var lines []string
	for _, turn := range slices.Backward(f.turns) {
		for _, text := range []string{turn.Task, i18n.SessionText(turn.Reply, turn.Restricted)} {
			if text != "" {
				lines = append(lines, wrapProse(text, max(width, 1))...)
				lines = append(lines, "")
			}
		}
	}
	if len(lines) == 0 {
		return []string{i18n.Text("NO COMPLETED TURNS")}
	}
	return lines
}
func (f sessionFinder) historyLabel() string {
	mark := "□"
	if f.include {
		mark = "☑"
	}
	return "[" + mark + " " + i18n.Text("INCLUDE HISTORY") + "]"
}
func (m Model) finderStart(n int) int        { return max(0, m.finder.index-max(n-1, 0)) }
func (f sessionFinder) inputWidth(w int) int { return max(1, w-lipgloss.Width(f.historyLabel())-2) }
func (m Model) renderSessionFinder(w, h int, colors palette) string {
	f := m.finder
	label := f.historyLabel()
	inputWidth := f.inputWidth(w)
	tint := monitorComposerBackground(colors)
	input := *f.input // Render-only styling and viewport adjustments.
	styles := input.Styles()
	styles.Focused.Text = colors.label().Background(tint)
	styles.Blurred.Text = styles.Focused.Text
	styles.Focused.Placeholder = colors.dimmed().Background(tint)
	styles.Blurred.Placeholder = styles.Focused.Placeholder
	styles.Cursor.Color = colors.primary
	input.SetStyles(styles)
	input.SetWidth(max(1, inputWidth-3))
	input.SetCursor(input.Position())
	line := ansi.Truncate(colors.label().Foreground(colors.primary).Render("⌕ ")+input.View(), inputWidth, "…")
	line = withDefaultBackground(line+strings.Repeat(" ", max(0, inputWidth-lipgloss.Width(line))), tint)
	line += strings.Repeat(" ", max(1, w-lipgloss.Width(label)-inputWidth))
	style := colors.label()
	if f.scope == "" {
		style = colors.dimmed()
	} else if f.focus == findHistory {
		style = style.Reverse(true)
	}
	lines := []string{ansi.Truncate(line+style.Render(label), w, "")}
	rows := m.savedMatches()
	var body []string
	if f.detail != "" {
		for _, r := range rows {
			if r.ID == f.detail {
				body = append(body, "[←] "+i18n.Text("SAVED")+" // "+shortSessionID(r.ID)+" // "+r.Name)
				text := m.savedLines(w)
				body = append(body, text[min(f.scroll, max(0, len(text)-max(h-2, 1))):]...)
				break
			}
		}
	} else if h > 1 {
		for i := m.finderStart(h - 1); i < len(rows); i++ {
			row := rows[i]
			text := i18n.Text("SAVED") + " // " + shortSessionID(row.ID) + " // " + strings.TrimSpace(row.Name+"  "+row.Directory)
			if i == f.index && f.focus == findResults {
				text = "› " + text
			}
			body = append(body, text)
			if len(body) >= h-1 {
				break
			}
		}
		if f.loading {
			body = append(body, "…")
		} else if f.failed {
			body = append(body, i18n.Text("UNAVAILABLE"))
		} else if f.next != "" {
			more := "[+] " + i18n.Text("MORE")
			if f.focus == findResults && f.index == len(rows) {
				more = "› " + more
			}
			body = append(body, more)
		} else if len(rows) == 0 {
			body = append(body, i18n.Text("NO RESULTS MATCH THE ACTIVE FILTER"))
		}
	}
	for _, text := range body[:min(len(body), h-1)] {
		lines = append(lines, colors.label().Render(ansi.Truncate(text, w, "…")))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
func (m *Model) leaveFinder() {
	m.finder.focus = findNone
	if m.finder.input != nil && m.finder.input.Focused() {
		m.finder.editInput().Blur()
	}
}
func (f *sessionFinder) closeDetail() {
	if f.detail == "" {
		return
	}
	f.detail = ""
	f.turns = nil
	f.epoch++
	f.loading = false
	f.failed = false
	f.scroll = 0
}

func (m *Model) scrollFinder(delta, width, height int) {
	f := &m.finder
	if f.detail != "" {
		f.scroll = max(0, min(f.scroll+delta, len(m.savedLines(width))-max(1, height-2)))
		return
	}
	last := len(m.savedMatches()) - 1
	if f.next != "" {
		last++
	}
	f.index = max(0, min(f.index+delta, last))
}

func (m Model) updateSessionFinder(message tea.Msg) (Model, tea.Cmd, bool) {
	f := &m.finder
	if r, ok := message.(savedResult); ok {
		f.accept(r)
		return m, nil, true
	}
	if m.meterView != viewMonitor || m.monitorContextDetail != "" || m.scheduleUI.open || m.monitorCommands.open || m.monitorQueue.open {
		return m, nil, false
	}
	if wheel, ok := message.(tea.MouseWheelMsg); ok && f.include {
		g := m.dashboardLayout()
		a := m.monitorArea(g.contentWidth, g.meterHeight)
		y := wheel.Y - g.meterY - a.topHeight - a.attentionRows
		if wheel.X >= 2 && wheel.X < 2+g.contentWidth && y > 0 && y < a.finderRows {
			d := 1
			if wheel.Button == tea.MouseWheelUp {
				d = -1
			}
			f.focus = findResults
			m.scrollFinder(d, g.contentWidth, a.finderRows)
			return m, nil, true
		}
	}
	if click, ok := message.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		g := m.dashboardLayout()
		a := m.monitorArea(g.contentWidth, g.meterHeight)
		y := click.Y - g.meterY - a.topHeight - a.attentionRows
		x := click.X - 2
		if x >= 0 && x < g.contentWidth && y >= 0 && y < a.finderRows {
			if y == 0 {
				if x >= g.contentWidth-lipgloss.Width(f.historyLabel()) {
					f.focus = findHistory
					cmd := m.historyToggle()
					return m, cmd, true
				}
				cmd := m.focusFinder()
				return m, cmd, true
			}
			f.focus = findResults
			if f.detail == "" {
				f.index = m.finderStart(a.finderRows-1) + y - 1
				cmd := m.activateSaved()
				return m, cmd, true
			}
			if y == 1 {
				f.closeDetail()
			}
			return m, nil, true
		}
		m.leaveFinder()
	}
	key, pressed := message.(tea.KeyPressMsg)
	if f.focus == findNone {
		if pressed && key.String() == "f" && !m.monitorPrompt.input.Focused() {
			cmd := m.focusFinder()
			return m, cmd, true
		}
		return m, nil, false
	}
	if pressed {
		switch key.String() {
		case "ctrl+c":
			m.leaveFinder()
			return m, nil, false
		case "esc":
			if f.detail != "" {
				f.closeDetail()
				f.focus = findResults
			} else {
				f.clearQuery()
				f.clearHistory()
				f.open = false
				m.leaveFinder()
			}
			return m, nil, true
		case "tab", "shift+tab":
			f.closeDetail()
			limit := findHistory
			if f.include && m.monitorFilter == monitorFilterAll {
				limit = findResults
			}
			if key.String() == "tab" {
				f.focus = f.focus%limit + 1
			} else {
				f.focus = (f.focus+limit-2)%limit + 1
			}
			if f.focus == findText {
				cmd := f.editInput().Focus()
				return m, cmd, true
			}
			f.editInput().Blur()
			return m, nil, true
		case "enter", "space", " ":
			if f.focus == findHistory {
				cmd := m.historyToggle()
				return m, cmd, true
			}
			if f.focus == findResults {
				if f.detail == "" {
					cmd := m.activateSaved()
					return m, cmd, true
				}
				return m, nil, true
			}
			if key.String() != "enter" {
				break
			}
			fallthrough
		case "down", "up", "pgdown", "pgup":
			if f.focus == findText {
				m.leaveFinder()
				m.returnFromMonitorHeader()
				if m.monitorSelectedID == "" && f.include && m.monitorFilter == monitorFilterAll {
					f.focus = findResults
				}
				return m, nil, true
			}
			if f.focus == findResults {
				d := 1
				if key.String() == "up" || key.String() == "pgup" {
					d = -1
				}
				if strings.HasPrefix(key.String(), "pg") {
					d *= 5
				}
				g := m.dashboardLayout()
				m.scrollFinder(d, g.contentWidth, m.monitorArea(g.contentWidth, g.meterHeight).finderRows)
			}
			return m, nil, true
		}
	}
	if pressed || isFinderPaste(message) {
		if f.focus == findText {
			old := f.query()
			input := *f.input
			input.SetWidth(max(1, f.inputWidth(m.dashboardLayout().contentWidth)-3))
			input, cmd := input.Update(message)
			f.input = &input
			if old != f.query() {
				f.index, m.monitorScroll = 0, 0
				m.reconcileMonitorFilterSelection()
			}
			return m, cmd, true
		}
		return m, nil, true
	}
	return m, nil, false
}
func isFinderPaste(msg tea.Msg) bool { _, ok := msg.(tea.PasteMsg); return ok }

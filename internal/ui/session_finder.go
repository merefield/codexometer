package ui

import (
	"context"
	"errors"
	"fmt"
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
	findResults
)

type savedRow struct {
	codex.SavedSession
	cursor string
}
type sessionFinder struct {
	input                          *textinput.Model
	filterInput                    *textinput.Model
	open, include, loading, failed bool
	filterOpen                     bool
	scope, next, detail            string
	resumeID                       string
	selected                       string
	resumeNotice                   string
	epoch                          uint64
	focus, index, scroll           int
	rows                           []savedRow
	turns                          []codex.SavedTurn
	cancel                         context.CancelFunc
}

func finderLabel() string { return i18n.Text("FILTER") }
func resumeLabel() string { return i18n.Text("[ (R)ESUME ]") }
func savedFinderTitle() string {
	return i18n.Text("SAVED") + " // " + i18n.Text("SESSIONS")
}
func (f sessionFinder) query() string {
	if f.input == nil {
		return ""
	}
	return f.input.Value()
}
func (f sessionFinder) liveQuery() string {
	if f.include {
		if f.filterInput == nil {
			return ""
		}
		return f.filterInput.Value()
	}
	return f.query()
}
func (f *sessionFinder) clearQuery() {
	if f.input != nil {
		f.editInput().SetValue("")
	}
	f.index = 0
}
func (f *sessionFinder) clearHistory() {
	f.stopRequest()
	if f.include {
		f.open, f.input = f.filterOpen, f.filterInput
		f.focus = findNone
	}
	f.filterInput = nil
	f.epoch++
	f.include, f.loading, f.failed = false, false, false
	f.detail, f.next = "", ""
	f.rows, f.turns = nil, nil
	f.resumeID = ""
	f.selected, f.resumeNotice = "", ""
	f.index, f.scroll = 0, 0
}
func (f *sessionFinder) stopRequest() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
}
func (f *sessionFinder) setScope(scope string) {
	if f.scope != scope {
		f.clearHistory()
		f.scope = scope
	}
}
func (m *Model) focusFinder() tea.Cmd {
	if m.finder.resumeID != "" {
		return nil
	}
	m.focusMonitorSummary()
	m.monitorSummaryFocused = false
	f := &m.finder
	f.open = true
	f.closeDetail()
	f.focus = findText
	return f.editInput().Focus()
}

func (m *Model) openResumePicker() tea.Cmd {
	return tea.Batch(m.beginResumePicker(), m.requestSaved("", ""))
}

func (m *Model) beginResumePicker() tea.Cmd {
	m.stashMonitorDraft()
	m.monitorContextDetail = ""
	m.monitorPrompt.input.Blur()
	m.finder.clearHistory()
	f := &m.finder
	f.filterOpen, f.filterInput = f.open, f.input
	f.open, f.include, f.input = true, true, nil
	return m.focusFinder()
}

// Keep the inactive finder cheap in Bubble Tea's frequently copied model.
func (f *sessionFinder) editInput() *textinput.Model {
	var input textinput.Model
	if f.input == nil {
		input = textinput.New()
		input.CharLimit = 160
		input.Prompt = ""
		input.Placeholder = finderLabel()
		if f.include {
			input.Placeholder = i18n.Text("SEARCH BY NAME")
		}
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
	if f.include {
		// Keep the frame, search and at least one result on short panes before
		// spending rows on the ordinary live-session list below it.
		if available < 7 {
			return available
		}
		return min(16, min(available-3, available*2/3))
	}
	return min(1, max(available-1, 0))
}

type savedResult struct {
	epoch             uint64
	scope, cursor, id string
	page              codex.SavedPage
	err               error
	resume            bool
}
type savedResumeDeadline struct{ savedResult }

func (m *Model) requestSaved(cursor, id string) tea.Cmd {
	f := &m.finder
	client, ok := m.fetcher.(codex.SavedSessionClient)
	if !ok || f.scope == "" {
		f.failed = true
		return nil
	}
	if f.loading {
		return nil
	}
	f.loading, f.failed = true, false
	f.epoch++
	msg := savedResult{epoch: f.epoch, scope: f.scope, cursor: cursor, id: id}
	query := strings.TrimSpace(f.query())
	search, _ := m.fetcher.(codex.SavedSessionSearchClient)
	ctx, stop := context.WithCancel(context.Background())
	f.cancel = stop
	return func() tea.Msg {
		defer stop()
		ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
		defer cancel()
		if id == "" && search != nil {
			msg.page, msg.err = search.SearchSavedSessions(ctx, msg.scope, cursor, query)
		} else {
			msg.page, msg.err = client.BrowseSavedSessions(ctx, msg.scope, cursor, id)
		}
		return msg
	}
}
func (f *sessionFinder) accept(r savedResult) {
	if !f.include || r.epoch != f.epoch || r.scope != f.scope {
		return
	}
	f.loading, f.failed = false, r.err != nil
	if f.failed {
		if r.cursor == "" || r.id != "" {
			f.rows, f.turns = nil, nil
			f.detail, f.next = "", ""
			f.index, f.scroll = 0, 0
		}
		return
	}
	if r.id != "" {
		f.turns = r.page.Turns
		return
	}
	if r.cursor == "" {
		f.rows = nil // A new query replaces the first page, never merges it.
	}
	seen := make(map[string]bool, len(f.rows))
	for _, row := range f.rows {
		seen[row.ID] = true
	}
	for _, row := range r.page.Sessions {
		if !seen[row.ID] {
			cursor := row.ListCursor
			if cursor == "" {
				cursor = r.cursor
			}
			f.rows = append(f.rows, savedRow{row, cursor})
			seen[row.ID] = true
		}
	}
	f.next = r.page.Next
	if f.next == r.cursor {
		f.next = ""
	}
}
func (m Model) savedMatches() []savedRow {
	if !m.finder.include {
		return nil
	}
	live := make(map[string]bool, len(m.monitorSessionData))
	for _, s := range m.monitorSessionData {
		if m.monitorSessionVisible(s) {
			live[s.id] = true
		}
	}
	var out []savedRow
	matches := codex.SavedSessionNameFilter(m.finder.query())
	for _, r := range m.finder.rows {
		if (!live[r.ID] || r.ID == m.finder.resumeID) && matches(r.SavedSession) {
			out = append(out, r)
		}
	}
	return out
}
func (m *Model) activateSaved(resume bool) tea.Cmd {
	f := &m.finder
	if f.loading || f.resumeID != "" {
		return nil
	}
	rows := m.savedMatches()
	index := f.savedIndex(rows)
	if index >= 0 && index < len(rows) {
		r := rows[index]
		f.selected = r.ID
		f.detail = r.ID
		f.failed = false
		f.scroll = 0
		f.turns = nil
		if resume {
			if cmd := m.resumeSaved(); cmd != nil || m.actionReview != nil {
				return cmd
			}
		}
		return m.requestSaved(r.cursor, r.ID)
	}
	if index == len(rows) && f.next != "" {
		return m.requestSaved(f.next, "")
	}
	return nil
}

func (f sessionFinder) savedIndex(rows []savedRow) int {
	if f.selected != "" {
		return slices.IndexFunc(rows, func(row savedRow) bool { return row.ID == f.selected })
	}
	return f.index
}

func (f *sessionFinder) selectIndex(rows []savedRow, index int) {
	f.index, f.selected = index, ""
	if index >= 0 && index < len(rows) {
		f.selected = rows[index].ID
	}
}

func (m *Model) resumeSaved() tea.Cmd {
	f := &m.finder
	client, ok := m.fetcher.(codex.SavedSessionResumeClient)
	if !ok || f.detail == "" || f.loading || f.failed || f.resumeID != "" {
		return nil
	}
	for _, row := range f.rows {
		if row.ID != f.detail {
			continue
		}
		identity := strings.TrimSpace(row.Name + "  " + row.Directory)
		if m.reviewMonitorAction("resume-saved", row.ID+" // "+identity, i18n.Text("RESUME SAVED SESSION"), i18n.Text("Codex may automatically continue an active goal in this session."), []string{f.scope, row.cursor, row.ID}) {
			return nil
		}
		f.loading = true
		f.resumeID = row.ID
		f.resumeNotice = ""
		f.epoch++
		msg := savedResult{epoch: f.epoch, scope: f.scope, id: row.ID, cursor: row.cursor, resume: true}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), codex.SavedResumeTimeout)
			defer cancel()
			msg.err = client.ResumeSavedSession(ctx, msg.scope, msg.cursor, msg.id)
			return msg
		}
	}
	return nil
}
func (m Model) savedLines(width int) []string {
	f := m.finder
	if f.resumeNotice != "" {
		return wrapProse(f.resumeNotice, width)
	}
	if f.loading {
		return inspectionStatus(false, true)
	}
	if f.resumeID != "" {
		return wrapProse(i18n.Text("Opening the resumed session…"), width)
	}
	if status := inspectionStatus(f.failed, f.loading); status != nil {
		return status
	}
	return m.savedTranscriptLines(f.turns, width)
}

func inspectionStatus(failed, loading bool) []string {
	if failed {
		return []string{i18n.Text("UNAVAILABLE")}
	}
	if loading {
		return []string{"…"}
	}
	return nil
}

func (m Model) savedTranscriptLines(turns []codex.SavedTurn, width int) []string {
	width = max(width, 1)
	var lines []string
	for _, turn := range slices.Backward(turns) {
		if task := codex.SanitizeSessionContext(turn.Task); task != "" {
			lines = append(lines, wrapProse(task, width)...)
			lines = append(lines, "")
		}
		if reply := i18n.SessionText(turn.Reply, turn.Restricted); reply != "" {
			lines = append(lines, m.proseLines(codex.SanitizeSessionContext(reply), width)...)
			lines = append(lines, "")
		}
	}
	if len(lines) == 0 {
		return []string{i18n.Text("NO COMPLETED TURNS")}
	}
	return lines
}

type finderGeometry struct {
	x, search, rows, width, ageWidth int
	framed                           bool
}

func (g finderGeometry) firstResult() int { return g.search + 1 }
func (g finderGeometry) nameWidth() int {
	if g.ageWidth != 0 {
		return g.width - g.ageWidth - 2
	}
	return g.width
}

func (f sessionFinder) layout(w, h int) finderGeometry {
	g := finderGeometry{rows: max(h-1, 0), width: w}
	if f.include && h >= 4 && w >= 8 {
		g.x, g.search, g.rows, g.width, g.framed = 2, 1, h-3, w-4, true
	}
	if f.include && f.detail == "" && g.width >= 30 {
		g.ageWidth = min(16, max(10, lipgloss.Width(i18n.Text("TIME AGO"))))
	}
	return g
}

func finderStart(index, n int) int { return max(0, index-max(n-1, 0)) }

func (f sessionFinder) inputWidth(w int) int { return max(1, w-1) }
func (m Model) renderSessionFinder(w, h int, colors palette) string {
	f := m.finder
	g := f.layout(w, h)
	inputWidth := f.inputWidth(g.nameWidth())
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
	if g.ageWidth != 0 {
		heading := ansi.Truncate(i18n.Text("TIME AGO"), g.ageWidth, "…")
		line = joinRight(line, colors.dimmed().Render(heading), g.width)
	} else {
		line += strings.Repeat(" ", max(0, g.width-inputWidth))
	}
	lines := []string{line}
	if f.include && g.rows > 0 {
		lines = append(lines, m.renderSavedResults(g, colors)...)
	}
	if g.framed {
		arrows := m.renderMonitorNavigationButtons(m.savedListButtons(g, w), colors.header(), colors)
		return frameSizedWithTitleAction(w, h-2, savedFinderTitle(), arrows, strings.Join(lines, "\n"), colors.primary, colors)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func savedIdentity(row savedRow) string {
	identity := strings.TrimSpace(row.Name + "  " + row.Directory)
	if identity == "" {
		return shortSessionID(row.ID)
	}
	return shortSessionID(row.ID) + " // " + identity
}

func savedSessionAge(updatedAt int64, now time.Time) string {
	if updatedAt <= 0 {
		return "—"
	}
	minutes := max(now.Unix()-updatedAt, 0) / 60
	switch {
	case minutes >= 24*60:
		return i18n.Format("%s ago", fmt.Sprintf("%dd", minutes/(24*60)))
	case minutes >= 60:
		return i18n.Format("%s ago", fmt.Sprintf("%dh", minutes/60))
	case minutes > 0:
		return i18n.Format("%s ago", fmt.Sprintf("%dm", minutes))
	default:
		return i18n.Text("JUST NOW")
	}
}

func (m Model) savedListButtons(g finderGeometry, width int) []monitorNavigationButton {
	const actionWidth = 7 // [↑] [↓]
	if !g.framed || m.finder.detail != "" || width < lipgloss.Width(savedFinderTitle())+actionWidth+6 {
		return nil
	}
	rows := m.savedMatches()
	index := m.finder.savedIndex(rows)
	last := len(rows) - 1
	if m.finder.next != "" {
		last++
	}
	buttons := []monitorNavigationButton{
		{label: "[↑]", action: "saved-up", enabled: index > 0},
		{label: "[↓]", action: "saved-down", enabled: index < last},
	}
	layoutNavigationButtons(buttons, width-actionWidth-2, 0)
	return buttons
}

func (m Model) savedPreviewButtons(g finderGeometry) []monitorNavigationButton {
	if g.rows == 0 || g.width < 3 {
		return nil
	}
	f := m.finder
	buttons := []monitorNavigationButton{{label: "[←]", action: "saved-back", enabled: f.resumeID == "" || !f.loading}}
	if _, ok := m.fetcher.(codex.SavedSessionResumeClient); ok && !f.loading && !f.failed && f.resumeID == "" {
		label := resumeLabel()
		if lipgloss.Width(buttons[0].label)+1+lipgloss.Width(label) <= g.width {
			buttons = append(buttons, monitorNavigationButton{label: label, action: "saved-resume", enabled: true})
		}
	}
	layoutNavigationButtons(buttons, g.x, g.firstResult())
	return buttons
}

func (m Model) renderSavedResults(g finderGeometry, colors palette) []string {
	f := m.finder
	rows := m.savedMatches()
	index := f.savedIndex(rows)
	var body []string
	label := colors.label()
	appendRow := func(text, age string, selected bool) {
		marker, style := "  ", label
		if selected && f.focus == findResults {
			marker, style = "› ", style.Bold(true)
		}
		if g.ageWidth != 0 && age != "" {
			left := style.Render(ansi.Truncate(marker+text, g.nameWidth(), "…"))
			right := colors.dimmed().Render(ansi.Truncate(age, g.ageWidth, "…"))
			body = append(body, joinRight(left, right, g.width))
			return
		}
		body = append(body, style.Render(ansi.Truncate(marker+text, g.width, "…")))
	}
	if f.detail != "" {
		for _, r := range rows {
			if r.ID == f.detail {
				header := m.renderMonitorNavigationButtons(m.savedPreviewButtons(g), colors.label(), colors)
				separator := ""
				if header != "" {
					separator = " "
				}
				if space := g.width - lipgloss.Width(header) - len(separator); space > 0 {
					header += separator + label.Render(ansi.Truncate(savedIdentity(r), space, "…"))
				}
				body = append(body, header)
				text := m.savedLines(g.width)
				start := min(f.scroll, max(0, len(text)-max(g.rows-1, 1)))
				for _, line := range text[start:min(len(text), start+g.rows-1)] {
					body = append(body, label.Render(ansi.Truncate(line, g.width, "…")))
				}
				break
			}
		}
		return body
	}
	now := time.Now()
	for i := finderStart(index, g.rows); i < len(rows) && len(body) < g.rows; i++ {
		appendRow(savedIdentity(rows[i]), savedSessionAge(rows[i].UpdatedAt, now), i == index)
	}
	if len(body) < g.rows {
		switch {
		case f.loading:
			appendRow("…", "", false)
		case f.next != "":
			label := i18n.Text("MORE")
			if strings.TrimSpace(f.query()) != "" {
				label = i18n.Text("SEARCH OLDER SESSIONS")
			}
			if f.failed {
				label += " // " + i18n.Text("UNAVAILABLE")
			}
			appendRow("[+] "+label, "", index == len(rows))
		case f.failed:
			appendRow(i18n.Text("UNAVAILABLE"), "", false)
		case len(rows) == 0:
			appendRow(i18n.Text("NO RESULTS MATCH THE ACTIVE FILTER"), "", false)
		}
	}
	return body
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
	if f.resumeID != "" {
		if !f.loading {
			f.clearHistory() // Explicitly leave the picker, not another attempt.
		}
		return
	}
	f.detail = ""
	f.turns = nil
	f.epoch++
	f.loading = false
	f.failed = false
	f.resumeNotice = ""
	f.scroll = 0
}

func (m *Model) scrollFinder(delta, width, height int) {
	f := &m.finder
	if f.detail != "" {
		g := f.layout(width, height)
		f.scroll = max(0, min(f.scroll+delta, len(m.savedLines(g.width))-max(1, g.rows-1)))
		return
	}
	rows := m.savedMatches()
	last := len(rows) - 1
	if f.next != "" {
		last++
	}
	f.selectIndex(rows, max(0, min(f.savedIndex(rows)+delta, last)))
}

func (m Model) updateSessionFinder(message tea.Msg) (Model, tea.Cmd, bool) {
	f := &m.finder
	if deadline, ok := message.(savedResumeDeadline); ok {
		if f.resumeID == deadline.id && f.scope == deadline.scope && f.epoch == deadline.epoch && f.resumeNotice == "" {
			f.resumeNotice = i18n.Text("Resume accepted; still waiting for the session to appear.")
		}
		return m, nil, true
	}
	if r, ok := message.(savedResult); ok {
		if r.resume {
			if f.include && r.epoch == f.epoch && r.scope == f.scope {
				f.loading, f.failed = false, false
				if r.err != nil && errors.Is(r.err, codex.ErrSavedSessions) {
					f.resumeID = ""
					f.resumeNotice = i18n.Text("Codex did not accept the resume.")
					return m, nil, true
				}
				f.resumeID = r.id
				var deadline tea.Cmd
				if r.err != nil {
					f.resumeNotice = i18n.Text("Resume outcome unknown. Check Codex before trying again.")
				} else {
					deadline = tea.Tick(15*time.Second, func(time.Time) tea.Msg { return savedResumeDeadline{r} })
				}
				if !m.monitorFetchActive {
					next, cmd := m.beginMonitorFetch(monitorFetchSample)
					return next, tea.Batch(cmd, deadline), true
				}
				return m, deadline, true
			}
			return m, nil, true
		}
		f.accept(r)
		if f.include && f.selected == "" && r.scope == f.scope && r.epoch == f.epoch && r.id == "" {
			f.selectIndex(m.savedMatches(), f.index)
		}
		return m, nil, true
	}
	if m.meterView != viewMonitor || m.monitorContextDetail != "" || m.scheduleUI.open || m.monitorCommands.open || m.monitorQueue.open || m.monitorQueue.compact != "" {
		return m, nil, false
	}
	if mouse, ok := message.(tea.MouseMsg); ok && f.open {
		cmd, handled := m.updateFinderMouse(mouse)
		return m, cmd, handled
	}
	key, pressed := message.(tea.KeyPressMsg)
	if f.focus == findNone {
		if pressed && (key.String() == "/" || key.String() == "r") && !key.IsRepeat && !m.monitorPrompt.input.Focused() && f.scope != "" && !f.include {
			cmd := m.openResumePicker()
			return m, cmd, true
		}
		if pressed && key.String() == "f" && !m.monitorPrompt.input.Focused() {
			cmd := m.focusFinder()
			return m, cmd, true
		}
		if !pressed || !f.include {
			return m, nil, false
		}
		switch key.String() {
		case "esc", "up", "down", "pgup", "pgdown":
			m.focusFinderResults()
		default:
			return m, nil, false
		}
	}
	if f.resumeID != "" {
		if pressed && key.String() == "esc" {
			f.closeDetail()
			return m, nil, true
		}
		// Keep this handoff intact while the resume is unresolved.
		if isFinderPaste(message) || pressed && key.String() != "ctrl+c" {
			return m, nil, true
		}
	}
	if pressed {
		if key.IsRepeat && (key.String() == "enter" || key.String() == "r") && f.include && f.focus == findResults {
			return m, nil, true
		}
		switch key.String() {
		case "ctrl+c":
			m.leaveFinder()
			return m, nil, false
		case "esc":
			if f.detail != "" {
				f.closeDetail()
				f.focus = findResults
			} else if f.include {
				f.clearHistory()
			} else {
				f.clearQuery()
				f.clearHistory()
				f.open = false
				m.leaveFinder()
			}
			return m, nil, true
		case "tab", "shift+tab":
			if !f.include {
				m.leaveFinder()
				return m, nil, false
			}
			f.closeDetail()
			if f.focus != findText {
				f.focus = findText
				cmd := f.editInput().Focus()
				return m, cmd, true
			}
			m.focusFinderResults()
			return m, nil, true
		case "left":
			if f.include && f.detail != "" && f.focus == findResults {
				f.closeDetail()
				return m, nil, true
			}
		case "right":
			if f.include && f.focus == findResults && f.detail == "" {
				cmd := m.activateSaved(false)
				return m, cmd, true
			}
		case "enter", "r", "space", " ":
			if f.focus == findResults {
				resume := key.String() == "enter" || key.String() == "r"
				if f.detail == "" {
					cmd := m.activateSaved(resume)
					return m, cmd, true
				}
				if resume {
					cmd := m.resumeSaved()
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
				if f.include {
					m.focusFinderResults()
					if key.IsRepeat {
						return m, nil, true
					}
					if key.String() == "enter" {
						cmd := m.activateSaved(true)
						return m, cmd, true
					}
				} else {
					m.leaveFinder()
					m.returnFromMonitorHeader()
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
			g := m.dashboardLayout()
			area := m.monitorArea(g.contentWidth, g.meterHeight)
			input.SetWidth(max(1, f.inputWidth(f.layout(g.contentWidth, area.finderRows).nameWidth())-3))
			input, cmd := input.Update(message)
			f.input = &input
			if old != f.query() {
				if f.include {
					f.index, f.selected = 0, ""
					if _, cachedSearch := m.fetcher.(codex.SavedSessionSearchClient); cachedSearch {
						f.stopRequest()
						f.next, f.loading = "", false
						return m, tea.Batch(cmd, m.requestSaved("", "")), true
					}
					return m, cmd, true
				}
				f.index, m.monitorScroll = 0, 0
				m.reconcileMonitorFilterSelection()
			}
			return m, cmd, true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m *Model) focusFinderResults() {
	m.leaveFinder()
	m.finder.focus = findResults
}

func (m *Model) updateFinderMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	f := &m.finder
	d := m.dashboardLayout()
	area := m.monitorArea(d.contentWidth, d.meterHeight)
	g := f.layout(d.contentWidth, area.finderRows)
	mouse := msg.Mouse()
	x, y := mouse.X-2, mouse.Y-d.meterY-area.topHeight-area.attentionRows
	inFrame := x >= 0 && x < d.contentWidth && y >= 0 && y < area.finderRows
	inBody := x >= g.x && x < g.x+g.width
	results := inBody && y >= g.firstResult() && y < g.firstResult()+g.rows
	hit := ""
	if f.detail != "" && results {
		hit = monitorNavigationButtonsHit(m.savedPreviewButtons(g), x, y)
	} else if inFrame && y == 0 && f.include {
		hit = monitorNavigationButtonsHit(m.savedListButtons(g, d.contentWidth), x, y)
	}
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if hit != "" {
			m.monitorContextHover = hit
			return nil, true
		}
	case tea.MouseWheelMsg:
		if f.include && results {
			delta := 1
			if mouse.Button == tea.MouseWheelUp {
				delta = -1
			}
			m.focusFinderResults()
			m.scrollFinder(delta, d.contentWidth, area.finderRows)
			return nil, true
		}
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			break
		}
		if !inFrame {
			m.leaveFinder()
			break
		}
		if inBody && x < g.x+g.nameWidth() && y == g.search {
			return m.focusFinder(), true
		}
		if hit == "saved-up" || hit == "saved-down" {
			m.focusFinderResults()
			delta := 1
			if hit == "saved-up" {
				delta = -1
			}
			m.scrollFinder(delta, d.contentWidth, area.finderRows)
			return nil, true
		}
		if results {
			m.focusFinderResults()
			switch {
			case hit == "saved-back":
				f.closeDetail()
			case hit == "saved-resume":
				return m.resumeSaved(), true
			case f.detail == "":
				rows := m.savedMatches()
				i := finderStart(f.savedIndex(rows), g.rows) + y - g.firstResult()
				if i < len(rows) || (i == len(rows) && f.next != "") {
					f.selectIndex(rows, i)
					return m.activateSaved(false), true
				}
			}
		}
		return nil, true
	}
	return nil, false
}

func isFinderPaste(msg tea.Msg) bool { _, ok := msg.(tea.PasteMsg); return ok }

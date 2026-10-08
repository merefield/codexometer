package ui

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
	"github.com/merefield/codexometer/internal/schedule"
)

type scheduleUI struct {
	queue                       *schedule.Queue
	open, polling               bool
	session, id, notice         string
	input                       monitorEditor
	mode, focus, hours, minutes int
	date                        time.Time
	confirmID, confirmToken     string
	confirmUntil                time.Time
	numeric                     string
	scroll, composerRows        int
	toggleHover                 bool
	hover                       string
	drafts                      map[string]scheduleDraft
}

type scheduleDraft struct {
	id, text             string
	mode, hours, minutes int
	date                 time.Time
}
type scheduleDone struct{ err error }

// A scheduled follow-up replaces the invitation to reply, not live work or warnings.
func (m Model) sessionTriggerStatus(s monitorSession) bool {
	return (s.attention == codex.SessionAttentionComplete || s.attention == codex.SessionAttentionNone) && !s.working && m.hasSchedule(s.id)
}

func (m Model) hasSchedule(id string) bool {
	if m.scheduleUI.queue == nil {
		return false
	}
	for _, j := range m.scheduleUI.queue.List(id) {
		if j.Status != "sent" {
			return true
		}
	}
	return false
}

func (m *Model) openSchedule() tea.Cmd {
	if m.meterView != viewMonitor || m.monitorContextDetail == "" {
		return nil
	}
	id := m.monitorContextDetail
	o := m.monitorPromptOffer()
	if !m.hasSchedule(id) && (o.Token == "" || o.TurnID != "" || o.ThreadID != id || len(o.Questions) > 0) {
		return nil
	}
	m.showLiveMonitorHistory()
	q := m.scheduleUI.queue
	if q == nil {
		q = schedule.New()
	}
	p := scheduleUI{queue: q, polling: m.scheduleUI.polling, drafts: m.scheduleUI.drafts, open: true, session: id, input: newMonitorEditor(), hours: 1, date: time.Now().Add(time.Hour).Truncate(time.Minute)}
	g := m.monitorDashboardLayout()
	p.composerRows = max(2, m.monitorPromptRows(g.contentWidth, g.meterHeight))
	if m.monitorPrompt.session == id {
		p.input = m.monitorPrompt.input
	}
	for _, j := range q.List(id) {
		if j.Status == "sent" {
			continue
		}
		p.id = j.ID
		p.input.SetValue(j.Text)
		p.notice = i18n.Text("Status: ") + i18n.Text(j.Status)
		if j.Trigger == "quota" {
			p.mode = 0
		} else {
			p.mode = 2
			p.date = j.At.Local()
		}
		break
	}
	if p.id == "" && schedule.QuotaReady(m.snapshot, time.Now()) {
		p.mode = 1
	}
	if draft, ok := p.drafts[id]; ok && draft.id == p.id {
		p.mode, p.hours, p.minutes, p.date = draft.mode, draft.hours, draft.minutes, draft.date
		p.input.SetValue(draft.text)
	}
	p.drafts = maps.Clone(p.drafts)
	delete(p.drafts, id)
	m.scheduleUI = p
	m.monitorPrompt.input.Blur()
	return m.scheduleUI.input.Focus()
}

func (m *Model) dispatchSchedules() tea.Cmd {
	q := m.scheduleUI.queue
	client, ok := m.fetcher.(codex.SessionPromptClient)
	if q == nil || !ok || m.scheduleUI.polling || m.err != nil || m.monitorError != "" || !schedule.QuotaReady(m.snapshot, time.Now()) {
		return nil
	}
	// Do not compete with a draft, manual confirmation or profile transition.
	if m.scheduleUI.open || (m.scheduleUI.confirmID != "" && time.Now().Before(m.scheduleUI.confirmUntil)) || m.monitorPrompt.input.Focused() || m.monitorPrompt.busy || m.quota.busySession != "" {
		return nil
	}
	ready := map[string]bool{}
	for _, s := range m.monitorSessionData {
		_, pending := m.quotaSessionCandidate(s)
		if !pending && s.preview.Kind != codex.SessionContextApproval && s.preview.Kind != codex.SessionContextQuestion {
			ready[s.id] = true
		}
	}
	account := m.snapshot.AccountFingerprint
	snapshot := m.snapshot
	m.scheduleUI.polling = true
	return func() tea.Msg {
		for _, j := range q.List("") {
			if !ready[j.Session] || j.Status != "pending" || !schedule.Covers(j, snapshot) || !schedule.Fresh(snapshot, time.Now()) {
				continue
			}
			o := client.SessionPrompt(j.Session)
			if o.Token == "" || o.ThreadID != j.Session || len(o.Questions) > 0 {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			_ = q.Dispatch(ctx, j.ID, account, true, time.Now(), func(ctx context.Context, j schedule.Job) error {
				return client.SendSessionPrompt(ctx, o.Token, []string{j.Text})
			})
			cancel()
		}
		return scheduleDone{}
	}
}

func (m Model) updateSchedule(msg tea.Msg) (Model, tea.Cmd, bool) {
	if done, ok := msg.(scheduleDone); ok {
		m.scheduleUI.polling = false
		if done.err != nil {
			m.scheduleUI.notice = i18n.Text("Send unconfirmed; check Codex before retrying.")
		}
		return m, nil, true
	}
	if !m.scheduleUI.open {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.MouseClickMsg, tea.MouseMotionMsg:
		default:
			return m, nil, false
		}
	}
	if motion, ok := msg.(tea.MouseMotionMsg); ok {
		m.scheduleUI.toggleHover = m.scheduleToggleAt(motion.X, motion.Y)
		if m.scheduleUI.open {
			m.scheduleUI.hover = m.scheduleControlAt(motion.X, motion.Y).key
			m.hoveredButton = m.footerButtonAt(motion.X, motion.Y)
			if m.editorNavigationAt(motion.X, motion.Y) {
				return m, nil, false
			}
		}
		return m, nil, m.scheduleUI.open
	}
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && m.scheduleToggleAt(click.X, click.Y) {
		if m.scheduleUI.open {
			m.closeSchedule()
			return m, nil, true
		}
		cmd := m.openSchedule()
		return m, cmd, true
	}
	key, isKey := msg.(tea.KeyPressMsg)
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && m.scheduleUI.open {
		if m.editorNavigationAt(click.X, click.Y) {
			p := &m.scheduleUI
			p.drafts = maps.Clone(p.drafts)
			if p.drafts == nil {
				p.drafts = map[string]scheduleDraft{}
			}
			p.drafts[p.session] = scheduleDraft{id: p.id, text: p.input.Value(), mode: p.mode, hours: p.hours, minutes: p.minutes, date: p.date}
			m.closeSchedule()
			return m, nil, false
		}
		if button := m.footerButtonAt(click.X, click.Y); button != footerButtonNone {
			next, cmd := m.pressFooterButton(button)
			return next.(Model), cmd, true
		}
	}
	if m.scheduleUI.open && isKey && m.scheduleUI.focus != 0 && (key.String() == "t" || key.String() == "r") {
		button := footerButtonTheme
		if key.String() == "r" {
			button = footerButtonRefresh
		}
		next, cmd := m.pressFooterButton(button)
		return next.(Model), cmd, true
	}
	if m.scheduleUI.open && isKey && (key.String() == "ctrl+c" || key.String() == "q" && m.scheduleUI.focus != 0) {
		return m, tea.Quit, true
	}
	if !m.scheduleUI.open {
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
			if id, ok := strings.CutPrefix(m.monitorContextAt(click.X, click.Y), "attention-trigger:"); ok {
				delete(m.monitorDismissed, id)
				if index := m.monitorSessionIndex(id); index >= 0 {
					m.monitorSessionData[index].displayed = true
				}
				m.monitorSessions = m.visibleMonitorSessionCount()
				m.setRowContext(id, contextFull)
				m.showLiveMonitorHistory()
				row := m.monitorContextRows[id]
				row.review = "context"
				m.monitorContextRows[id] = row
				return m, nil, true
			}
		}
		if next, cmd, handled := m.schedulePanelKey(msg); handled {
			return next, cmd, true
		}
		if isKey && key.String() == "ctrl+s" && m.monitorContextDetail != "" {
			cmd := m.openSchedule()
			return m, cmd, true
		}
		return m, nil, false
	}
	p := &m.scheduleUI
	if isKey && p.focus < 5 {
		p.notice = ""
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
		m.revealScheduleFocus()
		return m, nil, true
	}
	if isKey && (key.String() == "esc" || key.String() == "ctrl+s") {
		m.closeSchedule()
		return m, nil, true
	}
	g := m.monitorDashboardLayout()
	p.input.configure(g.contentWidth, m.schedulePaneLayout(g.contentWidth, g.meterHeight).composerRows+7)
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		layout := m.schedulePaneLayout(g.contentWidth, g.meterHeight)
		if wheel.Y >= g.meterY+1 && wheel.Y < g.meterY+1+layout.textRows {
			delta := 3
			if wheel.Button == tea.MouseWheelUp {
				delta = -3
			}
			p.scroll = min(max(layout.scroll+delta, 0), max(len(layout.indices)-layout.textRows, 0))
		}
		return m, nil, true
	}
	if click, ok := msg.(tea.MouseClickMsg); ok {
		if click.Button != tea.MouseLeft {
			return m, nil, true
		}
		if click.Button == tea.MouseLeft {
			button := m.footerButtonAt(click.X, click.Y)
			if button == footerButtonTheme || button == footerButtonRefresh || button == footerButtonQuit {
				next, cmd := m.pressFooterButton(button)
				return next.(Model), cmd, true
			}
		}
		layout := m.schedulePaneLayout(g.contentWidth, g.meterHeight)
		localY := click.Y - g.meterY
		click.X -= 2 // dashboard's outer left padding
		if click.X < 2 || click.X >= g.contentWidth-2 {
			return m, nil, true
		}
		if localY >= layout.composerY && localY < layout.composerY+layout.composerRows {
			p.focus = 0
			return m, p.input.Focus(), true
		}
		index := localY - 1 + layout.scroll
		if localY < 1 || localY >= 1+layout.textRows || index >= len(layout.indices) {
			return m, nil, true
		}
		hit := m.scheduleControlAt(click.X+2, click.Y)
		p.numeric = ""
		if hit.key != "" {
			p.focus = hit.focus
			switch {
			case strings.HasPrefix(hit.key, "mode:"):
				p.mode = hit.value
			case strings.HasPrefix(hit.key, "day:"):
				p.date = time.Date(p.date.Year(), p.date.Month(), hit.value, p.date.Hour(), p.date.Minute(), 0, 0, p.date.Location())
			case hit.focus == 5 || hit.focus == 6:
				isKey = true
				key = tea.KeyPressMsg{Code: tea.KeyEnter}
			}
		}
		p.input.Blur()
		if !isKey {
			m.revealScheduleFocus()
			return m, nil, true
		}
	}
	if isKey {
		if p.focus >= 2 && p.focus <= 4 && !(p.mode == 2 && p.focus == 2) && p.mode != 0 && (len(key.String()) == 1 && key.String() >= "0" && key.String() <= "9" || key.String() == "backspace") {
			if key.String() == "backspace" {
				if len(p.numeric) > 0 {
					p.numeric = p.numeric[:len(p.numeric)-1]
				}
			} else {
				p.numeric += key.String()
			}
			n, _ := strconv.Atoi(p.numeric)
			switch {
			case p.mode == 1 && p.focus == 2:
				p.hours = min(n, 8760)
			case p.mode == 1 && p.focus == 3:
				p.minutes = min(n, 59)
			case p.mode == 2 && p.focus == 3:
				p.date = time.Date(p.date.Year(), p.date.Month(), p.date.Day(), min(n, 23), p.date.Minute(), 0, 0, p.date.Location())
			case p.mode == 2 && p.focus == 4:
				p.date = time.Date(p.date.Year(), p.date.Month(), p.date.Day(), p.date.Hour(), min(n, 59), 0, 0, p.date.Location())
			}
			if len(p.numeric) > 4 {
				p.numeric = ""
			}
			return m, nil, true
		}
		switch key.String() {
		case "tab", "shift+tab":
			d := 1
			if key.String() == "shift+tab" {
				d = -1
			}
			p.moveFocus(d)
			m.revealScheduleFocus()
			p.input.Blur()
			if p.focus == 0 {
				return m, p.input.Focus(), true
			}
			return m, nil, true
		case "enter":
			if p.focus == 5 {
				if g.contentWidth < 40 || m.schedulePaneLayout(g.contentWidth, g.meterHeight).textRows < 1 {
					p.notice = i18n.Text("Enlarge the terminal before confirming.")
					return m, nil, true
				}
				now := time.Now()
				if reason := p.validation(now); reason != "" {
					p.notice = reason
					return m, nil, true
				}
				o := m.monitorPromptOffer()
				if o.Token == "" || o.TurnID != "" || o.ThreadID != p.session || len(o.Questions) > 0 || m.err != nil || !schedule.Fresh(m.snapshot, time.Now()) {
					p.notice = i18n.Text("Session controls unavailable.")
					return m, nil, true
				}
				j := schedule.Job{Session: p.session, Text: p.input.Value(), Trigger: "quota", Zone: time.Now().Location().String(), Windows: schedule.WindowKeys(m.snapshot)}
				if p.mode == 1 {
					j.Trigger = "at"
					j.At = p.targetTime(now)
				}
				if p.mode == 2 {
					j.Trigger = "at"
					j.At = p.date
				}
				if err := p.queue.SaveIfCurrent(j, m.snapshot.AccountFingerprint, time.Now(), p.id); err != nil {
					p.notice = err.Error()
				} else {
					p.open = false
					p.toggleHover = false
					p.input.Reset()
					m.monitorPrompt.input.Reset()
				}
			} else if p.focus == 6 {
				m.closeSchedule()
			} else {
				p.moveFocus(1)
				m.revealScheduleFocus()
				p.input.Blur()
				if p.focus == 0 {
					return m, p.input.Focus(), true
				}
			}
			return m, nil, true
		case "left", "right", "up", "down", "pgup", "pgdown":
			p.numeric = ""
			if p.focus != 0 {
				d := 1
				if key.String() == "left" || key.String() == "down" || key.String() == "pgup" {
					d = -1
				}
				switch p.focus {
				case 1:
					p.mode = (p.mode + d + 3) % 3
				case 2:
					if p.mode == 1 {
						p.hours = max(0, min(8760, p.hours+d))
					} else if p.mode == 2 {
						if key.String() == "pgup" || key.String() == "pgdown" {
							p.date = time.Date(p.date.Year(), p.date.Month()+time.Month(d), 1, p.date.Hour(), p.date.Minute(), 0, 0, p.date.Location())
						} else {
							if key.String() == "up" {
								d = -7
							}
							if key.String() == "down" {
								d = 7
							}
							p.date = p.date.AddDate(0, 0, d)
						}
					}
				case 3:
					if p.mode == 1 {
						p.minutes = (p.minutes + d + 60) % 60
					} else {
						p.date = p.date.Add(time.Duration(d) * time.Hour)
					}
				case 4:
					if p.mode == 2 {
						p.date = p.date.Add(time.Duration(d) * time.Minute)
					}
				}
				m.revealScheduleFocus()
				return m, nil, true
			}
		}
	}
	if p.focus == 0 {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		if isKey {
			return m, cmd, true
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return m, cmd, true
		}
	}
	if isKey {
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) scheduleFormLines() []string {
	lines, _ := m.scheduleFormDocument()
	return lines
}

func (m Model) scheduleFormDocument() ([]string, []scheduleControl) {
	p := m.scheduleUI
	now := time.Now()
	c := paletteFor(m.theme)
	lines := make([]string, 24)
	var controls []scheduleControl
	// Rendering and interaction use these exact cell ranges, including translated labels.
	control := func(row, x int, label string, focus int, key string, value int) {
		label = ansi.Truncate(label, max(m.width-4-x, 0), "")
		if label == "" {
			return
		}
		style := c.label()
		if strings.HasPrefix(key, "day:") {
			style = style.Bold(false).Foreground(c.primary).Background(c.background)
		}
		if focus == 1 && p.mode == value {
			style = style.Foreground(c.primary).Underline(true)
		}
		selected := p.focus == focus && (focus != 1 || p.mode == value) && (focus != 2 || !strings.HasPrefix(key, "day:") || p.date.Day() == value)
		if selected || p.hover == key {
			style = style.Underline(false).Foreground(c.background).Background(c.primary)
		} else if strings.HasPrefix(key, "day:") && p.date.Day() == value {
			style = style.Foreground(c.primary).Underline(true)
		}
		if focus == 5 && (p.validation(now) != "" || m.width < 40) {
			style = c.dimmed()
		}
		gap := strings.Repeat(" ", max(x-ansi.StringWidth(lines[row]), 0))
		if strings.HasPrefix(key, "day:") {
			gap = c.label().Bold(false).Foreground(c.primary).Background(c.background).Render(gap)
		}
		lines[row] += gap + style.Render(label)
		controls = append(controls, scheduleControl{key: key, x: x, row: row, width: ansi.StringWidth(label), focus: focus, value: value})
	}
	name := i18n.Text("Unnamed session")
	for _, session := range m.monitorSessionData {
		if session.id != p.session {
			continue
		}
		if label := strings.TrimSpace(terminalLabel(session.name)); label != "" {
			name = label
		} else if session.workingDirectory != "" {
			name = filepath.Base(terminalLabel(session.workingDirectory))
		}
		break
	}
	prefix, suffix := i18n.Text("SCHEDULE FOLLOW-UP // "), " // "+shortSessionID(p.session)
	if p.id != "" {
		prefix = i18n.Text("EDIT SCHEDULED FOLLOW-UP // ")
	}
	lines[0] = prefix + ansi.Truncate(name, max(m.width-6-ansi.StringWidth(prefix+suffix), 1), "…") + suffix
	lines[1] = i18n.Text("In memory only • closing Codexometer cancels this trigger")
	lines[2] = i18n.Text("Tab/Shift+Tab: fields • Wheel: scroll • Esc: back")
	// Kept in the canonical document for tests; only the composer shows this hint.
	lines[6] = c.dimmed().Render(p.focusHint())
	x := 0
	modeLabels := []string{i18n.Text("AFTER QUOTA REFRESH"), i18n.Text("IN…"), i18n.Text("AT…")}
	reserved := max(9, ansi.StringWidth(modeLabels[1])+4) + max(9, ansi.StringWidth(modeLabels[2])+4) + 6
	modeLabels[0] = ansi.Truncate(modeLabels[0], max(m.width-4-reserved, 3), "…")
	total := 2
	for _, label := range modeLabels {
		total += max(9, ansi.StringWidth(label)+4)
	}
	if total > m.width-4 {
		for i, label := range modeLabels {
			modeLabels[i] = ansi.Truncate(label, max((m.width-6)/3-4, 1), "…")
		}
	}
	for i, label := range modeLabels {
		label = "[ " + label + " ]"
		control(7, x, label, 1, fmt.Sprintf("mode:%d", i), i)
		x += max(9, ansi.StringWidth(label)) + 1
	}
	if p.mode == 1 {
		hours := i18n.Format("Hours: %d", p.hours)
		control(9, 0, hours, 2, "hours", 0)
		control(9, max(20, ansi.StringWidth(hours)+2), i18n.Format("Minutes: %02d", p.minutes), 3, "minutes", 0)
	}
	if p.mode == 2 {
		month := p.date.Format("January 2006")
		if i18n.Code() != "en-GB" {
			month = p.date.Format("2006-01")
		}
		control(9, 0, month+" // PgUp/PgDn", 2, "calendar", 0)
		for _, day := range strings.Fields(i18n.Text("Su Mo Tu We Th Fr Sa")) {
			lines[10] += day + strings.Repeat(" ", max(4-ansi.StringWidth(day), 0))
		}
		first := time.Date(p.date.Year(), p.date.Month(), 1, 0, 0, 0, 0, p.date.Location())
		for day := 1; day <= first.AddDate(0, 1, -1).Day(); day++ {
			cell := int(first.Weekday()) + day - 1
			control(11+cell/7, (cell%7)*4, fmt.Sprintf("%2d", day), 2, fmt.Sprintf("day:%d", day), day)
		}
		for row := 11; row <= 16; row++ {
			lines[row] += c.label().Bold(false).Foreground(c.primary).Background(c.background).Render(strings.Repeat(" ", max(28-ansi.StringWidth(lines[row]), 0)))
		}
		hour := i18n.Text("Hour: ") + p.date.Format("15")
		control(18, 0, hour, 3, "hour", 0)
		control(18, max(20, ansi.StringWidth(hour)+2), i18n.Text("Minute: ")+p.date.Format("04"), 4, "minute", 0)
	}
	row := p.actionRow()
	lines[row-2] = c.header().Render(i18n.Text("Will send: ") + scheduleDate(p.targetTime(now)))
	lines[row-1] = i18n.Text("Not before this time; when quota + idle allow")
	if p.mode == 0 {
		lines[row-2] = c.header().Render(i18n.Text("Will send: when quota is available"))
		lines[row-1] = i18n.Text("and this session is idle")
	}
	back := i18n.Text("[ BACK ]")
	confirm := ansi.Truncate(p.confirmLabel(), max(m.width-6-ansi.StringWidth(back), 1), "…")
	control(row, 0, confirm, 5, "confirm", 0)
	control(row, min(max(23, ansi.StringWidth(confirm)+2), max(m.width-4-ansi.StringWidth(back), 0)), back, 6, "back", 0)
	notice := p.notice
	if reason := p.validation(now); reason != "" {
		notice = reason
	}
	if row == 22 {
		lines[19] = notice
	} else {
		lines[row+1] = notice
	}
	for i := 1; i < len(lines); i++ {
		lines[i] = ansi.Truncate(lines[i], max(m.width-4, 1), "…")
	}
	return lines, controls
}
func (p scheduleUI) targetTime(now time.Time) time.Time {
	if p.mode == 1 {
		return now.Add(time.Duration(p.hours)*time.Hour + time.Duration(p.minutes)*time.Minute)
	}
	return p.date
}

func (p scheduleUI) confirmLabel() string {
	if p.id != "" {
		return i18n.Text("[ SAVE CHANGES ]")
	}
	return i18n.Text("[ CONFIRM SCHEDULE ]")
}

func (p scheduleUI) validation(now time.Time) string {
	text := strings.TrimSpace(p.input.Value())
	switch {
	case text == "":
		return i18n.Text("Enter a message to schedule.")
	case codex.IsSessionCommand(text):
		return i18n.Text("Slash commands cannot be scheduled. Use // for literal slash text.")
	case len([]rune(text)) > 4096:
		return i18n.Text("Keep the message within 4096 characters.")
	case codex.SanitizeSessionContext(text) != text:
		return i18n.Text("Remove terminal control characters.")
	case p.mode != 0 && !p.targetTime(now).After(now):
		return i18n.Text("Choose a future time (delay must exceed 0).")
	case p.mode != 0 && p.targetTime(now).After(now.AddDate(1, 0, 0)):
		return i18n.Text("Choose a time within one year.")
	}
	return ""
}

func (p scheduleUI) focusHint() string {
	switch p.focus {
	case 0:
		return i18n.Text("Type your message • Enter: next field")
	case 1:
		return i18n.Text("←/→: trigger • Enter: next • q: quit")
	case 2:
		if p.mode == 2 {
			return i18n.Text("Arrows: date • PgUp/Dn: month • q: quit")
		}
		fallthrough
	case 3, 4:
		return i18n.Text("Digits/arrows: time • Enter: next • q: quit")
	default:
		return i18n.Text("Enter: activate • q: quit")
	}
}

func (p scheduleUI) actionRow() int {
	if p.mode == 2 {
		return 22
	}
	return 13
}

func (p *scheduleUI) moveFocus(delta int) {
	fields := []int{0, 1, 5, 6}
	if p.mode == 1 {
		fields = []int{0, 1, 2, 3, 5, 6}
	} else if p.mode == 2 {
		fields = []int{0, 1, 2, 3, 4, 5, 6}
	}
	index := 0
	for i, f := range fields {
		if f == p.focus {
			index = i
			break
		}
	}
	p.focus = fields[(index+delta+len(fields))%len(fields)]
	p.numeric = ""
}

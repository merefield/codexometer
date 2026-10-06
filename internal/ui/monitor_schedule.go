package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
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
}
type scheduleDone struct{ err error }

// A scheduled follow-up replaces the invitation to reply, not live work or warnings.
func (m Model) sessionTriggerStatus(s monitorSession) bool {
	return s.attention == codex.SessionAttentionComplete && !s.working && m.hasSchedule(s.id)
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
	if !m.hasSchedule(id) && (o.Token == "" || o.ThreadID != id || len(o.Questions) > 0) {
		return nil
	}
	q := m.scheduleUI.queue
	if q == nil {
		q = schedule.New()
	}
	p := scheduleUI{queue: q, polling: m.scheduleUI.polling, open: true, session: id, input: newMonitorEditor(), hours: 1, date: time.Now().Add(time.Hour).Truncate(time.Minute)}
	if m.monitorPrompt.session == id {
		p.input.SetValue(m.monitorPrompt.input.Value())
	}
	for _, j := range q.List(id) {
		if j.Status == "sent" {
			continue
		}
		p.id = j.ID
		p.input.SetValue(j.Text)
		p.notice = "Status: " + j.Status
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
			m.scheduleUI.notice = "Send outcome uncertain or trigger changed; check Codex. No retry."
		}
		return m, nil, true
	}
	key, isKey := msg.(tea.KeyPressMsg)
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
		// The full-detail composer heading is a dedicated schedule click surface.
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && m.monitorContextDetail != "" {
			g := m.monitorDashboardLayout()
			rows := m.monitorPromptRows(g.contentWidth, g.meterHeight)
			_, _, y := monitorContextBodyLayout(g.meterHeight, rows)
			if rows > 0 && click.Y == g.meterY+y && click.X >= 4 && click.X < g.contentWidth {
				cmd := m.openSchedule()
				return m, cmd, true
			}
		}
		return m, nil, false
	}
	p := &m.scheduleUI
	if isKey && p.focus < 5 {
		p.notice = ""
	}
	p.input.configure(max(m.width-4, 20), 11)
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
		return m, nil, true
	}
	if isKey && (key.String() == "esc" || key.String() == "ctrl+s") {
		p.open = false
		p.input.Blur()
		return m, nil, true
	}
	if m.width < 48 || m.height < 24 {
		if _, mouse := msg.(tea.MouseMsg); isKey || mouse {
			return m, nil, true
		}
		return m, nil, false
	}
	if click, ok := msg.(tea.MouseClickMsg); ok {
		p.numeric = ""
		if click.Button != tea.MouseLeft {
			return m, nil, true
		}
		if click.X < 2 || click.X >= m.width-2 || click.Y < 1 || click.Y >= m.height-1 {
			return m, nil, true
		}
		switch {
		case click.Y >= 3 && click.Y <= 5:
			p.focus = 0
			return m, p.input.Focus(), true
		case click.Y == 7 && click.X >= 2 && click.X < 44:
			p.focus = 1
			if click.X < 25 {
				p.mode = 0
			} else if click.X < 35 {
				p.mode = 1
			} else {
				p.mode = 2
			}
		case click.Y == 9 && p.mode != 0:
			p.focus = 2
			if p.mode == 1 && click.X >= 22 {
				p.focus = 3
			}
		case click.Y == 18 && p.mode == 2:
			if click.X < 22 {
				p.focus = 3
			} else {
				p.focus = 4
			}
		case click.Y == p.actionRow() && click.X >= 2 && click.X < 33:
			if click.X < 2+ansi.StringWidth(p.confirmLabel()) {
				p.focus = 5
				isKey = true
				key = tea.KeyPressMsg{Code: tea.KeyEnter}
			} else if click.X >= 25 {
				p.focus = 6
				isKey = true
				key = tea.KeyPressMsg{Code: tea.KeyEnter}
			}
		case p.mode == 2 && click.Y >= 11 && click.Y <= 16 && click.X >= 2 && click.X < 30:
			first := time.Date(p.date.Year(), p.date.Month(), 1, 0, 0, 0, 0, p.date.Location())
			d := (click.Y-11)*7 + (click.X-2)/4 - int(first.Weekday()) + 1
			if d >= 1 && d <= time.Date(p.date.Year(), p.date.Month()+1, 0, 0, 0, 0, 0, p.date.Location()).Day() {
				p.date = time.Date(p.date.Year(), p.date.Month(), d, p.date.Hour(), p.date.Minute(), 0, 0, p.date.Location())
				p.focus = 2
			}
		}
		p.input.Blur()
		if !isKey {
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
			p.input.Blur()
			if p.focus == 0 {
				return m, p.input.Focus(), true
			}
			return m, nil, true
		case "enter":
			if p.focus == 5 {
				now := time.Now()
				if reason := p.validation(now); reason != "" {
					p.notice = reason
					return m, nil, true
				}
				o := m.monitorPromptOffer()
				if o.Token == "" || o.ThreadID != p.session || len(o.Questions) > 0 || m.err != nil || !schedule.Fresh(m.snapshot, time.Now()) {
					p.notice = "Session unavailable or changed; review in Codex."
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
				if err := p.queue.Save(j, m.snapshot.AccountFingerprint, time.Now()); err != nil {
					p.notice = err.Error()
				} else {
					p.open = false
					p.input.Reset()
					m.monitorPrompt.input.Reset()
				}
			} else if p.focus == 6 {
				p.open = false
			} else {
				p.moveFocus(1)
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

func (m Model) renderScheduleForm() string {
	p := m.scheduleUI
	now := time.Now()
	c := paletteFor(m.theme)
	if m.width < 48 || m.height < 24 {
		return "Scheduling needs 48 columns × 24 rows. Resize or Esc to return."
	}
	lines := make([]string, 24)
	name := "Unnamed session"
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
	prefix, suffix := "SCHEDULE FOLLOW-UP // ", " // "+shortSessionID(p.session)
	if p.id != "" {
		prefix = "EDIT SCHEDULED FOLLOW-UP // "
	}
	lines[0] = prefix + ansi.Truncate(name, max(m.width-6-ansi.StringWidth(prefix+suffix), 1), "…") + suffix
	lines[1] = "In memory only • closing Codexometer cancels this trigger"
	p.input.configure(m.width-4, 11)
	for i, s := range strings.Split(p.input.View(c), "\n") {
		if i < 3 {
			lines[3+i] = s
		}
	}
	control := func(label string, focus int) string {
		if p.focus == focus {
			return c.label().Foreground(c.primary).Reverse(true).Render(label)
		}
		return c.label().Render(label)
	}
	// Selection belongs to one option, never the whole focused control group.
	lines[7] = p.renderTriggerModes(c)
	lines[6] = c.dimmed().Render(p.focusHint())
	if p.mode == 1 {
		hours := fmt.Sprintf("Hours: %d", p.hours)
		lines[9] = control(hours, 2) + strings.Repeat(" ", max(20-ansi.StringWidth(hours), 1)) + control(fmt.Sprintf("Minutes: %02d", p.minutes), 3)
	}
	if p.mode == 2 {
		lines[9] = control(p.date.Format("January 2006")+" // PgUp/PgDn", 2)
		lines[10] = "Su  Mo  Tu  We  Th  Fr  Sa"
		first := time.Date(p.date.Year(), p.date.Month(), 1, 0, 0, 0, 0, p.date.Location())
		end := first.AddDate(0, 1, -1).Day()
		dayStyle := lipgloss.NewStyle().Foreground(c.primary).Background(c.background)
		for row := 0; row < 6; row++ {
			for col := 0; col < 7; col++ {
				d := row*7 + col - int(first.Weekday()) + 1
				s := dayStyle.Render("    ")
				if d >= 1 && d <= end {
					style := dayStyle
					if d == p.date.Day() {
						if p.focus == 2 {
							style = style.Foreground(c.background).Background(c.primary)
						} else {
							style = style.Underline(true)
						}
					}
					s = style.Render(fmt.Sprintf("%2d", d)) + dayStyle.Render("  ")
				}
				lines[11+row] += s
			}
		}
		lines[18] = control("Hour: "+p.date.Format("15"), 3) + "            " + control("Minute: "+p.date.Format("04"), 4)
	}
	row := p.actionRow()
	lines[row-2] = c.header().Render("Will send: " + p.targetTime(now).Format("02 Jan 2006 15:04 MST -07:00"))
	lines[row-1] = "Not before this time; when quota + idle allow"
	if p.mode == 0 {
		lines[row-2] = c.header().Render("Will send: when quota is available")
		lines[row-1] = "and this session is idle"
	}
	label := p.confirmLabel()
	confirm := control(label, 5)
	reason := p.validation(now)
	if reason != "" {
		confirm = c.dimmed().Render(label)
	}
	lines[row] = confirm + strings.Repeat(" ", 23-ansi.StringWidth(label)) + control("[ BACK ]", 6)
	notice := p.notice
	if reason != "" {
		notice = reason
	}
	if row == 22 {
		lines[19] = notice
	} else {
		lines[row+1] = notice
	}
	lines[2] = "Tab/Shift+Tab: fields • Esc: back • Ctrl+C: quit"
	for i := 1; i < 23; i++ {
		lines[i] = ansi.Truncate(lines[i], max(m.width-4, 1), "…")
	}
	// Frame content starts at column two. Rendering and hit testing share
	// actionRow so compact delay/quota forms do not reserve calendar space.
	return frameSized(m.width, m.height-2, lines[0], strings.Join(lines[1:23], "\n"), c.primary, c)
}

func (p scheduleUI) targetTime(now time.Time) time.Time {
	if p.mode == 1 {
		return now.Add(time.Duration(p.hours)*time.Hour + time.Duration(p.minutes)*time.Minute)
	}
	return p.date
}

func (p scheduleUI) confirmLabel() string {
	if p.id != "" {
		return "[ SAVE CHANGES ]"
	}
	return "[ CONFIRM SCHEDULE ]"
}

func (p scheduleUI) validation(now time.Time) string {
	text := strings.TrimSpace(p.input.Value())
	switch {
	case text == "":
		return "Enter a message to schedule."
	case len([]rune(text)) > 4096:
		return "Keep the message within 4096 characters."
	case codex.SanitizeSessionContext(text) != text:
		return "Remove terminal control characters."
	case p.mode != 0 && !p.targetTime(now).After(now):
		return "Choose a future time (delay must exceed 0)."
	case p.mode != 0 && p.targetTime(now).After(now.AddDate(1, 0, 0)):
		return "Choose a time within one year."
	}
	return ""
}

func (p scheduleUI) focusHint() string {
	switch p.focus {
	case 0:
		return "Type your message • Enter: next field"
	case 1:
		return "←/→: trigger • Enter: next • q: quit"
	case 2:
		if p.mode == 2 {
			return "Arrows: date • PgUp/Dn: month • q: quit"
		}
		fallthrough
	case 3, 4:
		return "Digits/arrows: time • Enter: next • q: quit"
	default:
		return "Enter: activate • q: quit"
	}
}

func (p scheduleUI) renderTriggerModes(c palette) string {
	mode := []string{"AFTER QUOTA REFRESH", "IN…", "AT…"}
	for i, s := range mode {
		label := fmt.Sprintf("%-9s", "[ "+s+" ]")
		style := c.label()
		if p.mode == i {
			style = style.Foreground(c.primary).Underline(true)
			if p.focus == 1 {
				style = style.Underline(false).Foreground(c.background).Background(c.primary)
			}
		}
		mode[i] = style.Render(label)
	}
	return strings.Join(mode, " ")
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

package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const monitorSelectionFlash = 2 * time.Second

// Capture what was actually visible, not the underlying Markdown or offscreen
// reply. Coordinates and a text check prevent a live redraw from copying a
// different selection (or replaying a click on a different line).
type monitorTextSelection struct {
	press             tea.MouseClickMsg
	rect              monitorRect
	id                string
	rows              []string
	first, last       int // first and last nonblank display rows, relative to the pane
	x, y              int
	dragged, released bool
}

type monitorSelectionExpired uint64

func (m Model) updateMonitorSelection(msg tea.Msg) (Model, tea.Msg, tea.Cmd) {
	s := m.monitorSelection
	switch event := msg.(type) {
	case monitorSelectionExpired:
		if uint64(event) == m.monitorSelectionSequence {
			m.monitorSelectionCopied = 0
			if s != nil && s.released {
				m.monitorSelection = nil
			}
		}
		return m, nil, nil
	case tea.MouseClickMsg:
		m.monitorSelection = nil
		if event.Button != tea.MouseLeft {
			break
		}
		id, rect := m.monitorSelectionPane(event.X, event.Y)
		if id == "" {
			break
		}
		if hit := m.monitorContextAt(event.X, event.Y); hit != "" &&
			!strings.HasPrefix(hit, "less:") && !strings.HasPrefix(hit, "more:") {
			break // Explicit controls retain their normal press handling.
		}
		rows := monitorSelectionRows(m.render(), rect)
		if event.X-rect.x >= ansi.StringWidth(rows[event.Y-rect.y]) {
			break // Empty cells retain their ordinary click behaviour.
		}
		first, last := event.Y-rect.y, event.Y-rect.y
		for i, line := range rows {
			if line != "" {
				first, last = min(first, i), max(last, i)
			}
		}
		m.monitorSelection = &monitorTextSelection{
			press: event, rect: rect, id: id, rows: rows, first: first, last: last, x: event.X, y: event.Y,
		}
		return m, nil, nil
	case tea.MouseMotionMsg:
		if s == nil || s.released {
			break
		}
		if event.Button == tea.MouseNone {
			m.monitorSelection = nil // The terminal lost the release.
			break
		}
		if event.Button == tea.MouseLeft {
			next := s.move(event.X, event.Y)
			m.monitorSelection = &next
			return m, nil, nil
		}
	case tea.MouseReleaseMsg:
		if s == nil || s.released || (event.Button != tea.MouseLeft && event.Button != tea.MouseNone) {
			break
		}
		m.monitorSelection = nil
		next := s.move(event.X, event.Y)
		id, rect := m.monitorSelectionPane(s.press.X, s.press.Y)
		if id != s.id || rect != s.rect {
			return m, nil, nil
		}
		rows := monitorSelectionRows(m.render(), rect)
		if !next.dragged {
			row := s.press.Y - rect.y
			if rows[row] != s.rows[row] {
				return m, nil, nil
			}
			return m, s.press, nil
		}
		selected, count := next.text(s.rows)
		if count == 0 || !next.matches(rows, selected) {
			return m, nil, nil
		}
		next.released = true
		m.monitorSelection = &next
		m.monitorSelectionCopied = count
		m.monitorSelectionSequence++
		sequence := monitorSelectionExpired(m.monitorSelectionSequence)
		return m, nil, tea.Batch(tea.SetClipboard(selected), tea.Tick(monitorSelectionFlash, func(time.Time) tea.Msg {
			return sequence
		}))
	case tea.KeyPressMsg, tea.PasteMsg, tea.MouseWheelMsg, tea.WindowSizeMsg, tea.BlurMsg:
		m.monitorSelection = nil
	}
	return m, msg, nil
}

func (m Model) monitorSelectionPane(x, y int) (string, monitorRect) {
	if m.meterView != viewMonitor || m.width <= 0 || m.height <= 0 || m.startup.active() ||
		m.loading && len(m.snapshot.Meters()) == 0 || m.finder.include || m.newSession.open() ||
		m.inspector.root != "" || m.actionReview != nil || m.hasThreadLinkPanel() ||
		m.scheduleUI.open || m.monitorQueue.open || m.monitorQueue.compact != "" ||
		m.monitorCommands.open || m.suggestionsActive() {
		return "", monitorRect{}
	}
	if id := m.monitorContextDetail; id != "" {
		if r, ok := m.monitorResultRect(id); ok && r.contains(x, y) {
			return id, r
		}
		return "", monitorRect{}
	}
	g := m.monitorDashboardLayout()
	for _, pane := range m.monitorPartialContextPanes() {
		if pane.frame.contains(x, y-g.meterY) {
			r := m.monitorPartialResultRect(pane, g.meterY)
			if r.contains(x, y) {
				return pane.session.id, r
			}
		}
	}
	return "", monitorRect{}
}

func monitorSelectionRows(content string, r monitorRect) []string {
	lines := strings.SplitN(content, "\n", r.y+r.height+1)
	rows := make([]string, r.height)
	for i := range min(r.height, len(lines)-r.y) {
		line := ansi.Cut(lines[r.y+i], r.x, r.x+r.width)
		rows[i] = strings.TrimRight(ansi.Strip(line), " ")
	}
	return rows
}

func (s monitorTextSelection) move(x, y int) monitorTextSelection {
	s.dragged = s.dragged || x != s.press.X || y != s.press.Y
	top, bottom := s.rect.y+s.first, s.rect.y+s.last
	if y < top {
		x = s.rect.x
	} else if y > bottom {
		x = s.rect.x + s.rect.width - 1
	}
	s.x = min(max(x, s.rect.x), s.rect.x+s.rect.width-1)
	s.y = min(max(y, top), bottom)
	return s
}

func (s monitorTextSelection) bounds() (x1, y1, x2, y2 int) {
	x1, y1, x2, y2 = s.press.X, s.press.Y, s.x, s.y
	if y2 < y1 || y2 == y1 && x2 < x1 {
		return x2, y2, x1, y1
	}
	return
}

func (s monitorTextSelection) text(rows []string) (string, int) {
	x1, y1, x2, y2 := s.bounds()
	var out strings.Builder
	count := 0
	for y := y1; y <= y2; y++ {
		if y != y1 {
			out.WriteByte('\n')
		}
		x := s.rect.x
		walkTerminalText(rows[y-s.rect.y], func(seq string, width int, _ *ansi.Parser) {
			if selectedTerminalCell(x, y, width, x1, y1, x2, y2) {
				out.WriteString(seq)
				count++
			}
			x += width
		})
	}
	return out.String(), count
}

func (s monitorTextSelection) matches(rows []string, text string) bool {
	current, _ := s.text(rows)
	return current == text
}

func selectedTerminalCell(x, y, width, x1, y1, x2, y2 int) bool {
	return width > 0 && (y != y1 || x+width > x1) && (y != y2 || x <= x2)
}

func (m Model) highlightMonitorSelection(content string) string {
	s := m.monitorSelection
	if s == nil || !s.dragged {
		return content
	}
	id, r := m.monitorSelectionPane(s.press.X, s.press.Y)
	if id != s.id || r != s.rect {
		return content
	}
	rows := monitorSelectionRows(content, r)
	if selected, count := s.text(rows); count == 0 || !s.matches(s.rows, selected) {
		return content
	}
	lines := strings.Split(content, "\n")
	x1, y1, x2, y2 := s.bounds()
	for y := y1; y <= y2 && y < len(lines); y++ {
		right := r.x + ansi.StringWidth(rows[y-r.y])
		var line strings.Builder
		x := 0
		walkTerminalText(lines[y], func(seq string, width int, _ *ansi.Parser) {
			if x >= r.x && x < right && selectedTerminalCell(x, y, width, x1, y1, x2, y2) {
				line.WriteString("\x1b[7m")
				line.WriteString(seq)
				line.WriteString("\x1b[27m")
			} else {
				line.WriteString(seq)
			}
			x += width
		})
		lines[y] = line.String()
	}
	return strings.Join(lines, "\n")
}

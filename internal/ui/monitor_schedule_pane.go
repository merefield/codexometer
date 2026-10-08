package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Canonical form rows are shared by rendering, focus scrolling and mouse hits.
// The editor is outside this viewport, anchored at the existing composer edge.
type schedulePaneLayout struct {
	indices                                   []int
	textRows, scroll, composerY, composerRows int
}

func (m Model) schedulePaneLayout(width, height int) schedulePaneLayout {
	p := m.scheduleUI
	l := schedulePaneLayout{indices: []int{0, 1, 2, 7, 8}}
	for row := 9; row <= min(p.actionRow()+1, 23); row++ {
		l.indices = append(l.indices, row)
	}
	l.composerRows = min(max(p.composerRows, 2), max(height-5, 1))
	l.textRows, _, l.composerY = monitorContextBodyLayout(height, l.composerRows)
	l.scroll = min(max(p.scroll, 0), max(len(l.indices)-l.textRows, 0))
	return l
}

func (m *Model) revealScheduleFocus() {
	g := m.monitorDashboardLayout()
	l := m.schedulePaneLayout(g.contentWidth, g.meterHeight)
	p := &m.scheduleUI
	target := 7
	switch p.focus {
	case 0:
		return
	case 2:
		target = 9
		if p.mode == 2 {
			// Month starts can offset the selected day into the following row.
			firstWeekday := (int(p.date.Weekday()) - (p.date.Day()-1)%7 + 7) % 7
			target = 11 + (firstWeekday+p.date.Day()-1)/7
		}
	case 3:
		target = 9
		if p.mode == 2 {
			target = 18
		}
	case 4:
		target = 18
	case 5, 6:
		target = p.actionRow()
	}
	for index, row := range l.indices {
		if row != target {
			continue
		}
		p.scroll = l.scroll
		if index < l.scroll {
			p.scroll = index
		}
		if index >= l.scroll+l.textRows {
			p.scroll = max(0, index-l.textRows+1)
		}
		break
	}
}

func (m *Model) closeSchedule() {
	p := &m.scheduleUI
	// Cancelling edits to an existing trigger must not overwrite an unrelated
	// reply draft. A new schedule uses the same ordinary follow-up draft.
	if p.id == "" {
		if !m.monitorPrompt.input.ready {
			m.monitorPrompt.input = newMonitorEditor()
		}
		m.monitorPrompt.session = p.session
		m.monitorPrompt.offer = m.monitorPromptOffer()
		m.monitorPrompt.input.SetValue(p.input.Value())
		m.monitorPrompt.input.Blur()
	}
	p.open = false
	p.toggleHover = false
	p.input.Blur()
}

func (m Model) renderScheduleDetail(width, height int, colors palette) string {
	l := m.schedulePaneLayout(width, height)
	m.width = width
	document := m.scheduleFormLines()
	document[0] = colors.header().Render(ansi.Truncate(document[0], max(width-4, 1), "…"))
	body := make([]string, max(height-2, 1))
	for row := 0; row < l.textRows && row+l.scroll < len(l.indices); row++ {
		body[row] = document[l.indices[row+l.scroll]]
	}
	editor := m.scheduleUI.input
	editor.configure(width, l.composerRows+7)
	composer := make([]string, l.composerRows)
	input := strings.Split(editor.View(colors), "\n")
	inputEnd := l.composerRows
	if l.composerRows >= 2 {
		inputEnd--
	}
	for row := 0; row < inputEnd; row++ {
		if row < len(input) {
			composer[row] = input[row]
		} else {
			composer[row] = colors.label().Background(monitorComposerBackground(colors)).Width(max(width-4, 1)).Render("")
		}
	}
	hint := m.scheduleUI.focusHint()
	hintStyle := colors.dimmed()
	if m.scheduleUI.notice != "" {
		hint = m.scheduleUI.notice
		hintStyle = colors.label()
	}
	if l.composerRows >= 2 {
		composer[l.composerRows-1] = m.renderScheduleToggleHint(width, hint, true, hintStyle, colors)
	} else if l.composerRows == 1 && len(input) > 0 {
		composer[0] = input[0]
	}
	for row, line := range composer {
		index := l.composerY - 1 + row
		if index < len(body) {
			body[index] = line
		}
	}
	return frameSized(width, max(height-2, 1), m.monitorDetailTitle(width, colors), strings.Join(body, "\n"), colors.primary, colors)
}

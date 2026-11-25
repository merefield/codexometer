package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
	"github.com/merefield/codexometer/internal/prose"
)

const (
	threadLinkMarker      = "about:blank#"
	threadLinkMarkerParam = "id=codexometer-thread-preview"
)

type threadLinkChoice struct{ id, label string }

type threadLinkPanel struct {
	parent        *threadLinkPanel
	scope, id     string
	choices       []threadLinkChoice
	index, scroll int
	request       uint64
	cancel        context.CancelFunc
	loading       bool
	preview       codex.SessionLinkPreview
}

type threadLinkResult struct {
	request uint64
	preview codex.SessionLinkPreview
	err     error
}

func (m Model) hasThreadLinkPanel() bool {
	return m.threadLinks != nil && m.threadLinks.scope == m.finder.scope && m.meterView == viewMonitor
}

func (m Model) threadLinkSource() bool {
	if m.meterView != viewMonitor || m.actionReview != nil || m.scheduleUI.open || m.monitorCommands.open {
		return false
	}
	check := func(text string) bool {
		return strings.Contains(text, "codex://threads/") && prose.HasThreadLinks(m.proseCache.Get(codex.SanitizeSessionContext(text)))
	}
	turns := func(turns []codex.SavedTurn) bool {
		for _, turn := range turns {
			if !turn.Restricted && check(turn.Reply) {
				return true
			}
		}
		return false
	}
	if m.hasThreadLinkPanel() {
		return turns(m.threadLinks.preview.Turns)
	}
	if m.inspector.root != "" {
		return turns(m.inspector.page.Turns)
	}
	if m.finder.include && m.finder.detail != "" {
		return turns(m.finder.turns)
	}
	if c, ok := m.historicalContext(); ok && check(c.AssistantProse()) {
		return true
	}
	for _, session := range m.monitorSessionData {
		if (m.monitorContextDetail == "" || m.monitorContextDetail == session.id) && m.monitorSessionVisible(session) && check(session.preview.AssistantProse()) {
			return true
		}
	}
	return false
}

// Marker hyperlinks exist only in a private geometry render. Normal output
// contains underlines but never OSC hyperlinks for internal thread links.
func (m Model) threadLinkTargets(x, y int) (hit string, choices []threadLinkChoice) {
	if !m.threadLinkSource() {
		return "", nil
	}
	m.threadLinkHitTest = true
	id, row, col := "", 0, 0
	labels := make(map[string]*strings.Builder)
	var order []string
	walkTerminalText(m.View().Content, func(seq string, width int, p *ansi.Parser) {
		switch {
		case ansi.HasOscPrefix(seq) && p.Command() == 8:
			id = ""
			parts := strings.SplitN(string(p.Data()), ";", 3)
			if len(parts) == 3 && parts[1] == threadLinkMarkerParam {
				if raw, ok := strings.CutPrefix(parts[2], threadLinkMarker); ok {
					id = prose.ThreadID("codex://threads/" + raw)
				}
			}
		case seq == "\n":
			row, col = row+1, 0
		case width > 0:
			if id != "" {
				if y == row && x >= col && x < col+width {
					hit = id
				}
				if labels[id] == nil && len(order) < 64 {
					labels[id] = &strings.Builder{}
					order = append(order, id)
				}
				if b := labels[id]; b != nil && b.Len() < 160 {
					b.WriteString(seq)
				}
			}
			col += width
		}
	})
	for _, id := range order {
		choices = append(choices, threadLinkChoice{id, terminalLabel(labels[id].String())})
	}
	return hit, choices
}

func (m *Model) previewThreadLink(id string) tea.Cmd {
	parent := m.threadLinks
	if parent != nil && len(parent.choices) != 0 {
		parent = parent.parent
	}
	for p, depth := parent, 0; p != nil; p, depth = p.parent, depth+1 {
		if p.id == id || depth >= 7 {
			return nil
		}
	}
	m.threadLinkRequest++
	s := &threadLinkPanel{parent: parent, scope: m.finder.scope, id: id, request: m.threadLinkRequest}
	m.threadLinks = s
	client, ok := m.fetcher.(codex.SessionLinkClient)
	if !ok || s.scope == "" {
		return nil // Keep the same unavailable screen regardless of failure reason.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	s.loading, s.cancel = true, cancel
	request, scope := s.request, s.scope
	return func() tea.Msg {
		defer cancel()
		preview, err := client.PreviewSessionLink(ctx, scope, id)
		return threadLinkResult{request, preview, err}
	}
}

func (m Model) threadLinkAction() string {
	p := m.threadLinks.preview
	if p.Root.ID == "" || p.Agent != nil {
		return ""
	}
	for _, session := range m.monitorSessionData {
		if session.id == p.Root.ID && m.monitorSessionVisible(session) {
			return "[O: " + i18n.Text("OPEN SESSION") + "]"
		}
	}
	if _, ok := m.fetcher.(codex.SavedSessionResumeClient); ok {
		return "[O: " + i18n.Text("RESUME SAVED SESSION") + "]"
	}
	return ""
}

func (m *Model) openThreadLinkTarget() tea.Cmd {
	if m.threadLinkAction() == "" {
		return nil
	}
	p := m.threadLinks.preview
	m.threadLinks = nil
	for _, session := range m.monitorSessionData {
		if session.id == p.Root.ID && m.monitorSessionVisible(session) {
			m.finder.clearHistory()
			m.inspector = sessionInspector{request: m.inspector.request + 1}
			m.setRowContext(session.id, contextFull)
			return nil
		}
	}
	m.inspector = sessionInspector{request: m.inspector.request + 1}
	_ = m.beginResumePicker()
	m.finder.editInput().Blur()
	f := &m.finder
	f.focus, f.detail, f.selected = findResults, p.Root.ID, p.Root.ID
	f.rows, f.turns = []savedRow{{SavedSession: p.Root}}, p.Turns
	return m.resumeSaved()
}

func (m Model) threadLinkLines(width int) (title string, lines []string) {
	s := m.threadLinks
	title = i18n.Text("SESSION PREVIEW")
	if len(s.choices) != 0 {
		for i, choice := range s.choices {
			prefix := "  "
			if i == s.index {
				prefix = "› "
			}
			lines = append(lines, prefix+choice.label+" // "+shortSessionID(choice.id))
		}
		return i18n.Text("LINKS"), lines
	}
	p := s.preview
	if s.loading {
		return title, inspectionStatus(false, true)
	}
	if p.Root.ID == "" {
		return title, []string{i18n.Text("Session unavailable here.")}
	}
	name, id := p.Root.Name, p.Root.ID
	if p.Agent != nil {
		title, name, id = i18n.Text("AGENT TRANSCRIPT"), p.Agent.Name, p.Agent.ID
	}
	title += " // " + shortSessionID(id)
	if name != "" {
		title += " // " + terminalLabel(name)
	}
	if len(p.Turns) == 0 && p.UnverifiedTurns {
		return title, wrapProse(i18n.Text("Could not verify which completed turns belong to this agent."), width)
	}
	return title, m.savedTranscriptLines(p.Turns, width)
}

func threadLinkOffset(p *threadLinkPanel, total, rows int) int {
	if len(p.choices) != 0 {
		return finderStart(p.index, rows)
	}
	return min(p.scroll, max(0, total-rows))
}

func (m Model) renderThreadLinks(width, height int, colors palette) string {
	rows, inner := max(height-2, 1), max(width-4, 1)
	title, lines := m.threadLinkLines(inner)
	start := threadLinkOffset(m.threadLinks, len(lines), rows)
	action := m.threadLinkAction()
	if action != "" && ansi.StringWidth(action)+14 <= width {
		action += " "
	} else {
		action = ""
	}
	return renderInspectionFrame(width, rows, start, title, action+"[ESC]", lines, colors)
}

func (m Model) updateThreadLinks(msg tea.Msg) (Model, tea.Cmd, bool) {
	if m.threadLinks != nil && !m.hasThreadLinkPanel() {
		if m.threadLinks.cancel != nil {
			m.threadLinks.cancel()
		}
		m.threadLinks = nil
	}
	if result, ok := msg.(threadLinkResult); ok {
		if s := m.threadLinks; s != nil && s.request == result.request {
			next := *s
			next.loading, next.cancel = false, nil
			if result.err == nil {
				next.preview = result.preview
			}
			m.threadLinks = &next
		}
		return m, nil, true
	}
	if m.meterView != viewMonitor || m.scheduleUI.open || m.monitorCommands.open {
		return m, nil, false
	}
	e, key := msg.(tea.KeyPressMsg)
	c, click := msg.(tea.MouseClickMsg)
	if click && c.Button == tea.MouseLeft {
		if id, _ := m.threadLinkTargets(c.X, c.Y); id != "" {
			cmd := m.previewThreadLink(id)
			return m, cmd, true
		}
	}
	if key && e.String() == "l" && (m.hasThreadLinkPanel() || !m.monitorPrompt.input.Focused() && m.finder.focus != findText) {
		if _, choices := m.threadLinkTargets(-1, -1); len(choices) != 0 {
			m.threadLinks = &threadLinkPanel{parent: m.threadLinks, scope: m.finder.scope, choices: choices}
			return m, nil, true
		}
	}
	if !m.hasThreadLinkPanel() {
		return m, nil, false
	}
	s := *m.threadLinks
	m.threadLinks = &s
	g := m.dashboardLayout()
	rows := max(g.meterHeight-2, 1)
	back := func() {
		if s.cancel != nil {
			s.cancel()
		}
		m.threadLinks = s.parent
	}
	move := func(delta int) {
		if len(s.choices) != 0 {
			s.index = min(max(s.index+delta, 0), len(s.choices)-1)
		} else {
			_, lines := m.threadLinkLines(max(g.contentWidth-4, 1))
			s.scroll = min(max(s.scroll+delta, 0), max(0, len(lines)-rows))
		}
	}
	var cmd tea.Cmd
	switch v := msg.(type) {
	case tea.KeyPressMsg:
		switch v.String() {
		case "esc", "left":
			back()
		case "down":
			move(1)
		case "up":
			move(-1)
		case "pgdown":
			move(rows)
		case "pgup":
			move(-rows)
		case "enter", "right":
			if len(s.choices) != 0 {
				cmd = m.previewThreadLink(s.choices[s.index].id)
			}
		case "o":
			if !v.IsRepeat {
				cmd = m.openThreadLinkTarget()
			}
		case "q", "ctrl+c":
			back()
			m.threadLinks = nil
		}
	case tea.MouseClickMsg:
		x, y := v.X-2, v.Y-g.meterY
		if v.Button == tea.MouseLeft && x >= 0 && x < g.contentWidth {
			actionWidth := ansi.StringWidth(m.threadLinkAction())
			if y == 0 && g.contentWidth >= 13 && x >= g.contentWidth-7 && x < g.contentWidth-2 {
				back()
			} else if y == 0 && actionWidth > 0 && g.contentWidth >= actionWidth+14 && x >= g.contentWidth-8-actionWidth && x < g.contentWidth-8 {
				cmd = m.openThreadLinkTarget()
			} else if y >= 1 && y <= rows && len(s.choices) != 0 {
				i := finderStart(s.index, rows) + y - 1
				if i < len(s.choices) {
					cmd = m.previewThreadLink(s.choices[i].id)
				}
			}
		}
	case tea.MouseWheelMsg:
		if v.Button == tea.MouseWheelUp {
			move(-3)
		} else {
			move(3)
		}
	default:
		return m, nil, false
	}
	return m, cmd, true
}

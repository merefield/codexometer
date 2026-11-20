package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type sessionInspector struct {
	root, child     string
	index, scroll   int
	request         uint64
	loading, failed bool
	page            codex.AgentInspection
}

type inspectorResult struct {
	request     uint64
	root, child string
	page        codex.AgentInspection
	err         error
}

func (m *Model) requestAgentInspection(root, child string) tea.Cmd {
	reader, ok := m.fetcher.(codex.SessionAgentClient)
	if !ok || root == "" {
		return nil
	}
	m.stashMonitorDraft()
	m.monitorPrompt.input.Blur()
	s := &m.inspector
	if s.root != root {
		*s = sessionInspector{request: s.request}
	}
	s.root, s.child, s.scroll = root, child, 0
	s.request++
	s.loading, s.failed = true, false
	result := inspectorResult{request: s.request, root: root, child: child}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result.page, result.err = reader.InspectSessionAgents(ctx, root, child)
		return result
	}
}

func (m Model) sessionAgentTargetAt(x, y int) string {
	if m.monitorContextDetail != "" || m.monitorQueue.compact != "" || m.monitorError != "" || m.scheduleUI.open || m.monitorCommands.open {
		return ""
	}
	g := m.monitorDashboardLayout()
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	w, _, ok := monitorSessionColumnWidths(a.width)
	if !ok || x < 4 || x >= 2+w {
		return ""
	}
	rows, heights, page := m.monitorSessionPage(a.graphHeight)
	rowY := g.meterY + a.topHeight + a.gap - 1
	for i, s := range rows {
		h := heights[i]
		if y > rowY && y < rowY+h-1 && s.agentCount > 0 {
			var rect monitorRect
			label := ""
			if i == len(rows)-1 {
				label = page
			}
			m.renderSessionMetrics(w, h, s, label, paletteFor(m.theme), &rect)
			if rect.contains(x-2, y-rowY) {
				return s.id
			}
			break
		}
		rowY += h
	}
	return ""
}

func (m Model) inspectorLines(width int) []string {
	s := m.inspector
	if s.failed {
		return []string{i18n.Text("UNAVAILABLE")}
	}
	if s.loading {
		return []string{"…"}
	}
	var lines []string
	if s.child == "" {
		for i, row := range s.page.Agents {
			name := row.Name
			if name == "" {
				name = shortSessionID(row.ID)
			}
			prefix := "  "
			if i == s.index {
				prefix = "› "
			}
			lines = append(lines, prefix+name+" // "+i18n.Text(row.Status))
		}
		if len(lines) == 0 {
			lines = append(lines, "No linked agents observed.")
		}
		return lines
	}
	return savedTranscriptLines(s.page.Turns, width)
}

func (m Model) inspectorOffset(total, rows int) int {
	s := m.inspector
	if s.child == "" {
		return max(0, s.index-rows+1)
	}
	return min(s.scroll, max(0, total-rows))
}

func (m Model) renderSessionInspector(width, height int, colors palette) string {
	s := m.inspector
	title := "LINKED AGENTS // " + shortSessionID(s.root)
	if s.child != "" {
		title = "AGENT TRANSCRIPT // " + shortSessionID(s.child)
	}
	rows := max(height-2, 1)
	inner := max(width-4, 1)
	lines := m.inspectorLines(inner)
	start := m.inspectorOffset(len(lines), rows)
	lines = lines[start:min(start+rows, len(lines))]
	for i := range lines {
		lines[i] = colors.label().Render(ansi.Truncate(lines[i], inner, "…"))
	}
	return frameSizedWithTitleAction(width, rows, title, "[ESC]", strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) updateSessionInspector(msg tea.Msg) (Model, tea.Cmd, bool) {
	s := &m.inspector
	if result, ok := msg.(inspectorResult); ok {
		if result.request == s.request && result.root == s.root && result.child == s.child {
			s.loading, s.failed, s.page = false, result.err != nil, result.page
			s.index = min(s.index, max(0, len(s.page.Agents)-1))
		}
		return m, nil, true
	}
	if m.meterView != viewMonitor {
		return m, nil, false
	}
	if s.root == "" {
		if e, ok := msg.(tea.MouseClickMsg); ok && e.Button == tea.MouseLeft {
			if root := m.sessionAgentTargetAt(e.X, e.Y); root != "" {
				cmd := m.requestAgentInspection(root, "")
				return m, cmd, cmd != nil
			}
		}
		return m, nil, false
	}
	g := m.monitorDashboardLayout()
	rows := max(g.meterHeight-2, 1)
	move := func(delta int) {
		if s.child == "" {
			s.index = min(max(s.index+delta, 0), max(0, len(s.page.Agents)-1))
		} else {
			s.scroll = min(max(s.scroll+delta, 0), max(0, len(m.inspectorLines(max(g.contentWidth-4, 1)))-rows))
		}
	}
	back := func() tea.Cmd {
		if s.child != "" {
			return m.requestAgentInspection(s.root, "")
		}
		*s = sessionInspector{request: s.request + 1}
		return nil
	}
	open := func() tea.Cmd {
		if s.child == "" && !s.loading && s.index < len(s.page.Agents) {
			return m.requestAgentInspection(s.root, s.page.Agents[s.index].ID)
		}
		return nil
	}
	var cmd tea.Cmd
	switch e := msg.(type) {
	case tea.KeyPressMsg:
		switch e.String() {
		case "esc", "left":
			cmd = back()
		case "up":
			move(-1)
		case "down":
			move(1)
		case "pgup":
			move(-rows)
		case "pgdown":
			move(rows)
		case "enter", "right":
			cmd = open()
		case "q", "ctrl+c", "tab", "shift+tab":
			*s = sessionInspector{request: s.request + 1}
			return m, nil, false
		}
	case tea.MouseClickMsg:
		x, y := e.X-2, e.Y-g.meterY
		if e.Button != tea.MouseLeft || x < 0 || x >= g.contentWidth || y < 0 || y >= g.meterHeight {
			return m, nil, false
		}
		if y == 0 && x >= g.contentWidth-8 {
			cmd = back()
		} else if y >= 1 && y <= rows && s.child == "" && !s.loading && !s.failed {
			i := m.inspectorOffset(len(s.page.Agents), rows) + y - 1
			if i < len(s.page.Agents) {
				s.index = i
				cmd = open()
			}
		}
	case tea.MouseWheelMsg:
		if e.Button == tea.MouseWheelUp {
			move(-3)
		} else {
			move(3)
		}
	default:
		return m, nil, false
	}
	return m, cmd, true
}

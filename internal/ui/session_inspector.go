package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type sessionInspector struct {
	kind, root, child string
	index, scroll     int
	request           uint64
	loading, failed   bool
	page              codex.AgentInspection
	terminals         codex.TerminalInspection
}

type inspectorResult struct {
	request           uint64
	kind, root, child string
	page              codex.AgentInspection
	terminals         codex.TerminalInspection
	err               error
}

func (m *Model) requestSessionInspection(kind, root, child string) tea.Cmd {
	agents, _ := m.fetcher.(codex.SessionAgentClient)
	terminals, _ := m.fetcher.(codex.SessionTerminalClient)
	if root == "" || kind == "ps" && terminals == nil || kind == "subagents" && agents == nil {
		return nil
	}
	m.stashMonitorDraft()
	m.monitorPrompt.input.Blur()
	s := &m.inspector
	if s.root != root || s.kind != kind {
		*s = sessionInspector{request: s.request}
	}
	s.kind, s.root, s.child, s.scroll = kind, root, child, 0
	s.request++
	s.loading, s.failed = true, false
	result := inspectorResult{request: s.request, kind: kind, root: root, child: child}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if kind == "ps" {
			result.terminals, result.err = terminals.InspectSessionTerminals(ctx, root)
		} else {
			result.page, result.err = agents.InspectSessionAgents(ctx, root, child)
		}
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
	if status := inspectionStatus(s.failed, s.loading); status != nil {
		return status
	}
	var lines []string
	if s.kind == "ps" {
		for _, process := range s.terminals.Terminals {
			label := process.ID
			if process.PID != 0 {
				label += fmt.Sprintf(" // PID %d", process.PID)
			}
			if process.Directory != "" {
				label += " // " + process.Directory
			}
			lines = append(lines, label)
			lines = append(lines, wrapProse(process.Command, width)...)
			lines = append(lines, "")
		}
		if len(lines) == 0 {
			lines = append(lines, "No background terminals reported.")
		}
		if s.terminals.More {
			lines = append(lines, "Additional background terminals are not shown.")
		}
		return lines
	}
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
	if len(s.page.Turns) == 0 && s.page.UnverifiedTurns {
		return wrapProse(i18n.Text("Could not verify which completed turns belong to this agent."), width)
	}
	return m.savedTranscriptLines(s.page.Turns, width)
}

func (m Model) inspectorOffset(total, rows int) int {
	s := m.inspector
	if s.kind == "subagents" && s.child == "" {
		return max(0, s.index-rows+1)
	}
	return min(s.scroll, max(0, total-rows))
}

func (m Model) renderSessionInspector(width, height int, colors palette) string {
	s := m.inspector
	title := "LINKED AGENTS // " + shortSessionID(s.root)
	action := "[ESC]"
	if s.kind == "ps" {
		title = "BACKGROUND TERMINALS // " + shortSessionID(s.root)
		action = "[R] [ESC]"
	}
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
	return frameSizedWithTitleAction(width, rows, title, action, strings.Join(lines, "\n"), colors.primary, colors)
}

func (m Model) updateSessionInspector(msg tea.Msg) (Model, tea.Cmd, bool) {
	s := &m.inspector
	if result, ok := msg.(inspectorResult); ok {
		s.accept(result)
		return m, nil, true
	}
	if m.meterView != viewMonitor {
		return m, nil, false
	}
	if s.root == "" {
		if e, ok := msg.(tea.MouseClickMsg); ok && e.Button == tea.MouseLeft {
			if root := m.sessionAgentTargetAt(e.X, e.Y); root != "" {
				cmd := m.requestSessionInspection("subagents", root, "")
				return m, cmd, cmd != nil
			}
		}
		return m, nil, false
	}
	g := m.dashboardLayout()
	rows := max(g.meterHeight-2, 1)
	move := func(delta int) {
		if s.kind == "subagents" && s.child == "" {
			s.index = min(max(s.index+delta, 0), max(0, len(s.page.Agents)-1))
		} else {
			s.scroll = min(max(s.scroll+delta, 0), max(0, len(m.inspectorLines(max(g.contentWidth-4, 1)))-rows))
		}
	}
	back := func() tea.Cmd {
		if s.child != "" {
			return m.requestSessionInspection(s.kind, s.root, "")
		}
		*s = sessionInspector{request: s.request + 1}
		return nil
	}
	open := func() tea.Cmd {
		if s.kind == "subagents" && s.child == "" && !s.loading && s.index < len(s.page.Agents) {
			return m.requestSessionInspection(s.kind, s.root, s.page.Agents[s.index].ID)
		}
		return nil
	}
	var cmd tea.Cmd
	switch e := msg.(type) {
	case tea.KeyPressMsg:
		switch e.String() {
		case "r":
			if s.kind == "ps" && !s.loading {
				cmd = m.requestSessionInspection(s.kind, s.root, "")
			}
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
		if y == 0 && g.contentWidth >= 13 && x >= g.contentWidth-7 && x < g.contentWidth-2 {
			cmd = back()
		} else if y == 0 && g.contentWidth >= 17 && x >= g.contentWidth-11 && x < g.contentWidth-8 && s.kind == "ps" && !s.loading {
			cmd = m.requestSessionInspection(s.kind, s.root, "")
		} else if y >= 1 && y <= rows && s.kind == "subagents" && s.child == "" && !s.loading && !s.failed {
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

func (s *sessionInspector) accept(result inspectorResult) {
	if result.request != s.request || result.kind != s.kind || result.root != s.root || result.child != s.child {
		return
	}
	s.loading, s.failed, s.page, s.terminals = false, result.err != nil, result.page, result.terminals
	s.index = min(s.index, max(0, len(s.page.Agents)-1))
}

package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

// Copy the complete observed reply, not its wrapped/truncated presentation.
func (m Model) monitorCopyText(id string) string {
	for _, s := range m.monitorSessionData {
		if s.id == id && m.monitorSessionVisible(s) &&
			(s.preview.Kind == codex.SessionContextReply || s.preview.Kind == codex.SessionContextActivity) {
			return codex.SanitizeSessionContext(s.preview.Text)
		}
	}
	return ""
}

func (m Model) renderMonitorCopy(width int, id string, colors palette) string {
	if _, ok := contextActionRect(width, 0, benchmarkDetailCopyLabel); !ok || m.monitorCopyText(id) == "" {
		return ""
	}
	style := colors.label().Foreground(colors.primary)
	if m.monitorContextHover == "copy:"+id || m.monitorCopyFlash == id {
		style = style.Foreground(colors.background).Background(colors.primary)
	}
	return style.Render(benchmarkDetailCopyLabel)
}

type monitorCopyFlashExpiredMsg struct{ sequence uint64 }

func (m Model) activateMonitorCopy(id string) (Model, tea.Cmd) {
	cmd := m.copyMonitorReply(id)
	if cmd == nil {
		return m, nil
	}
	m.monitorCopyFlash = id
	m.monitorCopySequence++
	sequence := m.monitorCopySequence
	return m, tea.Batch(cmd, tea.Tick(footerButtonFlashDuration, func(time.Time) tea.Msg {
		return monitorCopyFlashExpiredMsg{sequence: sequence}
	}))
}

// Coordinates are relative to the dashboard content, before any row hit areas.
func (m Model) monitorCopyAt(x, y int) string {
	g := m.monitorDashboardLayout()
	hit := func(id string, bx, by, width, height int) string {
		r, ok := contextActionRect(width, height-1, benchmarkDetailCopyLabel)
		if ok && r.contains(x-bx, y-by) && m.monitorCopyText(id) != "" {
			return "copy:" + id
		}
		return ""
	}
	if m.monitorContextDetail != "" && !m.contextTargetHidden() {
		return hit(m.monitorContextDetail, 0, 0, g.contentWidth, g.meterHeight)
	}
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
	mw, cw, _ := monitorSessionColumnWidths(a.width)
	y0 := a.topHeight + a.gap - 1
	for i, s := range sessions {
		width := cw
		mode := m.rowContextMode(s.id)
		if mode == contextSplit {
			_, width, _ = m.contextColumns(a.width, s)
		}
		if mode == contextSplit || mode == contextWide {
			if action := hit(s.id, mw+1, y0, width, heights[i]); action != "" {
				return action
			}
		}
		y0 += heights[i]
	}
	return ""
}

func (m Model) copyMonitorReply(id string) tea.Cmd {
	if text := m.monitorCopyText(id); text != "" {
		return tea.SetClipboard(text)
	}
	return nil
}

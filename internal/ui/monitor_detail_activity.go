package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type detailSentState struct {
	session   string
	preview   codex.SessionContext
	text      string
	attention codex.SessionAttention
}

func (m *Model) recordDetailSent(text string) {
	if m.monitorContextDetail == "" {
		return
	}
	if s, ok := m.contextDetailSession(); ok {
		m.monitorDetailSent = detailSentState{session: s.id, preview: s.preview, text: text, attention: s.attention}
	}
}

func (m Model) detailActivity() string {
	if m.meterView != viewMonitor || m.monitorContextDetail == "" || m.monitorContextHidden {
		return ""
	}
	s, ok := m.contextDetailSession()
	if !ok || m.monitorError != "" || !s.active || m.monitorState != monitorRunning {
		return ""
	}
	wave := []string{"●··", "·●·", "··●", "·●·"}[m.phase%4]
	if n := m.monitorDetailSent; n.session == s.id && n.text != "" && n.preview == s.preview && n.attention == s.attention {
		return strings.TrimSuffix(n.text, "...") + wave
	}
	if m.monitorState == monitorRunning && s.active && s.attention == codex.SessionAttentionNone {
		return wave
	}
	return ""
}

func sentNotice(text string) bool {
	return text == i18n.Text("Text sent ...") || text == i18n.Text("Decision sent ...")
}

// Share reserved control rows with scrolling so the final lines stay reachable.
func (m Model) detailControls(width, height int, colors palette) string {
	if m.monitorApprovalControls(width, height) {
		return m.renderMonitorApprovalControls(width, height, colors)
	}
	if m.monitorPromptRows(width, height) > 0 {
		return m.renderMonitorPrompt(width, height, colors)
	}
	notice := m.detailActivity()
	if m.monitorApprovalHasOutcome() && !sentNotice(m.monitorApprovalNotice) {
		notice = m.monitorApprovalNotice
	}
	if notice == "" {
		return ""
	}
	return colors.label().Render(ansi.Truncate(notice, max(width-4, 1), ""))
}

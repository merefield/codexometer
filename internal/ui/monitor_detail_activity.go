package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
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
	if s.attention == codex.SessionAttentionNone {
		return wave
	}
	return ""
}

func sentNotice(text string) bool {
	return text == i18n.Text("Text sent ...") || text == i18n.Text("Decision sent ...")
}

type detailControlLayout struct {
	kind   string
	rows   int
	notice string
}

// Scroll geometry needs only layout, not a styled rendering of the controls.
func (m Model) layoutDetailControls(width, height int) detailControlLayout {
	if rows := m.monitorApprovalControlRows(width, height); rows > 0 {
		return detailControlLayout{kind: "approval", rows: rows}
	}
	if rows := m.monitorPromptRows(width, height); rows > 0 {
		return detailControlLayout{kind: "prompt", rows: rows}
	}
	notice := m.detailActivity()
	if m.monitorApprovalHasOutcome() && !sentNotice(m.monitorApprovalNotice) {
		notice = m.monitorApprovalNotice
	}
	if notice == "" {
		return detailControlLayout{}
	}
	return detailControlLayout{kind: "notice", rows: 1, notice: notice}
}

func (layout detailControlLayout) render(m Model, width, height int, colors palette) string {
	switch layout.kind {
	case "approval":
		return m.renderMonitorApprovalControls(width, height, colors)
	case "prompt":
		return m.renderMonitorPrompt(width, height, colors)
	case "notice":
		return colors.label().Render(ansi.Truncate(layout.notice, max(width-4, 1), ""))
	default:
		return ""
	}
}

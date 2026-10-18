package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func headerFocusModel() Model {
	m := attentionTestModel()
	m.height = 65
	m.monitorSessionData[0].preview.Text = strings.Repeat("Detail text\n", 100)
	m.setRowContext("root-one", contextFull)
	return m
}

func TestMonitorHeaderFocusOverviewEntersFirstPill(t *testing.T) {
	for _, mode := range []int{contextGraph, contextSplit, contextWide} {
		m := attentionTestModel()
		m.height = 65
		// Put an input-waiting session first in the list, while a different
		// session's higher-priority approval remains first in the pill strip.
		m.monitorSessionData[0], m.monitorSessionData[1] = m.monitorSessionData[1], m.monitorSessionData[0]
		m.setRowContext("root-two", mode)
		_, buttons := m.visibleMonitorHeader()
		if len(buttons) < 2 || buttons[0].action != "attention:root-one" {
			t.Fatal("fixture must have a different session's pill first")
		}
		m = filterKey(t, m, tea.KeyUp)
		if !m.monitorAttentionFocused || m.monitorAttentionCursor != buttons[0].action || m.monitorSelectedID != "root-two" || m.monitorContextDetail != "" || m.rowContextMode("root-two") != mode {
			t.Fatal("overview did not enter the first pill while preserving its row selection")
		}
		m = filterKey(t, m, tea.KeyRight)
		m = filterKey(t, m, tea.KeyUp)
		m = filterKey(t, m, tea.KeyDown)
		if !m.monitorAttentionFocused || m.monitorAttentionCursor != "attention:root-two" {
			t.Fatal("returning from summary did not retrace the pill selection")
		}
		m = filterKey(t, m, tea.KeyDown)
		if m.monitorAttentionFocused || m.monitorSummaryFocused || m.monitorSelectedID != "root-two" || m.rowContextMode("root-two") != mode {
			t.Fatal("Down did not return to the original overview row")
		}
		m = filterKey(t, m, tea.KeyUp)
		m = filterKey(t, m, tea.KeyEnter)
		if m.monitorContextDetail != "root-one" {
			t.Fatal("Enter did not open the first visible pill's session")
		}
	}
}

func TestMonitorHeaderFocusDetailPillsSummaryAndBack(t *testing.T) {
	m := headerFocusModel()
	m.monitorContextScroll = 1
	m = filterKey(t, m, tea.KeyUp)
	if m.monitorContextScroll != 0 || m.monitorAttentionFocused || m.monitorSummaryFocused {
		t.Fatal("Up did not scroll before leaving detail")
	}
	m = filterKey(t, m, tea.KeyUp)
	if !m.monitorAttentionFocused || m.monitorAttentionCursor != "attention:root-one" || m.monitorContextDetail != "root-one" {
		t.Fatal("Up at top did not focus the current detail's pill")
	}
	m = filterKey(t, m, tea.KeyRight)
	if m.monitorAttentionCursor != "attention:root-two" || m.monitorContextDetail != "root-one" {
		t.Fatal("highlighting another pill opened it prematurely")
	}
	m = filterKey(t, m, tea.KeyUp)
	if !m.monitorSummaryFocused || m.monitorAttentionFocused || m.monitorContextDetail != "root-one" {
		t.Fatal("moving to summary replaced detail")
	}
	m = filterKey(t, m, tea.KeyDown)
	if !m.monitorAttentionFocused || m.monitorAttentionCursor != "attention:root-two" {
		t.Fatal("Down did not restore the highlighted pill")
	}
	m = filterKey(t, m, tea.KeyDown)
	if m.monitorAttentionFocused || m.monitorSummaryFocused || m.monitorContextDetail != "root-one" || m.monitorContextScroll != 0 {
		t.Fatal("Down did not return to the original detail")
	}
	m = filterKey(t, m, tea.KeyUp)
	m = filterKey(t, m, tea.KeyRight)
	m = filterKey(t, m, tea.KeyEnter)
	if m.monitorContextDetail != "root-two" || m.monitorAttentionFocused || m.monitorSummaryFocused || m.monitorPrompt.input.Focused() {
		t.Fatal("Enter did not open the highlighted session detail")
	}
}

func TestMonitorHeaderFocusFilterOnlyChangesDetailWhenChosen(t *testing.T) {
	m := headerFocusModel()
	m = filterKey(t, m, tea.KeyUp)
	m = filterKey(t, m, tea.KeyUp)
	if m.monitorContextDetail != "root-one" {
		t.Fatal("focus alone closed detail")
	}
	m = filterKey(t, m, tea.KeyRight)
	if m.monitorFilter != monitorFilterActive || m.monitorContextDetail != "" || !m.monitorSummaryFocused {
		t.Fatal("choosing a filter did not return to the filtered list")
	}
}

func TestMonitorHeaderFocusSkipsHiddenLevels(t *testing.T) {
	m := contextTestModel() // No attention pills.
	m.height = 65
	m.setRowContext("root-one", contextFull)
	m = filterKey(t, m, tea.KeyUp)
	if !m.monitorSummaryFocused || m.monitorAttentionFocused || m.monitorContextDetail != "root-one" {
		t.Fatal("did not skip absent pills")
	}
	m = filterKey(t, m, tea.KeyDown)
	if m.monitorSummaryFocused || m.monitorContextDetail != "root-one" {
		t.Fatal("Down did not skip absent pills back to detail")
	}
	m.height = 12
	m = filterKey(t, m, tea.KeyUp)
	if m.monitorSummaryFocused || m.monitorAttentionFocused {
		t.Fatal("focused hidden header controls")
	}
	found := false
	for height := 16; height < 40; height++ {
		m = headerFocusModel()
		m.height = height
		summary, buttons := m.visibleMonitorHeader()
		if summary || len(buttons) == 0 {
			continue
		}
		found = true
		m = filterKey(t, m, tea.KeyUp)
		m = filterKey(t, m, tea.KeyUp)
		if !m.monitorAttentionFocused || m.monitorSummaryFocused {
			t.Fatal("Up left visible pills for a hidden status bar")
		}
		break
	}
	if !found {
		t.Fatal("fixture never exercised pills without summary")
	}
}

func TestMonitorHeaderFocusTracksIdentityAndRejectsMissingPill(t *testing.T) {
	for _, disappears := range []bool{false, true} {
		m := headerFocusModel()
		m.monitorSessionData[0].attention = codex.SessionAttentionComplete
		m.monitorSessionData[1].attention = codex.SessionAttentionComplete
		m.monitorSessionData[0].working, m.monitorSessionData[1].working = false, false
		m = filterKey(t, m, tea.KeyUp)
		m = filterKey(t, m, tea.KeyRight)
		if m.monitorAttentionCursor != "attention:root-two" {
			t.Fatal("fixture did not highlight second pill")
		}
		if disappears {
			m.monitorSessionData[1].attention = codex.SessionAttentionNone
		} else {
			m.monitorSessionData[0], m.monitorSessionData[1] = m.monitorSessionData[1], m.monitorSessionData[0]
		}
		m = filterKey(t, m, tea.KeyEnter)
		want := "root-two"
		if disappears {
			want = "root-one"
		}
		if m.monitorContextDetail != want {
			t.Fatal("Enter followed a position rather than the pill's identity")
		}
	}
}

func TestMonitorHeaderFocusDoesNotStealComposerOrApprovalKeys(t *testing.T) {
	m, _ := promptTestModel()
	m.height = 65
	m.monitorSessionData[0].attention = codex.SessionAttentionComplete
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("draft")
	m = filterKey(t, m, tea.KeyUp)
	if m.monitorAttentionFocused || m.monitorSummaryFocused || !m.monitorPrompt.input.Focused() || m.monitorPrompt.input.Value() != "draft" {
		t.Fatal("composer arrows escaped to header")
	}
	m.monitorPrompt.input.Blur()
	m.monitorApprovalConfirm = "armed"
	m = filterKey(t, m, tea.KeyUp)
	if m.monitorApprovalConfirm != "" || m.monitorPrompt.input.Value() != "draft" {
		t.Fatal("entering header retained an armed approval or lost the draft")
	}
	for _, ch := range []rune{'1', 'c'} {
		next, cmd := m.Update(key(ch))
		m = next.(Model)
		if cmd != nil || m.monitorApprovalConfirm != "" {
			t.Fatal("header key acted on a detail approval or clipboard")
		}
	}
	m = filterKey(t, m, tea.KeyEscape)
	if m.monitorAttentionFocused || m.monitorContextDetail != "root-one" {
		t.Fatal("Escape did not release pill focus without closing detail")
	}
}

func TestMonitorHeaderFocusHighlightPreservesHitSurfaces(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		m := headerFocusModel()
		m.theme = theme
		g := m.dashboardLayout()
		summary, rows, buttons := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
		colors := paletteFor(theme)
		before := m.renderMonitorAttention(g.contentWidth, rows, buttons, colors)
		m = filterKey(t, m, tea.KeyUp)
		after := m.renderMonitorAttention(g.contentWidth, rows, buttons, colors)
		if before == after || ansi.Strip(before) != ansi.Strip(after) || lipgloss.Width(before) != lipgloss.Width(after) {
			t.Fatal("focus was invisible or changed pill geometry")
		}
		m.monitorContextHover = m.monitorAttentionCursor
		if m.renderMonitorAttention(g.contentWidth, rows, buttons, colors) != after {
			t.Fatal("hover over focused pill removed keyboard focus styling")
		}
		b := buttons[1]
		x, y := 2+b.rect.x, g.meterY+summary+b.rect.y
		if m.monitorAttentionAt(x, y) != b.action {
			t.Fatal("keyboard focus moved pill hit targets")
		}
		next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = next.(Model)
		if m.monitorAttentionFocused || m.monitorContextDetail != "root-two" {
			t.Fatal("mouse selection did not supersede keyboard pill focus")
		}
	}
}

func TestMonitorHeaderFocusPagingAndResize(t *testing.T) {
	m := headerFocusModel()
	m.width = 80
	for i := range 12 {
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{id: fmt.Sprintf("extra-%d", i), displayed: true, attention: codex.SessionAttentionComplete})
	}
	m = filterKey(t, m, tea.KeyUp)
	m = filterKey(t, m, tea.KeyEnd)
	if !strings.HasPrefix(m.monitorAttentionCursor, "attention-next:") {
		t.Fatal("overflow pager was not keyboard reachable")
	}
	m = filterKey(t, m, tea.KeyEnter)
	if m.monitorAttentionPage == 0 || !m.monitorAttentionFocused || m.monitorContextDetail != "root-one" {
		t.Fatal("Enter did not page the pills without opening a session")
	}
	m.width, m.height = 20, 12
	m = filterKey(t, m, tea.KeyEnter)
	if m.monitorAttentionFocused || m.monitorSummaryFocused || m.monitorContextDetail != "root-one" || m.monitorPrompt.input.Focused() {
		t.Fatal("resize activated an invisible pill or composer")
	}
}

func TestMonitorHeaderFocusMouseArrowsMirrorKeyboard(t *testing.T) {
	m := attentionTestModel()
	m.height = 65
	m.monitorSessionData = m.monitorSessionData[:1]
	m.monitorSelectedID = "root-one"
	for _, label := range []string{"[↑]", "[↑]", "[↓]", "[↓]"} {
		x, y := renderedTextStart(t, m, label)
		next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = next.(Model)
	}
	if m.monitorAttentionFocused || m.monitorSummaryFocused || m.monitorSelectedID != "root-one" {
		t.Fatal("mouse arrows did not retrace row -> pills -> summary -> pills -> row")
	}
}

func TestMonitorHeaderFocusKeepsSiblingReviewsDistinct(t *testing.T) {
	m, f := profileTestModel(t)
	m.height = 65
	m.monitorSessionData[0].attention = codex.SessionAttentionApproval
	m.monitorSessionData[0].preview.Kind = codex.SessionContextApproval
	m.openMonitorAttention("attention-profile:one")
	m = filterKey(t, m, tea.KeyUp)
	if m.monitorAttentionCursor != "attention-profile:one" {
		t.Fatal("did not start from the current review's specific pill")
	}
	m = filterKey(t, m, tea.KeyLeft)
	if m.monitorAttentionCursor != "attention:one" {
		t.Fatal("did not highlight the separate Codex approval for the same session")
	}
	m = filterKey(t, m, tea.KeyEnter)
	if m.monitorContextDetail != "one" || m.monitorContextRows["one"].review != "context" || f.updates != 0 {
		t.Fatal("Enter opened/applied the wrong sibling review")
	}
}

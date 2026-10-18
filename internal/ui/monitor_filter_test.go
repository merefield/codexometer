package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func statusFilterTestModel() Model {
	m := contextTestModel()
	m.monitorSessionData = []monitorSession{
		{id: "work", displayed: true, active: true, working: true},
		{id: "approval", displayed: true, active: true, working: true, attention: codex.SessionAttentionApproval},
		{id: "input", displayed: true, active: true, attention: codex.SessionAttentionInput},
		{id: "check", displayed: true, active: true, attention: codex.SessionAttentionCheck},
		{id: "complete", displayed: true, active: true, attention: codex.SessionAttentionComplete},
		{id: "idle", displayed: true},
		{id: "dismissed", displayed: true, active: true, working: true},
		{id: "undisplayed", active: true, working: true},
	}
	m.monitorDismissed = map[string]monitorSessionDismissal{"dismissed": {}}
	m.monitorBaseline, m.monitorLatest = 100, 725
	m.monitorStartedAt = time.Now().Add(-time.Minute)
	return m
}

func filterKey(t *testing.T, m Model, code rune) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyPressMsg{Code: code})
	if cmd != nil {
		t.Fatal("filter navigation unexpectedly issued a command")
	}
	return next.(Model)
}

func TestMonitorStatusFilterCategoriesKeepGlobalTotals(t *testing.T) {
	m := statusFilterTestModel()
	want := [][]string{{"work", "approval", "input", "check", "complete", "idle"}, {"work", "approval", "input", "complete"}, {"work"}, {"approval"}, {"input"}, {"check"}}
	global := strings.Join(m.monitorSummaryLines(120, 2, paletteFor(m.theme)), "\n")
	for filter := monitorFilterAll; filter < monitorFilterCount; filter++ {
		m.setMonitorFilter(filter)
		rows, _, _ := m.monitorSessionPage(100)
		var ids []string
		for _, row := range rows {
			ids = append(ids, row.id)
		}
		if !reflect.DeepEqual(ids, want[filter]) || m.monitorListSessionCount() != len(want[filter]) {
			t.Fatalf("filter %d rows %v", filter, ids)
		}
		if m.visibleMonitorSessionCount() != 6 || m.monitorRecordedTokens() != 625 {
			t.Fatal("filter changed accounting or dismissal visibility")
		}
		if got := ansi.Strip(strings.Join(m.monitorSummaryLines(120, 2, paletteFor(m.theme)), "\n")); got != ansi.Strip(global) {
			t.Fatal("filtered summary counts lost their global scope")
		}
	}
}

func TestMonitorStatusFilterKeyboardFocusAndDetailArrows(t *testing.T) {
	m := statusFilterTestModel()
	m.monitorSelectedID = "work"
	m = filterKey(t, m, tea.KeyUp)
	if !m.monitorSummaryFocused || m.monitorSelectedID != "" {
		t.Fatal("Up from the first session did not focus the status strip")
	}
	for filter := monitorFilterActive; filter < monitorFilterCount; filter++ {
		m = filterKey(t, m, tea.KeyRight)
		if m.monitorFilter != filter || !m.monitorSummaryFocused {
			t.Fatalf("Right selected filter %d, want %d", m.monitorFilter, filter)
		}
	}
	m = filterKey(t, m, tea.KeyRight)
	if m.monitorFilter != monitorFilterAll {
		t.Fatal("filter navigation did not wrap to all sessions")
	}
	m = filterKey(t, m, tea.KeyLeft)
	if m.monitorFilter != monitorFilterCheck {
		t.Fatal("filter navigation did not wrap backwards")
	}
	m = filterKey(t, m, tea.KeyDown)
	if m.monitorSummaryFocused || m.monitorSelectedID != "check" {
		t.Fatal("Down did not enter the first matching session")
	}
	before := m.rowContextMode("check")
	m = filterKey(t, m, tea.KeyRight)
	if m.rowContextMode("check") != before+1 || m.monitorFilter != monitorFilterCheck {
		t.Fatal("Right no longer controls detail outside the status strip")
	}
	m = filterKey(t, m, tea.KeyUp)
	if !m.monitorSummaryFocused || m.monitorContextExpanded != "" {
		t.Fatal("returning to filters left a live detail control target")
	}
	m = filterKey(t, m, tea.KeyEscape)
	if m.monitorFilter != monitorFilterAll || !m.monitorSummaryFocused {
		t.Fatal("Escape did not clear the filter without quitting")
	}
	m = filterKey(t, m, tea.KeyEnter)
	if m.monitorSelectedID != "work" || m.monitorSummaryFocused {
		t.Fatal("Enter did not return to the session list")
	}
}

func TestMonitorStatusFilterRenderedHitSurfaces(t *testing.T) {
	for _, size := range [][2]int{{32, 16}, {40, 24}, {64, 30}, {100, 40}, {180, 60}} {
		for _, full := range []bool{false, true} {
			m := contextTestModel()
			m.width, m.height = size[0], size[1]
			if full {
				m.setRowContext("root-one", contextFull)
			}
			g := m.dashboardLayout()
			width, height := 0, 0
			if full {
				width = g.contentWidth
				height, _, _ = m.monitorDetailHeader(width, g.meterHeight)
			} else {
				a := m.monitorArea(g.contentWidth, g.meterHeight)
				width, height = a.readoutWidth, a.topHeight
			}
			out := ansi.Strip(m.render())
			if lipgloss.Width(out) > m.width || lipgloss.Height(out) > m.height {
				t.Fatalf("filter layout overflow at %v full=%v", size, full)
			}
			lines := strings.Split(out, "\n")
			for _, b := range m.monitorFilterButtons(width, height) {
				for dy := range b.rect.height {
					for dx := range b.rect.width {
						x, y := 2+b.rect.x+dx, g.meterY+b.rect.y+dy
						if got := m.monitorContextAt(x, y); got != b.action {
							t.Fatalf("%v full=%v at %d,%d: %q want %q", size, full, x, y, got, b.action)
						}
						if y >= len(lines) || x >= lipgloss.Width(lines[y]) {
							t.Fatal("filter target extends beyond rendered content")
						}
					}
				}
				x, y := 2+b.rect.x, g.meterY+b.rect.y
				next, _ := m.Update(tea.MouseMotionMsg{X: x, Y: y})
				hovered := next.(Model)
				if hovered.monitorContextHover != b.action {
					t.Fatal("category has no hover feedback")
				}
				next, _ = hovered.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				selected := next.(Model)
				if !selected.monitorSummaryFocused || selected.monitorContextDetail != "" || monitorFilterAction(selected.monitorFilter) != b.action {
					t.Fatal("category click did not select a filter and return to the list")
				}
			}
		}
	}
}

func TestMonitorStatusFilterEmptyAndLiveChanges(t *testing.T) {
	m := statusFilterTestModel()
	m.setMonitorFilter(monitorFilterWorking)
	m = filterKey(t, m, tea.KeyDown)
	m.syncMonitorSessions(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{
		{ID: "work", Active: true, Attention: codex.SessionAttentionComplete},
	}}, time.Now())
	if m.monitorListSessionCount() != 0 || !m.monitorSummaryFocused || m.monitorSelectedID != "" {
		t.Fatal("a state transition retained a mismatched row or stale selection")
	}
	m = filterKey(t, m, tea.KeyDown)
	if !m.monitorSummaryFocused {
		t.Fatal("empty filter lost keyboard access to the categories")
	}
	if !strings.Contains(ansi.Strip(m.renderMonitorSessions(100, 20, paletteFor(m.theme))), i18n.Text("NO RESULTS MATCH THE ACTIVE FILTER")) {
		t.Fatal("empty filter showed an unrelated aggregate graph")
	}
	if m.monitorCloseAllEnabled() {
		t.Fatal("Close All enabled for an empty filter")
	}
	m.syncMonitorSessions(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "work", Active: true, Working: true}}}, time.Now())
	if m.monitorListSessionCount() != 1 {
		t.Fatal("working row did not reappear on the next observation")
	}
}

func TestMonitorActiveFilterFollowsLatestObservation(t *testing.T) {
	m := statusFilterTestModel()
	m.setMonitorFilter(monitorFilterActive)
	m = filterKey(t, m, tea.KeyDown)
	if m.monitorSelectedID != "work" {
		t.Fatal("Active did not include a working session")
	}
	now := time.Now()
	for _, tc := range []struct {
		active    bool
		attention codex.SessionAttention
		want      int
	}{
		{true, codex.SessionAttentionCheck, 0},
		{true, codex.SessionAttentionComplete, 1},
		{true, codex.SessionAttentionApproval, 1},
		{true, codex.SessionAttentionInput, 1},
		{false, codex.SessionAttentionNone, 0},
		{true, codex.SessionAttentionNone, 1},
	} {
		m.syncMonitorSessions(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "work", Active: tc.active, Attention: tc.attention}}}, now)
		if got := m.monitorListSessionCount(); got != tc.want {
			t.Fatalf("active=%v attention=%v: got %d rows, want %d", tc.active, tc.attention, got, tc.want)
		}
	}
	// Missing from a successful observation means inactive, not retained Active.
	m.syncMonitorSessions(codex.LiveUsageSnapshot{}, now.Add(time.Second))
	if m.monitorListSessionCount() != 0 || !m.monitorSummaryFocused || m.monitorSelectedID != "" {
		t.Fatal("Active retained a missing session or its stale selection")
	}
}

func TestMonitorSummaryAllCategoriesFitReservedHeight(t *testing.T) {
	m := statusFilterTestModel()
	for _, width := range []int{24, 32, 38, 39, 40, 54, 55, 64, 94, 95, 120, 180} {
		height := monitorSummaryHeight(width)
		lines := m.monitorSummaryLines(width-4, height-3, paletteFor(m.theme))
		if len(lines) > height-3 || len(m.monitorFilterButtons(width, height)) != int(monitorFilterCount) {
			t.Fatalf("width %d: missing category or summary overflow", width)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > width-4 {
				t.Fatalf("width %d: summary line overflow", width)
			}
		}
	}
}

func TestMonitorStatusFilterPreservesDraftAndApprovalAccess(t *testing.T) {
	m := contextTestModel()
	m.setRowContext("root-one", contextWide)
	m.monitorPrompt = monitorPromptState{session: "root-one", input: newMonitorEditor()}
	m.monitorPrompt.input.SetValue("keep this draft")
	m.monitorPrompt.input.Focus()
	m.monitorApprovalConfirm = "armed"
	m.setMonitorFilter(monitorFilterCheck)
	if m.monitorDrafts["root-one"] != "keep this draft" || m.monitorPrompt.input.Focused() || m.monitorApprovalConfirm != "" {
		t.Fatal("filter discarded a draft or left editing/approval focus behind")
	}
	m.monitorSessionData[1].attention = codex.SessionAttentionApproval
	items := m.monitorAttentionSessions()
	if len(items) != 1 || items[0].id != "root-two" {
		t.Fatal("filter hid an approval navigation pill")
	}
	m.openMonitorAttention("attention:root-two")
	if m.monitorContextDetail != "root-two" || m.monitorFilter != monitorFilterAll || m.monitorSummaryFocused {
		t.Fatal("opening an excluded approval did not reveal its session")
	}
}

func TestMonitorStatusFilterCloseAllAndPagingAreScoped(t *testing.T) {
	m := statusFilterTestModel()
	for i := range 12 {
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{id: fmt.Sprint("work-", i), displayed: true, working: true})
	}
	m.setMonitorFilter(monitorFilterApproval)
	m.scrollMonitorRows(100)
	if m.monitorScroll != 0 {
		t.Fatal("filtered scrolling counted hidden rows")
	}
	m.setMonitorFilter(monitorFilterWorking)
	m.requestMonitorCloseAll()
	if !m.monitorCloseAllArmed() || len(m.monitorCloseAllConfirm) != 13 {
		t.Fatal("Close All confirmation did not capture all matching rows")
	}
	m.requestMonitorCloseAll()
	if m.monitorListSessionCount() != 0 || m.visibleMonitorSessionCount() != 5 || !m.monitorSessionVisible(m.monitorSessionData[1]) {
		t.Fatal("Close All dismissed sessions outside the filter")
	}
	if m.monitorRecordedTokens() != 625 {
		t.Fatal("filter-scoped dismissal changed token measurements")
	}
}

func TestMonitorStatusFilterKeepsComposerAndApprovalKeysScoped(t *testing.T) {
	m, _ := promptTestModel()
	m.setRowContext("root-one", contextWide)
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("keep this ordinary draft")
	if !m.monitorPrompt.input.Focused() {
		t.Fatal("fixture needs a focused composer")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = next.(Model)
	if m.monitorFilter != monitorFilterAll || m.monitorSummaryFocused || m.rowContextMode("root-one") != contextWide || m.monitorPrompt.input.Value() != "keep this ordinary draft" {
		t.Fatal("composer arrows changed filters or detail")
	}
	g := m.dashboardLayout()
	a := m.monitorArea(g.contentWidth, g.meterHeight)
	for _, b := range m.monitorFilterButtons(a.readoutWidth, a.topHeight) {
		if b.action != monitorFilterAction(monitorFilterApproval) {
			continue
		}
		next, _ = m.Update(tea.MouseClickMsg{X: 2 + b.rect.x, Y: g.meterY + b.rect.y, Button: tea.MouseLeft})
		m = next.(Model)
	}
	if m.monitorFilter != monitorFilterApproval || !m.monitorSummaryFocused || m.monitorDrafts["root-one"] != "keep this ordinary draft" {
		t.Fatal("filter click did not release the composer without losing its draft")
	}
	m = approvalTestModel()
	m.setMonitorFilter(monitorFilterAll)
	next, cmd := m.Update(key('1'))
	if cmd != nil || next.(Model).monitorApprovalConfirm != "" {
		t.Fatal("approval shortcut targeted a hidden detail while filters owned focus")
	}
}

func TestMonitorStatusFilterDoesNotEvictFullReviewOnStateChange(t *testing.T) {
	m := statusFilterTestModel()
	m.setMonitorFilter(monitorFilterWorking)
	m.setRowContext("work", contextFull)
	m.syncMonitorSessions(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{
		{ID: "work", Active: true, Attention: codex.SessionAttentionComplete},
	}}, time.Now())
	if m.monitorContextDetail != "work" || m.monitorFilter != monitorFilterWorking {
		t.Fatal("a state change closed a full-detail review unexpectedly")
	}
	m.stepBackMonitorContext()
	if m.monitorContextDetail != "" || m.monitorFilter != monitorFilterAll || m.monitorContextExpanded != "work" {
		t.Fatal("leaving full detail did not reveal the now-excluded row")
	}
}

func TestMonitorStatusFilterSelectedAndFocusedStyles(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		m := statusFilterTestModel()
		colors := paletteFor(theme)
		m.setMonitorFilter(monitorFilterInput)
		focused := m.monitorFilterTitleStyle(int(monitorFilterInput)+1, colors).Render("INPUT")
		m.selectMonitorSession(1)
		selected := m.monitorFilterTitleStyle(int(monitorFilterInput)+1, colors).Render("INPUT")
		idle := m.monitorFilterTitleStyle(int(monitorFilterApproval)+1, colors).Render("APPROVAL")
		m.monitorContextHover = monitorFilterAction(monitorFilterApproval)
		hovered := m.monitorFilterTitleStyle(int(monitorFilterApproval)+1, colors).Render("APPROVAL")
		if focused == selected || selected == colors.dimmed().Render("INPUT") || hovered == idle {
			t.Fatalf("theme %d does not distinguish active, focused and hovered filters", theme)
		}
	}
}

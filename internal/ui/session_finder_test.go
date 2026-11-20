package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
)

func TestFinderCombinesFiltersWithoutHidingAttention(t *testing.T) {
	m := contextTestModel()
	m.monitorSessionData[0].name = "Refactor"
	m.monitorSessionData[1].workingDirectory = "/work/API"
	m.monitorSessionData[2].attention = codex.SessionAttentionApproval
	m.monitorFilter = monitorFilterWorking
	send := func(msg tea.Msg) { next, _ := m.Update(msg); m = next.(Model) }
	send(key('f'))
	for _, r := range "api" {
		send(key(r))
	}
	if m.finder.query() != "api" || m.monitorSessionInList(m.monitorSessionData[0]) || !m.monitorSessionInList(m.monitorSessionData[1]) {
		t.Fatal("directory filtering", m.finder.query())
	}
	if got := m.monitorAttentionSessions(); len(got) != 1 || got[0].id != "root-three" {
		t.Fatalf("attention hidden: %v", got)
	}
	send(specialKey(tea.KeyDown))
	if m.monitorSelectedID != "root-two" || m.finder.focus != findNone {
		t.Fatal(m.monitorSelectedID, m.finder.focus)
	}
	m.setRowContext("root-three", contextFull)
	if m.finder.query() != "" || m.monitorContextDetail != "root-three" || m.monitorFilter != monitorFilterAll {
		t.Fatal("filtered approval target unreachable")
	}
}

func TestFinderSavedRowsAreSeparateAndScoped(t *testing.T) {
	m := contextTestModel()
	m.focusFinder()
	f := &m.finder
	f.setScope("one")
	f.include = true
	result := savedResult{scope: "one", epoch: f.epoch, page: codex.SavedPage{Sessions: []codex.SavedSession{{ID: "root-one"}, {ID: "saved", Name: "Older work"}}, Next: "next"}}
	f.accept(result)
	f.accept(result)
	if rows := m.savedMatches(); len(rows) != 1 || rows[0].ID != "saved" || len(f.rows) != 2 || len(m.monitorSessionData) != 3 {
		t.Fatal(rows)
	}
	f.focus = findResults
	f.detail = "saved"
	f.turns = []codex.SavedTurn{{Task: "question", Reply: "restricted text", Restricted: true}}
	render := m.renderSessionFinder(100, 6, paletteFor(m.theme))
	if strings.Contains(render, "restricted text") || !strings.Contains(render, "Older work") || !strings.Contains(render, "question") {
		t.Fatal(render)
	}
	f.setScope("two")
	f.accept(result)
	if len(f.rows) != 0 || len(f.turns) != 0 || f.include || f.detail != "" {
		t.Fatal("cached history survived account switch")
	}
}

func TestFinderFailedDetailShowsUnavailable(t *testing.T) {
	m := contextTestModel()
	m.focusFinder()
	f := &m.finder
	f.setScope("one")
	f.include, f.loading = true, true
	f.rows = []savedRow{{SavedSession: codex.SavedSession{ID: "saved", Name: "Old title"}}}
	f.detail = "saved"
	f.turns = []codex.SavedTurn{{Reply: "Old reply"}}
	f.accept(savedResult{scope: f.scope, epoch: f.epoch, id: f.detail, err: codex.ErrSavedSessions})
	render := m.renderSessionFinder(100, 6, paletteFor(m.theme))
	if !strings.Contains(render, "UNAVAILABLE") || strings.Contains(render, "Old title") || strings.Contains(render, "Old reply") || f.detail != "" || f.loading {
		t.Fatal("expected visible error without stale detail:", render)
	}
}

func TestFinderUsesComposerSurfaceOnlyForInput(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		colors := paletteFor(theme)
		m := contextTestModel()
		m.focusFinder()
		for _, draft := range []string{"", "query"} {
			m.finder.input.SetValue(draft)
			const width = 72
			view := m.renderSessionFinder(width, 1, colors)
			line := backgroundTestLines(withDefaultBackground(view, colors.background))[0]
			if line[0].Content != "⌕" {
				t.Fatalf("missing search icon: %v", line[0].Content)
			}
			end := m.finder.inputWidth(width)
			for _, x := range []int{0, 3, end - 1} {
				assertBackground(t, line[x].Style.Bg, monitorComposerBackground(colors))
			}
			assertBackground(t, line[end].Style.Bg, colors.background)
			m.leaveFinder()
		}
	}
}

func TestResumePickerIsSeparateFromLiveFilter(t *testing.T) {
	m := contextTestModel()
	m.focusFinder()
	m.finder.input.SetValue("live query")
	m.finder.setScope("scope")
	next, _, ok := m.runLocalMonitorCommand("resume")
	if !ok || !next.finder.include || next.finder.query() != "" || next.finder.liveQuery() != "live query" {
		t.Fatal("/resume did not open an independent search picker", next.finder)
	}
	next.finder.input.SetValue("older conversation")
	next, _, _ = next.updateSessionFinder(specialKey(tea.KeyEscape))
	if next.finder.include || next.finder.query() != "live query" {
		t.Fatal("closing the picker changed the live filter", next.finder)
	}
}

type resumeFinderProbe struct {
	Fetcher
	resumed string
}

func (p *resumeFinderProbe) ResumeSavedSession(_ context.Context, scope, cursor, id string) error {
	if scope != "scope" || cursor != "" {
		return codex.ErrSavedSessions
	}
	p.resumed = id
	return nil
}

func TestResumePickerReturnOpensLiveDetail(t *testing.T) {
	m := contextTestModel()
	probe := &resumeFinderProbe{}
	m.fetcher = probe
	m.finder.setScope("scope")
	m.openResumePicker()
	m.finder.failed = false
	m.finder.rows = []savedRow{{SavedSession: codex.SavedSession{ID: "cold"}}}
	m, cmd, _ := m.updateSessionFinder(specialKey(tea.KeyEnter))
	if cmd == nil || !m.finder.loading || probe.resumed != "" {
		t.Fatal("Enter did not stage a resume of the selected saved row")
	}
	m, _, _ = m.updateSessionFinder(cmd())
	if m.finder.resumeID != "cold" || probe.resumed != "cold" || m.monitorContextDetail != "" {
		t.Fatal("resume did not wait for the session's live observation")
	}
	next, _, _ := m.applyMonitorFetch(monitorFetchedMsg{kind: monitorFetchSample, at: time.Now(), usage: codex.LiveUsageSnapshot{
		SavedScope: "scope", TotalTokens: m.monitorLatest, Sessions: []codex.LiveUsageSession{{ID: "cold", Active: true}},
	}})
	if m = next.(Model); m.monitorContextDetail != "cold" || m.finder.include {
		t.Fatal("resumed session did not take over the detail pane")
	}
}

func TestResumePickerCanOpenWithNoLiveSessions(t *testing.T) {
	m := contextTestModel()
	m.monitorSessionData = nil
	m.finder.setScope("scope")
	x, y := renderedTextStart(t, m, "[ (R)ESUME ]")
	for _, msg := range []tea.Msg{key('r'), key('/'), tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}} {
		next, _ := m.Update(msg)
		if n := next.(Model); !n.finder.include || n.loading != m.loading {
			t.Fatalf("%T could not open the saved picker without a live session", msg)
		}
	}
	if _, ok := footerButtonByID(m, footerButtonRefresh); ok {
		t.Fatal("Sessions should not show a quota Refresh button")
	}
	next, _ := m.Update(key('r'))
	next, _ = next.(Model).Update(key('r'))
	if got := next.(Model).finder.query(); got != "r" {
		t.Fatalf("R in the search field should be typed, got %q", got)
	}
}

func TestSavedFinderFrameAndCursorFollowResults(t *testing.T) {
	m := contextTestModel()
	m.fetcher = &resumeFinderProbe{}
	m.finder.setScope("scope")
	m.openResumePicker()
	m.finder.failed = false
	for i := range 30 {
		m.finder.rows = append(m.finder.rows, savedRow{SavedSession: codex.SavedSession{ID: fmt.Sprintf("row-%02d", i), Name: fmt.Sprintf("Conversation %02d", i)}})
	}
	m.finder.next = "page-two"
	m.finder.selectIndex(m.savedMatches(), 0)
	send := func(msg tea.Msg) tea.Cmd {
		next, cmd := m.Update(msg)
		m = next.(Model)
		return cmd
	}
	send(specialKey(tea.KeyDown))
	send(specialKey(tea.KeyDown))
	d := m.dashboardLayout()
	a := m.monitorArea(d.contentWidth, d.meterHeight)
	g := m.finder.layout(d.contentWidth, a.finderRows)
	rendered := ansi.Strip(m.renderSessionFinder(d.contentWidth, a.finderRows, paletteFor(m.theme)))
	if m.finder.selected != "row-01" || !strings.Contains(rendered, "› OW-01 // Conversation 01") {
		t.Fatal("expected cursor on the second saved row", rendered)
	}
	if !strings.HasPrefix(rendered, "╭─ SAVED // SESSIONS") || strings.Count(rendered, "SAVED //") != 1 {
		t.Fatal("expected a single framed heading", rendered)
	}
	y := d.meterY + a.topHeight + a.attentionRows
	down := m.savedListButtons(g, d.contentWidth)[1].rect
	if strings.Index(strings.Split(rendered, "\n")[0], "[↓]") < 0 ||
		ansi.StringWidth(strings.SplitN(rendered, "[↓]", 2)[0]) != down.x {
		t.Fatal("framed down arrow's rendered column and hit target differ", rendered)
	}
	send(tea.MouseClickMsg{X: 2 + down.x, Y: y + down.y, Button: tea.MouseLeft})
	if m.finder.selected != "row-02" {
		t.Fatal("frame down arrow did not move the saved cursor", m.finder.selected)
	}
	for range 28 {
		send(specialKey(tea.KeyDown))
	}
	rendered = ansi.Strip(m.renderSessionFinder(d.contentWidth, a.finderRows, paletteFor(m.theme)))
	if !strings.Contains(rendered, "› [+] MORE") {
		t.Fatal("cursor did not scroll the pagination row into view", rendered)
	}
	send(specialKey(tea.KeyUp))
	cmd := send(key('r'))
	if cmd == nil || m.finder.detail != "row-29" || m.loading {
		t.Fatal("R did not select the same saved row for resume")
	}
}

func TestSavedFinderNameOnlyAndAlignedAgeColumn(t *testing.T) {
	m := contextTestModel()
	m.openResumePicker()
	m.finder.failed = false
	m.finder.input.SetValue("blUE")
	now := time.Now()
	m.finder.rows = []savedRow{
		{SavedSession: codex.SavedSession{ID: "first", Name: "Blue dashboard", Directory: strings.Repeat("/very-long", 14), UpdatedAt: now.Add(-3*time.Hour - time.Minute).Unix()}},
		{SavedSession: codex.SavedSession{ID: "no-time", Name: "BLUE inventory"}},
		{SavedSession: codex.SavedSession{ID: "blue", Name: "Other", Directory: "/blue"}},
		{SavedSession: codex.SavedSession{ID: "untitled", Directory: "/blue"}},
	}
	if rows := m.savedMatches(); len(rows) != 2 || rows[0].ID != "first" || rows[1].ID != "no-time" {
		t.Fatal("saved results should only match names", rows)
	}
	render := func(width int) string {
		return ansi.Strip(m.renderSessionFinder(width, 9, paletteFor(m.theme)))
	}
	lines := strings.Split(render(56), "\n")
	for _, tc := range []struct {
		row  int
		text string
	}{{1, "TIME AGO"}, {2, "3h ago"}, {3, "—"}} {
		line := lines[tc.row]
		if !strings.HasSuffix(line, tc.text+" │") || ansi.StringWidth(line) != 56 {
			t.Fatalf("age column misplaced: %q", line)
		}
	}
	if !strings.Contains(lines[2], "…") || strings.Contains(render(28), "3h ago") {
		t.Fatal("long and narrow views did not preserve space for the names")
	}
	for _, tc := range []struct {
		updated int64
		want    string
	}{{0, "—"}, {now.Unix() + 5, "JUST NOW"}, {now.Unix() - 95, "1m ago"}, {now.Unix() - 2*86400, "2d ago"}} {
		if got := savedSessionAge(tc.updated, now); got != tc.want {
			t.Fatalf("age for %d = %q, want %q", tc.updated, got, tc.want)
		}
	}
}

func TestResumePickerSelectionAndStrictConfirmation(t *testing.T) {
	m := contextTestModel()
	probe := &resumeFinderProbe{}
	m.fetcher, m.confirmations = probe, confirmation.Policy{Mode: confirmation.Require}
	m.finder.setScope("scope")
	m.openResumePicker()
	m.finder.failed = false
	for _, id := range []string{"first", "wanted", "other"} {
		m.finder.rows = append(m.finder.rows, savedRow{SavedSession: codex.SavedSession{ID: id}})
	}
	m.finder.focus = findResults
	m.scrollFinder(1, 80, 12)
	m.monitorSessionData = append(m.monitorSessionData, monitorSession{id: "first", displayed: true})
	m, cmd, _ := m.updateSessionFinder(specialKey(tea.KeyEnter))
	if cmd != nil || m.finder.detail != "wanted" || m.actionReview == nil || probe.resumed != "" {
		t.Fatal("resume target changed or bypassed review")
	}
	m, cmd, _ = m.updateActionReview(key('c'))
	if cmd == nil {
		t.Fatal("confirmed resume unavailable")
	}
	m, _, _ = m.updateSessionFinder(cmd())
	if m.finder.resumeID != "wanted" || probe.resumed != "wanted" {
		t.Fatal("confirmed resume changed target")
	}
	m, _, _ = m.updateSessionFinder(savedResumeDeadline{savedResult{id: "wanted", scope: "scope", epoch: m.finder.epoch}})
	if m.finder.resumeID != "" || m.finder.resumeNotice == "" {
		t.Fatal("missing row did not clear the pending handoff")
	}
}

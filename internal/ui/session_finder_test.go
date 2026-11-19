package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
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

package ui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

type cachedCommandTestClient struct {
	*suggestionClient
	menus map[string]codex.SessionCommandMenu
	fresh int
}

func (f *cachedCommandTestClient) CachedSessionCommands(id, path string) (codex.SessionCommandMenu, bool) {
	if id != "root-one" {
		return codex.SessionCommandMenu{}, false
	}
	menu, ok := f.menus[path]
	return menu, ok
}

func (f *cachedCommandTestClient) SessionCommandsFresh(_ context.Context, _, path string) (codex.SessionCommandMenu, error) {
	f.fresh++
	menu := f.menus[path]
	menu.Revision = "fresh"
	return menu, nil
}

func TestMonitorCommandCacheOpensAndRetracesTreeImmediately(t *testing.T) {
	m, prompt := promptTestModel()
	f := &cachedCommandTestClient{suggestionClient: &suggestionClient{commandTestClient: &commandTestClient{promptTestClient: prompt}}, menus: map[string]codex.SessionCommandMenu{
		"":            {Path: "", Choices: []codex.SessionCommandChoice{{ID: "model", Label: "/model", Next: "model"}}},
		"model":       {Path: "model", Choices: []codex.SessionCommandChoice{{ID: "small", Label: "Small", Next: "model/small"}}},
		"model/small": {Path: "model/small", Choices: []codex.SessionCommandChoice{{ID: "medium", Label: "Medium", Action: true}}},
	}}
	m.fetcher = f
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("/m")
	if cmd := m.syncMonitorSuggestions(); cmd != nil || m.monitorSuggestions.busy || len(m.suggestionChoices()) != 1 {
		t.Fatal("warm root did not appear synchronously")
	}
	for _, path := range []string{"model", "model/small"} {
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)
		if cmd != nil || m.monitorCommands.busy || m.monitorCommands.menu.Path != path || !m.monitorPrompt.input.Focused() {
			t.Fatalf("cached level %q waited for a fetch or lost composer focus", path)
		}
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	m = next.(Model)
	if cmd != nil || m.monitorCommands.menu.Path != "model" || m.monitorPrompt.input.Value() != "/model " {
		t.Fatal("Back did not restore the cached level and draft immediately")
	}
	if f.reads != 0 || f.calls != 0 {
		t.Fatal("cached browsing performed an RPC or a write")
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = next.(Model)
	if cmd == nil || !m.monitorCommands.busy {
		t.Fatal("explicit refresh reused cached data")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if f.fresh != 1 || m.monitorCommands.menu.Revision != "fresh" || m.monitorPrompt.input.Value() != "/model " {
		t.Fatal("explicit refresh lost the draft or skipped a fresh read")
	}
}

func TestMonitorLocalSuggestionsRemainUsableDuringColdDiscovery(t *testing.T) {
	m, prompt := promptTestModel()
	f := &suggestionClient{commandTestClient: &commandTestClient{promptTestClient: prompt}}
	m.fetcher = f
	m.focusMonitorPrompt()
	m.monitorPrompt.input.SetValue("/theme")
	if m.syncMonitorSuggestions() == nil || !m.monitorSuggestions.busy {
		t.Fatal("fixture must have a pending discovery")
	}
	if choices := m.suggestionChoices(); len(choices) != 1 || choices[0].Label != "/theme" {
		t.Fatal("local command is hidden until discovery finishes")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if !m.monitorCommands.open || m.monitorCommands.menu.Path != "theme" || m.monitorCommands.busy || f.reads != 0 {
		t.Fatal("cold discovery blocked a local command")
	}
}

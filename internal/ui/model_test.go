package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/merefield/codexometer/internal/codex"
)

type stubFetcher struct {
	snapshot codex.Snapshot
	err      error
}

func (f stubFetcher) Fetch(context.Context) (codex.Snapshot, error) {
	return f.snapshot, f.err
}

func TestThemeHotkeyCyclesAndWraps(t *testing.T) {
	model := New(nil, time.Minute)
	for want := themeRust; want < themeCount; want++ {
		updated, _ := model.Update(key('t'))
		model = updated.(Model)
		if model.theme != want {
			t.Fatalf("got theme %d, want %d", model.theme, want)
		}
	}
	updated, _ := model.Update(key('t'))
	model = updated.(Model)
	if model.theme != themeHacker {
		t.Fatalf("theme did not wrap: got %d", model.theme)
	}
}

func TestStyleHotkeyCyclesAndWraps(t *testing.T) {
	model := New(nil, time.Minute)
	for want := styleRotary; want < styleCount; want++ {
		updated, _ := model.Update(key('s'))
		model = updated.(Model)
		if model.meterStyle != want {
			t.Fatalf("got meter style %d, want %d", model.meterStyle, want)
		}
	}
	updated, _ := model.Update(key('s'))
	model = updated.(Model)
	if model.meterStyle != styleBars {
		t.Fatalf("meter style did not wrap: got %d", model.meterStyle)
	}
}

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestModelLifecycleMessages(t *testing.T) {
	snapshot := codex.DemoSnapshot()
	model := New(stubFetcher{snapshot: snapshot}, 0)
	if model.refreshEvery != time.Minute || model.Init() == nil {
		t.Fatal("model did not apply defaults or initialize commands")
	}

	fetched := model.fetch()().(fetchedMsg)
	if fetched.err != nil || len(fetched.snapshot.Meters()) != 2 {
		t.Fatalf("fetch command returned %#v", fetched)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	if model.width != 100 || model.height != 30 {
		t.Fatalf("window size not stored: %#v", model)
	}

	updated, _ = model.Update(fetchedMsg{snapshot: snapshot})
	model = updated.(Model)
	if model.loading || model.lastRefresh.IsZero() || len(model.snapshot.Meters()) != 2 {
		t.Fatalf("successful fetch not applied: %#v", model)
	}

	previous := model.snapshot
	updated, _ = model.Update(fetchedMsg{err: errors.New("offline")})
	model = updated.(Model)
	if model.err == nil || len(model.snapshot.Meters()) != len(previous.Meters()) {
		t.Fatalf("failed fetch did not retain snapshot: %#v", model)
	}

	phase := model.phase
	updated, command := model.Update(secondMsg(time.Now()))
	model = updated.(Model)
	if model.phase != phase+1 || command == nil {
		t.Fatal("second tick did not advance phase and reschedule")
	}

	updated, command = model.Update(refreshMsg(time.Now()))
	model = updated.(Model)
	if !model.loading || command == nil || model.nextRefresh.Before(time.Now()) {
		t.Fatal("refresh tick did not fetch and reschedule")
	}
}

func TestModelManualRefreshAndQuitKeys(t *testing.T) {
	model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	model.loading = false

	updated, command := model.Update(key('r'))
	model = updated.(Model)
	if !model.loading || command == nil {
		t.Fatal("manual refresh did not start")
	}
	_, command = model.Update(key('r'))
	if command != nil {
		t.Fatal("refresh was started while one was already active")
	}

	for _, quitKey := range []tea.KeyMsg{
		key('q'),
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
	} {
		_, command = model.Update(quitKey)
		if command == nil {
			t.Fatalf("quit key %q returned no command", quitKey.String())
		}
	}
}

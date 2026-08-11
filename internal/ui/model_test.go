package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

func TestTabCyclesStylesAndShiftTabMovesBack(t *testing.T) {
	model := New(nil, time.Minute)
	for want := styleStopwatch; want < styleCount; want++ {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
		model = updated.(Model)
		if model.meterStyle != want {
			t.Fatalf("got meter style %d, want %d", model.meterStyle, want)
		}
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.meterStyle != styleBars {
		t.Fatalf("meter style did not wrap: got %d", model.meterStyle)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(Model)
	if model.meterStyle != styleFuel {
		t.Fatalf("reverse style navigation got %d, want %d", model.meterStyle, styleFuel)
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
	sequence := model.flashSequence
	updated, command = model.Update(key('r'))
	model = updated.(Model)
	if command == nil || model.flashSequence != sequence+1 || model.flashedButton != footerButtonRefresh {
		t.Fatal("busy refresh hotkey did not provide button feedback")
	}
	_, fetchCommand := model.activateFooterButton(footerButtonRefresh)
	if fetchCommand != nil {
		t.Fatal("refresh action started a second fetch while one was already active")
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

func TestFooterButtonsSupportHoverAndMouseClicks(t *testing.T) {
	model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width = 100
	model.height = 30

	button, _ := footerButtonByID(model, footerButtonTheme)
	colors := paletteFor(model.theme)
	wantDefault := lipgloss.NewStyle().Foreground(colors.dim).Background(colors.background).Render(button.label)
	if !strings.Contains(model.View(), wantDefault) {
		t.Fatal("idle theme button did not use the subdued theme colour")
	}

	hover := footerMouseMessage(t, model, footerButtonTheme, tea.MouseActionMotion)
	updated, command := model.Update(hover)
	model = updated.(Model)
	if command != nil || model.hoveredButton != footerButtonTheme {
		t.Fatalf("theme hover was not recorded: button=%d command=%v", model.hoveredButton, command)
	}
	wantHover := lipgloss.NewStyle().Bold(true).Foreground(colors.primary).Background(colors.background).Render(button.label)
	if !strings.Contains(model.View(), wantHover) {
		t.Fatal("hovered theme button was not highlighted")
	}

	updated, _ = model.Update(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionMotion})
	model = updated.(Model)
	if model.hoveredButton != footerButtonNone {
		t.Fatalf("moving outside buttons left button %d hovered", model.hoveredButton)
	}

	updated, _ = model.Update(footerMouseMessage(t, model, footerButtonTheme, tea.MouseActionPress))
	model = updated.(Model)
	if model.theme != themeRust || model.flashedButton != footerButtonTheme {
		t.Fatalf("theme click selected theme=%d flash=%d, want theme=%d flash=%d", model.theme, model.flashedButton, themeRust, footerButtonTheme)
	}

	updated, command = model.Update(footerMouseMessage(t, model, footerButtonRefresh, tea.MouseActionPress))
	model = updated.(Model)
	if !model.loading || command == nil {
		t.Fatal("refresh click did not start a fetch")
	}

	model.loading = false
	_, command = model.Update(footerMouseMessage(t, model, footerButtonQuit, tea.MouseActionPress))
	if command == nil {
		t.Fatal("quit click returned no command")
	}
}

func TestFooterHitGeometryMatchesRenderedButtonsAcrossSizes(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
		empty         bool
		withError     bool
		style         meterStyleID
	}{
		{name: "compact", width: 40, height: 24},
		{name: "standard", width: 80, height: 24},
		{name: "large error", width: 120, height: 40, withError: true},
		{name: "empty quota", width: 80, height: 24, empty: true},
		{name: "empty stopwatch", width: 80, height: 24, empty: true, style: styleStopwatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
			if !test.empty {
				model.snapshot = codex.DemoSnapshot()
			}
			model.loading = false
			model.width = test.width
			model.height = test.height
			model.meterStyle = test.style
			if test.withError {
				model.err = errors.New("stale quota signal")
			}
			layout := model.dashboardLayout()
			for _, id := range []footerButtonID{footerButtonTheme, footerButtonRefresh, footerButtonQuit} {
				mouse := footerMouseMessage(t, model, id, tea.MouseActionMotion)
				if mouse.Y != layout.footerY+1 {
					t.Errorf("rendered footer y=%d, calculated y=%d", mouse.Y, layout.footerY+1)
				}
				if got := model.footerButtonAt(mouse.X, mouse.Y); got != id {
					t.Errorf("hit button %d, want %d", got, id)
				}
			}
		})
	}
}

func TestFooterButtonHotkeyFlashesAndLatestPulseWins(t *testing.T) {
	model := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	model.snapshot = codex.DemoSnapshot()
	model.loading = false
	model.width = 100
	model.height = 30

	updated, command := model.Update(key('t'))
	model = updated.(Model)
	themeSequence := model.flashSequence
	if command == nil || model.flashedButton != footerButtonTheme {
		t.Fatalf("theme hotkey did not start a pulse: button=%d command=%v", model.flashedButton, command)
	}
	button, _ := footerButtonByID(model, footerButtonTheme)
	colors := paletteFor(model.theme)
	wantFlash := lipgloss.NewStyle().Bold(true).Foreground(colors.background).Background(colors.primary).Render(button.label)
	if !strings.Contains(model.View(), wantFlash) {
		t.Fatal("theme hotkey pulse was not rendered")
	}

	model.loading = false
	updated, _ = model.Update(key('r'))
	model = updated.(Model)
	if model.flashedButton != footerButtonRefresh || model.flashSequence == themeSequence {
		t.Fatal("newer refresh pulse did not replace theme pulse")
	}

	updated, _ = model.Update(footerButtonFlashExpiredMsg{button: footerButtonTheme, sequence: themeSequence})
	model = updated.(Model)
	if model.flashedButton != footerButtonRefresh {
		t.Fatal("stale theme expiry cleared the newer refresh pulse")
	}

	updated, _ = model.Update(footerButtonFlashExpiredMsg{button: footerButtonRefresh, sequence: model.flashSequence})
	model = updated.(Model)
	if model.flashedButton != footerButtonNone {
		t.Fatal("current refresh pulse did not expire")
	}
}

func TestFooterButtonLabelsEmbedHotkeysWithPadding(t *testing.T) {
	buttons, separator := footerButtonLayout(100)
	want := []string{"[ (T)HEME ]", "[ (R)EFRESH ]", "[ (Q)UIT ]"}
	if separator != "  " || len(buttons) != len(want) {
		t.Fatalf("unexpected full footer layout: separator=%q buttons=%#v", separator, buttons)
	}
	for index, label := range want {
		if buttons[index].label != label {
			t.Errorf("button %d label = %q, want %q", index, buttons[index].label, label)
		}
	}

	compact, separator := footerButtonLayout(20)
	if separator != " " {
		t.Fatalf("compact separator = %q, want one space", separator)
	}
	for index, label := range []string{"[T]", "[R]", "[Q]"} {
		if compact[index].label != label {
			t.Errorf("compact button %d label = %q, want %q", index, compact[index].label, label)
		}
	}
}

func TestFooterLayoutNeverHitTestsControlsTruncatedByTheme(t *testing.T) {
	for _, themeName := range []string{"HACKER", "BLUE STEEL", "ULTRAVIOLET"} {
		for width := 1; width <= 60; width++ {
			buttons, separator := footerButtonLayoutWithTheme(width, themeName)
			controlsWidth := 0
			for index, button := range buttons {
				if index > 0 {
					controlsWidth += len(separator)
				}
				controlsWidth += len(button.label)
			}
			available := max(width-len("THEME // ")-len(themeName)-1, 0)
			if controlsWidth > available {
				t.Fatalf("theme=%q width=%d exposes %d cells of controls in %d available cells", themeName, width, controlsWidth, available)
			}
		}
	}
}

func footerMouseMessage(t *testing.T, model Model, id footerButtonID, action tea.MouseAction) tea.MouseMsg {
	t.Helper()
	button, ok := footerButtonByID(model, id)
	if !ok {
		t.Fatalf("button %d is not in the current footer layout", id)
	}
	for y, line := range strings.Split(ansi.Strip(model.View()), "\n") {
		if x := strings.Index(line, button.label); x >= 0 {
			return tea.MouseMsg{X: x + len(button.label)/2, Y: y, Button: tea.MouseButtonLeft, Action: action}
		}
	}
	t.Fatalf("button %q was not rendered", button.label)
	return tea.MouseMsg{}
}

func footerButtonByID(model Model, id footerButtonID) (footerButton, bool) {
	buttons, _ := footerButtonLayoutWithTheme(model.contentWidth(), paletteFor(model.theme).name)
	for _, button := range buttons {
		if button.id == id {
			return button, true
		}
	}
	return footerButton{}, false
}

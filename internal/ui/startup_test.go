package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func startupTestModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	next, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = next.(Model)
	if !m.startup.active() || cmd == nil {
		t.Fatal("startup did not start after terminal size arrived")
	}
	return m
}

func TestStartupLogoReturnsToOriginalHeader(t *testing.T) {
	if got := scaledStartupLogo(len([]rune(headerLogo[0])), len(headerLogo)*2); !reflect.DeepEqual(got, headerLogo) {
		t.Fatalf("scaled logo changed header lettering: %#v", got)
	}
	for _, size := range [][2]int{{68, 8}, {80, 24}, {120, 40}, {240, 60}, {320, 8}} {
		m := startupTestModel(t, size[0], size[1])
		previousX := m.width
		for elapsed := time.Duration(0); elapsed <= startupEntrance; elapsed += 30 * time.Millisecond {
			m.startup.elapsed = elapsed
			g := m.startupGeometry()
			if g.x > previousX || g.width != m.width {
				t.Fatal("entrance did not move left at full width", g)
			}
			previousX = g.x
		}
		m.startup.elapsed = startupEntrance + startupHold/2
		g := m.startupGeometry()
		if g.x != 0 || g.width != m.width || g.y != (m.height-(g.pixelHeight+1)/2)/2 {
			t.Fatal("logo did not settle across the full centred screen", g)
		}
		previousWidth, previousY := g.width, g.y
		for elapsed := startupEntrance + startupHold; elapsed <= startupDuration; elapsed += 20 * time.Millisecond {
			m.startup.elapsed = elapsed
			g := m.startupGeometry()
			if g.width > previousWidth || g.y > previousY {
				t.Fatal("docking did not shrink toward the header", g)
			}
			previousWidth, previousY = g.width, g.y
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(lines) != m.height {
				t.Fatalf("frame height=%d want %d", len(lines), m.height)
			}
			for _, line := range lines {
				if lipgloss.Width(line) != m.width {
					t.Fatal("animation frame wrapped or left stale cells")
				}
			}
		}
		m.startup.elapsed = startupDuration
		lines := strings.Split(ansi.Strip(m.renderStartup()), "\n")
		for row, logo := range headerLogo {
			if got := lines[row+1]; got != "  "+logo+strings.Repeat(" ", m.width-2-lipgloss.Width(logo)) {
				t.Fatal("logo did not dock at normal header coordinates", got)
			}
		}
	}
}

func TestStartupLoadsDataWithoutShowingDashboardEarly(t *testing.T) {
	m := New(stubFetcher{snapshot: codex.DemoSnapshot()}, time.Minute)
	if m.View().Content != "" {
		t.Fatal("dashboard flashed before the first terminal size")
	}
	commands, ok := m.Init()().(tea.BatchMsg)
	if !ok || len(commands) < 1 {
		t.Fatal("startup did not schedule initial data loading")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	started := m.startup.started
	next, _ = m.Update(commands[0]())
	m = next.(Model)
	if m.loading || len(m.snapshot.Meters()) != 2 || !m.startup.active() {
		t.Fatal("quota load was blocked by the animation")
	}
	next, cmd := m.Update(secondMsg(started.Add(time.Second)))
	m = next.(Model)
	if cmd == nil || m.phase != 1 {
		t.Fatal("background timers stopped during animation")
	}
	m.startup.elapsed = startupEntrance
	if strings.Contains(ansi.Strip(m.View().Content), "VERSION") {
		t.Fatal("dashboard rendered before the logo docked")
	}
	next, cmd = m.Update(startupFrameMsg{started: started, at: started.Add(startupDuration)})
	m = next.(Model)
	if m.startup.active() || m.startup.pending || cmd != nil || !strings.Contains(ansi.Strip(m.View().Content), "VERSION") {
		t.Fatal("animation did not reveal the loaded dashboard")
	}
	next, cmd = m.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	m = next.(Model)
	if m.startup.active() || cmd != nil {
		t.Fatal("resize replayed the startup animation")
	}
	next, _ = m.Update(refreshMsg(time.Now()))
	if next.(Model).startup.active() {
		t.Fatal("refresh replayed the startup animation")
	}
}

func TestStartupRestoredViewAndErrorsStillUpdate(t *testing.T) {
	m := NewWithPreferences(historyStub{}, time.Minute, &memoryPreferenceStore{preferences: Preferences{MainTab: "usage"}})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	next, cmd := m.Update(initialViewMsg{})
	m = next.(Model)
	if cmd == nil || !m.history.loading || !m.startup.active() {
		t.Fatal("saved view did not load during animation")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.history.loading {
		t.Fatal("saved view results were not applied during animation")
	}
	next, _ = m.Update(fetchedMsg{err: errors.New("offline")})
	m = next.(Model)
	if m.err == nil || !m.startup.active() {
		t.Fatal("fetch errors were not processed during animation")
	}
}

func TestStartupSkipDoesNotActivateHiddenControls(t *testing.T) {
	for _, message := range []tea.Msg{key('t'), tea.PasteMsg{Content: "do not send"}, specialKey(tea.KeyEnter), specialKey(tea.KeyEscape), tea.MouseClickMsg{X: 3, Y: 1, Button: tea.MouseLeft}} {
		m := startupTestModel(t, 120, 40)
		m.meterView = viewMonitor
		started := m.startup.started
		next, cmd := m.Update(message)
		m = next.(Model)
		if m.startup.active() || cmd != nil || m.theme != themeHacker || m.meterView != viewMonitor {
			t.Fatal("skip activated an unseen control")
		}
		next, cmd = m.Update(startupFrameMsg{started: started, at: started.Add(time.Second)})
		if next.(Model).startup.active() || cmd != nil {
			t.Fatal("queued frame resurrected skipped animation")
		}
	}
	for _, message := range []tea.Msg{key('q'), modifiedKey('c', tea.ModCtrl)} {
		m := startupTestModel(t, 120, 40)
		next, cmd := m.Update(message)
		if next.(Model).startup.active() || cmd == nil {
			t.Fatal("quit blocked by startup")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("startup quit did not stop program")
		}
	}
	m := startupTestModel(t, 120, 40)
	next, cmd := m.Update(tea.MouseMotionMsg{X: 3, Y: 1})
	if !next.(Model).startup.active() || cmd != nil {
		t.Fatal("hover skipped startup or reached a hidden control")
	}
}

func TestStartupResizeAndDelayedFrames(t *testing.T) {
	m := startupTestModel(t, 80, 24)
	started := m.startup.started
	next, cmd := m.Update(startupFrameMsg{started: started, at: started.Add(500 * time.Millisecond)})
	m = next.(Model)
	if cmd == nil || m.startup.elapsed != 500*time.Millisecond {
		t.Fatal("delayed frame did not use elapsed time")
	}
	next, cmd = m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	if cmd != nil || m.startup.started != started || m.startup.elapsed != 500*time.Millisecond || m.startupGeometry().width != 160 {
		t.Fatal("resize restarted timing or failed to rescale")
	}
	next, cmd = m.Update(startupFrameMsg{started: started.Add(time.Second), at: started.Add(time.Second)})
	if cmd != nil || next.(Model).startup.elapsed != m.startup.elapsed {
		t.Fatal("stale animation changed progress")
	}
	next, _ = m.Update(startupFrameMsg{started: started, at: started.Add(200 * time.Millisecond)})
	m = next.(Model)
	if m.startup.elapsed != 500*time.Millisecond {
		t.Fatal("out-of-order frame moved animation backwards")
	}
	next, cmd = m.Update(tea.WindowSizeMsg{Width: 50, Height: 10})
	if next.(Model).startup.active() || cmd != nil {
		t.Fatal("resize to a compact terminal did not finish animation")
	}
}

func TestStartupCompactAndInlineFallback(t *testing.T) {
	for _, size := range [][2]int{{40, 24}, {80, 7}, {1, 1}} {
		m := New(nil, time.Minute)
		next, cmd := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		if m.startup.active() || m.startup.pending || cmd != nil || m.render() == "" {
			t.Fatal("compact startup did not show the normal UI")
		}
	}
	m := New(nil, time.Minute)
	next, cmd := m.Update(tea.WindowSizeMsg{})
	if !next.(Model).startup.pending || cmd != nil {
		t.Fatal("animation started without terminal dimensions")
	}
	m.SetInline(true)
	next, cmd = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	if m.startup.active() || m.startup.pending || cmd != nil || m.View().AltScreen {
		t.Fatal("inline mode used the fullscreen animation")
	}
}

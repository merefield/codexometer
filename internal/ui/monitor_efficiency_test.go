package ui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorModelFootprint(t *testing.T) {
	t.Logf("model=%d bytes editor=%d bytes", reflect.TypeOf(Model{}).Size(), reflect.TypeOf(monitorEditor{}).Size())
	if reflect.TypeOf(monitorEditor{}).Size() > 128 {
		t.Fatal("editor embeds heavy widget state in the application value model")
	}
}

func TestMonitorEditorCopiesDetachOnMutation(t *testing.T) {
	for _, secret := range []bool{false, true} {
		original := newMonitorEditor()
		original.setSecret(secret)
		original.SetValue("original draft")
		original.configure(80, 30)
		original.style(paletteFor(themeHacker))
		for name, mutate := range map[string]func(*monitorEditor){
			"text":   func(e *monitorEditor) { e.SetValue("replacement") },
			"reset":  func(e *monitorEditor) { e.Reset() },
			"focus":  func(e *monitorEditor) { e.Focus() },
			"resize": func(e *monitorEditor) { e.configure(40, 20) },
			"theme":  func(e *monitorEditor) { e.style(paletteFor(themeRust)) },
		} {
			t.Run(name+map[bool]string{true: "_secret", false: "_text"}[secret], func(t *testing.T) {
				copy := original
				width, height := original.area.Width(), original.area.Height()
				mutate(&copy)
				// Bubbles itself shallow-copies internal viewport/cache pointers.
				// Preserve its value semantics, without sharing additional state.
				if original.Value() != "original draft" || original.Focused() || original.area.Width() != width || original.area.Height() != height || original.styleName != "HACKER" {
					t.Fatal("mutating an editor copy altered original widget state")
				}
			})
		}
	}
}

func TestMonitorGeometryPassMatchesUncached(t *testing.T) {
	for _, full := range []bool{false, true} {
		m, _ := promptTestModel()
		if !full {
			m.monitorContextDetail = ""
		}
		for _, size := range [][2]int{{60, 20}, {120, 40}, {200, 65}} {
			m.width, m.height = size[0], size[1]
			cached := m
			cached.geometry = &monitorGeometryCache{}
			for repeat := 0; repeat < 2; repeat++ {
				if !reflect.DeepEqual(cached.monitorArea(size[0], size[1]), m.monitorArea(size[0], size[1])) || cached.monitorDashboardLayout() != m.monitorDashboardLayout() {
					t.Fatal("cached geometry differs")
				}
			}
			_ = m.render()
			_ = m.monitorContextAt(4, 5)
			if m.geometry != nil {
				t.Fatal("cache escaped the read-only pass")
			}
		}
	}
}

func TestMonitorEditorConfigurationStableAndResponsive(t *testing.T) {
	e := newMonitorEditor()
	e.SetValue(strings.Repeat("wrapped text ", 30))
	e.configure(80, 30)
	e.style(paletteFor(themeHacker))
	before := e.View(paletteFor(themeHacker))
	e.configure(80, 30)
	e.style(paletteFor(themeHacker))
	if e.View(paletteFor(themeHacker)) != before {
		t.Fatal("unchanged configuration changed editor")
	}
	e.configure(40, 30)
	if e.width != 40 || e.area.Width() >= 40 {
		t.Fatal("resize not applied")
	}
	e.style(paletteFor(themeRust))
	if e.styleName != "RUST" || e.Value() != strings.Repeat("wrapped text ", 30) {
		t.Fatal("theme update lost draft")
	}
}

func TestWorkingComposerHasSingleAnimation(t *testing.T) {
	for _, inline := range []bool{false, true} {
		m, _ := turnTestModel()
		if inline {
			m, _ = inlineTurnTestModel()
			m.focusMonitorPrompt()
		}
		m.monitorState = monitorRunning
		m.monitorSessionData[0].active = true
		m.monitorSessionData[0].working = true
		m.monitorSessionData[0].attention = codex.SessionAttentionNone
		m.monitorPrompt.notice = i18n.Text("Text sent ...")
		m.monitorPrompt.noticeUntil = time.Now().Add(time.Minute)
		for phase := 0; phase < 4; phase++ {
			m.phase = phase
			out := ansi.Strip(m.renderMonitorPrompt(100, 30, paletteFor(m.theme)))
			if strings.Count(out, monitorDotWave(phase)) != 1 || !strings.Contains(out, m.monitorPrompt.notice) {
				t.Fatalf("expected one wave and plain notice: %s", out)
			}
		}
		m.monitorPrompt.noticeUntil = time.Now().Add(-time.Minute)
		if strings.Contains(m.renderMonitorPrompt(100, 30, paletteFor(m.theme)), m.monitorPrompt.notice) {
			t.Fatal("notice did not expire")
		}
		m.monitorState = monitorPaused
		if strings.Contains(m.renderMonitorPrompt(100, 30, paletteFor(m.theme)), monitorDotWave(m.phase)) {
			t.Fatal("paused observation animated")
		}
	}
}

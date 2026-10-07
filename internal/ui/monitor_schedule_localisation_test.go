package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

// Invoked by the locale subprocess suite as well as the ordinary UI suite.
func TestScheduleLocalisedStatusAndHint(t *testing.T) {
	m, _ := scheduledTestModel(t)
	s := &m.monitorSessionData[0]
	s.working = false
	s.attention = codex.SessionAttentionComplete
	want := i18n.Text("TRIGGER SET")
	if i18n.Code() != "en-GB" && want == "TRIGGER SET" {
		t.Fatal("trigger status fell back to English")
	}
	for name, rendered := range map[string]string{
		"badge":  m.renderMonitorSessionBadge(*s, 120, paletteFor(m.theme)),
		"detail": m.renderMonitorContextDetail(160, 40, paletteFor(m.theme)),
	} {
		if !strings.Contains(ansi.Strip(rendered), want) {
			t.Fatalf("%s missing translated trigger status %q", name, want)
		}
	}
	buttons, _ := m.monitorAttentionButtons(240, 1)
	found := false
	for _, button := range buttons {
		if button.action == "attention-trigger:"+s.id {
			found = true
			if !strings.Contains(button.label, want) {
				t.Fatalf("pill missing translated trigger status %q: %q", want, button.label)
			}
		}
	}
	if !found {
		t.Fatal("trigger pill missing")
	}
	m.scheduleUI.notice = "Pending trigger"
	layout := m.layoutDetailControlsBase(20, 7)
	if layout.kind != "notice" || layout.notice != i18n.Text("Ctrl+S: EDIT") {
		t.Fatalf("compact edit hint not translated: %+v", layout)
	}
}

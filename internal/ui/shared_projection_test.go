package ui

import (
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
	"testing"
)

func TestStatusLineDoesNotInventInputForWorkingOrIdle(t *testing.T) {
	m := Model{monitorState: monitorRunning}
	for _, working := range []bool{false, true} {
		s := monitorSession{id: "synthetic", active: true, working: working}
		want := "IDLE"
		if working {
			want = "WORKING"
		}
		if got := m.monitorStatusValuesFor(s)["run-state"]; got != i18n.Text(want) {
			t.Fatalf("working=%v: %q", working, got)
		}
	}
}

func TestMonitorQuotaIdentityDoesNotUseLabels(t *testing.T) {
	minutes := int64(300)
	a := codex.Meter{LimitID: "a", Bucket: "Same", Name: "5 HOURS", Window: codex.Window{WindowDurationMins: &minutes}}
	b := a
	b.LimitID = "b"
	if monitorQuotaKey(a) == monitorQuotaKey(b) {
		t.Fatal("distinct backend allowances collided")
	}
	b = a
	b.Bucket = "Renamed"
	b.Name = "Changed label"
	if monitorQuotaKey(a) != monitorQuotaKey(b) {
		t.Fatal("display change changed identity")
	}
}

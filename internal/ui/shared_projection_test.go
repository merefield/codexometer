package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
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

func TestMonitorQuotaRenameRefreshesLabelsWithoutRebasing(t *testing.T) {
	label := "Original"
	snapshot := monitorQuotaSnapshot(20, 1800000000)
	snapshot.RateLimits.LimitName = &label
	m := Model{monitorState: monitorRunning, monitorStartedAt: time.Unix(1_700_000_000, 0)}
	m.startMonitorQuotaSnapshot(snapshot)
	original := m.monitorQuotaWindows[0]

	label = "Renamed"
	snapshot.RateLimits.Primary.UsedPercent = 23
	m.syncMonitorQuotaSnapshot(snapshot)
	if len(m.monitorQuotaWindows) != 1 {
		t.Fatal("rename created a new quota window", m.monitorQuotaWindows)
	}
	window := m.monitorQuotaWindows[0]
	if window.key != original.key || window.baselineUsed != 20 || window.latestUsed != 23 ||
		!sameOptionalInt64(window.latestReset, original.latestReset) || window.partial || window.resetDetected || window.stale {
		t.Fatalf("rename changed accounting: before=%+v after=%+v", original, window)
	}
	if got := m.monitorQuotaReadout(); !strings.Contains(got, "RENAMED 5 HOURS +3PP") || strings.Contains(got, "ORIGINAL") {
		t.Fatalf("account quota label did not refresh: %q", got)
	}
	if got := m.monitorSessionQuotaEstimate(1); !strings.Contains(got, "RENAMED 5H ~3PP") || strings.Contains(got, "ORIGINAL") {
		t.Fatalf("session estimate label did not refresh: %q", got)
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

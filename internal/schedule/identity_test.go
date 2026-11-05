package schedule

import (
	"github.com/merefield/codexometer/internal/codex"
	"testing"
)

func TestCoverageCannotHideMissingSameNamedWindow(t *testing.T) {
	minutes := int64(300)
	w := codex.Window{WindowDurationMins: &minutes}
	s := codex.Snapshot{RateLimits: codex.RateLimitSnapshot{Primary: &w, Secondary: &w}}
	j := Job{Windows: WindowKeys(s)}
	if !Covers(j, s) {
		t.Fatal("unchanged inventory not covered")
	}
	s.RateLimits.Primary = nil
	if Covers(j, s) {
		t.Fatal("missing primary was mistaken for identically named secondary")
	}
	// Label changes and normal resets are not disappearance, but a different
	// allowance duration must not silently inherit a pending trigger's coverage.
	s.RateLimits.Primary = &w
	changed := minutes + 60
	s.RateLimits.Secondary = &codex.Window{WindowDurationMins: &changed}
	if Covers(j, s) {
		t.Fatal("changed allowance definition inherited coverage")
	}
}

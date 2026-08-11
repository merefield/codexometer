package version

import "testing"

func TestCurrentReportsMaintainedFallbackForSourceBuild(t *testing.T) {
	got := Current()
	if got != "0.1.0" {
		t.Fatalf("Current() = %q, want 0.1.0", got)
	}
}

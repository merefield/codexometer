package version

import (
	"runtime/debug"
	"testing"
)

func TestCurrentUsesBuildOverride(t *testing.T) {
	previous := buildVersion
	buildVersion = "v0.7.8"
	t.Cleanup(func() { buildVersion = previous })

	if got := Current(); got != "0.7.8" {
		t.Fatalf("Current() = %q, want 0.7.8", got)
	}
}

func TestNormalizeRejectsDevelopmentMarker(t *testing.T) {
	if got := normalize("(devel)"); got != "" {
		t.Fatalf("normalize((devel)) = %q", got)
	}
}

func TestDevelopmentIncludesRevisionAndDirtyState(t *testing.T) {
	got := development([]debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef"},
		{Key: "vcs.modified", Value: "true"},
	})
	if got != "0.7.8-dev+0123456789ab.dirty" {
		t.Fatalf("development() = %q", got)
	}
}

func TestDevelopmentNeedsARevision(t *testing.T) {
	if got := development([]debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}); got != "" {
		t.Fatalf("development() without revision = %q", got)
	}
}

package version

import (
	"runtime/debug"
	"testing"
)

func TestCurrentUsesBuildOverride(t *testing.T) {
	previous := buildVersion
	buildVersion = "v0.11.0"
	t.Cleanup(func() { buildVersion = previous })

	if got := Current(); got != "0.11.0" {
		t.Fatalf("Current() = %q, want 0.11.0", got)
	}
}

func TestNormalizeRejectsDevelopmentMarker(t *testing.T) {
	if got := normalize("(devel)"); got != "" {
		t.Fatalf("normalize((devel)) = %q", got)
	}
}

func TestNormalizeStripsTagPrefix(t *testing.T) {
	if got := normalize(" v1.2.3 "); got != "1.2.3" {
		t.Fatalf("normalize(v1.2.3) = %q", got)
	}
}

func TestVCSFallbackIncludesRevisionAndDirtyState(t *testing.T) {
	got := vcsFallback([]debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef"},
		{Key: "vcs.modified", Value: "true"},
	})
	if got != "0.11.0-dev+0123456789ab.dirty" {
		t.Fatalf("vcsFallback() = %q", got)
	}
}

func TestVCSFallbackNeedsARevision(t *testing.T) {
	if got := vcsFallback([]debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}); got != "" {
		t.Fatalf("vcsFallback() without revision = %q", got)
	}
}

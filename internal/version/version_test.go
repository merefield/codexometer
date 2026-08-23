package version

import (
	"runtime/debug"
	"testing"
)

func TestCurrentUsesBuildOverride(t *testing.T) {
	previous := buildVersion
	buildVersion = "v1.2.3"
	t.Cleanup(func() { buildVersion = previous })

	if got := Current(); got != "1.2.3" {
		t.Fatalf("Current() = %q, want 1.2.3", got)
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
	want := maintainedVersion() + "-dev+0123456789ab.dirty"
	if got != want {
		t.Fatalf("vcsFallback() = %q, want %q", got, want)
	}
}

func TestVCSFallbackNeedsARevision(t *testing.T) {
	if got := vcsFallback([]debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}); got != "" {
		t.Fatalf("vcsFallback() without revision = %q", got)
	}
}

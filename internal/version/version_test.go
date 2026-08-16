package version

import "testing"

func TestCurrentReportsMaintainedFallbackForSourceBuild(t *testing.T) {
	got := Current()
	if got != "0.7.5" {
		t.Fatalf("Current() = %q, want 0.7.5", got)
	}
}

func TestSelectVersion(t *testing.T) {
	tests := []struct {
		name     string
		module   string
		fallback string
		want     string
	}{
		{name: "tagged install", module: "v1.2.3", fallback: "0.2.0", want: "1.2.3"},
		{name: "development build", module: "(devel)", fallback: "v0.2.0", want: "0.2.0"},
		{name: "source pseudo-version", module: "v0.0.0-20260811151632-815179a0b2e7+dirty", fallback: "0.2.0", want: "0.2.0"},
		{name: "pre-release tag", module: "v1.2.3-rc.1", fallback: "0.2.0", want: "1.2.3-rc.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selectVersion(test.module, test.fallback); got != test.want {
				t.Fatalf("selectVersion(%q, %q) = %q, want %q", test.module, test.fallback, got, test.want)
			}
		})
	}
}

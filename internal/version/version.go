// Package version owns Codexometer's release identity.
package version

import (
	"runtime/debug"
	"strings"
	"time"
)

// Fallback is the version reported by binaries built directly from a source
// checkout. Release automation may override it with:
//
//	-X github.com/merefield/codexometer/internal/version.Fallback=1.2.3
var Fallback = "0.7.6"

// Current returns the module version embedded by Go for tagged installs, or
// Fallback for ordinary source builds. The leading tag prefix is omitted so
// callers can choose how to present it.
func Current() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return selectVersion(info.Main.Version, Fallback)
	}
	return selectVersion("", Fallback)
}

func selectVersion(moduleVersion, fallback string) string {
	moduleVersion = strings.TrimSpace(moduleVersion)
	if moduleVersion != "" && moduleVersion != "(devel)" && !isPseudoVersion(moduleVersion) {
		return strings.TrimPrefix(moduleVersion, "v")
	}
	return strings.TrimPrefix(strings.TrimSpace(fallback), "v")
}

func isPseudoVersion(value string) bool {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), "-")
	if len(parts) < 3 {
		return false
	}
	timestamp, revision := parts[len(parts)-2], parts[len(parts)-1]
	if clean, _, found := strings.Cut(revision, "+"); found {
		revision = clean
	}
	if len(timestamp) != 14 || len(revision) < 7 {
		return false
	}
	if _, err := time.Parse("20060102150405", timestamp); err != nil {
		return false
	}
	for _, character := range revision {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

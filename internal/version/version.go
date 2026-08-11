// Package version owns Codexometer's release identity.
package version

import (
	"runtime/debug"
	"strings"
)

// Fallback is the version reported by binaries built directly from a source
// checkout. Release automation may override it with:
//
//	-X github.com/merefield/codexometer/internal/version.Fallback=1.2.3
var Fallback = "0.1.0"

// Current returns the module version embedded by Go for tagged installs, or
// Fallback for ordinary source builds. The leading tag prefix is omitted so
// callers can choose how to present it.
func Current() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		moduleVersion := strings.TrimSpace(info.Main.Version)
		if moduleVersion != "" && moduleVersion != "(devel)" {
			return strings.TrimPrefix(moduleVersion, "v")
		}
	}
	return strings.TrimPrefix(strings.TrimSpace(Fallback), "v")
}

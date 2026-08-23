// Package version owns Codexometer's release identity.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is the source fallback. Tagged module installs and builds made with
// an injected buildVersion use their embedded version instead.
const Version = "0.10.0"

// buildVersion may be populated at link time from the nearest Git tag.
var buildVersion string

// Current returns a display-ready version without the conventional v prefix
// used by Go module tags. It prefers an injected version, then Go's embedded
// module version, then a VCS-based development identity.
func Current() string {
	if value := normalize(buildVersion); value != "" {
		return value
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if value := normalize(info.Main.Version); value != "" {
			return value
		}
		if value := vcsFallback(info.Settings); value != "" {
			return value
		}
	}
	return Version
}

// vcsFallback identifies a local checkout when Go reports Main.Version as
// "(devel)". It is intentionally distinct from a Go pseudo-version: Go's own
// embedded module version remains authoritative whenever one is available.
func vcsFallback(settings []debug.BuildSetting) string {
	revision := ""
	modified := false
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	value := Version + "-dev+" + revision
	if modified {
		value += ".dirty"
	}
	return value
}

func normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(value, "v")
}

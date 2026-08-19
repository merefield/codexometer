// Package version owns Codexometer's release identity.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is the development fallback. Tagged module installs and builds made
// with an injected buildVersion use their embedded semantic version instead.
const Version = "0.7.8"

// buildVersion may be populated at link time from the nearest Git tag.
var buildVersion string

// Current returns a display-ready version without the conventional v prefix
// used by Go module tags. Untagged source builds include their VCS revision and
// dirty state when Go embeds that information.
func Current() string {
	if value := normalize(buildVersion); value != "" {
		return value
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if value := normalize(info.Main.Version); value != "" {
			return value
		}
		if value := development(info.Settings); value != "" {
			return value
		}
	}
	return Version
}

func development(settings []debug.BuildSetting) string {
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

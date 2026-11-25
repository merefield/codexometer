package codex

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A reported directory belongs to the server or execution environment. Its
// syntax does not establish that it is safe to resolve on the client host.
func reportedSessionDirectory(cwd string) bool {
	return len(cwd) <= 4096 && (strings.HasPrefix(cwd, "/") || windowsSessionDirectory(cwd)) &&
		utf8.ValidString(cwd) &&
		strings.IndexFunc(cwd, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) < 0
}

func windowsSessionDirectory(cwd string) bool {
	if len(cwd) >= 3 && cwd[1] == ':' {
		letter := cwd[0] | 0x20
		return 'a' <= letter && letter <= 'z' && (cwd[2] == '\\' || cwd[2] == '/')
	}
	if unc, ok := strings.CutPrefix(cwd, `\\`); ok {
		host, rest, ok := strings.Cut(strings.ReplaceAll(unc, `\`, "/"), "/")
		share, _, _ := strings.Cut(rest, "/")
		return ok && host != "" && share != ""
	}
	return false
}

// SessionDirectoryBase provides a short label without using the client OS's
// path rules or changing what is retained in the full directory readout.
func SessionDirectoryBase(cwd string) string {
	if windowsSessionDirectory(cwd) {
		cwd = strings.ReplaceAll(cwd, `\`, "/")
	}
	return path.Base(cwd)
}

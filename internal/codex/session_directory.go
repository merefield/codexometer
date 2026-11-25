package codex

import (
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// A reported directory belongs to the server or execution environment. Its
// syntax does not establish that it is safe to resolve on the client host.
func reportedSessionDirectory(cwd string) bool {
	return len(cwd) <= 4096 && (strings.HasPrefix(cwd, "/") || windowsSessionDirectory(cwd)) &&
		utf8.ValidString(cwd) && !strings.ContainsRune(cwd, 0)
}

// SessionDirectoryDisplay drops terminal sequences and quotes non-printing
// content without changing the path used for filesystem checks or RPCs.
func SessionDirectoryDisplay(cwd string) string {
	cwd = ansi.Strip(cwd)
	if !utf8.ValidString(cwd) || strings.ContainsFunc(cwd, func(r rune) bool { return !strconv.IsPrint(r) }) {
		return strconv.Quote(cwd)
	}
	return cwd
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
	cwd = ansi.Strip(cwd) // A terminal sequence may itself contain path separators.
	if windowsSessionDirectory(cwd) {
		cwd = strings.ReplaceAll(cwd, `\`, "/")
	}
	return path.Base(cwd)
}

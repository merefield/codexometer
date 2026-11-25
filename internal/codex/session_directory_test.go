package codex

import (
	"strings"
	"testing"
)

func TestReportedSessionDirectory(t *testing.T) {
	for _, tc := range []struct{ cwd, base string }{
		{"/work/two  spaces 界/", "two  spaces 界"},
		{`/work/names\with\backslashes`, `names\with\backslashes`},
		{`D:\work\server repo\`, "server repo"},
		{`d:/work/server repo`, "server repo"},
		{`\\build-host\share\repo`, "repo"},
		{`\\?\UNC\build-host\share\repo`, "repo"},
	} {
		if got := SessionDirectoryBase(tc.cwd); !reportedSessionDirectory(tc.cwd) || got != tc.base {
			t.Errorf("reported directory %q: basename %q, want %q", tc.cwd, got, tc.base)
		}
	}
	for _, cwd := range []string{"", "relative", `D:relative`, `\relative`, `\\host`, "/invalid\x00", "/invalid\xff", "/" + strings.Repeat("x", 4096)} {
		if reportedSessionDirectory(cwd) {
			t.Errorf("accepted invalid reported path (%d bytes): %.30q", len(cwd), cwd)
		}
	}
}

func TestSessionDirectoryDisplayEscapesNonPrintingCharacters(t *testing.T) {
	for raw, expected := range map[string]string{
		"/work/two\u200dwords": `"/work/two\u200dwords"`,
		"/work/\x1b[2J\tX\n":   `"/work/\tX\n"`,
		`D:\work\two  words`:   `D:\work\two  words`,
	} {
		if got := SessionDirectoryDisplay(raw); got != expected || !reportedSessionDirectory(raw) {
			t.Fatalf("display of %q: %q, want %q", raw, got, expected)
		}
	}
}

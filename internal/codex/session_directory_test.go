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
	for _, cwd := range []string{"", "relative", `D:relative`, `\relative`, `\\host`, "D:\\dir\nnew line", "/invalid\x1b", "/invalid\u202e", "/invalid\xff", "/" + strings.Repeat("x", 4096)} {
		if reportedSessionDirectory(cwd) {
			t.Errorf("accepted invalid reported path (%d bytes): %.30q", len(cwd), cwd)
		}
	}
}

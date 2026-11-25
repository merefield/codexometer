package web

import (
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestSessionDirectoryProjectionEscapesDisplayButKeepsControlPath(t *testing.T) {
	s := newStore()
	s.state.Control = true
	const cwd = "/work/two\u200dwords\nline"
	const shown = `"/work/two\u200dwords\nline"`
	s.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "session", WorkingDirectory: cwd}}}, nil, time.Now())
	got := s.state.Sessions[0]
	if got.Directory != shown || got.StatusLine["current-dir"] != shown || s.directories["session"] != cwd {
		t.Fatal("browser should escape only the displayed path", got, s.directories)
	}
}

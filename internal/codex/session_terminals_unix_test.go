//go:build unix

package codex

import (
	"errors"
	"testing"
)

func TestSessionTerminalsOnlyInspectSubscribedThread(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{"thread/backgroundTerminals/list": {
		"data":       []any{map[string]any{"processId": "p1", "command": "\x1b[31mgo test ./...\x1b[0m", "cwd": "/work", "osPid": 98}},
		"nextCursor": "more",
	}}
	if _, ok := p.Fetch(t.Context(), []string{"one"}); !ok {
		t.Fatal("shared daemon unavailable")
	}
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	if _, err := c.InspectSessionTerminals(t.Context(), "two"); !errors.Is(err, ErrSessionTerminals) {
		t.Fatal("inspected an unsubscribed session", err)
	}
	result, err := c.InspectSessionTerminals(t.Context(), "one")
	if err != nil || !result.More || len(result.Terminals) != 1 || result.Terminals[0] != (SessionTerminal{"p1", "go test ./...", "/work", 98}) {
		t.Fatal(result, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.terminalReads) != 1 || f.terminalReads[0]["threadId"] != "one" || len(f.writes)+len(f.goalWrites) != 0 {
		t.Fatal("not a session-scoped read", f.terminalReads, f.writes)
	}
}

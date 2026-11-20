//go:build unix

package codex

import (
	"errors"
	"reflect"
	"testing"
)

func TestNewSessionUsesNativeDefaultsAndTargetsFirstTurn(t *testing.T) {
	p, f := newQuotaDaemon(t)
	cwd := t.TempDir()
	f.catalogues = map[string]map[string]any{
		"initialize":   {"userAgent": "codex/0.171.0 (Linux)"},
		"account/read": {"account": map[string]string{"type": "chatgpt", "email": "test@example.test"}},
		"thread/start": {"cwd": cwd, "thread": map[string]string{"id": "created"}},
		"turn/start":   {"turn": map[string]string{"id": "turn-one"}},
	}
	ctx := t.Context()
	if err := p.ensureConnected(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	scope := p.savedScope
	p.mu.Unlock()
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	for _, input := range []struct {
		scope, dir, prompt string
		want               error
	}{
		{"wrong", cwd, "first", ErrSessionStartUnavailable},
		{scope, "relative", "first", ErrSessionStartDirectory},
		{scope, cwd, "/resume", ErrSessionStartPrompt},
		{scope, cwd, "   ", ErrSessionStartPrompt},
	} {
		if id, err := c.StartSession(ctx, input.scope, input.dir, input.prompt); id != "" || !errors.Is(err, input.want) {
			t.Fatal(id, err)
		}
	}
	if id, err := c.StartSession(ctx, scope, cwd, "Hello Codex"); id != "created" || err != nil {
		t.Fatal(id, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(f.starts, []map[string]any{{"cwd": cwd}}) {
		t.Fatal("thread/start should only specify the directory", f.starts)
	}
	want := []map[string]any{{"threadId": "created", "input": []any{map[string]any{"type": "text", "text": "Hello Codex"}}}}
	if !reflect.DeepEqual(f.turnStarts, want) {
		t.Fatal("must send the first prompt exactly once to the new thread", f.turnStarts)
	}
}

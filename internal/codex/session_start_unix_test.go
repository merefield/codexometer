//go:build unix

package codex

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func startFixture(t *testing.T) (Client, *quotaDaemonFixture, string, string) {
	t.Helper()
	p, f := newQuotaDaemon(t)
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trusted := map[string]any{"projects": map[string]any{cwd: map[string]string{"trust_level": "trusted"}}}
	f.catalogues = map[string]map[string]any{
		"initialize":   {"userAgent": "codex/0.171.0 (Linux)"},
		"account/read": {"account": map[string]string{"type": "chatgpt", "email": "test@example.test"}},
		"config/read": {"config": trusted, "layers": []any{
			map[string]any{"name": map[string]string{"type": "user"}, "config": trusted},
		}},
		"thread/start": {"cwd": cwd, "thread": map[string]string{"id": "created"}},
		"turn/start":   {"turn": map[string]string{"id": "turn-one"}},
	}
	if err := p.ensureConnected(t.Context()); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	scope := p.savedScope
	p.mu.Unlock()
	return Client{LiveUsage: &LiveUsageReader{statusProvider: p}}, f, cwd, scope
}

func TestNewSessionUsesNativeDefaultsAndTargetsFirstTurn(t *testing.T) {
	c, f, cwd, scope := startFixture(t)
	ctx := t.Context()
	for _, input := range []struct {
		scope, dir, prompt string
		want               error
	}{
		{"wrong", cwd, "first", ErrSessionStartUnavailable},
		{scope, "relative", "first", ErrSessionStartDirectory},
		{scope, cwd, "/resume", ErrSessionStartCommand},
		{scope, cwd, "   ", ErrSessionStartPrompt},
		{scope, cwd, "hidden\x1b[0m", ErrSessionStartPrompt},
	} {
		if id, err := c.StartSession(ctx, input.scope, input.dir, input.prompt); id != "" || !errors.Is(err, input.want) {
			t.Fatal(id, err)
		}
	}
	const prompt = "Explain this code:\n\treturn true"
	f.mu.Lock()
	f.startDelay = 2250 * time.Millisecond
	f.mu.Unlock()
	if id, err := c.StartSession(ctx, scope, cwd, prompt); id != "created" || err != nil {
		t.Fatal(id, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(f.starts, []map[string]any{{"cwd": cwd}}) {
		t.Fatal("thread/start should only specify the directory", f.starts)
	}
	want := []map[string]any{{"threadId": "created", "input": []any{map[string]any{"type": "text", "text": prompt}}}}
	if !reflect.DeepEqual(f.turnStarts, want) {
		t.Fatal("must send the first prompt exactly once to the new thread", f.turnStarts)
	}
}

func TestNewSessionRequiresExplicitEffectiveTrust(t *testing.T) {
	for _, mode := range []string{"missing", "disabled", "overridden", "project-layer-only", "parent-only"} {
		t.Run(mode, func(t *testing.T) {
			c, f, cwd, scope := startFixture(t)
			if mode == "parent-only" {
				cwd = filepath.Join(cwd, "subdir")
				if err := os.Mkdir(cwd, 0700); err != nil {
					t.Fatal(err)
				}
			}
			f.mu.Lock()
			reply := f.catalogues["config/read"]
			switch mode {
			case "missing":
				delete(f.catalogues, "config/read")
			case "disabled":
				reply["layers"] = append(reply["layers"].([]any), map[string]any{
					"name": map[string]string{"type": "project"}, "disabledReason": "restricted",
				})
			case "overridden":
				reply["config"] = map[string]any{"projects": map[string]any{cwd: map[string]string{"trust_level": "untrusted"}}}
			case "project-layer-only":
				reply["layers"] = []any{map[string]any{"name": map[string]string{"type": "project"}, "config": reply["config"]}}
			}
			f.mu.Unlock()
			if id, err := c.StartSession(t.Context(), scope, cwd, "first"); id != "" || !errors.Is(err, ErrSessionStartTrust) {
				t.Fatal("unverified folder accepted", id, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.starts) != 0 || len(f.turnStarts) != 0 {
				t.Fatal("unverified trust caused a write")
			}
		})
	}
}

func TestNewSessionKnownNativeRejection(t *testing.T) {
	c, f, cwd, scope := startFixture(t)
	f.mu.Lock()
	delete(f.catalogues, "thread/start")
	f.mu.Unlock()
	if id, err := c.StartSession(t.Context(), scope, cwd, "first"); id != "" || !errors.Is(err, ErrSessionStartRejected) {
		t.Fatal("definite error reported as unknown outcome", id, err)
	}
}

func TestNewSessionFirstTurnRejectionKeepsCreatedThread(t *testing.T) {
	for _, tc := range []struct {
		code int
		want error
	}{
		{-32600, ErrSessionFirstPrompt},
		{-32603, ErrSessionStartUncertain},
	} {
		c, f, cwd, scope := startFixture(t)
		f.mu.Lock()
		f.turnStartError = map[string]any{"code": tc.code, "message": "test turn rejection"}
		f.mu.Unlock()
		if id, err := c.StartSession(t.Context(), scope, cwd, "first"); id != "created" || !errors.Is(err, tc.want) {
			t.Fatalf("code %d: id=%q err=%v", tc.code, id, err)
		}
	}
}

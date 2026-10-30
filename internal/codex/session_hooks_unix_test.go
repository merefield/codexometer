//go:build unix

package codex

import (
	"context"
	"strings"
	"testing"
)

func TestSessionHooksUseThreadEnvironmentAndOlderServerFallback(t *testing.T) {
	for _, mode := range []string{"thread", "empty-inventory", "empty-thread-hooks", "ignored-selector", "rejected-selector", "real-thread-error"} {
		t.Run(mode, func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			row := func(cwd, key, environment string) map[string]any {
				return map[string]any{"data": []any{map[string]any{"cwd": cwd, "hooks": []any{
					map[string]any{"key": key, "eventName": "PreToolUse", "handlerType": "command", "command": "echo hello", "enabled": true, "environmentId": environment},
				}}}}
			}
			f.catalogues = map[string]map[string]any{"hooks/list": row("/work", "directory-hook", "")}
			f.threadHooks = row("/work", "environment-hook", "remote-env")
			switch mode {
			case "empty-inventory":
				f.threadHooks = map[string]any{"data": []any{}}
			case "empty-thread-hooks":
				f.threadHooks = map[string]any{"data": []any{map[string]any{"cwd": "/work", "hooks": []any{}}}}
			case "ignored-selector":
				f.threadHooks = row("/server-default-cwd", "wrong-hook", "")
			case "rejected-selector":
				f.hookError = map[string]any{"code": -32602, "message": "unknown field `threadId`"}
			case "real-thread-error":
				f.hookError = map[string]any{"code": -32600, "message": "thread not found: one"}
			}
			m, err := p.SessionCommands(context.Background(), "one", "hooks")
			if mode == "real-thread-error" {
				if err == nil {
					t.Fatal("hid a real thread failure behind directory discovery")
				}
			} else if strings.HasPrefix(mode, "empty-") {
				if err != nil || len(m.Choices) != 0 {
					t.Fatalf("successful empty thread inventory was replaced with directory hooks: %+v, %v", m, err)
				}
			} else {
				want := "directory-hook"
				if mode == "thread" {
					want = "environment-hook"
				}
				if err != nil || len(m.Choices) != 1 || m.Choices[0].Label != want || m.Choices[0].Action {
					t.Fatalf("wrong read-only inventory: %+v, %v", m, err)
				}
				if mode == "thread" && !strings.Contains(m.Choices[0].Help, "Environment: remote-env") {
					t.Fatal("environment provenance missing", m)
				}
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			wantReads := 2
			if mode == "thread" || mode == "real-thread-error" || strings.HasPrefix(mode, "empty-") {
				wantReads = 1
			}
			if len(f.hookReads) != wantReads || f.hookReads[0]["threadId"] != "one" || f.hookReads[0]["cwds"] != nil {
				t.Fatal("incorrect thread-scoped request/fallback", f.hookReads)
			}
			if wantReads == 2 {
				last := f.hookReads[1]
				cwds, ok := last["cwds"].([]any)
				if !ok || len(cwds) != 1 || cwds[0] != "/work" || last["threadId"] != nil {
					t.Fatal("directory fallback lost session scope", last)
				}
			}
			if len(f.writes) != 0 {
				t.Fatal("browsing hooks changed session settings")
			}
		})
	}
}

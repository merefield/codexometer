//go:build unix

package codex

import (
	"context"
	"strings"
	"testing"
)

func TestSessionCommandRejectsProviderChangeDuringCatalogueRead(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		for _, path := range []string{"model/small", "tier/flex"} {
			for _, execution := range []bool{false, true} {
				name := map[bool]string{false: "plain", true: "zstd"}[compressed] + "/" + path + "/" + map[bool]string{false: "browse", true: "execute"}[execution]
				t.Run(name, func(t *testing.T) {
					p, f := newQuotaDaemonTransport(t, compressed)
					f.mu.Lock()
					f.sessions["one"] = QuotaSession{ID: "one", Model: "small", ModelProvider: "openai", Effort: "medium"}
					f.mu.Unlock()
					ctx := context.Background()
					menu, err := p.SessionCommands(ctx, "one", path)
					if err != nil || len(menu.Choices) == 0 || !menu.Choices[0].Action {
						t.Fatal("initial catalogue unavailable", menu, err)
					}
					f.mu.Lock()
					// Run under the fixture lock after selecting the response,
					// before replying. No timing sleeps or real data races.
					f.modelReadHook = func() {
						s := f.sessions["one"]
						s.ModelProvider = "gateway"
						f.sessions["one"] = s
						f.modelReadHook = nil
					}
					f.mu.Unlock()
					if execution {
						err = p.ExecuteSessionCommand(ctx, "one", path, menu.Revision, menu.Choices[0].ID)
					} else {
						_, err = p.SessionCommands(ctx, "one", path)
					}
					if err == nil || !strings.Contains(err.Error(), "settings changed") {
						t.Fatal("provider changed during discovery without rejection", err)
					}
					f.mu.Lock()
					defer f.mu.Unlock()
					if len(f.writes) != 0 {
						t.Fatal("stale provider choice mutated the session", f.writes)
					}
				})
			}
		}
	}
}

func TestQuotaProfileRejectsSettingsChangeDuringCatalogueValidation(t *testing.T) {
	for _, field := range []string{"provider", "model", "effort", "tier"} {
		t.Run(field, func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			ctx := context.Background()
			if err := p.ensureConnected(ctx); err != nil {
				t.Fatal(err)
			}
			target, err := p.readQuotaSession(ctx, "one")
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.modelReadHook = func() {
				s := f.sessions["one"]
				switch field {
				case "provider":
					s.ModelProvider = "gateway"
				case "model":
					s.Model = "elsewhere"
				case "effort":
					s.Effort = "low"
				case "tier":
					tier := "priority"
					s.Tier = &tier
				}
				f.sessions["one"] = s
				f.modelReadHook = nil
			}
			f.mu.Unlock()
			n, err := p.ApplyQuotaProfile(ctx, []QuotaSession{target}, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "fast"})
			if n != 0 || err == nil || !strings.Contains(err.Error(), "settings changed") {
				t.Fatal("catalogue validation accepted a changed review snapshot", n, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.writes) != 0 {
				t.Fatal("profile mutated before detecting settings drift", f.writes)
			}
		})
	}
}

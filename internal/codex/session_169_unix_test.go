//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func providerCatalogue(model, effort, tier string) map[string]any {
	return map[string]any{"data": []any{map[string]any{
		"model": model, "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": effort}},
		"serviceTiers": []any{map[string]any{"id": tier, "name": "Fast"}},
	}}}
}

func TestProviderCataloguePaginationRetainsSelector(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.mu.Lock()
	f.providerModels = map[string]map[string]any{
		"gateway":      {"data": []any{}, "nextCursor": "next"},
		"gateway/next": providerCatalogue("gateway-model", "high", "gateway-fast"),
	}
	f.mu.Unlock()
	ctx := context.Background()
	if err := p.ensureConnected(ctx); err != nil {
		t.Fatal(err)
	}
	models, err := p.commandModels(ctx, "gateway")
	if err != nil || len(models) != 1 || models[0].Model != "gateway-model" {
		t.Fatal("model discovery lost paged provider catalogue", models, err)
	}
	step, err := p.validateQuotaStep(ctx, QuotaStep{Model: "gateway-model", Effort: "high", ServiceTier: "fast"}, "gateway")
	if err != nil || step.ServiceTier != "gateway-fast" {
		t.Fatal("quota validation lost paged provider catalogue", step, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.modelReads) != 4 {
		t.Fatal("expected two pages for each reader", f.modelReads)
	}
	for _, params := range f.modelReads {
		if params["modelProvider"] != "gateway" {
			t.Fatal("pagination dropped provider selector", params)
		}
	}
}

func TestProviderScopedCommandsAndCache(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "zstd"}[compressed], func(t *testing.T) {
			p, f := newQuotaDaemonTransport(t, compressed)
			f.mu.Lock()
			f.sessions["one"] = QuotaSession{ID: "one", Model: "small", ModelProvider: "openai", Effort: "medium"}
			f.sessions["two"] = QuotaSession{ID: "two", Model: "gateway-model", ModelProvider: "gateway", Effort: "high"}
			f.providerModels = map[string]map[string]any{
				"openai":  providerCatalogue("small", "medium", "priority"),
				"gateway": providerCatalogue("gateway-model", "high", "gateway-fast"),
			}
			f.mu.Unlock()
			ctx := context.Background()
			if _, ok := p.Fetch(ctx, []string{"one", "two"}); !ok {
				t.Fatal("daemon unavailable")
			}
			client := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
			for _, tc := range []struct{ id, model, tier string }{{"one", "small", "priority"}, {"two", "gateway-model", "gateway-fast"}} {
				menu, err := client.SessionCommands(ctx, tc.id, "model")
				if err != nil || len(menu.Choices) != 1 || menu.Choices[0].Label != tc.model {
					t.Fatalf("wrong catalogue for %s: %+v %v", tc.id, menu, err)
				}
				root, err := client.SessionCommands(ctx, tc.id, "")
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, option := range root.Choices {
					found = found || option.Label == "/fast" && option.Next == "tier/"+tc.tier
				}
				if !found {
					t.Fatalf("speed alias used another provider's catalogue: %+v", root)
				}
			}
			old, err := client.SessionCommands(ctx, "one", "model/small")
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			// Same model/effort names, but a different provider: old options
			// must not stay actionable merely because their labels match.
			changed := f.sessions["one"]
			changed.ModelProvider = "gateway"
			f.sessions["one"] = changed
			f.providerModels["gateway"] = providerCatalogue("small", "medium", "gateway-fast")
			f.mu.Unlock()
			p.handleNotification("thread/settings/updated", json.RawMessage(`{"threadId":"one","threadSettings":{"model":"small","modelProvider":"gateway","effort":"medium","serviceTier":null}}`))
			if _, ok := client.CachedSessionCommands("one", "model/small"); ok {
				t.Fatal("provider change retained cached action")
			}
			if err := p.ExecuteSessionCommand(ctx, "one", old.Path, old.Revision, old.Choices[0].ID); err == nil {
				t.Fatal("provider change accepted an old review")
			}
			f.mu.Lock()
			writes := len(f.writes)
			delete(f.providerModels, "gateway")
			f.mu.Unlock()
			if writes != 0 {
				t.Fatal("stale review wrote settings")
			}
			if _, err := p.SessionCommands(ctx, "one", "model"); err == nil {
				t.Fatal("failed provider lookup fell back to default catalogue")
			}
		})
	}
}

func TestProviderScopedQuotaProfiles(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.mu.Lock()
	f.sessions = map[string]QuotaSession{"one": {ID: "one", Model: "old", ModelProvider: "gateway", Effort: "high"}}
	f.providerModels = map[string]map[string]any{"gateway": providerCatalogue("new", "medium", "gateway-fast")}
	f.notify = true
	f.mu.Unlock()
	ctx := context.Background()
	step := QuotaStep{Model: "new", Effort: "medium", ServiceTier: "fast"}
	resolved, err := p.ResolveQuotaStep(ctx, step)
	if err != nil || resolved.ServiceTier != "gateway-fast" {
		t.Fatal("resolved against default instead of loaded provider", resolved, err)
	}
	sessions, err := p.QuotaSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].ModelProvider != "gateway" {
		t.Fatal("lost session provider", sessions, err)
	}
	if n, err := p.ApplyQuotaProfile(ctx, sessions, step); n != 1 || err != nil {
		t.Fatal("could not apply provider's supported model/tier", n, err)
	}
	current, err := p.readQuotaSession(ctx, "one")
	if err != nil || current.ModelProvider != "gateway" || current.Model != "new" || current.Tier == nil || *current.Tier != "gateway-fast" {
		t.Fatal(current, err)
	}
	f.mu.Lock()
	f.sessions["two"] = QuotaSession{ID: "two", Model: "old", ModelProvider: "openai", Effort: "high"}
	f.providerModels["openai"] = providerCatalogue("new", "medium", "priority")
	f.mu.Unlock()
	if _, err := p.ResolveQuotaStep(ctx, step); err == nil || !strings.Contains(err.Error(), "providers disagree") {
		t.Fatal("ambiguous cross-provider speed alias was guessed", err)
	}
	// An old review must not apply to a different provider, even when the
	// model and reasoning names happen to be identical.
	f.mu.Lock()
	before := len(f.writes)
	current.ModelProvider = "openai"
	f.sessions["one"] = current
	f.mu.Unlock()
	if n, err := p.ApplyQuotaProfile(ctx, []QuotaSession{{ID: "one", Model: current.Model, ModelProvider: "gateway", Effort: current.Effort, Tier: current.Tier}}, step); n != 0 || err == nil {
		t.Fatal("provider drift bypassed review", n, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != before {
		t.Fatal("provider drift wrote settings")
	}
}

func TestAttachedSideChatsRemainIndependent(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.mu.Lock()
	f.attachments = map[string]string{"two": "one"}
	f.mu.Unlock()
	snapshot, ok := p.Fetch(context.Background(), []string{"one", "two"})
	if !ok || snapshot.AttachedThreads["two"] != "one" {
		t.Fatal("lost attached-chat relationship", snapshot, ok)
	}
	now := time.Now()
	r := &LiveUsageReader{daemonAttachments: snapshot.AttachedThreads, files: map[string]*rolloutCursor{
		"one":   {threadID: "one", lastModified: now, observedTokens: 10},
		"two":   {threadID: "two", lastModified: now, observedTokens: 20},
		"child": {threadID: "child", parentThreadID: "one", nonRoot: true, lastModified: now, observedTokens: 5},
	}}
	rows, _, _ := r.sessionSnapshots(now, map[string]struct{}{"one": {}, "two": {}, "child": {}}, true, snapshot.Statuses)
	if len(rows) != 2 {
		t.Fatal("attached chat was grouped as a subagent", rows)
	}
	for _, row := range rows {
		switch row.ID {
		case "one":
			if row.TotalTokens != 15 || row.AgentCount != 1 || row.AttachedToThreadID != "" {
				t.Fatal("root aggregation changed", row)
			}
		case "two":
			if row.TotalTokens != 20 || row.AgentCount != 0 || row.AttachedToThreadID != "one" {
				t.Fatal("side-chat usage or relationship lost", row)
			}
		}
	}
	// Snapshot maps are detached, and a disconnected daemon must not retain
	// a relationship from its previous connection.
	snapshot.AttachedThreads["two"] = "tampered"
	if next, ok := p.cachedStatusSnapshot([]string{"one", "two"}, time.Now()); !ok || next.AttachedThreads["two"] != "one" {
		t.Fatal("snapshot aliases mutable provider state")
	}
	p.disconnect(nil)
	if len(p.threadAttachments) != 0 {
		t.Fatal("disconnect retained side-chat relationships")
	}
}

//go:build unix

package codex

import (
	"testing"
	"time"
)

func TestLinkedSessionPreviewVerifiesAccountAndAgentHistory(t *testing.T) {
	const root = "550e8400-e29b-41d4-a716-446655440000"
	const child = "550e8400-e29b-41d4-a716-446655440001"
	const missing = "550e8400-e29b-41d4-a716-446655440002"
	p, f := newQuotaDaemon(t)
	now := time.Now()
	turn := func(id string, sec int64, reply string) map[string]any {
		return map[string]any{"id": id, "startedAt": sec, "status": "completed", "items": []any{map[string]string{"id": id + "-answer", "type": "agentMessage", "phase": "final_answer", "text": reply}}}
	}
	f.catalogues = map[string]map[string]any{
		"initialize":        {"userAgent": "codex/0.172.0 (Linux)"},
		"account/read":      {"account": map[string]string{"type": "chatgpt", "email": "someone@example.test"}},
		"thread/list":       {"data": []any{map[string]string{"id": root, "name": "Root"}}},
		"thread/turns/list": {"data": []any{turn("two", now.Unix()+1, "Agent-owned result"), turn("one", now.Unix()-1, "Earlier parent context")}},
	}
	ctx := t.Context()
	if err := p.ensureConnected(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	scope := p.savedScope
	p.mu.Unlock()
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p, files: map[string]*rolloutCursor{
		"root": {threadID: root}, "child": {threadID: child, parentThreadID: root, nonRoot: true, agentName: "Worker", startedAt: now},
	}}}
	if got, err := c.PreviewSessionLink(ctx, scope, missing); err == nil || len(got.Turns) != 0 {
		t.Fatal("unlisted target was exposed", got, err)
	}
	if got, err := c.PreviewSessionLink(ctx, scope, root); err != nil || got.Root.ID != root || got.Agent != nil || len(got.Turns) != 2 {
		t.Fatal("verified root not previewed", got, err)
	}
	if got, err := c.PreviewSessionLink(ctx, scope, child); err != nil || got.Root.ID != root || got.Agent == nil || got.Agent.Name != "Worker" || len(got.Turns) != 1 || got.Turns[0].Reply != "Agent-owned result" {
		t.Fatal("agent preview was not limited to owned history", got, err)
	}
	f.mu.Lock()
	if len(f.historyReads) != 2 || len(f.resumeReads)+len(f.writes)+len(f.readUpdates) != 0 || f.savedReads[1]["currentAccountOnly"] != true || f.savedReads[1]["archived"] != false || f.savedReads[1]["limit"] != float64(1) {
		t.Error("unexpected history or write traffic", f.savedReads, f.historyReads, f.resumeReads)
	}
	if ids, ok := f.savedReads[2]["includedThreadIds"].([]any); !ok || len(ids) != 1 || ids[0] != root {
		t.Error("agent lookup was not scoped to owning session", f.savedReads[2])
	}
	f.historyReadHook = func() { p.mu.Lock(); defer p.mu.Unlock(); p.finderNotificationLocked("account/updated", nil) }
	f.mu.Unlock()
	if got, err := c.PreviewSessionLink(ctx, scope, root); err == nil || len(got.Turns) != 0 {
		t.Fatal("account switched while preview loaded", got, err)
	}
}

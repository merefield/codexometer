//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSettingsCompatibilityAcrossClientsAndReconnect(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "zstd"}[compressed], func(t *testing.T) {
			p, f := newQuotaDaemonTransport(t, compressed)
			f.catalogues = map[string]map[string]any{
				"permissionProfile/list":   {"data": []any{map[string]any{"id": "restricted", "description": "Workspace access only", "allowed": true}}},
				"experimentalFeature/list": {"data": []any{}},
			}
			ctx := context.Background()
			// A second attached client reads the same persisted runtime selections.
			second := &daemonStatusProvider{socketPath: p.socketPath}
			t.Cleanup(func() { second.disconnect(nil) })
			model, err := p.SessionCommands(ctx, "one", "model/small")
			if err != nil {
				t.Fatal(err)
			}
			if err = p.ExecuteSessionCommand(ctx, "one", model.Path, model.Revision, model.Choices[0].ID); err != nil {
				t.Fatal(err)
			}
			permissions, err := p.SessionCommands(ctx, "one", "permissions")
			if err != nil || len(permissions.Choices) != 1 {
				t.Fatal(permissions, err)
			}
			if err = p.ExecuteSessionCommand(ctx, "one", permissions.Path, permissions.Revision, permissions.Choices[0].ID); err != nil {
				t.Fatal(err)
			}
			if err = second.ensureConnected(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := second.readQuotaSession(ctx, "one")
			if err != nil || current.Model != "small" || current.Effort != "medium" || current.Tier != nil {
				t.Fatal("second client lost applied selections", current, err)
			}
			p.disconnect(nil)
			if err = p.ensureConnected(ctx); err != nil {
				t.Fatal(err)
			}
			resumed, err := p.readQuotaSession(ctx, "one")
			if err != nil || !sameQuotaSettings(current, resumed) {
				t.Fatal("reconnect replaced saved settings", resumed, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.permissions["one"] != "restricted" || f.sessions["two"].Model != "large" || len(f.writes) != 2 {
				t.Fatal("settings escaped selected thread", f.writes)
			}
			for _, read := range f.resumeReads {
				if len(read) != 2 || read["threadId"] == nil || read["excludeTurns"] != true {
					t.Fatal("read-only resume supplied overrides", read)
				}
			}
		})
	}
}

func TestQueuedModelAndPermissionAcknowledgementsAreNotApplied(t *testing.T) {
	for _, path := range []string{"model/small", "permissions"} {
		t.Run(path, func(t *testing.T) {
			p, f := newQuotaDaemonTransport(t, true)
			f.queued = true
			f.catalogues = map[string]map[string]any{"permissionProfile/list": {"data": []any{map[string]any{"id": "restricted", "description": "Workspace access only", "allowed": true}}}}
			menu, err := p.SessionCommands(context.Background(), "one", path)
			if err != nil || len(menu.Choices) != 1 {
				t.Fatal(menu, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[0].ID) }()
			// State updates for another attached session and approval state remain
			// responsive while the selected thread's queued write is being verified.
			p.handleNotification("thread/settings/updated", json.RawMessage(`{"threadId":"two","threadSettings":{"model":"other","effort":"high","serviceTier":"priority"}}`))
			responsive := make(chan struct{})
			go func() { p.SessionPrompt("two"); close(responsive) }()
			select {
			case <-responsive:
			case <-time.After(time.Second):
				t.Fatal("settings verification froze event state")
			}
			err = <-done
			if err == nil {
				t.Fatal("queued acknowledgement reported as applied")
			}
			if path == "model/small" && !errors.Is(err, ErrQuotaProfileUnverified) {
				t.Fatal("model failure lost unverified result", err)
			}
			if path == "permissions" && !strings.Contains(err.Error(), "unconfirmed") {
				t.Fatal("permission failure lost unconfirmed result", err)
			}
			f.mu.Lock()
			writes := len(f.writes)
			f.mu.Unlock()
			if writes != 1 {
				t.Fatal("queued mutation was retried", writes)
			}
		})
	}
}

func TestAccountChangeInvalidatesCatalogueWithoutDiscardingConversation(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{"experimentalFeature/list": {"data": []any{}}}
	if _, ok := p.Fetch(context.Background(), []string{"one"}); !ok {
		t.Fatal("daemon unavailable")
	}
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	if _, err := c.SessionCommands(context.Background(), "one", "model/small"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	if p.contexts == nil {
		p.contexts = make(map[string]*daemonContextState)
	}
	p.contexts["one"] = &daemonContextState{latest: SessionContext{Text: "retained reply"}}
	p.mu.Unlock()
	p.handleNotification("account/updated", json.RawMessage(`{"authMode":"chatgpt","planType":"pro"}`))
	if _, ok := c.CachedSessionCommands("one", "model/small"); ok {
		t.Fatal("account change retained model catalogue")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.contexts["one"].latest.Text != "retained reply" || !p.lastStatusAt.IsZero() {
		t.Fatal("account change discarded conversation or skipped refresh")
	}
}

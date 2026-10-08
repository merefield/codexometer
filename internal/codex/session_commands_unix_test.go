//go:build unix

package codex

import (
	"context"
	"strings"
	"testing"
)

func TestSessionCommandsModelAndTier(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	m, err := p.SessionCommands(ctx, "one", "/model")
	if err != nil || len(m.Choices) != 1 || m.Choices[0].Next != "model/small" {
		t.Fatalf("models: %+v %v", m, err)
	}
	m, err = p.SessionCommands(ctx, "one", m.Choices[0].Next)
	if err != nil || len(m.Choices) != 1 || !m.Choices[0].Action {
		t.Fatalf("efforts: %+v %v", m, err)
	}
	if err = p.ExecuteSessionCommand(ctx, "one", m.Path, "stale", m.Choices[0].ID); err == nil {
		t.Fatal("accepted stale revision")
	}
	if err = p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.writes) != 1 || f.writes[0]["model"] != "small" || f.writes[0]["effort"] != "medium" || f.writes[0]["serviceTier"] != nil || f.sessions["two"].Model != "large" {
		t.Errorf("wrong scoped mutation: %+v", f.writes)
	}
	f.mu.Unlock()
	m, err = p.SessionCommands(ctx, "one", "fast")
	if err != nil || len(m.Choices) != 2 {
		t.Fatalf("tier alias: %+v %v", m, err)
	}
	if err = p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.writes[1]["serviceTier"] != "flex" {
		t.Errorf("used display name instead of advertised ID: %+v", f.writes[1])
	}
	f.commandStatus = "active"
	f.mu.Unlock()
	if err = p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[1].ID); err == nil {
		t.Fatal("accepted action after turn started")
	}
	if _, err = p.SessionCommands(ctx, "closed", "model"); err == nil {
		t.Fatal("opened unloaded session")
	}
}

func TestSessionCommandCataloguesAndSafeHelp(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{
		"permissionProfile/list": {"data": []any{
			map[string]any{"id": "safe", "description": "Read-only workspace", "allowed": true},
			map[string]any{"id": "blocked", "description": "Not allowed", "allowed": false},
			map[string]any{"id": "bad-help", "description": "unsafe\x1b[31mhelp", "allowed": true},
		}},
		"skills/list": {"data": []any{map[string]any{"cwd": "/work", "skills": []any{map[string]any{"name": "review", "description": "Review help", "path": "/skills/review", "enabled": true}}}}},
	}
	ctx := context.Background()
	m, err := p.SessionCommands(ctx, "one", "permissions")
	if err != nil || len(m.Choices) != 2 || !m.Choices[0].Action || m.Choices[1].Action {
		t.Fatalf("unsafe permissions: %+v %v", m, err)
	}
	m, err = p.SessionCommands(ctx, "one", "skills")
	if err != nil || len(m.Choices) != 1 || m.Choices[0].Action || !strings.Contains(m.Choices[0].Help, "Review help") {
		t.Fatalf("skill help: %+v %v", m, err)
	}
	if err = p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("executed browse-only choice")
	}
	f.mu.Lock()
	f.catalogues["skills/list"] = map[string]any{"nextCursor": "repeat", "data": []any{}}
	f.mu.Unlock()
	if _, err = p.SessionCommands(ctx, "one", "skills"); err == nil {
		t.Fatal("accepted repeating cursor")
	}
}

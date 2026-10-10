//go:build unix

package codex

import (
	"context"
	"fmt"
	"net/url"
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

func TestCommandMenuSingleLineLabelsFailClosed(t *testing.T) {
	for _, label := range []string{"Enable\nFast", "Enable\r\nFast", "Enable\tFast", "Enable  Fast", " Enable Fast ", "Enable\u2028Fast", "Enable\x1b[31m Fast"} {
		menu := normalizeCommandMenu(SessionCommandMenu{
			Title: "Choose\na mode", Help: "First help line\nSecond help line",
			Choices: []SessionCommandChoice{
				{Label: label, Help: "Option help\nMore help", method: "thread/settings/update", params: map[string]any{"serviceTier": "flex"}},
				{Label: "Browse\nonly", Help: "First\nSecond"},
				{Label: "Unchanged", Help: "Safe\nmultiline help", method: "thread/settings/update", params: map[string]any{"serviceTier": nil}},
			},
		})
		if menu.Title != "Choose a mode" || menu.Help != "First help line\nSecond help line" {
			t.Fatalf("title/help normalization: %+v", menu)
		}
		changed := menu.Choices[0]
		if strings.ContainsAny(changed.Label, "\n\r\t\u2028\x1b") || changed.Action || changed.method != "" || changed.params != nil {
			t.Fatalf("normalized action remained executable: %+v", changed)
		}
		if menu.Choices[1].Label != "Browse only" || menu.Choices[1].Help != "First\nSecond" {
			t.Fatal("browse label must be single line while help retains paragraphs")
		}
		if !menu.Choices[2].Action || menu.Choices[2].Help != "Safe\nmultiline help" {
			t.Fatal("safe action or multiline help was unnecessarily disabled")
		}
	}
}

func TestSessionRenameScopedAndConfirmed(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	f.names = map[string]string{"one": "Original", "two": "Other session"}
	f.commandStatus = "active"
	root, err := p.SessionCommands(ctx, "one", "")
	found := false
	for _, o := range root.Choices {
		found = found || o.Next == "rename"
	}
	if err != nil || !found {
		t.Fatal("rename missing from root", err)
	}
	input, err := p.SessionCommands(ctx, "one", "rename")
	if err != nil || !input.Input || input.Value != "Original" {
		t.Fatal("missing current-name editor", input, err)
	}
	name := "Review / quota + rendering 界"
	m, err := p.SessionCommands(ctx, "one", "rename/"+url.PathEscape(name))
	if err != nil || len(m.Choices) != 1 || !m.Choices[0].Action {
		t.Fatal(m, err)
	}
	if len(f.nameWrites) != 0 {
		t.Fatal("preparing rename sent a mutation")
	}
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, "stale", m.Choices[0].ID); err == nil {
		t.Fatal("stale rename accepted")
	}
	if err := p.ExecuteSessionCommand(ctx, "two", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("cross-session rename accepted")
	}
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.nameWrites) != 1 || f.names["one"] != name || f.names["two"] != "Other session" || len(f.writes) != 0 {
		t.Error("wrong rename scope", f.nameWrites)
	}
	f.mu.Unlock()
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("changed name did not invalidate confirmation")
	}
}

func TestSessionRenameRejectsUnsafeAndChangedNames(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	for _, path := range []string{"rename/", "rename/%zz", "rename/a/b", "rename/" + url.PathEscape("bad\nname"), "rename/" + url.PathEscape("bad\x1b[31mname"), "rename/" + strings.Repeat("x", 513)} {
		if _, err := p.SessionCommands(ctx, "one", path); err == nil {
			t.Fatalf("accepted unsafe name %q", path)
		}
	}
	m, err := p.SessionCommands(ctx, "one", "/rename My session")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.names = map[string]string{"one": "Renamed elsewhere"}
	f.mu.Unlock()
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("overwrote concurrent rename")
	}
	m, err = p.SessionCommands(ctx, "one", m.Path)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.fail = "rename"
	f.mu.Unlock()
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("server rejection reported success")
	}
}

func TestSessionRenameMaximumUnicodeInput(t *testing.T) {
	for _, runeText := range []string{"界", "😀"} {
		for _, direct := range []bool{false, true} {
			t.Run(runeText+fmt.Sprint(direct), func(t *testing.T) {
				p, f := newQuotaDaemon(t)
				f.names = map[string]string{"one": "Original", "two": "Other session"}
				name := strings.Repeat(runeText, SessionRenameInputLimit)
				path := "rename/" + url.PathEscape(name)
				if len(path) <= 2048 {
					t.Fatal("regression fixture did not exceed old transport cap")
				}
				if direct {
					path = "/rename " + name
				}
				m, err := p.SessionCommands(context.Background(), "one", path)
				if err != nil || len(m.Choices) != 1 || !m.Choices[0].Action {
					t.Fatal("valid Unicode rename rejected", m, err)
				}
				if len(f.nameWrites) != 0 {
					t.Fatal("review changed name")
				}
				if err := p.ExecuteSessionCommand(context.Background(), "one", m.Path, m.Revision, m.Choices[0].ID); err != nil {
					t.Fatal(err)
				}
				f.mu.Lock()
				defer f.mu.Unlock()
				if len(f.nameWrites) != 1 || f.names["one"] != name || f.names["two"] != "Other session" {
					t.Fatal("incorrect Unicode rename", f.nameWrites)
				}
			})
		}
	}
}

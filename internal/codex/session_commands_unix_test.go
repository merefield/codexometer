//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
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
	if err != nil || len(m.Choices) != 3 || !m.Picker {
		t.Fatalf("tier alias: %+v %v", m, err)
	}
	if err = p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[1].ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.writes[1]["serviceTier"] != "flex" {
		t.Errorf("used display name instead of advertised ID: %+v", f.writes[1])
	}
	f.commandStatus = "active"
	f.mu.Unlock()
	if err = p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("accepted action after turn started")
	}
	if _, err = p.SessionCommands(ctx, "closed", "model"); err == nil {
		t.Fatal("opened unloaded session")
	}
}

func TestCachedCommandClientRevalidatesWritesAndSettingsScope(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	if _, ok := p.Fetch(ctx, []string{"one"}); !ok {
		t.Fatal("daemon unavailable")
	}
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	menu, err := c.SessionCommands(ctx, "one", "model/small")
	if err != nil || len(menu.Choices) != 1 || !menu.Choices[0].Action {
		t.Fatal("missing model option", err)
	}
	if _, ok := c.CachedSessionCommands("one", "model/small"); !ok {
		t.Fatal("menu did not enter the shared cache")
	}
	f.mu.Lock()
	f.commandStatus = "active" // The cached menu still describes idle settings.
	f.mu.Unlock()
	if err := c.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[0].ID); err == nil {
		t.Fatal("write trusted cached settings after a turn started")
	}
	f.mu.Lock()
	writes := len(f.writes)
	f.commandStatus = "idle"
	f.mu.Unlock()
	if writes != 0 {
		t.Fatal("stale cached command mutated the session")
	}
	if _, ok := c.CachedSessionCommands("one", "model/small"); ok {
		t.Fatal("attempted write retained its old catalogue")
	}
	if _, err := c.SessionCommands(ctx, "one", "model/small"); err != nil {
		t.Fatal(err)
	}
	settings := json.RawMessage(`{"threadId":"one","threadSettings":{"model":"large","effort":"high","serviceTier":null}}`)
	p.handleNotification("thread/settings/updated", settings)
	if _, ok := c.CachedSessionCommands("one", "model/small"); ok {
		t.Fatal("settings change did not invalidate cached options immediately")
	}
	if _, err := c.SessionCommands(ctx, "one", "model/small"); err != nil {
		t.Fatal(err)
	}
	p.handleNotification("thread/settings/updated", settings)
	if _, ok := c.CachedSessionCommands("one", "model/small"); !ok {
		t.Fatal("repeated unchanged settings notification invalidated the cache")
	}
}

func TestCommandDiscoveryRetainsFetchedSubmenus(t *testing.T) {
	p, fixture := newQuotaDaemon(t)
	ctx := context.Background()
	if _, ok := p.Fetch(ctx, []string{"one"}); !ok {
		t.Fatal("daemon unavailable")
	}
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	c.warmSessionCommands(LiveUsageSnapshot{CodexUp: true, Sessions: []LiveUsageSession{{ID: "one", Active: true}}})
	if _, err := c.SessionCommands(ctx, "one", ""); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"model", "rename", "cd"} {
		menu, ok := c.CachedSessionCommands("one", path)
		if !ok || menu.Path != path || menu.Revision == "" || path == "model" && len(menu.Choices) != 1 {
			t.Fatalf("discovery discarded the fetched %s submenu", path)
		}
		if menu.Input {
			encoded, err := json.Marshal(menu)
			if err != nil || !strings.Contains(string(encoded), `"choices":[]`) {
				t.Fatalf("cached %s input menu must serialize choices as an array: %s (%v)", path, encoded, err)
			}
		}
	}
	// Do not peek while working: the original idle scope must not revive after
	// a full idle -> working -> idle cycle within the cache lifetime.
	for _, status := range []string{"active", "idle"} {
		fixture.mu.Lock()
		fixture.commandStatus = status
		fixture.mu.Unlock()
		p.mu.Lock()
		p.lastStatusAt = time.Time{}
		p.mu.Unlock()
		if _, ok := p.Fetch(ctx, []string{"one"}); !ok {
			t.Fatal("status refresh failed")
		}
	}
	if _, ok := c.CachedSessionCommands("one", ""); !ok {
		t.Fatal("turn transition discarded stable root suggestions")
	}
	if _, ok := c.CachedSessionCommands("one", "model"); ok {
		t.Fatal("complete turn cycle revived the pre-turn submenu")
	}
}

type transitionMenuSource struct {
	*menuCacheSource
	provider *daemonStatusProvider
}

func (f *transitionMenuSource) sessionCommandScope(id, path string) string {
	return f.provider.sessionCommandScope(id, path)
}

func TestCommandCacheRejectsReadSpanningTurnBetweenPolls(t *testing.T) {
	p, _ := newQuotaDaemon(t)
	if _, ok := p.Fetch(context.Background(), []string{"one"}); !ok {
		t.Fatal("daemon unavailable")
	}
	f := &transitionMenuSource{menuCacheSource: &menuCacheSource{started: make(chan string, 1), release: make(chan struct{})}, provider: p}
	c := newSessionCommandCache(f)
	root := p.sessionCommandScope("one", "")
	done := make(chan error, 1)
	go func() { _, err := c.load(context.Background(), "one", "model", false); done <- err }()
	<-f.started
	p.handleNotification("turn/started", json.RawMessage(`{"threadId":"one","turn":{"id":"quick-turn"}}`))
	p.handleNotification("turn/completed", json.RawMessage(`{"threadId":"one","turn":{"id":"quick-turn","status":"completed"}}`))
	close(f.release)
	if err := <-done; err != ErrSessionCommand {
		t.Fatal("read spanning a complete turn was accepted", err)
	}
	if _, ok := c.peek("one", "model"); ok {
		t.Fatal("read spanning a complete turn repopulated the cache")
	}
	if p.sessionCommandScope("one", "") != root {
		t.Fatal("turn cycle invalidated stable root suggestions")
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

func TestSessionSpeedPickerListsCurrentAndAppliesAdvertisedIDs(t *testing.T) {
	p, f := newQuotaDaemon(t)
	tier := "priority"
	f.mu.Lock()
	f.sessions["one"] = QuotaSession{ID: "one", Model: "small", Effort: "medium", Tier: &tier}
	f.mu.Unlock()
	ctx := context.Background()
	menu, err := p.SessionCommands(ctx, "one", "fast")
	if err != nil || !menu.Picker || len(menu.Choices) != 3 {
		t.Fatalf("incomplete picker: %+v %v", menu, err)
	}
	for i, label := range []string{"Standard (default)", "Fast", "Slow"} {
		if menu.Choices[i].Label != label || !menu.Choices[i].Action || menu.Choices[i].Selected != (i == 2) {
			t.Fatalf("wrong label/action/current speed: %+v", menu.Choices)
		}
	}
	if err := p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[1].ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	current := f.sessions["one"]
	if len(f.writes) != 1 || f.writes[0]["serviceTier"] != "flex" || len(f.writes[0]) != 2 || current.Model != "small" || current.Effort != "medium" || f.sessions["two"].Tier != nil {
		t.Errorf("speed change altered other settings/session or guessed the tier: %+v %+v", f.writes, current)
	}
	f.mu.Unlock()
	menu, err = p.SessionCommands(ctx, "one", "fast")
	if err != nil || !menu.Choices[1].Selected || menu.Choices[2].Selected {
		t.Fatalf("applied speed not reflected on reopen: %+v %v", menu, err)
	}
	if err := p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[0].ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.writes) != 2 || f.writes[1]["serviceTier"] != nil || f.sessions["one"].Tier != nil {
		t.Error("default did not clear the override")
	}
	f.mu.Unlock()
	menu, err = p.SessionCommands(ctx, "one", "fast")
	if err != nil || !menu.Choices[0].Selected {
		t.Fatal("default speed not marked current", err)
	}
}

func TestSessionSpeedPickerDoesNotTreatQueuedWriteAsApplied(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.mu.Lock()
	f.sessions["one"] = QuotaSession{ID: "one", Model: "small", Effort: "medium"}
	f.queued = true
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	menu, err := p.SessionCommands(ctx, "one", "fast")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[1].ID); err == nil {
		t.Fatal("queued speed update reported as applied")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 1 || f.sessions["one"].Tier != nil {
		t.Fatal("unconfirmed write was retried or changed running speed")
	}
}

func TestSessionCommandAvailabilityDistinguishesBlockedSettingsFromHelp(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.mu.Lock()
	f.commandStatus = "active"
	f.sessions["one"] = QuotaSession{ID: "one", Model: "small", Effort: "medium"}
	f.mu.Unlock()
	for _, path := range []string{"fast", "model/small"} {
		menu, err := p.SessionCommands(context.Background(), "one", path)
		if err != nil || len(menu.Choices) == 0 {
			t.Fatal(menu, err)
		}
		for _, choice := range menu.Choices {
			if choice.Action || !strings.HasPrefix(choice.Unavailable, "Waiting for idle.") {
				t.Fatalf("blocked setting lacks reason: %+v", choice)
			}
			if err := p.ExecuteSessionCommand(context.Background(), "one", menu.Path, menu.Revision, choice.ID); err == nil {
				t.Fatal("executed unavailable option")
			}
		}
	}
	f.mu.Lock()
	f.commandStatus = "idle"
	f.mu.Unlock()
	menu, err := p.SessionCommands(context.Background(), "one", "fast")
	if err != nil || !menu.Choices[0].Action || menu.Choices[0].Unavailable != "" {
		t.Fatal("refreshed idle setting remains blocked", menu, err)
	}
}

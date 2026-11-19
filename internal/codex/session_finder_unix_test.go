//go:build unix

package codex

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSavedBrowserOnlyReadsBoundedAccountHistory(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{
		"initialize":        {"userAgent": "codexometer/0.170.0 (Linux)"},
		"account/read":      {"account": map[string]string{"type": "chatgpt", "email": "example@example.test"}},
		"thread/list":       {"data": []any{map[string]any{"id": "saved", "name": "Two\nlines", "cwd": "/work", "preview": "not exported"}}, "nextCursor": "page2"},
		"thread/turns/list": {"data": []any{map[string]any{"id": "turn", "status": "completed", "items": []any{map[string]any{"id": "reply", "type": "agentMessage", "phase": "final_answer", "text": "withheld", "moderation": map[string]any{"moderated": true}}}}}},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := p.ensureConnected(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	scope := p.savedScope
	p.mu.Unlock()
	if scope == "" {
		t.Fatal("supported handshake did not enable account scoping")
	}
	client := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	read := func(cursor, id string) (SavedPage, error) { return client.BrowseSavedSessions(ctx, scope, cursor, id) }
	page, err := read("", "")
	if err != nil || page.Next != "page2" || len(page.Sessions) != 1 || page.Sessions[0].Name != "Two lines" {
		t.Fatal(page, err)
	}
	// Reopening on the original page is cheap. Next, move the same thread
	// out of that page and also try a cursor that the server no longer accepts.
	assertDetail := func(cursor string) {
		t.Helper()
		page, err := read(cursor, "saved")
		if err != nil || len(page.Turns) != 1 || !page.Turns[0].Restricted || page.Turns[0].Reply != "" {
			t.Fatal(cursor, page, err)
		}
	}
	assertDetail("")
	f.mu.Lock()
	if len(f.savedReads) != 2 {
		t.Errorf("fast path listed %d pages for listing plus detail", len(f.savedReads))
	}
	f.savedPages = map[[2]string]map[string]any{
		{"updated_at", ""}:     {"data": []any{map[string]string{"id": "recent"}}, "nextCursor": "old-position"},
		{"created_at", ""}:     {"data": []any{map[string]string{"id": "recent"}}, "nextCursor": "next"},
		{"created_at", "next"}: {"data": []any{map[string]string{"id": "saved"}}},
	}
	f.mu.Unlock()
	assertDetail("")
	assertDetail("expired-cursor")
	if _, err := read("", "not-listed"); !errors.Is(err, ErrSavedSessions) {
		t.Fatal("unknown id accepted", err)
	}
	f.mu.Lock()
	for _, params := range f.savedReads {
		providers, ok := params["modelProviders"].([]any)
		if !ok || len(providers) != 0 || params["currentAccountOnly"] != true || params["useStateDbOnly"] != true || params["limit"] != float64(40) {
			t.Errorf("incorrect saved-page filters: %+v", params)
		}
	}
	if len(f.historyReads) != 3 || len(f.resumeReads)+len(f.readUpdates)+len(f.writes) != 0 {
		t.Errorf("unexpected turn reads or writes: %+v", f.historyReads)
	}
	f.historyReadHook = func() { p.mu.Lock(); defer p.mu.Unlock(); p.finderNotificationLocked("account/updated", nil) }
	f.mu.Unlock()
	if page, err := read("", "saved"); err == nil || len(page.Turns) > 0 {
		t.Fatal("account changed during read", page, err)
	}
	p.mu.Lock()
	scope = p.savedScope
	p.mu.Unlock()
	f.mu.Lock()
	f.catalogues["account/read"] = map[string]any{"account": map[string]string{"type": "apiKey"}}
	f.mu.Unlock()
	if _, err = read("", ""); err == nil {
		t.Fatal("API-key account allowed")
	}
}

func TestFinderVersionAndRenames(t *testing.T) {
	for agent, want := range map[string]bool{"codex/0.169.9 (Linux)": false, "unknown": false, "codex/0.170.0 (macOS)": true, "codex/0.171.1 (Linux)": true} {
		if supportsSavedScope(agent) != want {
			t.Errorf("version gate for %q", agent)
		}
	}
	p, f := newQuotaDaemon(t)
	f.names = map[string]string{"one": "Original"}
	snap, ok := p.Fetch(context.Background(), []string{"one"})
	if !ok || snap.Names["one"] != "Original" {
		t.Fatal(snap.Names)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finderNotificationLocked("thread/name/updated", []byte(`{"threadId":"one","threadName":"New\nname"}`))
	p.finderNotificationLocked("thread/name/updated", []byte(`{"threadId":"untracked","threadName":"ignore"}`))
	p.hydrateTitleLocked("one", []byte(`"stale read"`), 0)
	p.hydrateTitleLocked("local", []byte(`null`), 0)
	if p.statusSnapshotLocked([]string{"one"}).Names["one"] != "New name" || len(p.names) != 1 {
		t.Fatal(p.names)
	}
	p.finderNotificationLocked("thread/name/updated", []byte(`{"threadId":"one"}`))
	if title, ok := p.names["one"]; !ok || title.name != "" || title.version != 2 {
		t.Fatal(title)
	}
}

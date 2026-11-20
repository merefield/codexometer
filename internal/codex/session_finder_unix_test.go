//go:build unix

package codex

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestSavedBrowserOnlyReadsBoundedAccountHistory(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{
		"initialize":        {"userAgent": "codexometer/0.170.0 (Linux)"},
		"account/read":      {"account": map[string]string{"type": "chatgpt", "email": "example@example.test"}},
		"thread/list":       {"data": []any{map[string]any{"id": "saved", "name": "Two\nlines", "cwd": "/work", "updatedAt": 1_730_000_000, "preview": "not exported"}}, "nextCursor": "page2"},
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
	if err != nil || page.Next != "page2" || len(page.Sessions) != 1 || page.Sessions[0].Name != "Two lines" || page.Sessions[0].UpdatedAt != 1_730_000_000 {
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

func TestSavedSearchAndExplicitResume(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{
		"initialize":   {"userAgent": "codex/0.171.0 (Linux)"},
		"account/read": {"account": map[string]string{"type": "chatgpt", "email": "example@example.test"}},
	}
	f.savedPages = map[[2]string]map[string]any{
		{"updated_at", ""}: {"data": []any{
			map[string]string{"id": "strasse", "name": "Unrelated", "cwd": "/work/STRASSE", "preview": "STRASSE"},
		}, "nextCursor": "older"},
		{"updated_at", "older"}: {"data": []any{map[string]string{"id": "one", "name": "Straße Review"}}},
		{"created_at", ""}:      {"data": []any{map[string]string{"id": "one"}}},
	}
	ctx := t.Context()
	if err := p.ensureConnected(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	scope := p.savedScope
	p.mu.Unlock()
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	page, err := c.SearchSavedSessions(ctx, scope, "", "stRASSE")
	if err != nil || page.Next != "" || len(page.Sessions) != 1 || page.Sessions[0].Name != "Straße Review" || page.Sessions[0].ListCursor != "older" {
		t.Fatal(page, err)
	}
	if err := c.ResumeSavedSession(ctx, scope, "", "two"); !errors.Is(err, ErrSavedSessions) {
		t.Fatal("thread outside the saved selection was accepted", err)
	}
	f.mu.Lock()
	if len(f.resumeReads) != 0 || len(f.historyReads) != 0 {
		t.Fatal("name search or rejected resume accessed a transcript", f.resumeReads, f.historyReads)
	}
	for _, req := range f.savedReads {
		if _, ok := req["searchTerm"]; ok {
			t.Fatal("name search used Codex's case-sensitive metadata filter", req)
		}
	}
	f.mu.Unlock()
	if err := c.ResumeSavedSession(ctx, scope, page.Sessions[0].ListCursor, "one"); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := p.Fetch(ctx, nil)
	if _, resumed := snapshot.ResumedThreads["one"]; !ok || !resumed || p.SessionPrompt("one").Token == "" {
		t.Fatal("cold session did not become live and prompt-capable", snapshot)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.resumeReads) != 1 || len(f.writes) != 0 || f.resumeReads[0]["excludeTurns"] != true {
		t.Fatal("resume sent unexpected mutations", f.resumeReads, f.writes)
	}
}

func TestSavedNameSearchContinuesAfterBoundedEmptyPages(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{
		"initialize":   {"userAgent": "codex/0.171.0 (Linux)"},
		"account/read": {"account": map[string]string{"type": "chatgpt", "email": "example@example.test"}},
	}
	f.savedPages = map[[2]string]map[string]any{}
	position := ""
	for i := 1; i <= 12; i++ {
		next := strconv.Itoa(i)
		f.savedPages[[2]string{"updated_at", position}] = map[string]any{"nextCursor": next}
		position = next
	}
	f.savedPages[[2]string{"updated_at", position}] = map[string]any{
		"data": []any{map[string]string{"id": "older", "name": "Résumé"}},
	}
	ctx := t.Context()
	if err := p.ensureConnected(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	scope := p.savedScope
	p.mu.Unlock()
	p.savedIndex(scope, false).warm.Do(func() {}) // Exercise the foreground cold-page budget deterministically.
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	page, err := c.SearchSavedSessions(ctx, scope, "", "RÉSUMÉ")
	if err != nil || len(page.Sessions) != 0 || page.Next != position {
		t.Fatal("did not preserve continuation past empty pages", page, err)
	}
	page, err = c.SearchSavedSessions(ctx, scope, page.Next, "RÉSUMÉ")
	if err != nil || page.Next != "" || len(page.Sessions) != 1 || page.Sessions[0].ID != "older" {
		t.Fatal("did not match older title with a different Unicode form", page, err)
	}
}

func TestSavedMetadataIndexPreloadsAndReusesPagesAcrossQueries(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{
		"initialize":   {"userAgent": "codex/0.171.0 (Linux)"},
		"account/read": {"account": map[string]string{"type": "chatgpt", "email": "example@example.test"}},
	}
	f.savedReadDelay = 30 * time.Millisecond
	f.savedPages = map[[2]string]map[string]any{
		{"updated_at", ""}:      {"data": []any{map[string]string{"id": "new", "name": "Release plan"}}, "nextCursor": "older"},
		{"updated_at", "older"}: {"data": []any{map[string]string{"id": "old", "name": "Résumé notes"}}},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := p.ensureConnected(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	scope := p.savedScope
	p.mu.Unlock()
	c := Client{LiveUsage: &LiveUsageReader{statusProvider: p}}
	head, err := c.SearchSavedSessions(ctx, scope, "", "")
	if err != nil || len(head.Sessions) != 1 || head.Next != "older" {
		t.Fatal(head, err)
	}
	wait := time.NewTicker(time.Millisecond)
	defer wait.Stop()
	for {
		if _, ready := p.savedIndex(scope, false).cached("older"); ready {
			break
		}
		select {
		case <-wait.C:
		case <-ctx.Done():
			t.Fatal("the picker did not preload the older page")
		}
	}
	var group sync.WaitGroup
	for _, term := range []string{"ré", "RÉS", "RÉSUMÉ", "release", "nothing", ""} {
		group.Go(func() {
			results, err := c.SearchSavedSessions(ctx, scope, "", term)
			if err != nil || (term == "nothing") != (len(results.Sessions) == 0) {
				t.Errorf("%q: %v, %v", term, results, err)
			}
		})
	}
	group.Wait()
	f.mu.Lock()
	list, account, transcript := len(f.savedReads), f.accountReads, len(f.historyReads)
	f.mu.Unlock()
	if list != 2 || account != 4 || transcript != 0 {
		t.Fatalf("typing should reuse two verified metadata pages: list=%d account=%d transcript=%d", list, account, transcript)
	}

	p.mu.Lock()
	p.finderNotificationLocked("account/updated", nil)
	p.mu.Unlock()
	if results, err := c.SearchSavedSessions(ctx, scope, "", "RÉSUMÉ"); !errors.Is(err, ErrSavedSessions) || len(results.Sessions) != 0 {
		t.Fatal("old scope was able to read cached names", results, err)
	}
}

func TestColdResumeKeepsReplyAndCountsFreshUsage(t *testing.T) {
	p, f := newQuotaDaemon(t)
	f.catalogues = map[string]map[string]any{
		"initialize":   {"userAgent": "codex/0.171.0 (Linux)"},
		"account/read": {"account": map[string]string{"type": "chatgpt", "email": "test@example.test"}},
		"thread/list":  {"data": []any{map[string]string{"id": "one"}}},
		"thread/turns/list": {"data": []any{map[string]any{
			"id": "previous", "status": "completed",
			"items": []any{map[string]string{"type": "agentMessage", "phase": "final_answer", "text": "Last saved reply"}},
		}}},
	}
	home := t.TempDir()
	now := time.Now()
	created := now.Add(-72 * time.Hour)
	path := testRolloutPath(t, home, created, "one")
	writeRollout(t, path, sessionMetaLine("one", `"cli"`, "/work", nil)+"\n"+tokenCountLine(created, 1000)+"\n")
	if err := os.Chtimes(path, created, created); err != nil {
		t.Fatal(err)
	}
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	r.statusProvider = p
	initial, err := r.FetchTokenUsage(t.Context())
	if err != nil || initial.SavedScope == "" {
		t.Fatal(initial, err)
	}
	if err := (Client{LiveUsage: r}).ResumeSavedSession(t.Context(), initial.SavedScope, "", "one"); err != nil {
		t.Fatal(err)
	}
	resumed, err := r.FetchTokenUsage(t.Context())
	if err != nil || len(resumed.Sessions) != 1 || resumed.Sessions[0].Context.Text != "Last saved reply" {
		t.Fatal(resumed, err)
	}
	appendRollout(t, path, tokenCountLine(now, 1075)+"\n")
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	next, err := r.FetchTokenUsage(t.Context())
	if err != nil || len(next.Sessions) != 1 || next.Sessions[0].ID != "one" || next.TotalTokens != 75 {
		t.Fatal(next, err)
	}
}

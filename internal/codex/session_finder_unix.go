//go:build unix

package codex

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"go/version"
	"path/filepath"
	"strings"
	"time"
)

type threadTitle struct {
	name    string
	version uint64
}

// A rename (including an explicit clear) received during thread/read wins.
// With no live rename evidence, a missing server title leaves the local
// session_index.jsonl fallback intact for an ordinary CLI session.
func (p *daemonStatusProvider) hydrateTitleLocked(id string, raw json.RawMessage, version uint64) {
	var name *string
	if p.names[id].version != version || len(raw) == 0 || json.Unmarshal(raw, &name) != nil {
		return
	}
	if name == nil || *name == "" {
		if version == 0 {
			delete(p.names, id)
		} else {
			p.names[id] = threadTitle{version: version}
		}
		return
	}
	p.names[id] = threadTitle{singleLine(*name), version}
}

func (p *daemonStatusProvider) finderNotificationLocked(method string, params json.RawMessage) {
	switch method {
	case "account/updated":
		if p.savedScope != "" && !workspaceRoutingReady(params) {
			p.savedScope = rand.Text()
			clear(p.resumedThreads)
		}
	case "thread/name/updated":
		var n struct{ ThreadID, ThreadName string }
		if json.Unmarshal(params, &n) != nil {
			return
		}
		_, tracked := p.statusThreads[n.ThreadID]
		_, subscribed := p.subscribed[n.ThreadID]
		if tracked || subscribed {
			p.names[n.ThreadID] = threadTitle{singleLine(n.ThreadName), p.names[n.ThreadID].version + 1}
		}
	}
}

// Older servers silently ignore unknown thread/list fields.
func supportsSavedScope(userAgent string) bool {
	first, _, _ := strings.Cut(userAgent, " ")
	_, v, ok := strings.Cut(first, "/")
	return ok && version.IsValid("go"+v) && version.Compare("go"+v, "go0.170.0") >= 0
}

func (p *daemonStatusProvider) savedAccount(ctx context.Context, scope string) (string, error) {
	var raw json.RawMessage
	p.mu.Lock()
	valid := scope != "" && scope == p.savedScope
	p.mu.Unlock()
	if !valid || p.request(ctx, "account/read", map[string]bool{"refreshToken": false}, &raw) != nil {
		return "", ErrSavedSessions
	}
	var response struct{ Account struct{ Type string } }
	account := fingerprintAccount(raw)
	p.mu.Lock()
	defer p.mu.Unlock()
	if scope != p.savedScope || json.Unmarshal(raw, &response) != nil || response.Account.Type != "chatgpt" || account == "" {
		return "", ErrSavedSessions
	}
	return quotaAccount(account, workspaceRoutingID(raw)), nil
}

func (p *daemonStatusProvider) listSaved(ctx context.Context, scope, cursor, sortKey string) (SavedPage, error) {
	p.mu.Lock()
	conn, valid := p.connection, scope != "" && scope == p.savedScope
	p.mu.Unlock()
	if !valid {
		return SavedPage{}, ErrSavedSessions
	}
	params := map[string]any{"currentAccountOnly": true, "modelProviders": []string{}, "archived": false, "limit": 40, "sortKey": sortKey, "sortDirection": "desc", "useStateDbOnly": true}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var result struct {
		Data       []struct{ ID, Name, Cwd string }
		NextCursor string
	}
	if err := p.requestOn(ctx, conn, "thread/list", params, &result); err != nil {
		return SavedPage{}, err
	}
	page := SavedPage{Next: result.NextCursor}
	for _, t := range result.Data {
		if t.ID != "" && singleLine(t.ID) == t.ID {
			page.Sessions = append(page.Sessions, SavedSession{t.ID, singleLine(t.Name), singleLine(t.Cwd)})
		}
	}
	return page, nil
}

func (p *daemonStatusProvider) searchSaved(ctx context.Context, scope, cursor, query string) (SavedPage, error) {
	account, err := p.savedAccount(ctx, scope)
	if err != nil {
		return SavedPage{}, err
	}
	p.mu.Lock()
	conn, valid := p.connection, p.savedScope == scope
	p.mu.Unlock()
	if !valid {
		return SavedPage{}, ErrSavedSessions
	}
	params := map[string]any{"currentAccountOnly": true, "searchTerm": query, "archived": false, "limit": 40, "sortKey": "updated_at", "sortDirection": "desc"}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var result struct {
		Data []struct {
			Thread struct{ ID, Name, Cwd string }
		}
		NextCursor string
	}
	if err := p.requestOn(ctx, conn, "thread/search", params, &result); err != nil {
		return SavedPage{}, ErrSavedSessions
	}
	if current, err := p.savedAccount(ctx, scope); err != nil || current != account {
		return SavedPage{}, ErrSavedSessions
	}
	page := SavedPage{Next: result.NextCursor}
	for _, match := range result.Data {
		t := match.Thread
		if t.ID != "" && singleLine(t.ID) == t.ID {
			page.Sessions = append(page.Sessions, SavedSession{t.ID, singleLine(t.Name), singleLine(t.Cwd)})
		}
	}
	return page, nil
}

func (p *daemonStatusProvider) resumeSaved(ctx context.Context, scope, cursor, id string) error {
	p.mu.Lock()
	conn, generation := p.connection, p.commandGeneration
	p.mu.Unlock()
	account, err := p.savedAccount(ctx, scope)
	if conn == nil || err != nil {
		return ErrSavedSessions
	}
	if _, err := savedPageFor(ctx, p, scope, cursor, id); err != nil {
		return ErrSavedSessions
	}
	if current, err := p.savedAccount(ctx, scope); err != nil || current != account {
		return ErrSavedSessions
	}
	var result struct {
		Cwd, ApprovalsReviewer string
		Thread                 struct{ ID string }
	}
	if err := p.requestOnChecked(ctx, conn, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &result, &generation); err != nil || result.Thread.ID != id {
		return ErrSavedSessions
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.connection != conn || p.savedScope != scope || p.commandGeneration != generation {
		return ErrSavedSessions
	}
	if p.resumedThreads == nil {
		p.resumedThreads = map[string]time.Time{}
	}
	// Bound the extra subscriptions retained solely by explicit resumes.
	if len(p.resumedThreads) >= 32 {
		var oldest string
		for thread, at := range p.resumedThreads {
			if oldest == "" || at.Before(p.resumedThreads[oldest]) {
				oldest = thread
			}
		}
		delete(p.resumedThreads, oldest)
	}
	p.resumedThreads[id] = time.Now()
	p.subscribed[id] = struct{}{}
	p.permissionSettings[id] = result.ApprovalsReviewer
	if filepath.IsAbs(result.Cwd) {
		p.threadDirectories[id] = result.Cwd
	}
	p.lastStatusAt = time.Time{}
	return nil
}

//go:build unix

package codex

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"go/version"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type threadTitle struct {
	name    string
	version uint64
}

type resumedThread struct {
	at      time.Time
	context SessionContext
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
			p.clearSavedCatalogLocked()
			clear(p.resumedThreads)
		}
	case "thread/started":
		if p.savedCatalog != nil {
			p.savedCatalog.refreshHead = true // Refresh on the next browse, not on every agent spawn.
		}
	case "thread/archived", "thread/unarchived":
		p.clearSavedCatalogLocked()
	case "thread/name/updated":
		var n struct{ ThreadID, ThreadName string }
		if json.Unmarshal(params, &n) != nil {
			return
		}
		p.clearSavedCatalogLocked()
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

const savedPageSize = 40

func (p *daemonStatusProvider) listSaved(ctx context.Context, scope, cursor, sortKey string) (SavedPage, error) {
	p.mu.Lock()
	conn, valid := p.connection, scope != "" && scope == p.savedScope
	p.mu.Unlock()
	if !valid {
		return SavedPage{}, ErrSavedSessions
	}
	params := map[string]any{"currentAccountOnly": true, "modelProviders": []string{}, "archived": false, "limit": savedPageSize, "sortKey": sortKey, "sortDirection": "desc", "useStateDbOnly": true}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var result struct {
		Data       []savedWireThread
		NextCursor string
	}
	if err := p.requestOn(ctx, conn, "thread/list", params, &result); err != nil {
		return SavedPage{}, err
	}
	page := SavedPage{Next: result.NextCursor}
	for _, t := range result.Data {
		page.addWireThread(t)
	}
	return page, nil
}

type savedWireThread struct {
	ID, Name, Cwd string
	UpdatedAt     int64
}

func (page *SavedPage) addWireThread(t savedWireThread) {
	if t.ID != "" && singleLine(t.ID) == t.ID {
		page.Sessions = append(page.Sessions, SavedSession{ID: t.ID, Name: singleLine(t.Name), Directory: SessionDirectoryDisplay(t.Cwd), UpdatedAt: t.UpdatedAt})
	}
}

func (p *daemonStatusProvider) savedSessionAction(ctx context.Context, scope string) (*websocket.Conn, uint64, string, error) {
	p.mu.Lock()
	conn, generation := p.connection, p.commandGeneration
	p.mu.Unlock()
	if conn == nil {
		return nil, 0, "", ErrSavedSessions
	}
	account, err := p.savedAccount(ctx, scope)
	return conn, generation, account, err
}

func (p *daemonStatusProvider) resumeSaved(ctx context.Context, scope, cursor, id string) error {
	conn, generation, account, err := p.savedSessionAction(ctx, scope)
	if err != nil {
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
	if ctx.Err() != nil {
		return ErrSavedSessions
	}
	err = p.requestOnWithin(ctx, conn, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &result, &generation, 9*time.Second)
	if errors.Is(err, errDaemonWriteGuardRejected) {
		return ErrSavedSessions
	}
	var rejected *daemonResponseError
	var wire struct{ Code int }
	if errors.As(err, &rejected) && json.Unmarshal([]byte(rejected.payload), &wire) == nil {
		switch wire.Code {
		case -32600, -32601, -32602: // Invalid request, method or parameters.
			return ErrSavedSessions
		}
	}
	if err != nil || result.Thread.ID != id {
		return ErrSavedResumeUncertain
	}
	p.mu.Lock()
	if p.connection != conn || p.savedScope != scope || p.commandGeneration != generation {
		p.mu.Unlock()
		return ErrSavedResumeUncertain
	}
	p.rememberResumedThreadLocked(id, SessionContext{})
	p.recordThreadSubscriptionLocked(id, result.Cwd, result.ApprovalsReviewer)
	p.lastStatusAt = time.Time{}
	p.mu.Unlock()

	var previous SessionContext
	if turns, _, err := p.readSessionHistory(ctx, id, time.Time{}); err == nil && len(turns) != 0 {
		previous = HistoryExcerpt(turns[len(turns)-1])
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.connection != conn || p.savedScope != scope || p.commandGeneration != generation {
		return ErrSavedResumeUncertain
	}
	if retained, ok := p.resumedThreads[id]; ok {
		retained.context = previous
		p.resumedThreads[id] = retained
	}
	return nil
}

func (p *daemonStatusProvider) rememberResumedThreadLocked(id string, previous SessionContext) {
	if p.resumedThreads == nil {
		p.resumedThreads = make(map[string]resumedThread)
	}
	// Bound the extra subscriptions retained solely by explicit resumes.
	if _, known := p.resumedThreads[id]; !known && len(p.resumedThreads) >= 32 {
		var oldest string
		for thread, retained := range p.resumedThreads {
			if oldest == "" || retained.at.Before(p.resumedThreads[oldest].at) {
				oldest = thread
			}
		}
		delete(p.resumedThreads, oldest)
	}
	p.resumedThreads[id] = resumedThread{at: time.Now(), context: previous}
}

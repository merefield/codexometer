//go:build unix

package codex

import (
	"context"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	savedMetadataFreshness = 2 * time.Minute
	savedPrefetchPages     = 512
	savedSearchColdPages   = 12
)

type indexedSavedPage struct {
	page SavedPage
	keys []string // Unicode-folded names, parallel to page.Sessions.
}

// Only display metadata is cached. Preview and resume deliberately bypass
// this index and revalidate the selected ID against the daemon.
type savedMetadataIndex struct {
	scope       string
	created     time.Time
	refreshHead bool // guarded by daemonStatusProvider.mu
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	account     string
	pages       map[string]indexedSavedPage
	reads       singleflight.Group
	warm        sync.Once
}

func (p *daemonStatusProvider) clearSavedCatalogLocked() {
	if p.savedCatalog != nil {
		p.savedCatalog.cancel()
		p.savedCatalog = nil
	}
}

func (p *daemonStatusProvider) savedIndex(scope string, browseHead bool) *savedMetadataIndex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if scope == "" || scope != p.savedScope || p.connection == nil {
		return nil
	}
	if i := p.savedCatalog; i != nil && (time.Since(i.created) >= savedMetadataFreshness || browseHead && i.refreshHead) {
		p.clearSavedCatalogLocked()
	}
	if p.savedCatalog == nil {
		ctx, cancel := context.WithCancel(context.Background())
		p.savedCatalog = &savedMetadataIndex{
			scope: scope, created: time.Now(), ctx: ctx, cancel: cancel,
			pages: make(map[string]indexedSavedPage),
		}
	}
	return p.savedCatalog
}

func (i *savedMetadataIndex) cached(cursor string) (indexedSavedPage, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	page, ok := i.pages[cursor]
	return page, ok
}

func (p *daemonStatusProvider) savedIndexPage(ctx context.Context, i *savedMetadataIndex, cursor string) (indexedSavedPage, error) {
	if page, ok := i.cached(cursor); ok {
		return page, nil
	}
	// Cancel a superseded query without discarding its shared page load.
	read := i.reads.DoChan(cursor, func() (any, error) {
		if page, ok := i.cached(cursor); ok {
			return page, nil
		}
		call, finish := context.WithTimeout(i.ctx, 6*time.Second)
		defer finish()
		account, err := p.savedAccount(call, i.scope)
		if err != nil {
			return nil, ErrSavedSessions
		}
		page, err := p.listSaved(call, i.scope, cursor, "updated_at")
		if err != nil {
			return nil, ErrSavedSessions
		}
		if current, err := p.savedAccount(call, i.scope); err != nil || current != account || i.ctx.Err() != nil {
			if err == nil && current != account {
				p.discardMismatchedSavedIndex(i)
			}
			return nil, ErrSavedSessions
		}
		entry := indexedSavedPage{page: page, keys: make([]string, len(page.Sessions))}
		for n, row := range page.Sessions {
			entry.keys[n] = savedNameKey(row.Name)
		}
		i.mu.Lock()
		if i.account == "" {
			i.account = account
		}
		sameAccount := i.account == account
		if sameAccount {
			i.pages[cursor] = entry
		}
		i.mu.Unlock()
		if !sameAccount {
			p.discardMismatchedSavedIndex(i)
			return nil, ErrSavedSessions
		}
		return entry, nil
	})
	select {
	case result := <-read:
		if result.Err != nil {
			return indexedSavedPage{}, result.Err
		}
		return result.Val.(indexedSavedPage), nil
	case <-ctx.Done():
		return indexedSavedPage{}, ctx.Err()
	case <-i.ctx.Done():
		return indexedSavedPage{}, ErrSavedSessions
	}
}

func (p *daemonStatusProvider) discardMismatchedSavedIndex(i *savedMetadataIndex) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.savedCatalog == i {
		p.finderNotificationLocked("account/updated", nil)
		p.lastStatusAt = time.Time{}
	}
}

func (p *daemonStatusProvider) preloadSavedIndex(i *savedMetadataIndex) {
	ctx, cancel := context.WithTimeout(i.ctx, 45*time.Second)
	defer cancel()
	cursor := ""
	seen := make(map[string]bool)
	for range savedPrefetchPages {
		if ctx.Err() != nil || seen[cursor] {
			return
		}
		seen[cursor] = true
		entry, err := p.savedIndexPage(ctx, i, cursor)
		if err != nil || entry.page.Next == "" {
			return
		}
		cursor = entry.page.Next
	}
}

func (p *daemonStatusProvider) searchSaved(ctx context.Context, scope, cursor, query string) (SavedPage, error) {
	needle := savedNameQuery(query)
	i := p.savedIndex(scope, needle == "" && cursor == "")
	if i == nil {
		return SavedPage{}, ErrSavedSessions
	}
	i.warm.Do(func() { go p.preloadSavedIndex(i) })
	scan, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	page := SavedPage{Next: cursor}
	seen := make(map[string]bool)
	cold := 0
	for {
		position := page.Next
		entry, ok := i.cached(position)
		if !ok {
			// Never make a verified match wait for a cold page. Prefetch
			// continues independently; this cursor lets the caller look further.
			if len(page.Sessions) != 0 || cold == savedSearchColdPages {
				break
			}
			cold++
			var err error
			entry, err = p.savedIndexPage(scan, i, position)
			if err != nil {
				if len(seen) == 0 {
					return SavedPage{}, err
				}
				break // keep the continuation even if an older read failed
			}
		}
		seen[position] = true
		for n, row := range entry.page.Sessions {
			if strings.Contains(entry.keys[n], needle) {
				if position != cursor {
					row.ListCursor = position
				}
				page.Sessions = append(page.Sessions, row)
			}
		}
		page.Next = entry.page.Next
		if seen[page.Next] {
			page.Next = ""
		}
		if needle == "" || page.Next == "" || len(page.Sessions) >= savedPageSize {
			break
		}
	}
	if i.ctx.Err() != nil || ctx.Err() != nil {
		return SavedPage{}, ErrSavedSessions
	}
	return page, nil
}

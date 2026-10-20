package codex

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	commandMenuLifetime = 2 * time.Minute
	commandMenuLimit    = 128
	commandMenuMaxBytes = 256 * 1024
)

type commandMenuKey struct{ session, path string }
type commandMenuBranch struct {
	menu  SessionCommandMenu
	scope string
}
type commandMenuEntry struct {
	menu     *SessionCommandMenu
	scope    string
	at, used time.Time
}
type commandMenuObservation struct {
	name, directory string
	settings        SessionModelSettings
}

// A path-indexed tree of display metadata. Actions always go to the original
// client, which validates a freshly read revision before sending any write.
type sessionCommandCache struct {
	mu           sync.Mutex
	source       SessionCommandsClient
	entries      map[commandMenuKey]commandMenuEntry
	observations map[string]commandMenuObservation
	generation   uint64
	reads        singleflight.Group
	workers      chan struct{}
	warmers      chan struct{}
	now          func() time.Time
}

func newSessionCommandCache(source SessionCommandsClient) *sessionCommandCache {
	return &sessionCommandCache{source: source, entries: make(map[commandMenuKey]commandMenuEntry),
		workers: make(chan struct{}, 4), warmers: make(chan struct{}, 2), now: time.Now}
}

func commandMenuPath(path string) string {
	return strings.TrimPrefix(strings.TrimSpace(path), "/")
}

func cacheableCommandPath(path string) bool {
	// Typed names and paths are short-lived reviews, not catalogue branches.
	return ValidSessionCommandPath(path) && !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "rename/") &&
		!strings.HasPrefix(path, "rename ") && !strings.HasPrefix(path, "cd/") && !strings.HasPrefix(path, "cd ")
}

func (c *sessionCommandCache) scope(id, path string) string {
	if source, ok := c.source.(interface{ sessionCommandScope(string, string) string }); ok {
		return source.sessionCommandScope(id, path)
	}
	return "local"
}

func browsingCommandMenu(menu SessionCommandMenu) SessionCommandMenu {
	menu.Choices = append([]SessionCommandChoice(nil), menu.Choices...)
	for i := range menu.Choices {
		menu.Choices[i].method, menu.Choices[i].params = "", nil
	}
	return menu
}

func (c *sessionCommandCache) peek(id, path string) (SessionCommandMenu, bool) {
	path = commandMenuPath(path)
	scope := c.scope(id, path)
	if scope == "" || !cacheableCommandPath(path) {
		return SessionCommandMenu{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key, now := commandMenuKey{id, path}, c.now()
	e, ok := c.entries[key]
	if !ok || e.menu == nil || e.scope != scope || now.Sub(e.at) >= commandMenuLifetime {
		return SessionCommandMenu{}, false
	}
	e.used = now
	c.entries[key] = e
	return browsingCommandMenu(*e.menu), true
}

func (c *sessionCommandCache) invalidateLocked(id string) {
	c.generation++ // Also prevents reads started before invalidation repopulating it.
	for key := range c.entries {
		if id == "" || key.session == id {
			delete(c.entries, key)
		}
	}
}

func (c *sessionCommandCache) invalidate(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidateLocked(id)
}

func (c *sessionCommandCache) rememberLocked(key commandMenuKey, entry commandMenuEntry) {
	if _, exists := c.entries[key]; !exists && len(c.entries) >= commandMenuLimit {
		var oldest commandMenuKey
		var at time.Time
		for k, e := range c.entries {
			if at.IsZero() || e.used.Before(at) {
				oldest, at = k, e.used
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = entry
}

func (c *sessionCommandCache) load(ctx context.Context, id, path string, fresh bool) (SessionCommandMenu, error) {
	if err := ctx.Err(); err != nil {
		return SessionCommandMenu{}, err
	}
	originalPath := path
	path = commandMenuPath(path)
	if fresh {
		c.invalidate(id)
	}
	if !fresh {
		if menu, ok := c.peek(id, originalPath); ok {
			return menu, nil
		}
	}
	scope := c.scope(id, path)
	if scope == "" || !cacheableCommandPath(path) {
		return c.source.SessionCommands(ctx, id, originalPath)
	}
	c.mu.Lock()
	generation := c.generation
	c.mu.Unlock()
	key := commandMenuKey{id, path}
	flight, _ := json.Marshal([]any{id, path, scope, generation})
	result := c.reads.DoChan(string(flight), func() (any, error) {
		if !fresh {
			if menu, hit := c.peek(id, path); hit {
				return menu, nil
			}
		}
		// One caller cancelling must not cancel a shared read for other users.
		readCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		select {
		case c.workers <- struct{}{}:
			defer func() { <-c.workers }()
		case <-readCtx.Done():
			return nil, readCtx.Err()
		}
		var menu SessionCommandMenu
		var branches map[string]commandMenuBranch
		var err error
		if source, ok := c.source.(interface {
			sessionCommandTree(context.Context, string, string) (SessionCommandMenu, map[string]commandMenuBranch, error)
		}); ok {
			menu, branches, err = source.sessionCommandTree(readCtx, id, path)
		} else {
			menu, err = c.source.SessionCommands(readCtx, id, path)
		}
		if err == nil {
			err = readCtx.Err() // Do not retain a partial catalogue after a deadline.
		}
		if err != nil {
			return nil, err
		}
		menu = browsingCommandMenu(menu)
		if c.scope(id, path) != scope {
			return nil, ErrSessionCommand
		}
		for childPath, branch := range branches {
			if !cacheableCommandPath(childPath) || c.scope(id, childPath) != branch.scope {
				delete(branches, childPath)
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.generation != generation {
			return nil, ErrSessionCommand
		}
		for childPath, branch := range branches {
			c.storeMenuLocked(commandMenuKey{id, childPath}, branch.menu, branch.scope)
		}
		c.storeMenuLocked(key, menu, scope)
		return menu, nil
	})
	select {
	case <-ctx.Done():
		return SessionCommandMenu{}, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return SessionCommandMenu{}, r.Err
		}
		return browsingCommandMenu(r.Val.(SessionCommandMenu)), nil
	}
}

func (c *sessionCommandCache) storeMenuLocked(key commandMenuKey, menu SessionCommandMenu, scope string) {
	menu = browsingCommandMenu(menu)
	encoded, _ := json.Marshal(menu)
	if len(encoded) <= commandMenuMaxBytes {
		now := c.now()
		c.rememberLocked(key, commandMenuEntry{menu: &menu, scope: scope, at: now, used: now})
	}
}

// Observe normal telemetry; it never waits for catalogue I/O. Only two root
// warmers run at once, with backoff for unavailable catalogues. Deeper levels
// join the same cache when first visited, regardless of path depth.
func (c *sessionCommandCache) observe(usage LiveUsageSnapshot) {
	observations := make(map[string]commandMenuObservation, len(usage.Sessions))
	for _, s := range usage.Sessions {
		observations[s.ID] = commandMenuObservation{s.Name, s.WorkingDirectory, s.ModelSettings}
	}
	c.mu.Lock()
	for id, previous := range c.observations {
		if current, ok := observations[id]; !ok || current != previous {
			c.invalidateLocked(id)
		}
	}
	c.observations = observations
	if !usage.CodexUp {
		c.invalidateLocked("")
	}
	c.mu.Unlock()
	if !usage.CodexUp {
		return
	}
	for _, s := range usage.Sessions {
		if !s.Active || s.ID == "" {
			continue
		}
		c.warm(s.ID)
	}
}

func (c *sessionCommandCache) warm(id string) {
	if _, ok := c.peek(id, ""); ok {
		return
	}
	scope := c.scope(id, "")
	if scope == "" {
		return
	}
	c.mu.Lock()
	key, now := commandMenuKey{id, ""}, c.now()
	if e, ok := c.entries[key]; ok && e.scope == scope && now.Sub(e.at) < 15*time.Second {
		c.mu.Unlock()
		return
	}
	select {
	case c.warmers <- struct{}{}:
	default:
		c.mu.Unlock()
		return
	}
	c.rememberLocked(key, commandMenuEntry{scope: scope, at: now, used: now})
	c.mu.Unlock()
	go func() {
		defer func() { <-c.warmers }()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		_, _ = c.load(ctx, id, "", false)
	}()
}

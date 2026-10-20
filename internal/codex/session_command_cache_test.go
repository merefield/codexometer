package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type menuCacheSource struct {
	mu      sync.Mutex
	reads   map[string]int
	epoch   string
	fail    bool
	help    string
	started chan string
	release chan struct{}
}

func (f *menuCacheSource) sessionCommandScope(string, string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.epoch
}

func (f *menuCacheSource) SessionCommands(ctx context.Context, id, path string) (SessionCommandMenu, error) {
	f.mu.Lock()
	if f.reads == nil {
		f.reads = map[string]int{}
	}
	f.reads[id+":"+path]++
	fail, help, epoch := f.fail, f.help, f.epoch
	f.mu.Unlock()
	if f.started != nil {
		f.started <- id + ":" + path
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return SessionCommandMenu{}, ctx.Err()
		}
	}
	if fail {
		return SessionCommandMenu{}, ErrSessionCommand
	}
	return SessionCommandMenu{Path: path, Revision: epoch, Choices: []SessionCommandChoice{{
		ID: "choice", Label: "Option", Help: help, Action: true,
		method: "thread/settings/update", params: map[string]any{"threadId": id},
	}}}, nil
}

func (f *menuCacheSource) ExecuteSessionCommand(context.Context, string, string, string, string) error {
	return errors.New("cache must never execute actions")
}

func (f *menuCacheSource) count(id, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads[id+":"+path]
}

func loadMenu(t *testing.T, c *sessionCommandCache, id, path string) SessionCommandMenu {
	t.Helper()
	menu, err := c.load(context.Background(), id, path, false)
	if err != nil {
		t.Fatal(err)
	}
	return menu
}

func TestCommandCacheRetainsTreeAndIsolatesSessionsAndCopies(t *testing.T) {
	f := &menuCacheSource{epoch: "connection-one"}
	c := newSessionCommandCache(f)
	for _, path := range []string{"", "model", "model/small", "nested/one/two/three"} {
		menu := loadMenu(t, c, "one", path)
		menu.Choices[0].Label = "Mutated by caller"
		again := loadMenu(t, c, "one", path)
		if again.Choices[0].Label != "Option" || again.Choices[0].method != "" || again.Choices[0].params != nil || !again.Choices[0].Action || f.count("one", path) != 1 {
			t.Fatal("cache lost metadata isolation or retained executable parameters")
		}
		loadMenu(t, c, "two", path)
		if f.count("two", path) != 1 {
			t.Fatal("menus crossed session boundaries")
		}
	}
	loadMenu(t, c, "one", "//model")
	if f.count("one", "//model") != 1 {
		t.Fatal("cache changed the source semantics of a double-slash path")
	}
}

func TestCommandCacheExpiryRefreshAndConnectionScope(t *testing.T) {
	f := &menuCacheSource{epoch: "first"}
	c := newSessionCommandCache(f)
	now := time.Unix(100, 0)
	c.now = func() time.Time { return now }
	loadMenu(t, c, "one", "model")
	now = now.Add(commandMenuLifetime)
	loadMenu(t, c, "one", "model")
	if f.count("one", "model") != 2 {
		t.Fatal("expired menu was reused")
	}
	if _, err := c.load(context.Background(), "one", "model", true); err != nil || f.count("one", "model") != 3 {
		t.Fatal("explicit refresh did not bypass cache", err)
	}
	f.mu.Lock()
	f.epoch = "reconnected"
	f.mu.Unlock()
	if _, ok := c.peek("one", "model"); ok {
		t.Fatal("old connection menu remained available")
	}
	if got := loadMenu(t, c, "one", "model"); got.Revision != "reconnected" {
		t.Fatal("connection scope was not refreshed")
	}
	f.mu.Lock()
	f.epoch = ""
	f.mu.Unlock()
	if _, ok := c.peek("one", "model"); ok {
		t.Fatal("disconnected cache remained available")
	}
}

func TestCommandCacheDeduplicatesReadsAndCallerCancellation(t *testing.T) {
	f := &menuCacheSource{epoch: "one", started: make(chan string, 20), release: make(chan struct{})}
	c := newSessionCommandCache(f)
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := c.load(ctx, "one", "model", false); first <- err }()
	<-f.started
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := c.load(context.Background(), "one", "model", false); err != nil {
				t.Error(err)
			}
		})
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled caller did not return promptly", err)
	}
	close(f.release)
	wg.Wait()
	if f.count("one", "model") != 1 {
		t.Fatal("concurrent browsing made duplicate catalogue reads")
	}
}

func TestCommandCacheRejectsInvalidatedInflightResult(t *testing.T) {
	for _, scopeChange := range []bool{false, true} {
		t.Run(fmt.Sprint(scopeChange), func(t *testing.T) {
			f := &menuCacheSource{epoch: "old", started: make(chan string, 1), release: make(chan struct{})}
			c := newSessionCommandCache(f)
			done := make(chan error, 1)
			go func() { _, err := c.load(context.Background(), "one", "model", false); done <- err }()
			<-f.started
			if scopeChange {
				f.mu.Lock()
				f.epoch = "new"
				f.mu.Unlock()
			} else {
				c.invalidate("one")
			}
			close(f.release)
			if err := <-done; !errors.Is(err, ErrSessionCommand) {
				t.Fatal("invalidated in-flight result was returned", err)
			}
			if _, ok := c.peek("one", "model"); ok {
				t.Fatal("invalidated in-flight result repopulated cache")
			}
		})
	}
}

func TestCommandCacheBoundsAndExcludesTypedArgumentsAndFailures(t *testing.T) {
	f := &menuCacheSource{epoch: "one"}
	c := newSessionCommandCache(f)
	for i := range commandMenuLimit + 4 {
		loadMenu(t, c, "one", fmt.Sprintf("branch/%d", i))
	}
	if len(c.entries) != commandMenuLimit {
		t.Fatal("unbounded menu retention")
	}
	for _, path := range []string{"rename/secret-name", "cd/%2Fprivate", "rename secret", "cd /private", "/rename/name", "//rename/name", "//cd/private"} {
		loadMenu(t, c, "one", path)
		loadMenu(t, c, "one", path)
		if f.count("one", path) != 2 {
			t.Fatal("typed argument was retained", path)
		}
	}
	f.fail = true
	if _, err := c.load(context.Background(), "one", "failed", false); err == nil {
		t.Fatal("missing source failure")
	}
	f.fail = false
	loadMenu(t, c, "one", "failed")
	if f.count("one", "failed") != 2 {
		t.Fatal("failure prevented recovery")
	}
	f.help = strings.Repeat("x", commandMenuMaxBytes)
	loadMenu(t, c, "one", "oversized")
	if _, ok := c.peek("one", "oversized"); ok {
		t.Fatal("oversized menu was retained")
	}
}

func TestCommandCacheWarmsWithoutBlockingAndInvalidatesObservedSettings(t *testing.T) {
	f := &menuCacheSource{epoch: "one", started: make(chan string, 20), release: make(chan struct{})}
	c := newSessionCommandCache(f)
	usage := LiveUsageSnapshot{CodexUp: true, Sessions: []LiveUsageSession{{ID: "one", Active: true, Name: "A", WorkingDirectory: "/work"}}}
	c.observe(usage) // Must return while the metadata request is blocked.
	<-f.started
	close(f.release)
	loadMenu(t, c, "one", "") // Joins the startup warm, rather than repeating it.
	if f.count("one", "") != 1 {
		t.Fatal("first slash repeated the warmed catalogue fetch")
	}
	loadMenu(t, c, "one", "model/small")
	usage.Sessions[0].Working = true
	c.observe(usage)
	if _, ok := c.peek("one", "model/small"); !ok {
		t.Fatal("unrelated snapshot churn cleared the tree")
	}
	usage.Sessions[0].Name = "Renamed"
	usage.Sessions[0].Active = false // Check invalidation without scheduling another warm.
	c.observe(usage)
	if _, ok := c.peek("one", "model/small"); ok {
		t.Fatal("renamed session retained old metadata")
	}
	loadMenu(t, c, "one", "model")
	usage.Sessions[0].ModelSettings.Model = "changed-model"
	c.observe(usage)
	if _, ok := c.peek("one", "model"); ok {
		t.Fatal("changed model retained old metadata")
	}
	loadMenu(t, c, "one", "model")
	c.observe(LiveUsageSnapshot{})
	if _, ok := c.peek("one", "model"); ok {
		t.Fatal("removed or disconnected session retained old metadata")
	}
}

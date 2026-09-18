//go:build unix

package codex

import (
	"context"
	"errors"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type quotaDaemonFixture struct {
	mu       sync.Mutex
	sessions map[string]QuotaSession
	writes   []map[string]any
	fail     string
	queued   bool
	started  chan struct{}
	release  chan struct{}
}

func newQuotaDaemon(t *testing.T) (*daemonStatusProvider, *quotaDaemonFixture) {
	t.Helper()
	fixture := &quotaDaemonFixture{sessions: map[string]QuotaSession{"one": {ID: "one", Model: "large", Effort: "high"}, "two": {ID: "two", Model: "large", Effort: "high"}}}
	dir, err := os.MkdirTemp("", "cxq-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(dir) })
	socket := filepath.Join(dir, "q.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skip("sandbox does not permit Unix-domain socket listeners")
		}
		t.Fatal(err)
	}
	upgrader := websocket.Upgrader{}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var req struct {
				ID     *int64
				Method string
				Params map[string]any
			}
			if conn.ReadJSON(&req) != nil {
				return
			}
			if req.ID == nil {
				continue
			}
			fixture.mu.Lock()
			result := map[string]any{}
			failure := false
			id, _ := req.Params["threadId"].(string)
			switch req.Method {
			case "initialize":
				result["userAgent"] = "test"
			case "model/list":
				result["data"] = []any{map[string]any{"model": "small", "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "medium"}}, "serviceTiers": []any{map[string]any{"id": "flex", "name": "Fast"}, map[string]any{"id": "priority", "name": "Slow"}}}}
			case "thread/loaded/list":
				ids := []string{}
				for id := range fixture.sessions {
					ids = append(ids, id)
				}
				result["data"] = ids
			case "thread/resume":
				s, ok := fixture.sessions[id]
				failure = !ok || fixture.fail == id
				result = map[string]any{"model": s.Model, "reasoningEffort": s.Effort, "serviceTier": s.Tier, "thread": map[string]any{"id": id}}
			case "thread/settings/update":
				fixture.writes = append(fixture.writes, req.Params)
				if !fixture.queued {
					s := fixture.sessions[id]
					if v, ok := req.Params["model"].(string); ok {
						s.Model = v
					}
					if v, ok := req.Params["effort"].(string); ok {
						s.Effort = v
					}
					if v, ok := req.Params["serviceTier"]; ok {
						s.Tier = nil
						if value, ok := v.(string); ok {
							s.Tier = &value
						}
					}
					fixture.sessions[id] = s
				}
				if fixture.started != nil {
					close(fixture.started)
					fixture.started = nil
					release := fixture.release
					fixture.mu.Unlock()
					<-release
					fixture.mu.Lock()
				}
			default:
				failure = true
			}
			fixture.mu.Unlock()
			response := map[string]any{"id": *req.ID, "result": result}
			if failure {
				delete(response, "result")
				response["error"] = map[string]any{"code": -1, "message": "test failure"}
			}
			if conn.WriteJSON(response) != nil {
				return
			}
		}
	})}
	go server.Serve(listener)
	p := &daemonStatusProvider{socketPath: socket}
	t.Cleanup(func() { p.disconnect(nil); server.Close() })
	return p, fixture
}
func TestQuotaSettingsCatalogueDriftAndRestoration(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	sessions, err := p.QuotaSessions(ctx)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("scan %v %v", sessions, err)
	}
	n, err := p.ApplyQuotaProfile(ctx, sessions, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "slow"})
	if err != nil || n != 2 {
		t.Fatalf("apply %d %v", n, err)
	}
	f.mu.Lock()
	if *f.sessions["one"].Tier != "priority" {
		t.Error("slow did not resolve by catalogue name")
	}
	s := f.sessions["one"]
	s.Model = "manual"
	s.Tier = nil
	f.sessions["one"] = s
	f.fail = "two"
	f.mu.Unlock()
	n, err = p.RestoreSessionSettings(ctx)
	if n != 1 || err == nil {
		t.Fatalf("partial restore %d %v", n, err)
	}
	f.mu.Lock()
	s = f.sessions["one"]
	f.fail = ""
	f.mu.Unlock()
	if s.Model != "manual" || s.Effort != "high" || s.Tier != nil {
		t.Fatalf("manual settings clobbered: %#v", s)
	}
	n, err = p.CloseQuotaProfiles(ctx)
	if n != 1 || err != nil {
		t.Fatalf("close %d %v", n, err)
	}
	if _, err = p.ApplyQuotaProfile(ctx, sessions, QuotaStep{Model: "small", Effort: "medium"}); err == nil {
		t.Fatal("write after close accepted")
	}
}
func TestQuotaSettingsRejectUnsupportedAndChangedTargets(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	sessions, err := p.QuotaSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []QuotaStep{{Model: "missing", Effort: "medium"}, {Model: "small", Effort: "ultra"}, {Model: "small", Effort: "medium", ServiceTier: "unknown"}} {
		if _, err := p.ApplyQuotaProfile(ctx, sessions, step); err == nil {
			t.Fatalf("accepted %#v", step)
		}
	}
	f.mu.Lock()
	s := f.sessions["one"]
	s.Effort = "low"
	f.sessions["one"] = s
	delete(f.sessions, "two")
	f.mu.Unlock()
	n, err := p.ApplyQuotaProfile(ctx, sessions, QuotaStep{Model: "small", Effort: "medium"})
	if n != 0 || err == nil {
		t.Fatalf("drift %d %v", n, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 0 {
		t.Fatal("wrote unapproved settings")
	}
}
func TestQuotaSettingsPreservesUnownedSpeedAndStandardClearsTier(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	sessions, _ := p.QuotaSessions(ctx)
	if _, err := p.ApplyQuotaProfile(ctx, sessions, QuotaStep{Model: "small", Effort: "medium"}); err != nil {
		t.Fatal(err)
	}
	tier := "flex"
	f.mu.Lock()
	s := f.sessions["one"]
	s.Tier = &tier
	f.sessions["one"] = s
	f.mu.Unlock()
	if _, err := p.RestoreSessionSettings(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	s = f.sessions["one"]
	f.mu.Unlock()
	if s.Tier == nil || *s.Tier != "flex" {
		t.Fatal("unowned speed overwritten")
	}
	sessions, _ = p.QuotaSessions(ctx)
	if _, err := p.ApplyQuotaProfile(ctx, sessions, QuotaStep{Model: "small", Effort: "medium", ServiceTier: "default"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions["one"].Tier != nil {
		t.Fatal("standard must clear tier")
	}
}
func TestQuotaSettingsNoopCloseDoesNotConnect(t *testing.T) {
	p := &daemonStatusProvider{socketPath: "/missing/no-socket"}
	if n, err := p.CloseQuotaProfiles(context.Background()); n != 0 || err != nil {
		t.Fatalf("noop close %d %v", n, err)
	}
}

func TestQuotaRestorationOnlyTouchesOwnedUnmodifiedFields(t *testing.T) {
	tier := "flex"
	manualTier := "priority"
	original := QuotaSession{ID: "one", Model: "large", Effort: "high"}
	applied := QuotaSession{ID: "one", Model: "small", Effort: "medium", Tier: &tier}
	owner := quotaOwnership{original: original, applied: applied, tierChanged: true}
	restored, params := quotaRestoration(owner, applied)
	if !sameQuotaSettings(restored, original) || len(params) != 4 {
		t.Fatalf("full restore %#v %#v", restored, params)
	}
	current := applied
	current.Model = "manual"
	current.Tier = &manualTier
	restored, params = quotaRestoration(owner, current)
	if restored.Model != "manual" || restored.Effort != "high" || !tierEqual(restored.Tier, &manualTier) || len(params) != 2 {
		t.Fatalf("manual restore %#v %#v", restored, params)
	}
	owner.tierChanged = false
	restored, params = quotaRestoration(owner, applied)
	if _, ok := params["serviceTier"]; ok || !tierEqual(restored.Tier, applied.Tier) {
		t.Fatal("unowned speed overwritten")
	}
	owner.pending = true
	_, params = quotaRestoration(owner, original)
	if params["model"] != "large" || params["effort"] != "high" {
		t.Fatal("uncertain write has no compensating restore")
	}
}
func TestQuotaSettingsCloseDrainsInflightWrite(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	sessions, _ := p.QuotaSessions(ctx)
	started, release := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.started = started
	f.release = release
	f.mu.Unlock()
	applied := make(chan error, 1)
	closed := make(chan error, 1)
	go func() {
		_, err := p.ApplyQuotaProfile(ctx, sessions[:1], QuotaStep{Model: "small", Effort: "medium"})
		applied <- err
	}()
	<-started
	go func() { _, err := p.CloseQuotaProfiles(ctx); closed <- err }()
	select {
	case <-closed:
		t.Fatal("close raced the write")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-applied; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions["one"].Model != "large" {
		t.Fatal("inflight profile survived close")
	}
}
func TestQuotaSettingsQueuedAcknowledgementIsNotSuccess(t *testing.T) {
	p, f := newQuotaDaemon(t)
	sessions, _ := p.QuotaSessions(context.Background())
	f.mu.Lock()
	f.queued = true
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	n, err := p.ApplyQuotaProfile(ctx, sessions[:1], QuotaStep{Model: "small", Effort: "medium"})
	if n != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unverified write %d %v", n, err)
	}
	if _, err := p.CloseQuotaProfiles(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 2 || f.writes[1]["model"] != "large" {
		t.Fatalf("missing queued compensation: %#v", f.writes)
	}
}

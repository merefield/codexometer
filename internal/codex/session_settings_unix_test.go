//go:build unix

package codex

import (
	"context"
	"encoding/json"
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
	notify   bool
	started  chan struct{}
	release  chan struct{}
}

func TestQuotaEffortDefaultFallback(t *testing.T) {
	for _, tc := range []struct {
		payload, effort string
		want            bool
	}{
		{`{"defaultReasoningEffort":"medium","supportedReasoningEfforts":[]}`, "medium", true},
		{`{"defaultReasoningEffort":"medium"}`, "medium", true},
		{`{"defaultReasoningEffort":"medium"}`, "high", false},
		{`{}`, "medium", false},
		{`{"defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"high"}]}`, "medium", false},
		{`{"defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"high"}]}`, "high", true},
	} {
		var model benchmarkModel
		if err := json.Unmarshal([]byte(tc.payload), &model); err != nil {
			t.Fatal(err)
		}
		if got := quotaEffortSupported(model, tc.effort); got != tc.want {
			t.Fatalf("payload %s effort %s: got %v want %v", tc.payload, tc.effort, got, tc.want)
		}
	}
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
			var settingsNotification map[string]any
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
				desired := fixture.sessions[id]
				if v, ok := req.Params["model"].(string); ok {
					desired.Model = v
				}
				if v, ok := req.Params["effort"].(string); ok {
					desired.Effort = v
				}
				if v, ok := req.Params["serviceTier"]; ok {
					desired.Tier = nil
					if value, ok := v.(string); ok {
						desired.Tier = &value
					}
				}
				if !fixture.queued {
					fixture.sessions[id] = desired
				}
				if fixture.notify {
					settingsNotification = map[string]any{
						"method": "thread/settings/updated",
						"params": map[string]any{"threadId": id, "threadSettings": map[string]any{
							"model": desired.Model, "effort": desired.Effort, "serviceTier": desired.Tier,
						}},
					}
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
			if settingsNotification != nil && conn.WriteJSON(settingsNotification) != nil {
				return
			}
		}
	})}
	go server.Serve(listener)
	p := &daemonStatusProvider{socketPath: socket}
	t.Cleanup(func() { p.disconnect(nil); server.Close() })
	return p, fixture
}
func TestQuotaSettingsPersistAfterCloseAndFreshController(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	sessions, err := p.QuotaSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	step := QuotaStep{Model: "small", Effort: "medium", ServiceTier: "slow"}
	n, err := p.ApplyQuotaProfile(ctx, sessions, step)
	if err != nil || n != 2 {
		t.Fatalf("apply %d %v", n, err)
	}
	p.CloseQuotaProfiles()
	fresh := &daemonStatusProvider{socketPath: p.socketPath}
	t.Cleanup(func() { fresh.disconnect(nil) })
	resolved, err := fresh.ResolveQuotaStep(ctx, step)
	if err != nil || resolved.ServiceTier != "priority" {
		t.Fatalf("resolve %#v %v", resolved, err)
	}
	current, err := fresh.QuotaSessions(ctx)
	if err != nil || len(current) != 2 {
		t.Fatalf("read %#v %v", current, err)
	}
	for _, session := range current {
		if !session.MatchesQuotaStep(resolved) {
			t.Fatalf("profile did not persist: %#v", session)
		}
	}
	if _, err := p.ApplyQuotaProfile(ctx, sessions, step); err == nil {
		t.Fatal("write after close accepted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 2 {
		t.Fatal("close issued settings writes")
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
	if _, err := p.ApplyQuotaProfile(ctx, []QuotaSession{s}, QuotaStep{Model: "small", Effort: "medium"}); err != nil {
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
func TestQuotaSettingsTreatsExplicitAndImplicitDefaultTierAsEquivalent(t *testing.T) {
	p, f := newQuotaDaemon(t)
	explicitDefault := "default"
	f.mu.Lock()
	s := f.sessions["one"]
	s.Tier = &explicitDefault
	f.sessions["one"] = s
	f.mu.Unlock()

	sessions, err := p.QuotaSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sessions[0].MatchesQuotaStep(QuotaStep{Model: sessions[0].Model, Effort: sessions[0].Effort, ServiceTier: "default"}) {
		t.Fatal("explicit default tier was offered as a redundant profile change")
	}
	if !sameQuotaSettings(sessions[0], QuotaSession{Model: sessions[0].Model, Effort: sessions[0].Effort}) {
		t.Fatal("explicit and implicit default tiers compare unequal")
	}
}
func TestQuotaSettingsNoopCloseDoesNotConnect(t *testing.T) {
	p := &daemonStatusProvider{socketPath: "/missing/no-socket"}
	p.CloseQuotaProfiles()
	if !p.settingsClosed {
		t.Fatal("controller not closed")
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
	go func() { p.CloseQuotaProfiles(); closed <- nil }()
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
	if f.sessions["one"].Model != "small" {
		t.Fatal("close reverted approved profile")
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
	p.CloseQuotaProfiles()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 1 {
		t.Fatalf("unexpected rollback: %#v", f.writes)
	}
}

func TestQuotaSettingsOldNotificationCannotVerifyNewWrite(t *testing.T) {
	p, f := newQuotaDaemon(t)
	sessions, err := p.QuotaSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.handleNotification("thread/settings/updated", json.RawMessage(`{"threadId":"one","threadSettings":{"model":"small","effort":"medium","serviceTier":null}}`))
	// Live readback has drifted from the previously observed target.
	f.mu.Lock()
	f.queued = true
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	n, err := p.ApplyQuotaProfile(ctx, sessions[:1], QuotaStep{Model: "small", Effort: "medium"})
	if n != 0 || !errors.Is(err, ErrQuotaProfileUnverified) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stale observation verified write: %d %v", n, err)
	}
}

func TestQuotaSettingsLostAcknowledgementIsUncertain(t *testing.T) {
	p, f := newQuotaDaemon(t)
	sessions, err := p.QuotaSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.started = started
	f.release = release
	f.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := p.ApplyQuotaProfile(ctx, sessions[:1], QuotaStep{Model: "small", Effort: "medium"})
		done <- err
	}()
	<-started
	cancel()
	err = <-done
	close(release)
	if !errors.Is(err, ErrQuotaProfileUncertain) || errors.Is(err, ErrQuotaProfileUnverified) {
		t.Fatalf("lost acknowledgement misclassified: %v", err)
	}
}

func TestQuotaSettingsUpdatedNotificationVerifiesQueuedWrite(t *testing.T) {
	p, f := newQuotaDaemon(t)
	sessions, _ := p.QuotaSessions(context.Background())
	f.mu.Lock()
	f.queued = true // thread/resume deliberately remains stale
	f.notify = true
	f.mu.Unlock()
	n, err := p.ApplyQuotaProfile(context.Background(), sessions[:1], QuotaStep{Model: "small", Effort: "medium"})
	if err != nil || n != 1 {
		t.Fatalf("authoritative notification was not accepted: %d %v", n, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions["one"].Model != "large" {
		t.Fatal("fixture no longer proves resume readback stayed stale")
	}
}

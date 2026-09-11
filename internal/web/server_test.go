package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func testServer() *server {
	return &server{store: newStore(), host: "127.0.0.1:32123", pairSecret: "one-use-secret", pairUntil: time.Now().Add(time.Minute), streams: make(chan struct{}, 16)}
}

func request(s *server, method, path, body, token string) *http.Request {
	r := httptest.NewRequest(method, "http://"+s.host+path, strings.NewReader(body))
	r.Header.Set("Origin", "http://"+s.host)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func pairBrowser(t *testing.T, s *server) string {
	t.Helper()
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, request(s, "POST", "/api/pair", `{"secret":"one-use-secret"}`, ""))
	if w.Code != 200 {
		t.Fatalf("pair: %d %s", w.Code, w.Body)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Token == "" {
		t.Fatalf("pair response: %s", w.Body)
	}
	return result.Token
}

func TestSecurityBoundary(t *testing.T) {
	s := testServer()
	token := pairBrowser(t, s)
	for _, test := range []struct {
		name, method, path string
		edit               func(*http.Request)
		want               int
	}{
		{"authorized", "GET", "/api/state", func(r *http.Request) {}, 200},
		{"no token", "GET", "/api/state", func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"wrong token", "GET", "/api/state", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }, 401},
		{"query token rejected", "GET", "/api/state?token=" + token, func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"cookie token rejected", "GET", "/api/state", func(r *http.Request) { r.Header.Del("Authorization"); r.Header.Set("Cookie", "token="+token) }, 401},
		{"DNS rebinding", "GET", "/api/state", func(r *http.Request) { r.Host = "attacker.example:32123" }, 403},
		{"evil origin", "GET", "/api/state", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"other local port", "GET", "/api/state", func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:54321") }, 403},
		{"null origin", "GET", "/api/state", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"cross site", "GET", "/api/state", func(r *http.Request) { r.Header.Del("Origin"); r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"same site not same origin", "GET", "/api/state", func(r *http.Request) { r.Header.Del("Origin"); r.Header.Set("Sec-Fetch-Site", "same-site") }, 403},
		{"no browser headers still needs token", "GET", "/api/state", func(r *http.Request) { r.Header.Del("Origin"); r.Header.Del("Sec-Fetch-Site") }, 200},
		{"no writes", "POST", "/api/state", func(r *http.Request) {}, 404},
		{"no approval endpoint", "POST", "/api/approve", func(r *http.Request) {}, 404},
		{"no reset endpoint", "POST", "/api/reset", func(r *http.Request) {}, 404},
		{"no prompt endpoint", "POST", "/api/prompt", func(r *http.Request) {}, 404},
		{"no benchmark endpoint", "POST", "/api/benchmark", func(r *http.Request) {}, 404},
		{"no CORS preflight", "OPTIONS", "/api/state", func(r *http.Request) {}, 404},
		{"public shell", "GET", "/", func(r *http.Request) { r.Header.Del("Authorization") }, 200},
		{"no directory listing", "GET", "/assets/", func(r *http.Request) {}, 404},
		{"no source files", "GET", "/server.go", func(r *http.Request) {}, 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := request(s, test.method, test.path, "", token)
			test.edit(r)
			w := httptest.NewRecorder()
			s.handler().ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("got %d, want %d: %s", w.Code, test.want, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("missing safety headers")
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Set-Cookie") != "" {
				t.Fatal("ambient auth/CORS introduced")
			}
		})
	}
}

func TestPairingOneUseExpiryAndValidation(t *testing.T) {
	for _, test := range []struct {
		name, body string
		edit       func(*server, *http.Request)
		want       int
	}{
		{"wrong secret", `{"secret":"wrong"}`, func(*server, *http.Request) {}, 401},
		{"expired", `{"secret":"one-use-secret"}`, func(s *server, _ *http.Request) { s.pairUntil = time.Now().Add(-time.Second) }, 401},
		{"malformed", `{`, func(*server, *http.Request) {}, 400},
		{"trailing JSON", `{"secret":"one-use-secret"}{}`, func(*server, *http.Request) {}, 400},
		{"unknown field", `{"secret":"one-use-secret","extra":1}`, func(*server, *http.Request) {}, 400},
		{"large body", strings.Repeat("x", 2048), func(*server, *http.Request) {}, 400},
		{"form", `{"secret":"one-use-secret"}`, func(_ *server, r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 403},
		{"missing origin", `{"secret":"one-use-secret"}`, func(_ *server, r *http.Request) { r.Header.Del("Origin") }, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testServer()
			r := request(s, "POST", "/api/pair", test.body, "")
			test.edit(s, r)
			w := httptest.NewRecorder()
			s.handler().ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("got %d: %s", w.Code, w.Body)
			}
		})
	}
	s := testServer()
	token := pairBrowser(t, s)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, request(s, "POST", "/api/pair", `{"secret":"one-use-secret"}`, ""))
	if w.Code != 401 {
		t.Fatal("pairing link reused")
	}
	s.tokenUntil = time.Now().Add(-time.Second)
	w = httptest.NewRecorder()
	s.handler().ServeHTTP(w, request(s, "GET", "/api/state", "", token))
	if w.Code != 401 {
		t.Fatal("expired browser capability accepted")
	}
}

func TestConcurrentPairingOnlyOneSucceeds(t *testing.T) {
	s := testServer()
	var wg sync.WaitGroup
	results := make(chan int, 10)
	for range 10 {
		wg.Go(func() {
			w := httptest.NewRecorder()
			s.handler().ServeHTTP(w, request(s, "POST", "/api/pair", `{"secret":"one-use-secret"}`, ""))
			results <- w.Code
		})
	}
	wg.Wait()
	close(results)
	success := 0
	for code := range results {
		if code == 200 {
			success++
		} else if code != 401 {
			t.Fatalf("unexpected %d", code)
		}
	}
	if success != 1 {
		t.Fatalf("%d pairing successes", success)
	}
}

func TestLiveEventsAndCancellation(t *testing.T) {
	s := testServer()
	token := pairBrowser(t, s)
	listener := httptest.NewServer(s.handler())
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", listener.URL+"/api/events", nil)
	r.Host = s.host
	r.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%v", response)
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, "data: ") || !strings.Contains(line, `"version"`) {
		t.Fatalf("event %q %v", line, err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	s.store.quota(codex.DemoSnapshot(), nil, time.Now())
	line, err = reader.ReadString('\n')
	if err != nil || !strings.Contains(line, `"used":`) {
		t.Fatalf("update %q %v", line, err)
	}
	cancel()
}

func TestStreamCapacity(t *testing.T) {
	s := testServer()
	token := pairBrowser(t, s)
	for range cap(s.streams) {
		s.streams <- struct{}{}
	}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, request(s, "GET", "/api/events", "", token))
	if w.Code != 429 {
		t.Fatalf("got %d", w.Code)
	}
}

type cancelledSource struct{}

func (cancelledSource) Fetch(ctx context.Context) (codex.Snapshot, error) {
	<-ctx.Done()
	return codex.Snapshot{}, ctx.Err()
}
func (cancelledSource) FetchTokenUsage(ctx context.Context) (codex.LiveUsageSnapshot, error) {
	<-ctx.Done()
	return codex.LiveUsageSnapshot{}, ctx.Err()
}
func (cancelledSource) FetchAccountUsage(ctx context.Context) (codex.AccountUsage, error) {
	<-ctx.Done()
	return codex.AccountUsage{}, ctx.Err()
}

func TestRunStopsCollectorsAndServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	defer reader.Close()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cancelledSource{}, time.Minute, 0, writer); writer.Close() }()
	buffer := make([]byte, 2048)
	n, err := reader.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buffer[:n]), "http://127.0.0.1:") || !strings.Contains(string(buffer[:n]), "#pair=") {
		t.Fatalf("unsafe/missing launch link: %s", buffer[:n])
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server or poller leaked on shutdown")
	}
	if err := Run(context.Background(), cancelledSource{}, time.Minute, -1, io.Discard); err == nil {
		t.Fatal("invalid port accepted")
	}
}

func TestDisplayProjectionAndAccountIsolation(t *testing.T) {
	s := newStore()
	now := time.Now()
	q := codex.DemoSnapshot()
	q.AccountFingerprint = "account-A"
	s.quota(q, nil, now)
	s.usage(codex.AccountUsage{AccountFingerprint: "account-A", DailyUsageBuckets: []codex.AccountUsageDay{{StartDate: "2026-09-01", Tokens: 5}}}, nil, now)
	s.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "id", Active: true, Working: true, TotalTokens: 25, Context: codex.SessionContext{Text: "visible reply", ApprovalToken: "secret-approval", InputToken: "secret-input", ThreadID: "capability-id"}}}}, nil, now)
	data, _ := s.snapshot()
	for _, forbidden := range []string{"account-A", "secret-approval", "secret-input", "capability-id", "ApprovalToken", "InputToken"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("leaked %s", forbidden)
		}
	}
	if !strings.Contains(string(data), "visible reply") || s.state.Usage == nil {
		t.Fatal("display data missing")
	}
	q.AccountFingerprint = "account-B"
	s.quota(q, nil, now)
	if s.state.Usage != nil {
		t.Fatal("history leaked between accounts")
	}
	s.usage(codex.AccountUsage{AccountFingerprint: "account-A"}, nil, now)
	if s.state.Usage != nil {
		t.Fatal("late old-account history shown")
	}
	s.quota(q, errors.New("secret-upstream-error"), now)
	data, _ = s.snapshot()
	if strings.Contains(string(data), "secret-upstream-error") || !s.state.QuotaError {
		t.Fatal("error not safely projected")
	}
	s.live(codex.LiveUsageSnapshot{}, errors.New("private"), now)
	if s.state.Sessions[0].Status != "STALE" {
		t.Fatal("failed refresh kept authoritative status")
	}
}

func TestSynchronizedBoundedSamples(t *testing.T) {
	s := newStore()
	now := time.Now()
	for tick := range 130 {
		rows := []codex.LiveUsageSession{{ID: "a", TotalTokens: int64(100 + tick*10)}, {ID: "b", TotalTokens: int64(200 + tick*20)}}
		s.live(codex.LiveUsageSnapshot{Sessions: rows}, nil, now.Add(time.Duration(tick)*30*time.Second))
	}
	a, b := s.samples["a"], s.samples["b"]
	if len(a) != 120 || len(b) != 120 || a[119].Tokens != 10 || b[119].Tokens != 20 || !a[119].At.Equal(b[119].At) {
		t.Fatalf("samples: %v / %v", a, b)
	}
	s.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "a", TotalTokens: 1}}}, nil, now.Add(131*30*time.Second))
	if len(s.samples) != 1 || s.samples["a"][119].Tokens != 0 {
		t.Fatal("removed session retained or counter regression became usage")
	}
}

func TestSessionStatus(t *testing.T) {
	for _, test := range []struct {
		row  codex.LiveUsageSession
		want string
	}{
		{codex.LiveUsageSession{}, "INACTIVE"}, {codex.LiveUsageSession{Active: true}, "IDLE"}, {codex.LiveUsageSession{Working: true}, "WORKING"},
		{codex.LiveUsageSession{Attention: codex.SessionAttentionComplete}, "TURN COMPLETE"},
		{codex.LiveUsageSession{Attention: codex.SessionAttentionCheck}, "CHECK SESSION"},
		{codex.LiveUsageSession{Attention: codex.SessionAttentionInput}, "INPUT NEEDED"},
		{codex.LiveUsageSession{Attention: codex.SessionAttentionApproval}, "APPROVAL NEEDED"},
	} {
		if got := sessionStatus(test.row); got != test.want {
			t.Fatalf("%s != %s", got, test.want)
		}
	}
	for _, kind := range []codex.SessionContextKind{codex.SessionContextReply, codex.SessionContextApproval, codex.SessionContextQuestion, codex.SessionContextActivity} {
		if contextKind(kind) == "" {
			t.Fatal(fmt.Sprint(kind))
		}
	}
}

package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func assertSecurityHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for key, want := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Cross-Origin-Resource-Policy": "same-origin", "Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=(), usb=()"} {
		if w.Header().Get(key) != want {
			t.Fatalf("%s: %q", key, w.Header().Get(key))
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("missing/unsafe security headers")
	}
}

func TestEveryDataEndpointRejectsInvalidAccess(t *testing.T) {
	for _, path := range []string{"/api/state", "/api/events"} {
		for _, kind := range []string{"missing", "wrong", "expired", "cookie", "query", "other-port", "cross-site", "rebinding"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				s := testServer()
				token := pairBrowser(t, s)
				r := request(s, "GET", path, "", token)
				want := 401
				switch kind {
				case "missing":
					r.Header.Del("Authorization")
				case "wrong":
					r.Header.Set("Authorization", "Bearer wrong")
				case "expired":
					s.tokenUntil = time.Now().Add(-time.Second)
				case "cookie":
					r.Header.Del("Authorization")
					r.Header.Set("Cookie", "token="+token)
				case "query":
					r.Header.Del("Authorization")
					r.URL.RawQuery = "token=" + token
				case "other-port":
					r.Header.Set("Origin", "http://127.0.0.1:54321")
					want = 403
				case "cross-site":
					r.Header.Del("Origin")
					r.Header.Set("Sec-Fetch-Site", "cross-site")
					want = 403
				case "rebinding":
					r.Host = "attacker.invalid"
					want = 403
				}
				w := httptest.NewRecorder()
				s.handler().ServeHTTP(w, r)
				if w.Code != want || len(s.streams) != 0 || strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), `"sessions"`) {
					t.Fatalf("unsafe rejection: %d %s", w.Code, w.Body)
				}
				assertSecurityHeaders(t, w)
			})
		}
	}
}

func TestSecurityHeadersOnSuccessfulResponses(t *testing.T) {
	for _, path := range []string{"/", "/api/pair", "/api/state"} {
		t.Run(path, func(t *testing.T) {
			s := testServer()
			method, body, token := "GET", "", ""
			if path == "/api/pair" {
				method, body = "POST", `{"secret":"one-use-secret"}`
			} else if path == "/api/state" {
				token = pairBrowser(t, s)
			}
			w := httptest.NewRecorder()
			s.handler().ServeHTTP(w, request(s, method, path, body, token))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			assertSecurityHeaders(t, w)
		})
	}
}

func TestRejectedPairingDoesNotConsumeSecret(t *testing.T) {
	for _, body := range []string{"{", `{"secret":"wrong"}`, `{"secret":"one-use-secret","extra":true}`, strings.Repeat("x", 2048)} {
		s := testServer()
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, request(s, "POST", "/api/pair", body, ""))
		if w.Code < 400 {
			t.Fatal("invalid pairing accepted")
		}
		assertSecurityHeaders(t, w)
		pairBrowser(t, s)
	}
}

func TestUnsupportedAPIMethodsAndRoutesKeepHeaders(t *testing.T) {
	s := testServer()
	token := pairBrowser(t, s)
	for _, path := range []string{"/api/state", "/api/events", "/api/approve", "/api/prompt", "/api/reset", "/api/benchmark"} {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE"} {
			w := httptest.NewRecorder()
			s.handler().ServeHTTP(w, request(s, method, path, `{}`, token))
			if w.Code < 400 {
				t.Fatalf("unexpected endpoint: %s %s", method, path)
			}
			assertSecurityHeaders(t, w)
		}
	}
}

func TestHTTPTimeoutsAndSSEOverride(t *testing.T) {
	s := testServer()
	token := pairBrowser(t, s)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = s.host; s.handler().ServeHTTP(w, r) })
	cfg := newHTTPServer(context.Background(), h)
	for name, pair := range map[string][2]time.Duration{
		"header read":    {cfg.ReadHeaderTimeout, 5 * time.Second},
		"request read":   {cfg.ReadTimeout, 10 * time.Second},
		"response write": {cfg.WriteTimeout, 15 * time.Second},
		"idle":           {cfg.IdleTimeout, 30 * time.Second},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("%s timeout = %s, want %s", name, pair[0], pair[1])
		}
	}
	if cfg.MaxHeaderBytes != 8192 {
		t.Fatalf("header limit = %d, want 8192", cfg.MaxHeaderBytes)
	}
	// SSE must not inherit an ordinary-response lifetime limit.
	cfg.WriteTimeout = time.Millisecond
	server := httptest.NewUnstartedServer(h)
	server.Config = cfg
	server.Start()
	defer server.Close()
	r, _ := http.NewRequest("GET", server.URL+"/api/events", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for range 2 {
		if _, err = reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(20 * time.Millisecond)
	s.store.quota(codex.DemoSnapshot(), nil, time.Now())
	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, `"used"`) {
		t.Fatalf("SSE expired with response deadline: %q %v", line, err)
	}
}

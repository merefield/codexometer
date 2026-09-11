package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Assets are checked in so go install and terminal-only builds need no Node.
//
//go:embed dist
var assets embed.FS

type server struct {
	store      *store
	host       string
	mu         sync.Mutex
	pairSecret string
	pairUntil  time.Time
	token      string
	tokenUntil time.Time
	streams    chan struct{}
}

// Run binds IPv4 loopback only. Remote hosting, proxy forwarding and write
// operations are intentionally not supported by this experimental release.
func Run(ctx context.Context, source Source, refresh time.Duration, port int, output io.Writer) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("web port must be between 0 and 65535")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer listener.Close()
	s := &server{store: newStore(), host: loopbackAuthority(listener.Addr().String()), pairSecret: rand.Text(), pairUntil: time.Now().Add(5 * time.Minute), streams: make(chan struct{}, 16)}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait := s.store.collect(ctx, source, refresh)
	defer func() { cancel(); wait() }()
	httpServer := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	fmt.Fprintf(output, "Experimental web interface // READ ONLY\nOpen this private, one-use link within 5 minutes:\nhttp://%s/#pair=%s\nKeep this terminal open. Ctrl+C stops the server. Do not share the link.\n", s.host, s.pairSecret)
	stop := context.AfterFunc(ctx, func() { _ = httpServer.Close() })
	defer stop()
	err = httpServer.Serve(listener)
	cancel()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Run only binds IPv4 loopback. Browsers omit the default HTTP port from
// both Host and Origin, so print and validate that same canonical authority.
func loopbackAuthority(address string) string {
	return strings.TrimSuffix(address, ":80")
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/pair", s.pair)
	mux.Handle("GET /api/state", s.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := s.store.snapshot()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})))
	mux.Handle("GET /api/events", s.authorize(http.HandlerFunc(s.events)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	root, _ := fs.Sub(assets, "dist")
	files := http.FileServer(http.FS(root))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// No directory listings, SPA fallback, or file-system paths outside embed.
		if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/assets/") {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") && r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		// Host validation protects against DNS rebinding; Origin and Fetch Metadata
		// reject other websites and other ports on localhost. CORS is never enabled.
		origin := r.Header.Get("Origin")
		site := r.Header.Get("Sec-Fetch-Site")
		if r.Host != s.host || (origin != "" && origin != "http://"+s.host) || (site != "" && site != "same-origin" && site != "none") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *server) pair(w http.ResponseWriter, r *http.Request) {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Header.Get("Origin") != "http://"+s.host || media != "application/json" {
		http.Error(w, "Forbidden", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Secret string `json:"secret"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "Invalid request", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairSecret == "" || !time.Now().Before(s.pairUntil) || subtle.ConstantTimeCompare([]byte(body.Secret), []byte(s.pairSecret)) != 1 {
		http.Error(w, "Pairing link invalid or expired; restart web mode for a new link", 401)
		return
	}
	s.pairSecret = ""
	s.token = rand.Text()
	s.tokenUntil = time.Now().Add(8 * time.Hour)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": s.token})
}

func (s *server) authorized(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token != "" && time.Now().Before(s.tokenUntil) && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) == 1
}

func (s *server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "Pair this browser using the private link printed in your terminal", 401)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	// ServeMux also matches HEAD to GET routes. Never allocate a stream for it.
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		http.Error(w, "Too many live connections", 429)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		if !s.authorized(r) {
			return
		}
		data, changed := s.store.snapshot()
		if controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return
		}
		if controller.Flush() != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-heartbeat.C:
		}
	}
}

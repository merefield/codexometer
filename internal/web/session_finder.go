package web

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

// Authenticated observation only, including in read-only web mode.
func (s *server) savedSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	var page codex.SavedPage
	var err error
	if term := q.Get("q"); term != "" && q.Get("id") == "" && s.searcher != nil {
		page, err = s.searcher.SearchSavedSessions(ctx, q.Get("scope"), q.Get("cursor"), term)
	} else {
		page, err = s.finder.BrowseSavedSessions(ctx, q.Get("scope"), q.Get("cursor"), q.Get("id"))
	}
	if err != nil {
		http.Error(w, "Saved sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (s *server) resumeSession(w http.ResponseWriter, r *http.Request) {
	if !sameOriginJSON(r, "http://"+s.host) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	var target struct{ Scope, Cursor, ID string }
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if json.NewDecoder(r.Body).Decode(&target) != nil || target.ID == "" {
		http.Error(w, "Invalid resume target", http.StatusBadRequest)
		return
	}
	if err := s.resumer.ResumeSavedSession(r.Context(), target.Scope, target.Cursor, target.ID); err != nil {
		http.Error(w, "Resume not confirmed", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) sessionRoute(timeout time.Duration, handler http.HandlerFunc) http.Handler {
	return s.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.savedRequests <- struct{}{}:
			defer func() { <-s.savedRequests }()
		default:
			http.Error(w, "Busy", http.StatusTooManyRequests)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		handler(w, r.WithContext(ctx))
	}))
}

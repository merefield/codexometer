package web

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Authenticated observation only, including in read-only web mode.
func (s *server) savedSessions(w http.ResponseWriter, r *http.Request) {
	select {
	case s.savedRequests <- struct{}{}:
		defer func() { <-s.savedRequests }()
	default:
		http.Error(w, "Busy", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
	defer cancel()
	q := r.URL.Query()
	page, err := s.finder.BrowseSavedSessions(ctx, q.Get("scope"), q.Get("cursor"), q.Get("id"))
	if err != nil {
		http.Error(w, "Saved sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

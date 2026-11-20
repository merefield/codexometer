package web

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"
)

func (s *server) inspectSession(w http.ResponseWriter, r *http.Request) {
	root, child := r.URL.Query().Get("root"), r.URL.Query().Get("agent")
	if !s.inspectableSession(root) {
		http.Error(w, "Session unavailable", http.StatusNotFound)
		return
	}
	select {
	case s.savedRequests <- struct{}{}:
		defer func() { <-s.savedRequests }()
	default:
		http.Error(w, "Busy", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	page, err := s.agents.InspectSessionAgents(ctx, root, child)
	if err != nil || !s.inspectableSession(root) {
		http.Error(w, "Linked agents unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (s *server) inspectableSession(id string) bool {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	return id != "" && !s.store.state.SessionsError && slices.ContainsFunc(s.store.state.Sessions, func(row session) bool { return row.ID == id })
}

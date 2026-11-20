package web

import (
	"encoding/json"
	"net/http"
	"slices"
)

func (s *server) inspectSession(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	root, child := query.Get("root"), query.Get("agent")
	if !s.inspectableSession(root) {
		http.Error(w, "Session unavailable", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	var page any
	var err error
	switch query.Get("kind") {
	case "", "agents":
		if s.agents != nil {
			page, err = s.agents.InspectSessionAgents(ctx, root, child)
		}
	case "terminals":
		if s.terminals != nil && child == "" {
			page, err = s.terminals.InspectSessionTerminals(ctx, root)
		}
	}
	if page == nil || err != nil || !s.inspectableSession(root) {
		http.Error(w, "Session information unavailable", http.StatusServiceUnavailable)
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

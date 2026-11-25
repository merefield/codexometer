package web

import (
	"encoding/json"
	"net/http"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/prose"
)

type threadLinkPreview struct {
	Root            codex.SavedSession  `json:"root"`
	Agent           *codex.SessionAgent `json:"agent,omitempty"`
	Turns           []transcriptTurn    `json:"turns,omitempty"`
	UnverifiedTurns bool                `json:"unverifiedTurns,omitempty"`
}

func (s *server) previewThreadLink(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope, id := q.Get("scope"), q.Get("id")
	if !s.canPreviewThreadLink(scope) || prose.ThreadID("codex://threads/"+id) == "" {
		http.Error(w, "Session unavailable here.", http.StatusNotFound)
		return
	}
	p, err := s.threadLinks.PreviewSessionLink(r.Context(), scope, id)
	if err != nil || !s.canPreviewThreadLink(scope) {
		http.Error(w, "Session unavailable here.", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(threadLinkPreview{p.Root, p.Agent, s.store.projectTranscript(p.Turns), p.UnverifiedTurns})
}

func (s *server) canPreviewThreadLink(scope string) bool {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	return scope != "" && scope == s.store.state.SavedScope && !s.store.state.SessionsError
}

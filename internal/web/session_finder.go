package web

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

// Authenticated observation only, including in read-only web mode.
func (s *server) savedSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	var page codex.SavedPage
	var err error
	term, id := q.Get("q"), q.Get("id")
	if id == "" && s.searcher != nil {
		page, err = s.searcher.SearchSavedSessions(ctx, q.Get("scope"), q.Get("cursor"), term)
	} else {
		page, err = s.finder.BrowseSavedSessions(ctx, q.Get("scope"), q.Get("cursor"), id)
		if term != "" && id == "" {
			keep := codex.SavedSessionNameFilter(term)
			page.Sessions = slices.DeleteFunc(slices.Clone(page.Sessions), func(row codex.SavedSession) bool { return !keep(row) })
		}
	}
	if err != nil {
		http.Error(w, "Saved sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(transcriptPage{Sessions: page.Sessions, Next: page.Next, Turns: s.store.projectTranscript(page.Turns)})
}

type sessionResumeTarget struct {
	sessionMutationConsent
	Cursor, ID string
}

func (t sessionResumeTarget) valid() bool {
	return t.ID != "" && len(t.ID) <= 512 && len(t.Cursor) <= 4096
}

func (t sessionResumeTarget) fields() []string { return []string{t.Cursor, t.ID} }

func (s *server) resumeSession(w http.ResponseWriter, r *http.Request, target sessionResumeTarget) {
	switch s.resumer.ResumeSavedSession(r.Context(), target.Scope, target.Cursor, target.ID) {
	case nil:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Resume not confirmed", http.StatusConflict)
	}
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

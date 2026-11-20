package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
)

type sessionMutationReview struct {
	token  string
	digest [32]byte
	until  time.Time
}

// Lifecycle reviews hold only a digest, not unsent prompt text. Bind consent
// to the exact target and consume it before contacting the daemon.
func (s *server) allowSessionMutation(w http.ResponseWriter, kind, mode, token string, fields ...string) bool {
	if mode != "" && mode != "prepare" {
		http.Error(w, "Invalid session action", http.StatusBadRequest)
		return false
	}
	s.store.mu.Lock()
	required := (confirmation.Policy{Mode: s.store.state.ConfirmationMode}).Required(confirmation.Narrow)
	s.store.mu.Unlock()
	if !required && mode == "" {
		return true
	}
	encoded, _ := json.Marshal(append([]string{kind}, fields...))
	signature := sha256.Sum256(encoded)
	now := time.Now()
	s.mu.Lock()
	if mode == "prepare" {
		review := sessionMutationReview{rand.Text(), signature, now.Add(30 * time.Second)}
		s.lifecycle = review
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"confirmation": review.token, "expires": review.until})
		return false
	}
	review := s.lifecycle
	matched := token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(review.token)) == 1
	if matched {
		s.lifecycle = sessionMutationReview{}
	}
	s.mu.Unlock()
	if !matched || review.digest != signature || !now.Before(review.until) {
		http.Error(w, "Session action requires confirmation", http.StatusConflict)
		return false
	}
	return true
}

type sessionMutationConsent struct{ Scope, Mode, Confirmation string }

func (c sessionMutationConsent) review() sessionMutationConsent { return c }

type sessionMutationTarget interface {
	review() sessionMutationConsent
	valid() bool
	fields() []string
}

func sessionMutationHandler[T sessionMutationTarget](s *server, kind string, maxBody int64, execute func(http.ResponseWriter, *http.Request, T)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOriginJSON(r, "http://"+s.host) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		var t T
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&t) != nil || d.Decode(new(any)) != io.EOF || !t.valid() {
			http.Error(w, "Invalid session action", http.StatusBadRequest)
			return
		}
		c := t.review()
		if c.Scope == "" || len(c.Scope) > 128 {
			http.Error(w, "Invalid session scope", http.StatusBadRequest)
			return
		}
		if s.allowSessionMutation(w, kind, c.Mode, c.Confirmation, append([]string{c.Scope}, t.fields()...)...) {
			execute(w, r, t)
		}
	}
}

type sessionStartTarget struct {
	sessionMutationConsent
	Directory, Prompt string
}

func (t sessionStartTarget) valid() bool {
	return filepath.IsAbs(t.Directory) && len(t.Directory) <= 4096 && strings.TrimSpace(t.Prompt) != ""
}

func (t sessionStartTarget) fields() []string { return []string{t.Directory, t.Prompt} }

func (s *server) startSession(w http.ResponseWriter, r *http.Request, target sessionStartTarget) {
	id, err := s.starter.StartSession(r.Context(), target.Scope, target.Directory, target.Prompt)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, codex.ErrSessionStartDirectory) || errors.Is(err, codex.ErrSessionStartPrompt) || errors.Is(err, codex.ErrSessionStartUnavailable) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

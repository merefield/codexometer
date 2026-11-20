package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"

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

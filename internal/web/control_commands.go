package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"time"
)

type commandRequest struct {
	Mode     string `json:"mode"`
	Path     string `json:"path"`
	Revision string `json:"revision,omitempty"`
	Choice   string `json:"choice,omitempty"`
}

func (c *control) handleCommands(w http.ResponseWriter, r *http.Request, body actionRequest) {
	q := body.Command
	if c.commands == nil || q == nil || body.Review != "" || body.Offer != "" || body.Choice != nil || len(body.Answers) != 0 || body.Schedule != nil || body.EditID != "" || body.SendID != "" || body.CancelID != "" || len(q.Path) > 2048 || len(q.Revision) > 128 || len(q.Choice) > 128 || (q.Mode != "list" && q.Mode != "prepare" && q.Mode != "commit") {
		http.Error(w, "Commands unavailable", 400)
		return
	}
	c.store.mu.Lock()
	_, exists := c.store.contexts[body.Session]
	age := time.Since(c.store.state.SessionsAt)
	fresh := !c.store.state.SessionsError && age >= 0 && age < 10*time.Second
	c.store.mu.Unlock()
	if !exists || !fresh {
		http.Error(w, errUnavailable.Error(), 409)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	if q.Mode == "commit" {
		c.mu.Lock()
		p := c.pending
		if p == nil || p.request.Command == nil || p.id != body.Confirmation || p.request.Session != body.Session || !time.Now().Before(p.until) {
			c.mu.Unlock()
			http.Error(w, errUnavailable.Error(), 409)
			return
		}
		c.pending = nil
		c.mu.Unlock()
		q = p.request.Command
		if err := c.commands.ExecuteSessionCommand(ctx, body.Session, q.Path, q.Revision, q.Choice); err != nil {
			http.Error(w, "Command unconfirmed; check Codex before retrying", 409)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Change requested. Codex will apply it to subsequent turns."})
		return
	}
	m, err := c.commands.SessionCommands(ctx, body.Session, q.Path)
	if err != nil {
		http.Error(w, "Command catalogue unavailable; use Codex", 409)
		return
	}
	if q.Mode == "list" {
		_ = json.NewEncoder(w).Encode(m)
		return
	}
	if m.Revision != q.Revision {
		http.Error(w, errUnavailable.Error(), 409)
		return
	}
	for _, o := range m.Choices {
		if o.ID == q.Choice && o.Action {
			p := &preparedAction{request: body, id: rand.Text(), until: time.Now().Add(30 * time.Second)}
			c.mu.Lock()
			c.pending = p
			c.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"confirmation": p.id, "expires": p.until})
			return
		}
	}
	http.Error(w, errUnavailable.Error(), 409)
}

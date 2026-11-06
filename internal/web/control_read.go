package web

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type resultReadRequest struct {
	Turn     string `json:"turn"`
	Revision string `json:"revision"`
}

func (q *resultReadRequest) valid() bool {
	return q != nil && q.Turn != "" && len(q.Turn) <= 512 && q.Revision != "" && len(q.Revision) <= 2048
}

func (c *control) handleReadResult(w http.ResponseWriter, r *http.Request, body actionRequest) {
	viewed := body.Read
	if c.readReceipts == nil || !viewed.valid() {
		http.Error(w, "Result read state unavailable", 409)
		return
	}
	c.store.mu.Lock()
	age := time.Since(c.store.state.SessionsAt)
	fresh := !c.store.state.SessionsError && age >= 0 && age < 10*time.Second
	eligible := false
	for _, row := range c.store.state.Sessions {
		if row.ID == body.Session && row.Unread && row.ReadStateKnown && row.ReadRevision == viewed.Revision && row.ReplyTurn == viewed.Turn {
			eligible = true
		}
	}
	c.store.mu.Unlock()
	if !fresh || !eligible {
		http.Error(w, "Result changed; review it again", 409)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	state, err := c.readReceipts.AcknowledgeSessionResult(ctx, body.Session, viewed.Turn, viewed.Revision)
	if err != nil || !state.Known() || state.Unread {
		http.Error(w, "Result changed or could not be marked read; review it again", 409)
		return
	}
	c.store.mu.Lock()
	for i := range c.store.state.Sessions {
		row := &c.store.state.Sessions[i]
		if row.ID == body.Session && row.ReadRevision == viewed.Revision {
			row.ReadStateKnown, row.Unread, row.ReadRevision = state.Known(), state.Unread, state.Revision
		}
	}
	c.store.publish()
	c.store.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"read": state.Known() && !state.Unread})
}

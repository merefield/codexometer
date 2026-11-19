package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
)

type commandRequest struct {
	Mode     string `json:"mode"`
	Path     string `json:"path"`
	Revision string `json:"revision,omitempty"`
	Choice   string `json:"choice,omitempty"`
	Refresh  bool   `json:"refresh,omitempty"`
}

type commandMenu struct {
	codex.SessionCommandMenu
	InputReview bool `json:"inputReview,omitempty"`
}

func (c *control) handleCommands(w http.ResponseWriter, r *http.Request, body actionRequest) {
	q := body.Command
	if c.commands == nil || q == nil || body.Review != "" || body.Offer != "" || body.Choice != nil || len(body.Answers) != 0 || body.Schedule != nil || body.EditID != "" || body.SendID != "" || body.CancelID != "" || !codex.ValidSessionCommandPath(q.Path) || len(q.Revision) > 128 || len(q.Choice) > 128 || (q.Mode != "list" && q.Mode != "prepare" && q.Mode != "commit") || q.Refresh && q.Mode != "list" {
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
		var err error
		if q.Path == codex.ScheduleGoalResumePath {
			err = codex.ErrSessionCommand
			if goals, ok := c.commands.(codex.ScheduledGoalClient); ok {
				err = c.schedules.ArmGoalResume(ctx, goals, c.scheduleSnapshot(), time.Now(), body.Session, q.Revision, q.Choice)
			}
			if err == schedule.ErrGoalSlotOccupied || err == schedule.ErrGoalQuotaUnavailable {
				http.Error(w, err.Error(), 409)
				return
			}
			c.publishSchedules()
		} else {
			disarm := ""
			if codex.IsGoalMutation(q.Path) {
				disarm = c.schedules.PendingGoalResumeID(body.Session)
			}
			err = c.commands.ExecuteSessionCommand(ctx, body.Session, q.Path, q.Revision, q.Choice)
			if err == nil && disarm != "" {
				c.schedules.DisarmGoalResume(disarm)
				c.publishSchedules()
			}
		}
		if err != nil {
			http.Error(w, "Command unconfirmed; check Codex before retrying", 409)
			return
		}
		message := codex.SessionCommandSuccess(q.Path, p.speed)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
		return
	}
	read := c.commands.SessionCommands
	if cache, ok := c.commands.(codex.CachedSessionCommandsClient); ok && (q.Refresh || q.Mode == "prepare") {
		read = cache.SessionCommandsFresh
	}
	m, err := read(ctx, body.Session, q.Path)
	if err != nil {
		http.Error(w, "Command catalogue unavailable; use Codex", 409)
		return
	}
	if q.Mode == "list" {
		_, _, review := codex.SessionCommandInputValue(m.Path)
		_ = json.NewEncoder(w).Encode(commandMenu{m, review})
		return
	}
	if m.Revision != q.Revision {
		http.Error(w, errUnavailable.Error(), 409)
		return
	}
	for _, o := range m.Choices {
		if o.ID == q.Choice && o.Action {
			p := &preparedAction{request: body, speed: m.Picker, id: rand.Text(), until: time.Now().Add(30 * time.Second)}
			c.mu.Lock()
			c.pending = p
			c.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"confirmation": p.id, "expires": p.until})
			return
		}
	}
	http.Error(w, errUnavailable.Error(), 409)
}

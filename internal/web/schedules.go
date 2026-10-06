package web

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
)

type scheduleRule struct {
	Trigger string    `json:"trigger"`
	At      time.Time `json:"at"`
	Zone    string    `json:"zone"`
}

func (r scheduleRule) valid(now time.Time) bool {
	return len(r.Zone) <= 128 && ((r.Trigger == "quota" && r.At.IsZero()) || (r.Trigger == "at" && r.At.After(now) && r.At.Before(now.AddDate(1, 0, 0))))
}

type triggerSummary struct {
	Session string `json:"session"`
	Status  string `json:"status"`
}

func (c *control) scheduleSnapshot() codex.Snapshot {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	if c.store.state.QuotaError {
		return codex.Snapshot{}
	}
	return c.store.scheduleQuota
}
func (c *control) scheduleBlocked(session string) bool {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	if c.store.state.ProfileError {
		return true
	}
	for _, p := range c.store.state.Profiles {
		if p.Session == session && (p.Pending || p.Notice != "") {
			return true
		}
	}
	return false
}
func (c *control) publishSchedules() {
	jobs := c.schedules.List("")
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	var next []triggerSummary
	for _, j := range jobs {
		if j.Status != "sent" {
			next = append(next, triggerSummary{j.Session, j.Status})
		}
	}
	if slices.Equal(c.store.state.Triggers, next) {
		return
	}
	c.store.state.Triggers = next
	c.store.publish()
}
func (c *control) handleSchedules(w http.ResponseWriter, r *http.Request, b actionRequest) {
	if b.Schedule != nil || b.Offer != "" || b.Choice != nil || len(b.Answers) > 0 || b.Review != "" || b.Confirmation != "" {
		http.Error(w, "Invalid schedule request", 400)
		return
	}
	if b.CancelID != "" {
		if err := c.schedules.Cancel(b.Session, b.CancelID); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		c.publishSchedules()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(c.schedules.List(b.Session))
}
func (c *control) collectSchedules(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.dispatchSchedules(ctx)
			}
		}
	}()
	return func() { <-done }
}
func (c *control) dispatchSchedules(ctx context.Context) {
	if c.prompts == nil {
		return
	}
	q := c.scheduleSnapshot()
	if !schedule.QuotaReady(q, time.Now()) {
		return
	}
	c.mu.Lock()
	confirming := c.pending != nil && time.Now().Before(c.pending.until)
	c.mu.Unlock()
	if confirming {
		return
	}
	for _, j := range c.schedules.List("") {
		if j.Status != "pending" || c.scheduleBlocked(j.Session) || !schedule.Covers(j, q) || !schedule.Fresh(q, time.Now()) {
			continue
		}
		o, err := c.offer(j.Session)
		if err != nil || o.Kind != "prompt" || o.Thread != j.Session || len(o.Questions) > 0 {
			continue
		}
		call, cancel := context.WithTimeout(ctx, 6*time.Second)
		_ = c.schedules.Dispatch(call, j.ID, q.AccountFingerprint, true, time.Now(), func(ctx context.Context, j schedule.Job) error {
			latest := c.scheduleSnapshot()
			if latest.AccountFingerprint != q.AccountFingerprint || !schedule.QuotaReady(latest, time.Now()) || !schedule.Covers(j, latest) || c.scheduleBlocked(j.Session) {
				return errUnavailable
			}
			return c.prompts.SendSessionPrompt(ctx, o.token, []string{j.Text})
		})
		cancel()
	}
	c.publishSchedules()
}

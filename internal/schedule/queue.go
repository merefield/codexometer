// Package schedule owns in-memory one-off follow-ups independently of the UI.
// Closing Codexometer cancels pending work. Uncertain dispatch is never retried.
package schedule

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/merefield/codexometer/internal/codex"
	"strings"
	"sync"
	"time"
)

type Job struct {
	ID      string    `json:"id"`
	Session string    `json:"session"`
	Text    string    `json:"text"`
	Trigger string    `json:"trigger"` // at or quota
	At      time.Time `json:"at,omitempty"`
	Zone    string    `json:"zone"`
	Status  string    `json:"status"` // pending, sending, sent, uncertain
	Windows []string  `json:"-"`      // Observed windows must not silently disappear.
	account string
}

type Queue struct {
	mu   sync.Mutex
	jobs []Job
}

func New() *Queue                                { return &Queue{} }
func (j Job) MatchesAccount(account string) bool { return account != "" && j.account == account }
func (q *Queue) List(session string) []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := []Job{}
	for _, j := range q.jobs {
		if session == "" || j.Session == session {
			j.Windows = append([]string(nil), j.Windows...)
			out = append(out, j)
		}
	}
	return out
}

func (q *Queue) Save(j Job, account string, now time.Time) error {
	j.Text = strings.TrimSpace(j.Text)
	if account == "" || j.Session == "" || len(j.Session) > 512 || j.Text == "" || len([]rune(j.Text)) > 4096 || len(j.Zone) > 128 || codex.SanitizeSessionContext(j.Text) != j.Text || (j.Trigger == "quota" && !j.At.IsZero()) ||
		(j.Trigger != "at" && j.Trigger != "quota") || (j.Trigger == "at" && (!j.At.After(now) || j.At.After(now.AddDate(1, 0, 0)))) {
		return errors.New("choose a live session, a message and a future time within one year, or quota recovery")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	index := -1
	for i, v := range q.jobs {
		if v.Session == j.Session {
			if v.Status == "sending" || v.Status == "uncertain" {
				return errors.New("check the previous send in Codex before cancelling or replacing it")
			}
			index = i
			break
		}
	}
	j.Status = "pending"
	j.ID = rand.Text() // An edit invalidates confirmations for the old payload.
	j.Windows = append([]string(nil), j.Windows...)
	j.account = account
	j.At = j.At.UTC()
	if index >= 0 {
		q.jobs[index] = j
	} else {
		if len(q.jobs) >= 128 {
			return errors.New("schedule limit reached; remove old entries first")
		}
		q.jobs = append(q.jobs, j)
	}
	return nil
}

func (q *Queue) Cancel(session, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, j := range q.jobs {
		if j.Session != session || j.ID != id {
			continue
		}
		if j.Status == "sending" {
			return errors.New("send in progress; check Codex")
		}
		q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
		return nil
	}
	return errors.New("schedule changed; refresh")
}

// Dispatch is called only after the UI adapter has checked freshness, session
// readiness and pending reviews. Claim sending before network IO. This is
// intentionally at-most-one attempt, not a claim of exactly-once delivery.
func (q *Queue) Dispatch(ctx context.Context, id, account string, quotaReady bool, now time.Time, send func(context.Context, Job) error) error {
	return q.dispatch(ctx, id, account, quotaReady, now, false, send)
}

// DispatchNow shares the automatic dispatch claim; only the time rule is bypassed.
func (q *Queue) DispatchNow(ctx context.Context, id, account string, now time.Time, send func(context.Context, Job) error) error {
	return q.dispatch(ctx, id, account, true, now, true, send)
}

func (q *Queue) dispatch(ctx context.Context, id, account string, quotaReady bool, now time.Time, immediate bool, send func(context.Context, Job) error) error {
	q.mu.Lock()
	index := -1
	for i, j := range q.jobs {
		if j.ID == id && j.Status == "pending" && account != "" && j.account == account &&
			(immediate || (j.Trigger == "at" && !now.Before(j.At)) || (j.Trigger == "quota" && quotaReady)) {
			index = i
			break
		}
	}
	if index < 0 {
		q.mu.Unlock()
		if immediate {
			return errors.New("trigger changed or already dispatched; refresh")
		}
		return nil
	}
	j := q.jobs[index]
	q.jobs[index].Status = "sending"
	q.mu.Unlock()
	err := send(ctx, j)
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, v := range q.jobs {
		if v.ID == id {
			index = i
			break
		}
	}
	q.jobs[index].Status = "sent"
	if err != nil {
		q.jobs[index].Status = "uncertain"
	}
	return err
}

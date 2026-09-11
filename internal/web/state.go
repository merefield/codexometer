// Package web provides the opt-in, read-only browser presentation. It does not
// import the terminal UI or own any Codex mutation capabilities.
package web

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/version"
)

type Source interface {
	Fetch(context.Context) (codex.Snapshot, error)
	FetchTokenUsage(context.Context) (codex.LiveUsageSnapshot, error)
	FetchAccountUsage(context.Context) (codex.AccountUsage, error)
}

// Deliberately project source types: never serialize approval/input capabilities,
// account fingerprints, arbitrary errors, or authentication objects to browsers.
type session struct {
	ID          string    `json:"id"`
	Directory   string    `json:"directory"`
	Tokens      int64     `json:"tokens"`
	Agents      int       `json:"agents"`
	Status      string    `json:"status"`
	ContextKind string    `json:"contextKind"`
	Text        string    `json:"text"`
	Command     string    `json:"command"`
	Source      string    `json:"source"`
	Activity    time.Time `json:"activity"`
	Samples     []sample  `json:"samples"`
}

type sample struct {
	At     time.Time `json:"at"`
	Tokens int64     `json:"tokens"`
}

type meter struct {
	Name     string `json:"name"`
	Used     int    `json:"used"`
	Duration *int64 `json:"duration"`
	Reset    *int64 `json:"reset"`
	Details  string `json:"details"`
}

type credit struct {
	Title       string `json:"title"`
	Status      string `json:"status"`
	Expires     *int64 `json:"expires"`
	ExpiryKnown bool   `json:"expiryKnown"`
}

type state struct {
	Version       string              `json:"version"`
	Meters        []meter             `json:"meters"`
	Credits       []credit            `json:"credits"`
	CreditCount   int                 `json:"creditCount"`
	Sessions      []session           `json:"sessions"`
	Usage         *codex.AccountUsage `json:"usage"`
	QuotaAt       time.Time           `json:"quotaAt"`
	SessionsAt    time.Time           `json:"sessionsAt"`
	UsageAt       time.Time           `json:"usageAt"`
	QuotaError    bool                `json:"quotaError"`
	SessionsError bool                `json:"sessionsError"`
	UsageError    bool                `json:"usageError"`
}

type store struct {
	mu         sync.Mutex
	state      state
	account    string
	history    codex.AccountUsage
	previous   map[string]int64
	samples    map[string][]sample
	nextSample time.Time
	data       []byte
	changed    chan struct{}
}

func newStore() *store {
	s := &store{state: state{Version: version.Current(), Meters: []meter{}, Credits: []credit{}, Sessions: []session{}}, previous: map[string]int64{}, samples: map[string][]sample{}, changed: make(chan struct{})}
	s.publish()
	return s
}

// Called with mu held (except initialization); published byte slices are immutable.
func (s *store) publish() {
	s.data, _ = json.Marshal(s.state)
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *store) snapshot() ([]byte, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data, s.changed
}

func (s *store) quota(q codex.Snapshot, err error, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.QuotaError = err != nil
	if err == nil {
		s.account = q.AccountFingerprint
		s.state.QuotaAt = now
		s.state.Meters = []meter{}
		for _, m := range q.Meters() {
			s.state.Meters = append(s.state.Meters, meter{Name: m.Bucket + " // " + m.Name, Used: max(0, min(100, m.Window.UsedPercent)), Duration: m.Window.WindowDurationMins, Reset: m.Window.ResetsAt, Details: m.Details})
		}
		s.state.CreditCount = 0
		s.state.Credits = []credit{}
		if q.RateLimitResetCredits != nil {
			s.state.CreditCount = q.RateLimitResetCredits.AvailableCount
			for _, c := range q.RateLimitResetCredits.Credits {
				s.state.Credits = append(s.state.Credits, credit{Title: c.Title, Status: c.Status, Expires: c.ExpiresAt, ExpiryKnown: c.HasKnownExpiry()})
			}
		}
	}
	s.reconcileUsage()
	s.publish()
}

func (s *store) usage(h codex.AccountUsage, err error, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.UsageError = err != nil
	if err == nil {
		s.history = h
		s.history.FetchedAt = now
	}
	s.reconcileUsage()
	s.publish()
}

func (s *store) reconcileUsage() {
	s.state.Usage = nil
	s.state.UsageAt = time.Time{}
	// Do not show one account's cached history alongside another's quota.
	if !s.state.QuotaError && s.account != "" && s.history.AccountFingerprint == s.account {
		h := s.history
		s.state.Usage = &h
		s.state.UsageAt = h.FetchedAt
	}
}

func (s *store) live(l codex.LiveUsageSnapshot, err error, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.SessionsError = err != nil
	if err == nil {
		s.state.SessionsAt = now
		s.state.Sessions = []session{}
		tick := !now.Before(s.nextSample)
		seen := map[string]bool{}
		for _, row := range l.Sessions {
			seen[row.ID] = true
			before, known := s.previous[row.ID]
			if !known || row.TotalTokens < before {
				s.previous[row.ID] = row.TotalTokens
			}
			if tick {
				delta := int64(0)
				if known {
					delta = max(0, row.TotalTokens-before)
				}
				points := append(s.samples[row.ID], sample{At: now, Tokens: delta})
				if len(points) > 120 {
					points = points[len(points)-120:]
				}
				s.samples[row.ID] = points
				s.previous[row.ID] = row.TotalTokens
			}
			text := row.Context.Text
			if row.Context.CommandDetails.Command != "" && row.Context.CommandDetails.Justification != "" {
				text = row.Context.CommandDetails.Justification
			}
			s.state.Sessions = append(s.state.Sessions, session{
				ID: row.ID, Directory: row.WorkingDirectory, Tokens: row.TotalTokens, Agents: row.AgentCount,
				Status: sessionStatus(row), ContextKind: contextKind(row.Context.Kind), Text: text,
				Command: row.Context.CommandDetails.Command, Source: row.Context.Source, Activity: row.LastActivity,
				Samples: s.samples[row.ID],
			})
		}
		for id := range s.previous {
			if !seen[id] {
				delete(s.previous, id)
				delete(s.samples, id)
			}
		}
		if tick {
			s.nextSample = now.Add(30 * time.Second)
		}
	} else {
		// Never keep an apparently authoritative approval/working status on failure.
		for i := range s.state.Sessions {
			s.state.Sessions[i].Status = "STALE"
		}
	}
	s.publish()
}

func sessionStatus(s codex.LiveUsageSession) string {
	switch s.Attention {
	case codex.SessionAttentionInput:
		return "INPUT NEEDED"
	case codex.SessionAttentionApproval:
		return "APPROVAL NEEDED"
	case codex.SessionAttentionCheck:
		return "CHECK SESSION"
	case codex.SessionAttentionComplete:
		return "TURN COMPLETE"
	}
	if s.Working {
		return "WORKING"
	}
	if !s.Active {
		return "INACTIVE"
	}
	return "IDLE"
}

func contextKind(k codex.SessionContextKind) string {
	switch k {
	case codex.SessionContextReply:
		return "LAST REPLY"
	case codex.SessionContextApproval:
		return "APPROVAL REQUEST"
	case codex.SessionContextQuestion:
		return "QUESTION"
	default:
		return "LAST ACTIVITY"
	}
}

// Pollers run once per process, never once per browser tab. Each source has an
// independent schedule so slow account requests cannot stall session updates.
func (s *store) collect(ctx context.Context, source Source, refresh time.Duration) func() {
	var wg sync.WaitGroup
	poll := func(interval time.Duration, read func(context.Context)) {
		wg.Go(func() {
			for {
				if ctx.Err() != nil {
					return
				}
				request, cancel := context.WithTimeout(ctx, 20*time.Second)
				read(request)
				cancel()
				timer := time.NewTimer(interval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	poll(max(10*time.Second, refresh), func(ctx context.Context) { q, e := source.Fetch(ctx); s.quota(q, e, time.Now()) })
	poll(2*time.Second, func(ctx context.Context) { l, e := source.FetchTokenUsage(ctx); s.live(l, e, time.Now()) })
	poll(5*time.Minute, func(ctx context.Context) { h, e := source.FetchAccountUsage(ctx); s.usage(h, e, time.Now()) })
	return wg.Wait
}

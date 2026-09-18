package web

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/merefield/codexometer/internal/codex"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type profileSource struct {
	actionSource
	steps        []codex.QuotaStep
	sessions     []codex.QuotaSession
	writes       int
	failure      error
	readError    error
	hideSessions bool
	readStarted  chan struct{}
	readRelease  chan struct{}
}

func (f *profileSource) QuotaStepPolicy() []codex.QuotaStep { return f.steps }
func (f *profileSource) ResolveQuotaStep(_ context.Context, s codex.QuotaStep) (codex.QuotaStep, error) {
	return s, nil
}
func (f *profileSource) QuotaSessions(context.Context) ([]codex.QuotaSession, error) {
	if f.readStarted != nil {
		select {
		case f.readStarted <- struct{}{}:
		default:
		}
		<-f.readRelease
	}
	if f.hideSessions {
		return nil, f.readError
	}
	return append([]codex.QuotaSession{}, f.sessions...), f.readError
}

func TestWebProfilePartialInventoryStillProcessesKnownSessions(t *testing.T) {
	for _, mode := range []string{"ask", "auto"} {
		s, f, token := profileServer(t, mode)
		f.readError = errors.New("another thread cannot be read")
		s.control.refreshProfiles(context.Background())
		if !s.store.state.ProfileError {
			t.Fatal("partial failure hidden")
		}
		if mode == "auto" {
			if f.writes != 1 {
				t.Fatal("known session not processed")
			}
			continue
		}
		if len(s.store.state.Profiles) != 1 || !s.store.state.Profiles[0].Pending {
			t.Fatal("known review discarded")
		}
		o := profileOfferForTest(t, s, token)
		choice := 0
		r := prepareAction(t, s, token, actionRequest{Session: "parent", Review: "profile", Offer: o.ID, Choice: &choice})
		r.Review = "profile"
		if w := actionCall(s, token, "commit", r); w.Code != 200 || f.writes != 1 {
			t.Fatalf("partial inventory blocked target: %s", w.Body)
		}
	}
}

func TestWebProfileOutcomesAndErrorPrivacy(t *testing.T) {
	for _, failure := range []error{errors.New("private path /secret/token abc123"), codex.ErrQuotaProfileUnverified, codex.ErrQuotaProfileUncertain} {
		s, f, token := profileServer(t, "ask")
		f.failure = failure
		o := profileOfferForTest(t, s, token)
		choice := 0
		r := prepareAction(t, s, token, actionRequest{Session: "parent", Review: "profile", Offer: o.ID, Choice: &choice})
		r.Review = "profile"
		w := actionCall(s, token, "commit", r)
		want := 502
		if failure.Error() == "private path /secret/token abc123" {
			want = 409
		}
		if w.Code != want {
			t.Fatalf("status %d want %d", w.Code, want)
		}
		if len(s.store.state.Profiles) != 1 || s.store.state.Profiles[0].Notice == "" {
			t.Fatal("outcome not published immediately")
		}
		s.control.refreshProfiles(context.Background())
		data, _ := s.store.snapshot()
		if strings.Contains(string(data), "abc123") || strings.Contains(w.Body.String(), "abc123") {
			t.Fatal("raw upstream error disclosed")
		}
		if len(s.store.state.Profiles) != 1 {
			t.Fatal("outcome missing")
		}
		original := s.store.state.Profiles[0].Notice
		f.readError = errors.New("unavailable")
		f.hideSessions = true
		s.control.refreshProfiles(context.Background())
		if len(s.store.state.Profiles) != 1 || s.store.state.Profiles[0].Notice != original || s.store.state.Profiles[0].Pending {
			t.Fatal("transient error lost notice or enabled action")
		}
		s.store.state.QuotaError = true
		s.control.refreshProfiles(context.Background())
		if len(s.store.state.Profiles) != 1 || s.store.state.Profiles[0].Notice != original {
			t.Fatal("quota error lost outcome")
		}
		s.store.state.QuotaError = false
		// A successfully read match clears the notice even with other read errors.
		f.hideSessions = false
		f.sessions[0].Model = "small"
		f.sessions[0].Effort = "medium"
		s.control.refreshProfiles(context.Background())
		if len(s.store.state.Profiles) != 0 {
			t.Fatal("verified notice retained")
		}
		if f.writes != 1 {
			t.Fatal("failed write retried")
		}
	}
}

func TestSlowProfilesDoNotBlockNativeControls(t *testing.T) {
	for _, background := range []bool{true, false} {
		s, f, token := profileServer(t, "ask")
		f.readStarted = make(chan struct{}, 1)
		f.readRelease = make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			if background {
				s.control.refreshProfiles(context.Background())
			} else {
				actionCall(s, token, "offer", actionRequest{Session: "parent", Review: "profile"})
			}
		}()
		<-f.readStarted
		func() {
			defer close(f.readRelease)
			native := make(chan int, 1)
			go func() { native <- actionCall(s, token, "offer", actionRequest{Session: "parent"}).Code }()
			select {
			case code := <-native:
				if code != 200 {
					t.Errorf("native offer failed: %d", code)
				}
			case <-time.After(time.Second):
				t.Error("profile IO blocked native control")
			}
		}()
		<-done
	}
}
func (f *profileSource) CloseQuotaProfiles() {}
func (f *profileSource) ApplyQuotaProfile(_ context.Context, targets []codex.QuotaSession, step codex.QuotaStep) (int, error) {
	f.writes++
	if f.failure != nil {
		return 0, f.failure
	}
	for i, s := range f.sessions {
		if s.ID == targets[0].ID {
			f.sessions[i].Model = step.Model
			f.sessions[i].Effort = step.Effort
		}
	}
	return 1, nil
}

func profileServer(t *testing.T, mode string) (*server, *profileSource, string) {
	s, actions, token := controlServer(t)
	f := &profileSource{steps: []codex.QuotaStep{{Threshold: 80, Model: "small", Effort: "medium", Mode: mode}}, sessions: []codex.QuotaSession{{ID: "parent", Model: "large", Effort: "high"}}}
	f.pending = actions.pending
	s.control = newControl(f, s.store)
	q := codex.DemoSnapshot()
	q.AccountFingerprint = "private-account"
	q.FetchedAt = time.Now()
	q.RateLimits.Secondary.UsedPercent = 85
	reset := time.Now().Add(time.Hour).Unix()
	q.RateLimits.Secondary.ResetsAt = &reset
	s.store.quota(q, nil, time.Now())
	return s, f, token
}

func profileOfferForTest(t *testing.T, s *server, token string) actionOffer {
	t.Helper()
	w := actionCall(s, token, "offer", actionRequest{Session: "parent", Review: "profile"})
	var o actionOffer
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &o) != nil || o.ID == "" {
		t.Fatalf("offer: %d %s", w.Code, w.Body)
	}
	return o
}

func TestWebProfileConfirmedOnceAlongsideNativeApproval(t *testing.T) {
	s, f, token := profileServer(t, "ask")
	s.control.refreshProfiles(context.Background())
	if len(s.store.state.Profiles) != 1 || !s.store.state.Profiles[0].Pending || f.writes != 0 {
		t.Fatal("ask wrote or lacked pill")
	}
	if getOffer(t, s, token).Kind != "approval" {
		t.Fatal("profile displaced native approval")
	}
	o := profileOfferForTest(t, s, token)
	choice := 0
	r := prepareAction(t, s, token, actionRequest{Session: "parent", Review: "profile", Offer: o.ID, Choice: &choice})
	r.Review = "profile"
	if f.writes != 0 {
		t.Fatal("prepare wrote")
	}
	if w := actionCall(s, token, "commit", r); w.Code != 200 {
		t.Fatalf("commit: %s", w.Body)
	}
	if w := actionCall(s, token, "commit", r); w.Code != 409 || f.writes != 1 {
		t.Fatal("replayed profile")
	}
	s.control.refreshProfiles(context.Background())
	if len(s.store.state.Profiles) != 0 {
		t.Fatal("resolved pill retained")
	}
}

func TestWebProfileRejectsDriftAndStaleQuota(t *testing.T) {
	for _, change := range []string{"settings", "threshold", "window", "stale", "account", "origin", "auth"} {
		t.Run(change, func(t *testing.T) {
			s, f, token := profileServer(t, "ask")
			o := profileOfferForTest(t, s, token)
			choice := 0
			r := prepareAction(t, s, token, actionRequest{Session: "parent", Review: "profile", Offer: o.ID, Choice: &choice})
			r.Review = "profile"
			switch change {
			case "settings":
				f.sessions[0].Effort = "low"
			case "threshold":
				s.store.quotaSnapshot.RateLimitsByLimitID["codex"].Secondary.UsedPercent = 10
			case "window":
				value := time.Now().Add(2 * time.Hour).Unix()
				s.store.quotaSnapshot.RateLimitsByLimitID["codex"].Secondary.ResetsAt = &value
			case "stale":
				s.store.quotaSnapshot.FetchedAt = time.Now().Add(-time.Hour)
			case "account":
				s.store.quotaSnapshot.AccountFingerprint = "other"
			case "auth":
				token = "invalid"
			}
			if change == "origin" {
				data, _ := json.Marshal(r)
				req := request(s, "POST", "/api/control/commit", string(data), token)
				req.Header.Set("Origin", "https://evil.example")
				w := httptest.NewRecorder()
				s.handler().ServeHTTP(w, req)
				if w.Code != 403 {
					t.Fatal("foreign origin accepted")
				}
			} else if w := actionCall(s, token, "commit", r); w.Code == 200 {
				t.Fatal("changed review accepted")
			}
			if f.writes != 0 {
				t.Fatal("unsafe profile write")
			}
		})
	}
}

func TestWebProfileAutoSkipAndNoRetry(t *testing.T) {
	s, f, token := profileServer(t, "auto")
	f.failure = errors.New("test rejection")
	s.control.refreshProfiles(context.Background())
	s.control.refreshProfiles(context.Background())
	if f.writes != 1 || len(s.store.state.Profiles) != 1 || s.store.state.Profiles[0].Pending || s.store.state.Profiles[0].Notice == "" {
		t.Fatal("auto failure retried or hidden")
	}
	f.sessions = append(f.sessions, codex.QuotaSession{ID: "new", Model: "large", Effort: "high"})
	f.failure = nil
	s.control.refreshProfiles(context.Background())
	if f.writes != 2 {
		t.Fatal("new session ignored after failure")
	}
	// Read-only server has no control handlers, even with a profile payload.
	s.control = nil
	if w := actionCall(s, token, "offer", actionRequest{Session: "parent", Review: "profile"}); w.Code != 404 {
		t.Fatal("read-only control exposed")
	}
	s, f, token = profileServer(t, "ask")
	o := profileOfferForTest(t, s, token)
	choice := 1
	r := prepareAction(t, s, token, actionRequest{Session: "parent", Review: "profile", Offer: o.ID, Choice: &choice})
	r.Review = "profile"
	if w := actionCall(s, token, "commit", r); w.Code != 200 {
		t.Fatal(w.Body)
	}
	s.control.refreshProfiles(context.Background())
	if f.writes != 0 || len(s.store.state.Profiles) != 0 {
		t.Fatal("skip changed settings or retained review")
	}
}

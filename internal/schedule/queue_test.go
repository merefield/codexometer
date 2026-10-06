package schedule

import (
	"context"
	"errors"
	"fmt"
	"github.com/merefield/codexometer/internal/codex"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPreflightDeferralDoesNotBecomeUncertain(t *testing.T) {
	q := New()
	now := time.Now()
	if err := q.Save(Job{Session: "root", Text: "later", Trigger: "quota"}, "a", now); err != nil {
		t.Fatal(err)
	}
	id := q.List("")[0].ID
	err := q.Dispatch(context.Background(), id, "a", true, now, func(context.Context, Job) error { return fmt.Errorf("quota changed: %w", ErrDeferred) })
	if !errors.Is(err, ErrDeferred) || q.List("")[0].Status != "pending" {
		t.Fatal("preflight failure blocked the job", err, q.List(""))
	}
	calls := 0
	if err := q.Dispatch(context.Background(), id, "a", true, now, func(context.Context, Job) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatal("job did not recover", err)
	}
}

func TestCompletedJobsDoNotConsumeQueueCapacity(t *testing.T) {
	q := New()
	now := time.Now()
	for i := range 300 {
		if err := q.Save(Job{Session: fmt.Sprint(i), Text: "later", Trigger: "quota"}, "a", now); err != nil {
			t.Fatal(i, err)
		}
		jobs := q.List("")
		if len(jobs) != 1 {
			t.Fatal("completed jobs retained", len(jobs))
		}
		if err := q.Dispatch(context.Background(), jobs[0].ID, "a", true, now, func(context.Context, Job) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQueueGuardsAndSingleAttempt(t *testing.T) {
	now := time.Now()
	q := New()
	j := Job{Session: "root", Text: "hello", Trigger: "at", At: now.Add(time.Minute)}
	if err := q.Save(j, "account", now); err != nil {
		t.Fatal(err)
	}
	id := q.List("root")[0].ID
	var calls atomic.Int32
	send := func(context.Context, Job) error { calls.Add(1); return errors.New("lost connection") }
	for _, tc := range []struct {
		account string
		at      time.Time
	}{{"other", now.Add(time.Hour)}, {"account", now}} {
		_ = q.Dispatch(context.Background(), id, tc.account, true, tc.at, send)
	}
	if calls.Load() != 0 {
		t.Fatal("premature dispatch")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { _ = q.Dispatch(context.Background(), id, "account", true, now.Add(time.Hour), send) })
	}
	wg.Wait()
	if calls.Load() != 1 || q.List("root")[0].Status != "uncertain" {
		t.Fatal("must attempt once and retain uncertain outcome")
	}
	if q.Save(j, "account", now) == nil {
		t.Fatal("replaced uncertain send")
	}
	if q.Cancel("other", id) == nil {
		t.Fatal("cancelled wrong session")
	}
	if err := q.Cancel("root", id); err != nil {
		t.Fatal(err)
	}
	if len(q.List("")) != 0 {
		t.Fatal("not cancelled")
	}
}

func TestQueueReplaceQuotaCancelAndMemory(t *testing.T) {
	now := time.Now()
	q := New()
	j := Job{Session: "root", Text: "one", Trigger: "quota"}
	if err := q.Save(j, "account", now); err != nil {
		t.Fatal(err)
	}
	id := q.List("")[0].ID
	j.Text = "two"
	if err := q.Save(j, "account", now); err != nil {
		t.Fatal(err)
	}
	if len(q.List("")) != 1 || q.List("")[0].ID == id {
		t.Fatal("replacement must invalidate the old ID without duplicating the job")
	}
	id = q.List("")[0].ID
	calls := 0
	send := func(_ context.Context, j Job) error {
		calls++
		if j.Text != "two" {
			t.Fatal(j.Text)
		}
		return nil
	}
	_ = q.Dispatch(context.Background(), id, "account", false, now, send)
	if calls != 0 {
		t.Fatal("quota not ready")
	}
	_ = q.Dispatch(context.Background(), id, "account", true, now, send)
	_ = q.Dispatch(context.Background(), id, "account", true, now, send)
	if calls != 1 {
		t.Fatal(calls)
	}
	if len(New().List("")) != 0 {
		t.Fatal("persisted queue")
	}
	if err := q.Save(j, "account", now); err != nil {
		t.Fatal(err)
	}
	if len(q.List("")) != 1 {
		t.Fatal("sent records accumulated")
	}
}

func TestQuotaReadiness(t *testing.T) {
	now := time.Now()
	s := codex.Snapshot{AccountFingerprint: "a", FetchedAt: now, RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 10}}}
	if !QuotaReady(s, now) {
		t.Fatal("ready quota rejected")
	}
	s.RateLimits.Primary.UsedPercent = 100
	if QuotaReady(s, now) {
		t.Fatal("exhausted quota accepted")
	}
	s.RateLimits.Primary.UsedPercent = 0
	if QuotaReady(s, now.Add(91*time.Second)) || QuotaReady(s, now.Add(-time.Second)) {
		t.Fatal("stale/future snapshot")
	}
	flag := true
	s.RateLimits.SpendControlReached = &flag
	if QuotaReady(s, now) {
		t.Fatal("spend control ignored")
	}
	s.RateLimits.SpendControlReached = nil
	s.RateLimits.Primary = nil
	if QuotaReady(s, now) {
		t.Fatal("missing windows accepted")
	}
}

func TestInvalidJobs(t *testing.T) {
	now := time.Now()
	for _, j := range []Job{{Session: "s", Text: "hi", Trigger: "at", At: now.Add(-time.Minute)}, {Session: "s", Text: "\x1b[31mhello", Trigger: "quota"}, {Session: "s", Text: "hi", Trigger: "quota", At: now}, {Session: "s", Trigger: "quota"}} {
		if New().Save(j, "a", now) == nil {
			t.Fatalf("accepted %+v", j)
		}
	}
}

func TestMissingWindowIsNotRecovery(t *testing.T) {
	s := codex.Snapshot{RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 100}}}
	j := Job{Windows: WindowKeys(s)}
	if !Covers(j, s) {
		t.Fatal("same window rejected")
	}
	s.RateLimits.Primary = nil
	if Covers(j, s) {
		t.Fatal("missing exhausted window treated as recovered")
	}
}

func TestManualAndAutomaticDispatchShareClaim(t *testing.T) {
	now := time.Now()
	q := New()
	j := Job{Session: "root", Text: "one", Trigger: "at", At: now.Add(time.Hour)}
	if err := q.Save(j, "account", now); err != nil {
		t.Fatal(err)
	}
	id := q.List("")[0].ID
	var calls atomic.Int32
	var wg sync.WaitGroup
	send := func(context.Context, Job) error { calls.Add(1); return nil }
	for range 20 {
		wg.Go(func() { _ = q.DispatchNow(context.Background(), id, "account", now, send) })
		wg.Go(func() { _ = q.Dispatch(context.Background(), id, "account", true, now.Add(time.Hour), send) })
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate sends", calls.Load())
	}
	if err := q.Save(j, "account", now); err != nil {
		t.Fatal(err)
	}
	if q.DispatchNow(context.Background(), id, "account", now, send) == nil {
		t.Fatal("old confirmation accepted")
	}
}

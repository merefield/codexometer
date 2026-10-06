package web

import (
	"context"
	"github.com/merefield/codexometer/internal/codex"
	"testing"
	"time"
)

func TestSchedulePrepareCommitDispatchAndCancel(t *testing.T) {
	s, f, token := controlServer(t)
	s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextReply}
	f.prompt = codex.SessionPromptOffer{ThreadID: "parent", Token: "idle"}
	s.store.scheduleQuota = codex.Snapshot{AccountFingerprint: "a", FetchedAt: time.Now(), RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 100}}}
	o := getOffer(t, s, token)
	r := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Answers: []string{"later"}, Schedule: &scheduleRule{Trigger: "quota"}})
	if len(s.control.schedules.List("")) != 0 {
		t.Fatal("prepare saved")
	}
	if w := actionCall(s, token, "commit", r); w.Code != 200 {
		t.Fatalf("commit: %s", w.Body)
	}
	if f.calls != 0 {
		t.Fatal("schedule sent immediately")
	}
	s.control.dispatchSchedules(context.Background())
	if f.calls != 0 {
		t.Fatal("sent while exhausted")
	}
	s.store.scheduleQuota.RateLimits.Primary.UsedPercent = 0
	s.control.dispatchSchedules(context.Background())
	s.control.dispatchSchedules(context.Background())
	if f.calls != 1 || len(f.answers) != 1 || f.answers[0] != "later" {
		t.Fatal("wrong dispatch", f.calls, f.answers)
	}
	jobs := s.control.schedules.List("parent")
	if jobs[0].Status != "sent" {
		t.Fatal(jobs)
	}
	if w := actionCall(s, token, "schedules", actionRequest{Session: "parent", CancelID: jobs[0].ID}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if len(s.control.schedules.List("")) != 0 {
		t.Fatal("not cancelled")
	}
}

func TestScheduleCannotAttachToApproval(t *testing.T) {
	s, _, token := controlServer(t)
	o := getOffer(t, s, token)
	choice := 0
	w := actionCall(s, token, "prepare", actionRequest{Session: "parent", Offer: o.ID, Choice: &choice, Schedule: &scheduleRule{Trigger: "quota"}})
	if w.Code != 409 {
		t.Fatal("scheduled approval", w.Code)
	}
}

func TestScheduleFreshnessAndAuthentication(t *testing.T) {
	s, f, token := controlServer(t)
	s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextReply}
	f.prompt = codex.SessionPromptOffer{ThreadID: "parent", Token: "idle"}
	o := getOffer(t, s, token)
	r := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Answers: []string{"later"}, Schedule: &scheduleRule{Trigger: "quota"}})
	if w := actionCall(s, token, "commit", r); w.Code != 409 {
		t.Fatal("accepted missing quota", w.Code)
	}
	if w := actionCall(s, "", "schedules", actionRequest{Session: "parent"}); w.Code == 200 {
		t.Fatal("unauthenticated schedule access")
	}
}

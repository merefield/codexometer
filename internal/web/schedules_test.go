package web

import (
	"context"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/schedule"
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

func TestScheduleSendNowConfirmationAndReplacement(t *testing.T) {
	s, f, token := controlServer(t)
	s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextReply}
	f.prompt = codex.SessionPromptOffer{ThreadID: "parent", Token: "idle"}
	s.store.scheduleQuota = codex.Snapshot{AccountFingerprint: "a", FetchedAt: time.Now(), RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 0}}}
	job := schedule.Job{Session: "parent", Text: "exact saved text", Trigger: "at", At: time.Now().Add(time.Hour)}
	if err := s.control.schedules.Save(job, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	o := getOffer(t, s, token)
	id := s.control.schedules.List("parent")[0].ID
	if w := actionCall(s, token, "prepare", actionRequest{Session: "parent", Offer: o.ID, Answers: []string{"bypass pending job"}}); w.Code != 409 {
		t.Fatal("ordinary composer bypassed pending trigger")
	}
	r := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, SendID: id})
	if f.calls != 0 {
		t.Fatal("sent on preparation")
	}
	if err := s.control.schedules.Save(job, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := actionCall(s, token, "commit", r); w.Code != 409 {
		t.Fatal("replaced trigger confirmation accepted", w.Code)
	}
	id = s.control.schedules.List("parent")[0].ID
	r = prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, SendID: id})
	if w := actionCall(s, token, "commit", r); w.Code != 200 {
		t.Fatal(w.Body)
	}
	s.control.dispatchSchedules(context.Background())
	if f.calls != 1 || f.answers[0] != "exact saved text" {
		t.Fatal(f.calls, f.answers)
	}
}

func TestScheduleSendNowRejectsExhaustedQuota(t *testing.T) {
	s, f, token := controlServer(t)
	s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextReply}
	f.prompt = codex.SessionPromptOffer{ThreadID: "parent", Token: "idle"}
	s.store.scheduleQuota = codex.Snapshot{AccountFingerprint: "a", FetchedAt: time.Now(), RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 100}}}
	if err := s.control.schedules.Save(schedule.Job{Session: "parent", Text: "later", Trigger: "quota"}, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	o := getOffer(t, s, token)
	if w := actionCall(s, token, "prepare", actionRequest{Session: "parent", Offer: o.ID, SendID: s.control.schedules.List("parent")[0].ID}); w.Code != 409 {
		t.Fatal("quota bypassed", w.Code)
	}
}

type preflightPromptSource struct {
	*actionSource
	beforeReturn func()
}

func (f *preflightPromptSource) SessionPrompt(thread string) codex.SessionPromptOffer {
	o := f.actionSource.SessionPrompt(thread)
	if f.beforeReturn != nil {
		f.beforeReturn()
	}
	return o
}

func TestSchedulerDefersWhenQuotaChangesBeforeSend(t *testing.T) {
	s, f, _ := controlServer(t)
	s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextReply}
	f.prompt = codex.SessionPromptOffer{ThreadID: "parent", Token: "idle"}
	s.store.scheduleQuota = codex.Snapshot{AccountFingerprint: "a", FetchedAt: time.Now(), RateLimits: codex.RateLimitSnapshot{Primary: &codex.Window{UsedPercent: 0}}}
	if err := s.control.schedules.Save(schedule.Job{Session: "parent", Text: "later", Trigger: "quota"}, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	p := &preflightPromptSource{actionSource: f, beforeReturn: func() { s.store.scheduleQuota.RateLimits.Primary.UsedPercent = 100 }}
	s.control.prompts = p
	s.control.dispatchSchedules(context.Background())
	if f.calls != 0 || s.control.schedules.List("parent")[0].Status != "pending" {
		t.Fatal("preflight deferral dispatched or became uncertain")
	}
	p.beforeReturn = nil
	s.store.scheduleQuota.RateLimits.Primary.UsedPercent = 0
	s.control.dispatchSchedules(context.Background())
	if f.calls != 1 {
		t.Fatal("deferred request did not recover")
	}
}

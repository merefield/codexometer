package web

import (
	"errors"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestSessionFailurePresentation(t *testing.T) {
	row := codex.LiveUsageSession{Attention: codex.SessionAttentionError, Context: codex.SessionContext{Kind: codex.SessionContextFailure, FailureCode: "serverOverloaded"}}
	if sessionStatus(row) != "MODEL BUSY" || contextKind(row.Context.Kind) != "TURN ERROR" {
		t.Fatal("capacity failure not exposed")
	}
	row.Context.FailureCode = "httpConnectionFailed"
	if sessionStatus(row) != "TURN FAILED" {
		t.Fatal("generic failure mislabeled")
	}
	for code, want := range map[string]string{"modelNotFound": "MODEL UNAVAILABLE", "policyCapacityUnavailable": "CAPACITY RESTRICTED"} {
		row.Context.FailureCode = code
		if sessionStatus(row) != want || contextKind(row.Context.Kind) != "TURN ERROR" {
			t.Fatal("new error lost shared failure classification", row)
		}
	}
}

func TestSessionFailureKeepsRecoveryActions(t *testing.T) {
	for _, code := range []string{"serverOverloaded", "modelNotFound", "policyCapacityUnavailable", "httpConnectionFailed"} {
		t.Run(code, func(t *testing.T) {
			s, f, token := controlServer(t)
			s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextFailure, ThreadID: "parent", FailureCode: code, Text: "Failure explanation"}
			f.prompt = codex.SessionPromptOffer{ThreadID: "parent", Token: "recovery"}
			o := getOffer(t, s, token)
			if o.Kind != "prompt" || o.Thread != "parent" || len(o.Questions) != 0 || f.calls != 0 {
				t.Fatal("failed session lost its explicit recovery action or retried automatically")
			}
			commit := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Answers: []string{"continue"}})
			if w := actionCall(s, token, "commit", commit); w.Code != 200 || f.calls != 1 || len(f.answers) != 1 || f.answers[0] != "continue" {
				t.Fatal("explicit browser recovery failed", w.Code)
			}
			if w := actionCall(s, token, "commit", commit); w.Code != 409 || f.calls != 1 {
				t.Fatal("browser recovery permitted a replay")
			}
			// Slash discovery is independent of a consumed/absent prompt token.
			commands := &commandsFake{revision: "recovery"}
			s.control.commands = commands
			body := actionRequest{Session: "parent", Command: &commandRequest{Mode: "list", Path: "model"}}
			if w := actionCall(s, token, "commands", body); w.Code != 200 || commands.calls != 0 {
				t.Fatal("failure blocked independent browser command discovery", w.Code)
			}
		})
	}
}

func TestSessionFailureProjectionUsesCurrentAttentionNotSavedContext(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		active       bool
		working      bool
		attention    codex.SessionAttention
		kind         codex.SessionContextKind
		failed       bool
	}{
		{"closed", "INACTIVE", false, false, codex.SessionAttentionNone, codex.SessionContextFailure, false},
		{"idle", "IDLE", true, false, codex.SessionAttentionNone, codex.SessionContextFailure, false},
		{"working", "WORKING", true, true, codex.SessionAttentionNone, codex.SessionContextFailure, false},
		{"current failure", "MODEL BUSY", true, false, codex.SessionAttentionError, codex.SessionContextFailure, true},
		{"failure without saved context", "TURN FAILED", true, false, codex.SessionAttentionError, codex.SessionContextNone, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore()
			row := codex.LiveUsageSession{ID: "thread", Active: tc.active, Working: tc.working, Attention: tc.attention,
				Context: codex.SessionContext{Kind: tc.kind, FailureCode: "serverOverloaded", Text: "Saved explanation"}}
			if tc.kind == codex.SessionContextNone {
				row.Context.FailureCode = ""
			}
			now := time.Now()
			snapshot := codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{row}}
			s.live(snapshot, nil, now)
			if got := s.state.Sessions[0]; got.Failed != tc.failed || got.Status != tc.status || got.Text != "Saved explanation" {
				t.Fatal("current attention and retained context were conflated", got)
			}
			s.live(codex.LiveUsageSnapshot{}, errors.New("offline"), now.Add(time.Second))
			if got := s.state.Sessions[0]; got.Failed || got.Status != "STALE" || got.Text != "Saved explanation" {
				t.Fatal("stale observation retained current failure classification or lost its text", got)
			}
			s.live(snapshot, nil, now.Add(2*time.Second))
			if got := s.state.Sessions[0]; got.Failed != tc.failed || got.Status != tc.status {
				t.Fatal("failure classification did not recover after a successful refresh", got)
			}
		})
	}
}

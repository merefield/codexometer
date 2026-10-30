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

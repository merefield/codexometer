package web

import (
	"context"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

type readSource struct {
	cancelledSource
	calls          int
	revision, turn string
}

func (s *readSource) AcknowledgeSessionResult(_ context.Context, id, turn, revision string) (codex.SessionReadState, error) {
	s.calls++
	s.revision = revision
	s.turn = turn
	return codex.SessionReadState{Revision: "v1-read"}, nil
}

func TestBrowserReadReceiptRequiresWritableFreshMatchingResult(t *testing.T) {
	for _, mode := range []string{"valid", "read-only", "stale", "old-revision", "old-turn", "child", "no-auth"} {
		t.Run(mode, func(t *testing.T) {
			s := testServer()
			f := &readSource{}
			s.control = newControl(f, s.store)
			s.store.state.Control = true
			row := codex.LiveUsageSession{ID: "root", ReadState: codex.SessionReadState{Revision: "v1", Unread: true}, Attention: codex.SessionAttentionComplete, Context: codex.SessionContext{ThreadID: "root", TurnID: "t1", Kind: codex.SessionContextReply, Text: "Finished"}}
			now := time.Now()
			if mode == "stale" {
				now = now.Add(-time.Minute)
			}
			if mode == "child" {
				row.Context.ThreadID = "child"
			}
			s.store.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{row}}, nil, now)
			token := pairBrowser(t, s)
			body := actionRequest{Session: "root", Read: &resultReadRequest{Turn: "t1", Revision: "v1"}}
			switch mode {
			case "read-only":
				s.control = nil
			case "old-revision":
				body.Read.Revision = "old"
			case "old-turn":
				body.Read.Turn = "old"
			case "no-auth":
				token = ""
			}
			w := actionCall(s, token, "read", body)
			if mode == "valid" {
				if w.Code != 200 || f.calls != 1 || f.revision != "v1" || f.turn != "t1" {
					t.Fatal("valid receipt rejected", w.Code, w.Body, f)
				}
				if s.store.state.Sessions[0].Unread {
					t.Fatal("read completion still highlighted")
				}
				if w = actionCall(s, token, "read", body); w.Code != 409 || f.calls != 1 {
					t.Fatal("old receipt replayed")
				}
			} else if w.Code == 200 || f.calls != 0 {
				t.Fatal("unseen result acknowledged", w.Code, f.calls)
			}
		})
	}
}

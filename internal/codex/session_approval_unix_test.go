//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestApprovalWireOneUseAndDisconnect(t *testing.T) {
	received := make(chan map[string]json.RawMessage, 2)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var msg map[string]json.RawMessage
			if conn.ReadJSON(&msg) != nil {
				return
			}
			received <- msg
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	states, c := approvalFixture(t, nil)
	p := &daemonStatusProvider{connection: conn, contexts: states}
	if !p.SessionApprovalPending(c.ApprovalToken) {
		t.Fatal("missing pending")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.RespondSessionApproval(ctx, c.ApprovalToken, "accept"); err == nil {
		t.Fatal("cancelled response accepted")
	}
	if !p.SessionApprovalPending(c.ApprovalToken) {
		t.Fatal("cancelled operation consumed request")
	}
	if err := p.RespondSessionApproval(context.Background(), c.ApprovalToken, "acceptForSession"); err == nil {
		t.Fatal("persistent grant accepted")
	}
	if err := p.RespondSessionApproval(context.Background(), c.ApprovalToken, "accept"); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-received:
		if string(msg["id"]) != `"req"` || string(msg["result"]) != `{"decision":"accept"}` || len(msg) != 2 {
			t.Fatalf("wrong response: %v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no decision sent")
	}
	if p.SessionApprovalPending(c.ApprovalToken) {
		t.Fatal("consumed request still actionable")
	}
	if err := p.RespondSessionApproval(context.Background(), c.ApprovalToken, "decline"); err == nil {
		t.Fatal("duplicate response allowed")
	}
	p.contexts, c = approvalFixture(t, nil)
	if err := p.RespondSessionApproval(context.Background(), c.ApprovalToken, "decline"); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-received:
		if string(msg["result"]) != `{"decision":"decline"}` {
			t.Fatal("wrong decline", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("missing decline")
	}
	p.contexts, c = approvalFixture(t, nil)
	for _, decision := range []any{"cancel", "acceptForSession", map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []string{"pwd"}}}} {
		p.contexts, c = approvalFixture(t, map[string]any{"availableDecisions": []any{decision}})
		if c.ApprovalToken == "" {
			t.Fatal("advertised choice unavailable")
		}
		if err := p.RespondSessionApproval(context.Background(), c.ApprovalToken, "decline"); err == nil {
			t.Fatal("unadvertised choice accepted")
		}
		if err := p.RespondSessionApproval(context.Background(), c.ApprovalToken, c.ApprovalOptions[0].Value); err != nil {
			t.Fatal(err)
		}
		select {
		case msg := <-received:
			want, _ := json.Marshal(map[string]any{"decision": decision})
			if string(msg["result"]) != string(want) {
				t.Fatalf("wire mismatch %s want %s", msg["result"], want)
			}
		case <-time.After(time.Second):
			t.Fatal("missing advertised decision")
		}
	}
	p.contexts, c = approvalFixture(t, nil)
	conn.Close()
	if err := p.RespondSessionApproval(context.Background(), c.ApprovalToken, "accept"); err == nil {
		t.Fatal("closed transport succeeded")
	}
	if p.SessionApprovalPending(c.ApprovalToken) {
		t.Fatal("failed write may be retried")
	}
	p.contexts, c = approvalFixture(t, nil)
	p.disconnect(conn)
	if p.SessionApprovalPending(c.ApprovalToken) {
		t.Fatal("disconnected request actionable")
	}
	if err := p.RespondSessionApproval(context.Background(), c.ApprovalToken, "accept"); err == nil {
		t.Fatal("disconnected response allowed")
	}
}

func TestDaemonSuccessfulCompletionIsSeparateFromIdle(t *testing.T) {
	for _, status := range []string{"completed", "interrupted", "failed", ""} {
		t.Run(status, func(t *testing.T) {
			p := &daemonStatusProvider{contexts: map[string]*daemonContextState{}, statuses: map[string]sessionRuntimeStatus{"root": sessionRuntimeIdle}}
			emit := func(method, body string) {
				daemonContextEvent(p.contexts, method, nil, json.RawMessage(body), time.Now())
			}
			if got := p.statusSnapshotLocked([]string{"root"}).Statuses["root"]; got != sessionRuntimeIdle {
				t.Fatal("unobserved idle became completion")
			}
			emit("turn/completed", `{"threadId":"root","turn":{"id":"turn","status":"`+status+`"}}`)
			want := sessionRuntimeIdle
			if status == "completed" {
				want = sessionRuntimeComplete
			}
			if got := p.statusSnapshotLocked([]string{"root"}).Statuses["root"]; got != want {
				t.Fatalf("completion %q: got %v want %v", status, got, want)
			}
			for _, active := range []sessionRuntimeStatus{sessionRuntimeWorking, sessionRuntimeInput, sessionRuntimeApproval} {
				p.statuses["root"] = active
				if got := p.statusSnapshotLocked([]string{"root"}).Statuses["root"]; got != active {
					t.Fatal("completion displaced active state")
				}
			}
			p.statuses["root"] = sessionRuntimeIdle
			emit("turn/started", `{"threadId":"root","turn":{"id":"next","status":"inProgress"}}`)
			if got := p.statusSnapshotLocked([]string{"root"}).Statuses["root"]; got != sessionRuntimeIdle {
				t.Fatal("completion survived new turn")
			}
		})
	}
}

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

func promptProvider(t *testing.T, runtime string) (*daemonStatusProvider, <-chan daemonEnvelope) {
	t.Helper()
	received := make(chan daemonEnvelope, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var e daemonEnvelope
			if conn.ReadJSON(&e) != nil {
				return
			}
			received <- e
			if e.Method != "" {
				result := map[string]any{}
				if e.Method == "thread/read" {
					result = map[string]any{"thread": map[string]any{"status": map[string]any{"type": runtime}}}
				}
				if conn.WriteJSON(map[string]any{"id": e.ID, "result": result}) != nil {
					return
				}
			}
		}
	}))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &daemonStatusProvider{connection: conn, contexts: map[string]*daemonContextState{}, pending: map[int64]chan daemonEnvelope{}, subscribed: map[string]struct{}{"root": {}}, statuses: map[string]sessionRuntimeStatus{"root": sessionRuntimeIdle}}
	go p.readLoop(conn)
	t.Cleanup(func() { p.disconnect(conn); server.Close() })
	return p, received
}

func TestSessionPromptIdleWireAndOneUse(t *testing.T) {
	p, received := promptProvider(t, "idle")
	o := p.SessionPrompt("root")
	if o.Token == "" || p.SessionPrompt("root").Token != o.Token || p.SessionPrompt("other").Token != "" {
		t.Fatal("invalid capability")
	}
	if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"Hello 世界"}); err != nil {
		t.Fatal(err)
	}
	read, start := <-received, <-received
	if read.Method != "thread/read" || start.Method != "turn/start" {
		t.Fatal(read.Method, start.Method)
	}
	var params map[string]json.RawMessage
	json.Unmarshal(start.Params, &params)
	if len(params) != 2 || string(params["threadId"]) != `"root"` || string(params["input"]) != `[{"text":"Hello 世界","type":"text"}]` {
		t.Fatal(string(start.Params))
	}
	if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"again"}); err == nil {
		t.Fatal("duplicate allowed")
	}
	if p.SessionPrompt("root").Token != "" {
		t.Fatal("new capability minted while sending")
	}
}

func TestSessionPromptFreshBusyCheck(t *testing.T) {
	p, received := promptProvider(t, "active")
	o := p.SessionPrompt("root")
	if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"do not send"}); err == nil {
		t.Fatal("fresh busy status ignored")
	}
	if e := <-received; e.Method != "thread/read" {
		t.Fatal(e.Method)
	}
	select {
	case e := <-received:
		t.Fatal("unexpected send", e.Method)
	default:
	}
}

func TestSessionPromptQuestionsWireAndInvalidation(t *testing.T) {
	p, received := promptProvider(t, "active")
	question := json.RawMessage(`{"threadId":"root","turnId":"turn","itemId":"item","questions":[{"id":"pick","question":"Choose","options":[{"label":"First","description":"One"}]},{"id":"why","question":"Why?","isOther":true,"isSecret":true}]}`)
	emit := func(method string, id, body json.RawMessage) {
		p.mu.Lock()
		daemonContextEvent(p.contexts, method, id, body, time.Now())
		p.promptLifecycleLocked(method, body)
		p.mu.Unlock()
	}
	emit("item/tool/requestUserInput", json.RawMessage(`"request"`), question)
	o := p.SessionPrompt("root")
	if len(o.Questions) != 2 || !o.Questions[1].Secret || o.Questions[0].FreeText {
		t.Fatal(o)
	}
	for _, a := range [][]string{{"First"}, {"Other", "why"}, {"First", ""}, {"First", "bad\x1b[31m"}} {
		if err := p.SendSessionPrompt(context.Background(), o.Token, a); err == nil {
			t.Fatal("invalid answers sent", a)
		}
	}
	if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"First", "Because"}); err != nil {
		t.Fatal(err)
	}
	e := <-received
	if string(e.ID) != `"request"` || string(e.Result) != `{"answers":{"pick":{"answers":["First"]},"why":{"answers":["Because"]}}}` {
		t.Fatal(string(e.Result))
	}
	for _, method := range []string{"serverRequest/resolved", "turn/started", "thread/closed"} {
		p.mu.Lock()
		p.subscribed["root"] = struct{}{}
		p.mu.Unlock()
		emit("item/tool/requestUserInput", json.RawMessage(`"request"`), question)
		o = p.SessionPrompt("root")
		emit(method, nil, json.RawMessage(`{"threadId":"root","requestId":"request"}`))
		if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"First", "Because"}); err == nil {
			t.Fatal("stale request sent", method)
		}
	}
}

func TestSessionPromptBlocksUnsafeOrMixedRequests(t *testing.T) {
	p, _ := promptProvider(t, "idle")
	for _, raw := range []string{
		`{"questions":[{"question":"Missing ID"}]}`,
		`{"questions":[{"id":"same","question":"a"},{"id":"same","question":"b"}]}`,
		`{"questions":[{"id":"x","question":"hidden\u001b[31m"}]}`,
	} {
		var fields map[string]any
		json.Unmarshal([]byte(raw), &fields)
		fields["threadId"], fields["turnId"], fields["itemId"] = "root", "turn", "item"
		body, _ := json.Marshal(fields)
		p.mu.Lock()
		daemonContextEvent(p.contexts, "item/tool/requestUserInput", json.RawMessage(`1`), body, time.Now())
		p.mu.Unlock()
		if p.SessionPrompt("root").Token != "" {
			t.Fatal("unsafe prompt offered", raw)
		}
	}
	p.mu.Lock()
	p.contexts = map[string]*daemonContextState{}
	p.mu.Unlock()
	o := p.SessionPrompt("root")
	p.mu.Lock()
	p.promptLifecycleLocked("turn/started", json.RawMessage(`{"threadId":"root"}`))
	p.mu.Unlock()
	if err := p.SendSessionPrompt(context.Background(), o.Token, []string{"stale"}); err == nil {
		t.Fatal("active session accepted old editor")
	}
	p.disconnect(p.connection)
	if p.SessionPrompt("root").Token != "" {
		t.Fatal("disconnected editor offered")
	}
}

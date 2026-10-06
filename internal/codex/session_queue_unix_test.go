//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

type queueFixture struct {
	mu     sync.Mutex
	items  []map[string]any
	writes []daemonEnvelope
	fail   bool
}

func nativeQueueProvider(t *testing.T) (*daemonStatusProvider, *queueFixture) {
	t.Helper()
	f := &queueFixture{items: []map[string]any{{"id": "one", "input": []any{map[string]any{"type": "text", "text": "Original text"}}}, {"id": "two", "input": []any{map[string]any{"type": "image", "url": "image"}}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var e daemonEnvelope
			if conn.ReadJSON(&e) != nil {
				return
			}
			var params map[string]any
			json.Unmarshal(e.Params, &params)
			f.mu.Lock()
			var result any
			switch e.Method {
			case "thread/queue/list":
				result = map[string]any{"data": f.items}
			case "thread/queue/update":
				f.writes = append(f.writes, e)
				result = map[string]any{"queuedSubmission": map[string]any{"id": params["queuedSubmissionId"]}}
			case "thread/queue/delete":
				f.writes = append(f.writes, e)
				result = map[string]any{"deleted": true}
			}
			response := map[string]any{"id": e.ID, "result": result}
			if f.fail {
				response = map[string]any{"id": e.ID, "error": map[string]any{"code": -32601, "message": "unsupported"}}
			}
			f.mu.Unlock()
			if conn.WriteJSON(response) != nil {
				return
			}
		}
	}))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &daemonStatusProvider{connection: conn, pending: map[int64]chan daemonEnvelope{}, subscribed: map[string]struct{}{"root": {}}}
	go p.readLoop(conn)
	t.Cleanup(func() { p.disconnect(conn); server.Close() })
	return p, f
}

func TestNativeQueueReadAndMutate(t *testing.T) {
	for _, remove := range []bool{false, true} {
		p, f := nativeQueueProvider(t)
		items, err := p.SessionQueue(context.Background(), "root")
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 || items[0].Text != "Original text" || !items[0].Editable || items[1].Editable {
			t.Fatal(items)
		}
		if err = p.ChangeSessionQueue(context.Background(), "root", items[0], "Revised text", remove); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		writes := append([]daemonEnvelope(nil), f.writes...)
		f.mu.Unlock()
		if len(writes) != 1 {
			t.Fatal(writes)
		}
		want := "thread/queue/update"
		if remove {
			want = "thread/queue/delete"
		}
		if writes[0].Method != want || !strings.Contains(string(writes[0].Params), `"queuedSubmissionId":"one"`) {
			t.Fatal(writes[0])
		}
		if err = p.ChangeSessionQueue(context.Background(), "root", items[0], "retry", remove); err == nil {
			t.Fatal("reused capability")
		}
	}
}

func TestNativeQueueRejectsStaleEditsAndOtherThread(t *testing.T) {
	for _, change := range []string{"removed", "changed", "other-thread", "unsupported", "nontext"} {
		t.Run(change, func(t *testing.T) {
			p, f := nativeQueueProvider(t)
			items, err := p.SessionQueue(context.Background(), "root")
			if err != nil {
				t.Fatal(err)
			}
			item, thread := items[0], "root"
			f.mu.Lock()
			switch change {
			case "removed":
				f.items = nil
			case "changed":
				f.items[0]["input"] = []any{map[string]any{"type": "text", "text": "Changed elsewhere"}}
			case "other-thread":
				thread = "another"
			case "unsupported":
				f.fail = true
			case "nontext":
				item = items[1]
			}
			f.mu.Unlock()
			if err = p.ChangeSessionQueue(context.Background(), thread, item, "replacement", false); err == nil {
				t.Fatal("unsafe edit allowed")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.writes) != 0 {
				t.Fatal("stale edit mutated queue")
			}
		})
	}
}

func TestNativeQueueSanitizesPreview(t *testing.T) {
	p, f := nativeQueueProvider(t)
	f.mu.Lock()
	f.items[0]["input"] = []any{map[string]any{"type": "text", "text": "Hello\x1b[31m red"}}
	f.mu.Unlock()
	items, err := p.SessionQueue(context.Background(), "root")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(items[0].Text, "\x1b") || items[0].Editable {
		t.Fatal("terminal control passed through or lossy edit allowed")
	}
}

//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

func TestDaemonStatusProviderReadsExactThreadStates(t *testing.T) {
	// Darwin limits Unix-domain socket paths to roughly 104 bytes. Go's
	// descriptive t.TempDir path can exceed that before the socket name is
	// appended, so reserve a short unique name directly under the system temp
	// directory instead.
	socketFile, err := os.CreateTemp("", "cxm-*.sock")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := socketFile.Name()
	if err := socketFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skip("sandbox does not permit Unix-domain socket listeners")
		}
		t.Fatal(err)
	}
	upgrader := websocket.Upgrader{}
	var resumedHistorical atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, upgradeErr := upgrader.Upgrade(writer, request, nil)
		if upgradeErr != nil {
			return
		}
		defer connection.Close()
		for {
			var message struct {
				ID     *int64         `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if readErr := connection.ReadJSON(&message); readErr != nil {
				return
			}
			if message.ID == nil {
				continue
			}
			switch message.Method {
			case "initialize":
				_ = connection.WriteJSON(map[string]any{"id": *message.ID, "result": map[string]any{"userAgent": "codex/1.0"}})
			case "thread/loaded/list":
				_ = connection.WriteJSON(map[string]any{"id": *message.ID, "result": map[string]any{
					"data": []string{"working", "approval"}, "nextCursor": nil,
				}})
			case "thread/resume":
				threadID, _ := message.Params["threadId"].(string)
				if threadID == "historical" {
					resumedHistorical.Store(true)
				}
				_ = connection.WriteJSON(map[string]any{"id": *message.ID, "result": map[string]any{
					"thread": map[string]any{"id": threadID},
				}})
				if threadID == "working" {
					_ = connection.WriteJSON(map[string]any{
						"id": "approval-1", "method": "item/commandExecution/requestApproval", "params": map[string]any{},
					})
					_ = connection.WriteJSON(map[string]any{"method": "model/rerouted", "params": map[string]any{
						"threadId": threadID, "turnId": "turn-1", "fromModel": "gpt-5.6-sol", "toModel": "gpt-5.6-terra",
					}})
					_ = connection.WriteJSON(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{
						"threadId": threadID, "turnId": "turn-1", "tokenUsage": map[string]any{
							"total": map[string]any{"inputTokens": 120, "cachedInputTokens": 20, "outputTokens": 30, "totalTokens": 150},
							"last":  map[string]any{"inputTokens": 120, "cachedInputTokens": 20, "outputTokens": 30, "totalTokens": 150},
						},
					}})
				}
			case "thread/read":
				threadID, _ := message.Params["threadId"].(string)
				status := map[string]any{"type": "active", "activeFlags": []string{}}
				if threadID == "approval" {
					status["activeFlags"] = []string{"waitingOnApproval"}
				}
				_ = connection.WriteJSON(map[string]any{"id": *message.ID, "result": map[string]any{
					"thread": map[string]any{"id": threadID, "status": status},
				}})
			}
		}
	})}
	done := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(done)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		<-done
	})

	provider := &daemonStatusProvider{socketPath: socketPath}
	t.Cleanup(func() { provider.disconnect(nil) })
	snapshot, exact := provider.Fetch(context.Background(), []string{"working", "approval", "historical"})
	if !exact {
		t.Fatal("shared daemon was not detected")
	}
	if snapshot.Statuses["working"] != sessionRuntimeWorking || snapshot.Statuses["approval"] != sessionRuntimeApproval {
		t.Fatalf("exact daemon statuses = %#v", snapshot.Statuses)
	}
	if len(snapshot.ModelObservations) != 1 {
		t.Fatalf("reroute observations = %#v", snapshot.ModelObservations)
	}
	observation := snapshot.ModelObservations[0]
	if observation.ThreadID != "working" || observation.TurnID != "turn-1" ||
		observation.Model != "gpt-5.6-terra" || observation.Usage.TotalTokens != 150 || observation.CumulativeTotal != 150 {
		t.Fatalf("reroute observation = %#v", observation)
	}
	if resumedHistorical.Load() {
		t.Fatal("provider subscribed to a thread that was not already loaded")
	}
}

func TestDaemonStatusProviderFallsBackWhenSocketIsMissing(t *testing.T) {
	provider := &daemonStatusProvider{socketPath: filepath.Join(t.TempDir(), "missing.sock")}
	snapshot, exact := provider.Fetch(context.Background(), []string{"thread"})
	if exact || snapshot.Statuses != nil || snapshot.ModelObservations != nil {
		encoded, _ := json.Marshal(snapshot)
		t.Fatalf("missing daemon = exact %v statuses %s", exact, encoded)
	}
}

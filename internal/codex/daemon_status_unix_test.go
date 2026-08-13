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

	provider := daemonStatusProvider{socketPath: socketPath}
	statuses, exact := provider.Fetch(context.Background(), []string{"working", "approval"})
	if !exact {
		t.Fatal("shared daemon was not detected")
	}
	if statuses["working"] != sessionRuntimeWorking || statuses["approval"] != sessionRuntimeApproval {
		t.Fatalf("exact daemon statuses = %#v", statuses)
	}
}

func TestDaemonStatusProviderFallsBackWhenSocketIsMissing(t *testing.T) {
	provider := daemonStatusProvider{socketPath: filepath.Join(t.TempDir(), "missing.sock")}
	statuses, exact := provider.Fetch(context.Background(), []string{"thread"})
	if exact || statuses != nil {
		encoded, _ := json.Marshal(statuses)
		t.Fatalf("missing daemon = exact %v statuses %s", exact, encoded)
	}
}

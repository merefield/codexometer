//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
)

const daemonStatusTimeout = 2 * time.Second

type daemonStatusProvider struct {
	socketPath string
}

func newSessionStatusProvider(codexHome string) sessionStatusProvider {
	return daemonStatusProvider{socketPath: filepath.Join(codexHome, "app-server-control", "app-server-control.sock")}
}

func (p daemonStatusProvider) Fetch(ctx context.Context, threadIDs []string) (map[string]sessionRuntimeStatus, bool) {
	dialer := websocket.Dialer{
		HandshakeTimeout: daemonStatusTimeout,
		NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: daemonStatusTimeout}).DialContext(ctx, "unix", p.socketPath)
		},
	}
	connection, response, err := dialer.DialContext(ctx, "ws://localhost/", http.Header{})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, false
	}
	defer connection.Close()
	setDaemonDeadline := func() {
		deadline := time.Now().Add(daemonStatusTimeout)
		_ = connection.SetReadDeadline(deadline)
		_ = connection.SetWriteDeadline(deadline)
	}
	setDaemonDeadline()

	requestID := int64(1)
	var initialized struct {
		UserAgent string `json:"userAgent"`
	}
	if err := daemonRequest(connection, requestID, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "codexometer", "title": "Codexometer", "version": "monitor"},
	}, &initialized); err != nil {
		return nil, false
	}
	if err := connection.WriteJSON(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, false
	}

	statuses := make(map[string]sessionRuntimeStatus, len(threadIDs))
	for _, threadID := range threadIDs {
		setDaemonDeadline()
		requestID++
		var result struct {
			Thread struct {
				ID     string `json:"id"`
				Status struct {
					Type        string   `json:"type"`
					ActiveFlags []string `json:"activeFlags"`
				} `json:"status"`
			} `json:"thread"`
		}
		if err := daemonRequest(connection, requestID, "thread/read", map[string]any{
			"threadId": threadID, "includeTurns": false,
		}, &result); err != nil {
			continue
		}
		status := parseSessionRuntimeStatus(result.Thread.Status.Type, result.Thread.Status.ActiveFlags)
		if status != sessionRuntimeUnknown {
			statuses[threadID] = status
		}
	}
	return statuses, true
}

func daemonRequest(connection *websocket.Conn, id int64, method string, params any, target any) error {
	if err := connection.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var envelope struct {
			ID     *int64          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := connection.ReadJSON(&envelope); err != nil {
			return err
		}
		if envelope.ID == nil || *envelope.ID != id {
			continue
		}
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			return &daemonResponseError{payload: string(envelope.Error)}
		}
		return json.Unmarshal(envelope.Result, target)
	}
}

type daemonResponseError struct{ payload string }

func (e *daemonResponseError) Error() string { return e.payload }

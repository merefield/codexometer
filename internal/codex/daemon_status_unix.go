//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const daemonStatusTimeout = 2 * time.Second

type daemonStatusProvider struct {
	socketPath string

	mu            sync.Mutex
	connection    *websocket.Conn
	nextRequestID int64
	pending       map[int64]chan daemonEnvelope
	subscribed    map[string]struct{}
	reroutedTurns map[daemonTurnKey]string
	observations  []resolvedModelObservation
	nextSequence  uint64
	writeMu       sync.Mutex
}

type daemonTurnKey struct {
	threadID string
	turnID   string
}

type daemonEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func newSessionStatusProvider(codexHome string) sessionStatusProvider {
	return &daemonStatusProvider{socketPath: filepath.Join(codexHome, "app-server-control", "app-server-control.sock")}
}

func (p *daemonStatusProvider) Fetch(ctx context.Context, threadIDs []string) (sessionDaemonSnapshot, bool) {
	if err := p.ensureConnected(ctx); err != nil {
		return sessionDaemonSnapshot{}, false
	}

	loaded, err := p.loadedThreads(ctx)
	if err == nil {
		p.unsubscribeMissing(ctx, threadIDs)
		for _, threadID := range threadIDs {
			if _, ok := loaded[threadID]; !ok || p.isSubscribed(threadID) {
				continue
			}
			var resumed json.RawMessage
			if err := p.request(ctx, "thread/resume", map[string]any{
				"threadId": threadID, "excludeTurns": true,
			}, &resumed); err != nil {
				continue
			}
			p.mu.Lock()
			p.subscribed[threadID] = struct{}{}
			p.mu.Unlock()
		}
	}

	statuses := make(map[string]sessionRuntimeStatus, len(threadIDs))
	for _, threadID := range threadIDs {
		var result struct {
			Thread struct {
				ID     string `json:"id"`
				Status struct {
					Type        string   `json:"type"`
					ActiveFlags []string `json:"activeFlags"`
				} `json:"status"`
			} `json:"thread"`
		}
		if err := p.request(ctx, "thread/read", map[string]any{
			"threadId": threadID, "includeTurns": false,
		}, &result); err != nil {
			continue
		}
		status := parseSessionRuntimeStatus(result.Thread.Status.Type, result.Thread.Status.ActiveFlags)
		if status != sessionRuntimeUnknown {
			statuses[threadID] = status
		}
	}

	p.mu.Lock()
	observations := append([]resolvedModelObservation(nil), p.observations...)
	subscribed := make(map[string]struct{}, len(p.subscribed))
	for threadID := range p.subscribed {
		subscribed[threadID] = struct{}{}
	}
	p.mu.Unlock()
	return sessionDaemonSnapshot{
		Statuses: statuses, ModelObservations: observations, SubscribedThreads: subscribed,
	}, true
}

func (p *daemonStatusProvider) unsubscribeMissing(ctx context.Context, threadIDs []string) {
	wanted := make(map[string]struct{}, len(threadIDs))
	for _, threadID := range threadIDs {
		wanted[threadID] = struct{}{}
	}
	p.mu.Lock()
	stale := make([]string, 0)
	for threadID := range p.subscribed {
		if _, ok := wanted[threadID]; !ok {
			stale = append(stale, threadID)
		}
	}
	p.mu.Unlock()
	for _, threadID := range stale {
		var result json.RawMessage
		if p.request(ctx, "thread/unsubscribe", map[string]any{"threadId": threadID}, &result) != nil {
			continue
		}
		p.mu.Lock()
		delete(p.subscribed, threadID)
		for key := range p.reroutedTurns {
			if key.threadID == threadID {
				delete(p.reroutedTurns, key)
			}
		}
		p.mu.Unlock()
	}
}

func (p *daemonStatusProvider) ensureConnected(ctx context.Context) error {
	p.mu.Lock()
	if p.connection != nil {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

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
		return err
	}

	p.mu.Lock()
	if p.connection != nil {
		p.mu.Unlock()
		_ = connection.Close()
		return nil
	}
	p.connection = connection
	p.pending = make(map[int64]chan daemonEnvelope)
	p.subscribed = make(map[string]struct{})
	p.reroutedTurns = make(map[daemonTurnKey]string)
	p.mu.Unlock()
	go p.readLoop(connection)

	var initialized struct {
		UserAgent string `json:"userAgent"`
	}
	if err := p.request(ctx, "initialize", map[string]any{
		"clientInfo":   codexometerClientInfo(),
		"capabilities": map[string]any{"experimentalApi": true},
	}, &initialized); err != nil {
		p.disconnect(connection)
		return err
	}
	if err := p.notify("initialized", map[string]any{}); err != nil {
		p.disconnect(connection)
		return err
	}
	return nil
}

func (p *daemonStatusProvider) loadedThreads(ctx context.Context) (map[string]struct{}, error) {
	loaded := make(map[string]struct{})
	var cursor any
	for {
		params := map[string]any{}
		if cursor != nil {
			params["cursor"] = cursor
		}
		var result struct {
			Data       []string `json:"data"`
			NextCursor any      `json:"nextCursor"`
		}
		if err := p.request(ctx, "thread/loaded/list", params, &result); err != nil {
			return nil, err
		}
		for _, threadID := range result.Data {
			loaded[threadID] = struct{}{}
		}
		if result.NextCursor == nil {
			return loaded, nil
		}
		cursor = result.NextCursor
	}
}

func (p *daemonStatusProvider) isSubscribed(threadID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.subscribed[threadID]
	return ok
}

func (p *daemonStatusProvider) request(ctx context.Context, method string, params any, target any) error {
	p.mu.Lock()
	connection := p.connection
	if connection == nil {
		p.mu.Unlock()
		return errors.New("daemon connection is closed")
	}
	p.nextRequestID++
	id := p.nextRequestID
	response := make(chan daemonEnvelope, 1)
	p.pending[id] = response
	p.mu.Unlock()

	if err := p.write(connection, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		p.disconnect(connection)
		return err
	}
	timer := time.NewTimer(daemonStatusTimeout)
	defer timer.Stop()
	select {
	case envelope := <-response:
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			return &daemonResponseError{payload: string(envelope.Error)}
		}
		if target == nil {
			return nil
		}
		return json.Unmarshal(envelope.Result, target)
	case <-ctx.Done():
		p.dropPending(id)
		return ctx.Err()
	case <-timer.C:
		p.dropPending(id)
		return fmt.Errorf("daemon %s request timed out", method)
	}
}

func (p *daemonStatusProvider) notify(method string, params any) error {
	p.mu.Lock()
	connection := p.connection
	p.mu.Unlock()
	if connection == nil {
		return errors.New("daemon connection is closed")
	}
	return p.write(connection, map[string]any{"method": method, "params": params})
}

func (p *daemonStatusProvider) write(connection *websocket.Conn, value any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = connection.SetWriteDeadline(time.Now().Add(daemonStatusTimeout))
	return connection.WriteJSON(value)
}

func (p *daemonStatusProvider) dropPending(id int64) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *daemonStatusProvider) readLoop(connection *websocket.Conn) {
	for {
		var envelope daemonEnvelope
		if err := connection.ReadJSON(&envelope); err != nil {
			p.disconnect(connection)
			return
		}
		var responseID int64
		if envelope.Method == "" && len(envelope.ID) > 0 && json.Unmarshal(envelope.ID, &responseID) == nil {
			p.mu.Lock()
			response := p.pending[responseID]
			delete(p.pending, responseID)
			p.mu.Unlock()
			if response != nil {
				select {
				case response <- envelope:
				default:
				}
			}
			continue
		}
		p.handleNotification(envelope.Method, envelope.Params)
	}
}

func (p *daemonStatusProvider) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "model/rerouted":
		var notification struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			ToModel  string `json:"toModel"`
		}
		if json.Unmarshal(params, &notification) != nil || notification.ThreadID == "" ||
			notification.TurnID == "" || notification.ToModel == "" {
			return
		}
		p.mu.Lock()
		p.reroutedTurns[daemonTurnKey{notification.ThreadID, notification.TurnID}] = notification.ToModel
		p.mu.Unlock()
	case "thread/tokenUsage/updated":
		var notification struct {
			ThreadID   string `json:"threadId"`
			TurnID     string `json:"turnId"`
			TokenUsage struct {
				Total BenchmarkUsage `json:"total"`
				Last  BenchmarkUsage `json:"last"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(params, &notification) != nil {
			return
		}
		key := daemonTurnKey{notification.ThreadID, notification.TurnID}
		p.mu.Lock()
		model := p.reroutedTurns[key]
		if model != "" {
			p.nextSequence++
			p.observations = appendBounded(p.observations, resolvedModelObservation{
				Sequence: p.nextSequence, ThreadID: notification.ThreadID,
				TurnID: notification.TurnID, Model: model, Usage: notification.TokenUsage.Last,
				CumulativeTotal: notification.TokenUsage.Total.TotalTokens,
			}, telemetryHistoryMax)
		}
		p.mu.Unlock()
	case "turn/completed":
		var notification struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &notification) == nil {
			p.mu.Lock()
			delete(p.reroutedTurns, daemonTurnKey{notification.ThreadID, notification.Turn.ID})
			p.mu.Unlock()
		}
	}
}

func (p *daemonStatusProvider) disconnect(connection *websocket.Conn) {
	p.mu.Lock()
	if connection != nil && p.connection != connection {
		p.mu.Unlock()
		return
	}
	current := p.connection
	p.connection = nil
	for id, response := range p.pending {
		delete(p.pending, id)
		select {
		case response <- daemonEnvelope{Error: json.RawMessage(`{"message":"daemon connection closed"}`)}:
		default:
		}
	}
	p.subscribed = nil
	p.reroutedTurns = nil
	p.mu.Unlock()
	if current != nil {
		_ = current.Close()
	}
}

type daemonResponseError struct{ payload string }

func (e *daemonResponseError) Error() string { return e.payload }

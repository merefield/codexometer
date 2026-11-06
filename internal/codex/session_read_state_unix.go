//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"time"
)

type readStateObservation struct {
	state   SessionReadState
	version uint64
}

func (p *daemonStatusProvider) observeReadStateLocked(id string, state SessionReadState) {
	if p.readStates == nil {
		p.readStates = make(map[string]readStateObservation)
	}
	if len(p.readStates) >= 256 {
		if _, known := p.readStates[id]; !known {
			return
		}
	}
	p.readStateVersion++
	p.readStates[id] = readStateObservation{state: state, version: p.readStateVersion}
}

func (p *daemonStatusProvider) readStateNotificationLocked(method string, raw json.RawMessage) {
	switch method {
	case "thread/readState/changed":
		var notification struct {
			ThreadID  string
			ReadState *wireReadState
		}
		if json.Unmarshal(raw, &notification) == nil && notification.ThreadID != "" {
			p.observeReadStateLocked(notification.ThreadID, notification.ReadState.snapshot())
		}
	case "turn/completed", "thread/closed", "thread/archived", "thread/deleted":
		var notification struct{ ThreadID string }
		if json.Unmarshal(raw, &notification) == nil && notification.ThreadID != "" {
			p.observeReadStateLocked(notification.ThreadID, SessionReadState{})
			p.lastStatusAt = time.Time{}
		}
	}
}

func (p *daemonStatusProvider) AcknowledgeSessionResult(ctx context.Context, id, turn, revision string) (SessionReadState, error) {
	if id == "" || turn == "" || revision == "" || len(id) > 512 || len(turn) > 512 || len(revision) > 2048 {
		return SessionReadState{}, ErrSessionReadState
	}
	p.mu.Lock()
	connection, observed := p.connection, p.readStates[id].state
	p.mu.Unlock()
	if connection == nil || !observed.Unread || observed.Revision != revision {
		return SessionReadState{}, ErrSessionReadState
	}
	// A stale excerpt may coexist with a freshly polled receipt. Verify which
	// completed result the receipt would acknowledge before sending any write.
	var history struct{ Data []historyTurn }
	if err := p.requestOn(ctx, connection, "thread/turns/list", map[string]any{"threadId": id, "limit": 2, "sortDirection": "desc", "itemsView": "summary"}, &history); err != nil {
		return SessionReadState{}, err
	}
	matched := false
	for _, latest := range history.Data {
		if latest.Status == "inProgress" {
			continue
		}
		if latest.Status == "completed" && latest.ID == turn {
			for _, item := range latest.Items {
				if item.Type == "agentMessage" && (item.Phase == "final_answer" || item.Phase == "") && item.Text != "" {
					matched = true
				}
			}
		}
		break
	}
	if !matched {
		return SessionReadState{}, ErrSessionReadState
	}
	p.mu.Lock()
	current := p.readStates[id].state
	valid := p.connection == connection && current == observed
	p.mu.Unlock()
	if !valid {
		return SessionReadState{}, ErrSessionReadState
	}
	var response struct{ ReadState *wireReadState }
	// No retry, resume or reconnect: this mutation belongs to the viewed revision
	// and the original connection. Codex atomically rejects newer unseen activity.
	if err := p.requestOn(ctx, connection, "thread/readState/update", map[string]any{"threadId": id, "operation": "read", "expectedRevision": revision}, &response); err != nil {
		p.mu.Lock()
		if p.connection == connection {
			p.lastStatusAt = time.Time{}
		}
		p.mu.Unlock()
		return SessionReadState{}, err
	}
	next := response.ReadState.snapshot()
	if !next.Known() || next.Unread {
		return SessionReadState{}, ErrSessionReadState
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.connection != connection {
		return SessionReadState{}, ErrSessionReadState
	}
	current = p.readStates[id].state
	if current == observed || current == next {
		p.observeReadStateLocked(id, next)
	}
	return p.readStates[id].state, nil
}

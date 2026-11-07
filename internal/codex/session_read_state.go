package codex

import (
	"context"
	"encoding/json"
	"errors"
)

// SessionReadState is Codex's durable local result receipt. An empty revision
// means unavailable; never substitute a local guess for shared read state.
type SessionReadState struct {
	Revision string
	Unread   bool
}

func (s SessionReadState) Known() bool { return s.Revision != "" }

var ErrSessionReadState = errors.New("result changed or read state is unavailable; review the result again")

// AcknowledgeSessionResult marks only a deliberately viewed latest completed
// reply. The server's revision check must protect newer, unseen results.
type SessionReadStateClient interface {
	AcknowledgeSessionResult(context.Context, string, string, string) (SessionReadState, error)
}

func (c Client) AcknowledgeSessionResult(ctx context.Context, id, turn, revision string) (SessionReadState, error) {
	if p, ok := sessionCapability[SessionReadStateClient](c); ok {
		return p.AcknowledgeSessionResult(ctx, id, turn, revision)
	}
	return SessionReadState{}, ErrSessionReadState
}

// Wire receipts are deliberately separate from the comparable display value.
type wireReadState struct {
	Revision    string          `json:"revision"`
	FirstUnread json.RawMessage `json:"firstUnread"`
}

func (s *wireReadState) snapshot() SessionReadState {
	if s == nil || s.Revision == "" || len(s.Revision) > 2048 || len(s.FirstUnread) == 0 {
		return SessionReadState{}
	}
	unread := string(s.FirstUnread) != "null"
	if unread {
		var position struct {
			Type   string
			TurnID string
		}
		if json.Unmarshal(s.FirstUnread, &position) != nil || position.Type != "threadStart" && (position.Type != "turn" || position.TurnID == "") {
			return SessionReadState{}
		}
	}
	return SessionReadState{Revision: s.Revision, Unread: unread}
}

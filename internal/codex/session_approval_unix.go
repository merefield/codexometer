//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
)

// Caller holds mu. Tokens are regenerated on replay/reconnect; stale UI state
// cannot answer a reused JSON-RPC id on another connection.
func (p *daemonStatusProvider) approvalLocked(token string) (*daemonContextState, string) {
	if token == "" || p.connection == nil {
		return nil, ""
	}
	for _, state := range p.contexts {
		for id, c := range state.requests {
			if c.ApprovalToken == token {
				return state, id
			}
		}
	}
	return nil, ""
}

func (p *daemonStatusProvider) SessionApprovalPending(token string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, _ := p.approvalLocked(token)
	return s != nil
}

func (p *daemonStatusProvider) RespondSessionApproval(ctx context.Context, token, decision string) error {
	p.mu.Lock()
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return err
	}
	s, id := p.approvalLocked(token)
	if s == nil {
		p.mu.Unlock()
		return errors.New("request expired or connection closed; check Codex")
	}
	var wire string
	for _, option := range s.requests[id].ApprovalOptions {
		if option.Kind != "" && option.Value == decision {
			wire = option.Wire
			break
		}
	}
	if wire == "" {
		p.mu.Unlock()
		return errors.New("decision was not offered by this request")
	}
	// Consume before writing. A failed/ambiguous write must never auto-retry.
	delete(s.requests, id)
	connection := p.connection
	p.mu.Unlock()
	// Never hold the state lock during IO: rendering can check pending state
	// without waiting for the network. The server arbitrates concurrent replies.
	return p.write(connection, map[string]any{"id": json.RawMessage(id), "result": map[string]any{"decision": json.RawMessage(wire)}})
}

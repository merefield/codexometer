//go:build unix

package codex

import (
	"context"
	"encoding/json"
)

// Recover missed item/started data, never an authorisation or a local file diff.
// At most four reads share one RPC-timeout budget per status refresh; a pending
// request gets at most three attempts (history may not be flushed on the first).
func (p *daemonStatusProvider) refreshFileApprovals(ctx context.Context, threads []string) {
	ctx, cancel := context.WithTimeout(ctx, daemonStatusTimeout)
	defer cancel()
	p.mu.Lock()
	connection := p.connection
	type recovery struct {
		state  *daemonContextState
		review SessionContext
	}
	var pending []recovery
	for _, thread := range threads {
		s := p.contexts[thread]
		if s == nil || s.ended || p.statuses[thread] != sessionRuntimeApproval {
			continue
		}
		for id, c := range s.requests {
			_, observed := s.patches[c.TurnID+"/"+c.ItemID]
			if observed || !c.fileApprovalRecoverable || c.fileApprovalFetches >= 3 || c.ApprovalToken != "" || c.FileChanges != "" || s.activeTurn != "" && c.TurnID != s.activeTurn || len(pending) >= 4 {
				continue
			}
			c.fileApprovalFetches++
			s.requests[id] = c
			pending = append(pending, recovery{s, c})
		}
	}
	p.mu.Unlock()
	for _, job := range pending {
		c := job.review
		var response struct {
			Data []struct {
				TurnID         string
				OutputDeferred bool
				Item           struct {
					ID, Type, Status string
					Changes          json.RawMessage
				}
			}
		}
		if err := p.requestOn(ctx, connection, "thread/items/read", map[string]any{
			"threadId": c.ThreadID, "turnId": c.TurnID, "itemIds": []string{c.ItemID},
		}, &response); err != nil || len(response.Data) != 1 {
			continue
		}
		entry := response.Data[0]
		if entry.TurnID != c.TurnID || entry.OutputDeferred || entry.Item.ID != c.ItemID || entry.Item.Type != "fileChange" || entry.Item.Status != "inProgress" || validatedFileChanges(entry.Item.Changes) == "" {
			continue
		}
		p.mu.Lock()
		s := p.contexts[c.ThreadID]
		if p.connection == connection && s == job.state && !s.ended && (s.activeTurn == "" || s.activeTurn == c.TurnID) && p.statuses[c.ThreadID] == sessionRuntimeApproval && s.requests[c.RequestID] == c {
			if _, observed := s.patches[c.TurnID+"/"+c.ItemID]; !observed {
				s.capturePatch(c.TurnID, c.ItemID, entry.Item.Changes)
			}
		}
		p.mu.Unlock()
	}
}

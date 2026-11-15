//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"sort"
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
		for _, c := range s.requests {
			_, observed := s.patches[c.TurnID+"/"+c.ItemID]
			if observed || !c.fileApprovalRecoverable || c.fileApprovalFetches >= 3 || c.ApprovalToken != "" || c.FileChanges != "" || s.activeTurn != "" && c.TurnID != s.activeTurn || len(pending) >= 4 {
				continue
			}
			pending = append(pending, recovery{s, c})
		}
	}
	p.mu.Unlock()
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].review.ThreadID != pending[j].review.ThreadID {
			return pending[i].review.ThreadID < pending[j].review.ThreadID
		}
		return pending[i].review.RequestID < pending[j].review.RequestID
	})
	// Caller holds mu. The same guard protects reservation and application.
	pendingState := func(job recovery) *daemonContextState {
		c := job.review
		s := p.contexts[c.ThreadID]
		if connection == nil || p.connection != connection || s != job.state || s.ended || s.activeTurn != "" && s.activeTurn != c.TurnID || p.statuses[c.ThreadID] != sessionRuntimeApproval || s.requests[c.RequestID] != c {
			return nil
		}
		if _, observed := s.patches[c.TurnID+"/"+c.ItemID]; observed {
			return nil
		}
		return s
	}
	for _, job := range pending {
		p.mu.Lock()
		if ctx.Err() != nil {
			p.mu.Unlock()
			break
		}
		s := pendingState(job)
		if s == nil {
			p.mu.Unlock()
			continue
		}
		job.review.fileApprovalFetches++
		s.requests[job.review.RequestID] = job.review
		p.mu.Unlock()
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
		if s := pendingState(job); s != nil {
			s.capturePatch(c.TurnID, c.ItemID, entry.Item.Changes)
		}
		p.mu.Unlock()
	}
}

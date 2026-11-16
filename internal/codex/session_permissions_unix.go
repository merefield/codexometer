//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

// Caller holds p.mu. An overflowed inventory is not evidence that all human
// checkpoints have resolved, even after the retained requests drain.
func (p *daemonStatusProvider) commandHumanWaitLocked(id string) bool {
	state := p.contexts[id]
	return state != nil && (len(state.requests) > 0 || state.approvalsLimited) || p.statuses[id] == sessionRuntimeApproval || p.statuses[id] == sessionRuntimeInput
}

// A settings acknowledgement means queued. Read the loaded thread without
// overrides until native permission provenance confirms the chosen profile.
// This neither waits under the event-state lock nor retries the mutation.
func (p *daemonStatusProvider) writeVerifiedPermissions(ctx context.Context, conn *websocket.Conn, id, permission string, params map[string]any) error {
	p.mu.Lock()
	allowed := p.connection == conn && !p.commandHumanWaitLocked(id)
	p.mu.Unlock()
	if !allowed {
		return ErrSessionCommand
	}
	if err := p.requestOn(ctx, conn, "thread/settings/update", params, nil); err != nil {
		return err
	}
	verify, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		loaded, err := p.loadedThreads(verify)
		if err != nil {
			return fmt.Errorf("permission update unconfirmed: %w", err)
		}
		if _, ok := loaded[id]; !ok {
			return errors.New("permission update unconfirmed: session unloaded")
		}
		var result struct {
			Thread                  struct{ ID string }
			ActivePermissionProfile *struct{ ID string }
			ApprovalPolicy          json.RawMessage
			ApprovalsReviewer       string
		}
		if err := p.requestOn(verify, conn, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &result); err != nil {
			return fmt.Errorf("permission update unconfirmed: %w", err)
		}
		policyOK, reviewerOK := true, true
		if v, ok := params["approvalPolicy"]; ok {
			policyOK = equalPermissionPolicy(v, result.ApprovalPolicy)
		}
		if v, ok := params["approvalsReviewer"]; ok {
			reviewerOK = v == result.ApprovalsReviewer
		}
		if result.Thread.ID == id && result.ActivePermissionProfile != nil && result.ActivePermissionProfile.ID == permission && policyOK && reviewerOK {
			p.mu.Lock()
			if p.connection == conn {
				p.commandGeneration++
				if result.ApprovalsReviewer != "" {
					if p.permissionSettings == nil {
						p.permissionSettings = map[string]string{}
					}
					p.permissionSettings[id] = result.ApprovalsReviewer
				}
			}
			p.mu.Unlock()
			return nil
		}
		select {
		case <-verify.Done():
			return fmt.Errorf("permission update queued but application unconfirmed: %w", verify.Err())
		case <-ticker.C:
		}
	}
}

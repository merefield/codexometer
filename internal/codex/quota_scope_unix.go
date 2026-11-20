//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gorilla/websocket"
)

var errQuotaAccountChanged = errors.New("scheduled quota account changed")

func (p *daemonStatusProvider) accountNotificationLocked(raw json.RawMessage) {
	p.lastStatusAt = time.Time{}
	if workspaceRoutingReady(raw) {
		return // This event specifically confirms the authenticated owner did not change.
	}
	p.commandGeneration++
	clear(p.readStates)
	p.finderNotificationLocked("account/updated", raw)
}

// verifyQuotaAccount uses the same connection as the subsequent mutation.
// Some backends only expose their workspace ID on the quota response.
func (p *daemonStatusProvider) verifyQuotaAccount(ctx context.Context, conn *websocket.Conn, expected string) error {
	var raw json.RawMessage
	if conn == nil || expected == "" || p.requestOn(ctx, conn, "account/read", map[string]bool{"refreshToken": false}, &raw) != nil {
		return ErrScheduledQuotaUnverified
	}
	account := fingerprintAccount(raw)
	if account == "" {
		return ErrScheduledQuotaUnverified
	}
	workspace := workspaceRoutingID(raw)
	if IsWorkspaceQuotaAccount(expected) && workspace == "" {
		var limits struct {
			AccountID string `json:"accountId"`
		}
		if p.requestOn(ctx, conn, "account/rateLimits/read", map[string]bool{"excludeResetCreditDetails": true}, &limits) != nil || limits.AccountID == "" {
			return ErrScheduledQuotaUnverified
		}
		workspace = limits.AccountID
	}
	if quotaAccount(account, workspace) != expected {
		// Don't upgrade an older email-only authorisation to a newly known
		// workspace. Wait for the scheduler's next workspace-aware sample.
		if workspace != "" && !IsWorkspaceQuotaAccount(expected) && account == expected {
			return ErrScheduledQuotaUnverified
		}
		return errQuotaAccountChanged
	}
	return nil
}

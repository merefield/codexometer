//go:build unix

package codex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

// A settings acknowledgement means queued. Read the loaded thread without
// overrides until native permission provenance confirms the chosen profile.
// This neither waits under the event-state lock nor retries the mutation.
func (p *daemonStatusProvider) writeVerifiedPermissions(ctx context.Context, conn *websocket.Conn, id, permission string, params map[string]any) error {
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
		}
		if err := p.requestOn(verify, conn, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &result); err != nil {
			return fmt.Errorf("permission update unconfirmed: %w", err)
		}
		if result.Thread.ID == id && result.ActivePermissionProfile != nil && result.ActivePermissionProfile.ID == permission {
			return nil
		}
		select {
		case <-verify.Done():
			return fmt.Errorf("permission update queued but application unconfirmed: %w", verify.Err())
		case <-ticker.C:
		}
	}
}

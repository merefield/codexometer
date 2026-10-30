//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Codex 0.166 adds the loaded thread's effective environment hooks. Older
// servers may reject or silently ignore threadId; keep their cwd inventory,
// without falling back after a real thread, permission or transport failure.
func (p *daemonStatusProvider) commandHookRows(ctx context.Context, t commandThread) ([]json.RawMessage, error) {
	rows, err := p.commandRows(ctx, "hooks/list", map[string]any{"threadId": t.ID})
	if err != nil {
		var rpc *daemonResponseError
		var detail struct {
			Code    int
			Message string
		}
		if !errors.As(err, &rpc) || json.Unmarshal([]byte(rpc.payload), &detail) != nil ||
			(detail.Code != -32600 && detail.Code != -32602) ||
			!strings.Contains(strings.ToLower(detail.Message), "unknown field") ||
			!strings.Contains(strings.ToLower(detail.Message), "thread") {
			return nil, err
		}
	} else {
		// A successful empty inventory is a valid thread result, not evidence
		// of an unsupported selector. Never replace it with directory hooks.
		if len(rows) == 0 {
			return rows, nil
		}
		for _, raw := range rows {
			var entry struct{ Cwd string }
			if json.Unmarshal(raw, &entry) != nil || entry.Cwd == "" {
				return nil, ErrSessionCommand
			}
			if entry.Cwd == t.Cwd {
				return rows, nil
			}
		}
	}
	return p.commandRows(ctx, "hooks/list", map[string]any{"cwds": []string{t.Cwd}})
}

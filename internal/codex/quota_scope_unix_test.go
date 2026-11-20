//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestQuotaAccountPreflightWithoutRouting(t *testing.T) {
	const workspace = "workspace-from-quota"
	account := fingerprintAccount(json.RawMessage(scheduledPromptAccount))
	legacy := map[string]any{"rateLimits": map[string]any{"limitId": "codex"}}
	modern := map[string]any{"accountId": workspace}
	for _, tc := range []struct {
		name, expected string
		limits         map[string]any
		want           error
	}{
		{"new workspace on quota response", account, modern, ErrScheduledQuotaUnverified},
		{"scoped workspace matches", quotaAccount(account, workspace), modern, nil},
		{"scoped workspace changed", quotaAccount(account, "other"), modern, errQuotaAccountChanged},
		{"scoped workspace not reported", quotaAccount(account, workspace), legacy, ErrScheduledQuotaUnverified},
		{"legacy quota response", account, legacy, nil},
		{"invalid quota response", account, map[string]any{}, ErrScheduledQuotaUnverified},
		{"quota read failed", account, nil, ErrScheduledQuotaUnverified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			f.mu.Lock()
			f.catalogues = map[string]map[string]any{
				"account/read": {"account": map[string]any{"type": "chatgpt", "email": "test@example.test"}},
			}
			if tc.limits != nil {
				f.catalogues["account/rateLimits/read"] = tc.limits
			}
			f.mu.Unlock()
			ctx := context.Background()
			if err := p.ensureConnected(ctx); err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			conn := p.connection
			p.mu.Unlock()
			if err := p.verifyQuotaAccount(ctx, conn, tc.expected); !errors.Is(err, tc.want) {
				t.Fatalf("preflight error = %v, want %v", err, tc.want)
			}
		})
	}
}

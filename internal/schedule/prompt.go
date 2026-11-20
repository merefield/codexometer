package schedule

import (
	"context"
	"errors"

	"github.com/merefield/codexometer/internal/codex"
)

// SendPrompt binds scheduled sends to the workspace in the active connection.
// The fallback supports older adapters that have only an account-level scope.
func SendPrompt(ctx context.Context, client codex.SessionPromptClient, token string, j Job) error {
	if scoped, ok := client.(codex.ScheduledPromptClient); ok {
		err := scoped.SendScheduledPrompt(ctx, token, j.Text, j.account)
		if errors.Is(err, codex.ErrScheduledQuotaUnverified) {
			return ErrDeferred
		}
		return err
	}
	if codex.IsWorkspaceQuotaAccount(j.account) {
		return ErrDeferred
	}
	return client.SendSessionPrompt(ctx, token, []string{j.Text})
}

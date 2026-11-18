package web

import "github.com/merefield/codexometer/internal/codex"

func projectPendingModel(pending codex.PendingModelChange) *codex.PendingModelChange {
	if pending.Summary == "" {
		return nil
	}
	return &codex.PendingModelChange{Summary: codex.SanitizeSessionContext(pending.Summary), State: codex.SanitizeSessionContext(pending.State)}
}

package codex

// PendingModelChange is display-only, memory-only and shared by terminal/web.
// It is separate from Codex's prompt queue: it never sends a user message.
type PendingModelChange struct {
	Summary string `json:"summary,omitempty"`
	State   string `json:"state,omitempty"`
}

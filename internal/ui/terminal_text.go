package ui

import (
	"github.com/merefield/codexometer/internal/codex"
	"strings"
)

// Sanitize display copies only: session IDs, filesystem paths and approval
// payloads must remain unchanged for routing and execution.
func terminalLabel(text string) string {
	return strings.ReplaceAll(codex.SanitizeSessionContext(text), "\n", " ")
}

package web

import (
	"strings"
	"testing"

	"github.com/merefield/codexometer/internal/codex"
)

func TestPendingModelProjectionIsOptionalAndSanitized(t *testing.T) {
	if projectPendingModel(codex.PendingModelChange{}) != nil {
		t.Fatal("empty queue was published")
	}
	pending := projectPendingModel(codex.PendingModelChange{Summary: "small\x1b[31m // medium", State: "queued\x00"})
	if pending == nil || strings.ContainsAny(pending.Summary+pending.State, "\x1b\x00") || pending.State != "queued" {
		t.Fatal("unsafe queued model display", pending)
	}
}

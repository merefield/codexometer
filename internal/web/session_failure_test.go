package web

import (
	"github.com/merefield/codexometer/internal/codex"
	"testing"
)

func TestSessionFailurePresentation(t *testing.T) {
	row := codex.LiveUsageSession{Attention: codex.SessionAttentionError, Context: codex.SessionContext{Kind: codex.SessionContextFailure, FailureCode: "serverOverloaded"}}
	if sessionStatus(row) != "MODEL BUSY" || contextKind(row.Context.Kind) != "TURN ERROR" {
		t.Fatal("capacity failure not exposed")
	}
	row.Context.FailureCode = "httpConnectionFailed"
	if sessionStatus(row) != "TURN FAILED" {
		t.Fatal("generic failure mislabeled")
	}
}

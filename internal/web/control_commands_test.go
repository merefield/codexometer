package web

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

type commandsFake struct {
	calls    int
	revision string
}

func (f *commandsFake) SessionCommands(context.Context, string, string) (codex.SessionCommandMenu, error) {
	return codex.SessionCommandMenu{Revision: f.revision, Choices: []codex.SessionCommandChoice{{ID: "choice", Label: "Medium", Help: "Dynamic help", Action: true}}}, nil
}
func (f *commandsFake) ExecuteSessionCommand(_ context.Context, id, path, revision, choice string) error {
	if id != "parent" || revision != f.revision || choice != "choice" {
		return codex.ErrSessionCommand
	}
	f.calls++
	return nil
}
func TestCommandConfirmationIsBoundAndSingleUse(t *testing.T) {
	s, _, token := controlServer(t)
	f := &commandsFake{revision: "current"}
	s.control.commands = f
	b := actionRequest{Session: "parent", Command: &commandRequest{Mode: "prepare", Path: "model/test", Revision: "current", Choice: "choice"}}
	w := actionCall(s, token, "commands", b)
	var response struct{ Confirmation string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Confirmation == "" {
		t.Fatalf("prepare %d %s", w.Code, w.Body)
	}
	if f.calls != 0 {
		t.Fatal("prepare mutated settings")
	}
	wrong := actionRequest{Session: "parent", Confirmation: response.Confirmation}
	if w = actionCall(s, token, "commit", wrong); w.Code != 409 {
		t.Fatalf("normal route accepted command: %d", w.Code)
	}
	b.Confirmation = response.Confirmation
	b.Command = &commandRequest{Mode: "commit"}
	if w = actionCall(s, token, "commands", b); w.Code != 200 || f.calls != 1 {
		t.Fatalf("commit %d %s calls %d", w.Code, w.Body, f.calls)
	}
	if w = actionCall(s, token, "commands", b); w.Code != 409 || f.calls != 1 {
		t.Fatal("replayed command")
	}
}

func TestCommandsRejectStaleAndMixedRequests(t *testing.T) {
	s, _, token := controlServer(t)
	f := &commandsFake{revision: "new"}
	s.control.commands = f
	b := actionRequest{Session: "parent", Command: &commandRequest{Mode: "prepare", Revision: "old", Choice: "choice"}}
	if w := actionCall(s, token, "commands", b); w.Code != 409 {
		t.Fatal("stale options accepted")
	}
	b.Command.Mode = "list"
	b.Answers = []string{"prompt"}
	if w := actionCall(s, token, "commands", b); w.Code != 400 {
		t.Fatal("mixed payload accepted")
	}
	if w := actionCall(s, token, "schedules", b); w.Code != 400 {
		t.Fatal("commands accepted on schedule route")
	}
	b.Answers = nil
	s.store.mu.Lock()
	s.store.state.SessionsAt = time.Now().Add(-time.Minute)
	s.store.mu.Unlock()
	if w := actionCall(s, token, "commands", b); w.Code != 409 {
		t.Fatal("stale sessions accepted")
	}
	if f.calls != 0 {
		t.Fatal("invalid request mutated settings")
	}
}

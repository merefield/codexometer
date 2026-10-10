package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

type commandsFake struct {
	calls                     int
	lists                     int
	listedPath, committedPath string
	revision                  string
	picker                    bool
}

func (f *commandsFake) SessionCommands(_ context.Context, _ string, path string) (codex.SessionCommandMenu, error) {
	f.lists++
	f.listedPath = path
	return codex.SessionCommandMenu{Revision: f.revision, Picker: f.picker, Choices: []codex.SessionCommandChoice{{ID: "choice", Label: "Medium", Help: "Dynamic help", Action: true}}}, nil
}
func (f *commandsFake) ExecuteSessionCommand(_ context.Context, id, path, revision, choice string) error {
	if id != "parent" || revision != f.revision || choice != "choice" {
		return codex.ErrSessionCommand
	}
	f.calls++
	f.committedPath = path
	return nil
}
func TestCommandConfirmationIsBoundAndSingleUse(t *testing.T) {
	s, _, token := controlServer(t)
	f := &commandsFake{revision: "current"}
	s.control.commands = f
	b := actionRequest{Session: "parent", Command: &commandRequest{Mode: "prepare", Path: "rename/new-name", Revision: "current", Choice: "choice"}}
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
	var result struct{ Message string }
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Message != "Session renamed." {
		t.Fatal("confirmed rename reported as still pending", w.Body)
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

func TestDirectoryCommandReportsVerifiedSuccess(t *testing.T) {
	s, _, token := controlServer(t)
	f := &commandsFake{revision: "current"}
	s.control.commands = f
	b := actionRequest{Session: "parent", Command: &commandRequest{Mode: "prepare", Path: "cd/new", Revision: "current", Choice: "choice"}}
	w := actionCall(s, token, "commands", b)
	var response struct{ Confirmation string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal(w.Body)
	}
	b.Confirmation = response.Confirmation
	b.Command = &commandRequest{Mode: "commit"}
	w = actionCall(s, token, "commands", b)
	var result struct{ Message string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Message != "Working directory changed." || f.calls != 1 {
		t.Fatal(w.Body)
	}
}

func TestCommandsLongEncodedInputConfirmation(t *testing.T) {
	for _, path := range []string{
		"rename/" + url.PathEscape(strings.Repeat("界", codex.SessionRenameInputLimit)),
		"rename/" + url.PathEscape(strings.Repeat("😀", codex.SessionRenameInputLimit)),
		"cd/" + url.PathEscape(strings.Repeat(" ", codex.SessionDirectoryInputLimit)),
		"cd/" + url.PathEscape(strings.Repeat("😀", codex.SessionDirectoryInputLimit)),
	} {
		t.Run(path[:strings.IndexByte(path, '/')]+fmt.Sprint(len(path)), func(t *testing.T) {
			s, _, token := controlServer(t)
			f := &commandsFake{revision: "current"}
			s.control.commands = f
			b := actionRequest{Session: "parent", Command: &commandRequest{Mode: "list", Path: path}}
			if w := actionCall(s, token, "commands", b); w.Code != 200 {
				t.Fatalf("list %d %s", w.Code, w.Body)
			}
			b.Command.Mode, b.Command.Revision, b.Command.Choice = "prepare", "current", "choice"
			w := actionCall(s, token, "commands", b)
			var response struct{ Confirmation string }
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Confirmation == "" || f.calls != 0 || f.listedPath != path {
				t.Fatalf("prepare %d %s", w.Code, w.Body)
			}
			b.Confirmation, b.Command = response.Confirmation, &commandRequest{Mode: "commit"}
			if w := actionCall(s, token, "commands", b); w.Code != 200 || f.calls != 1 || f.committedPath != path {
				t.Fatalf("commit %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestCommandsRejectInvalidDecodedInputBeforeAdapter(t *testing.T) {
	for _, path := range []string{
		"rename/" + url.PathEscape(strings.Repeat("界", codex.SessionRenameInputLimit+1)),
		"cd/" + url.PathEscape(strings.Repeat("界", codex.SessionDirectoryInputLimit+1)),
		"rename/%zz", "cd/%FF", "model/" + strings.Repeat("x", 2043),
	} {
		s, _, token := controlServer(t)
		f := &commandsFake{revision: "current"}
		s.control.commands = f
		b := actionRequest{Session: "parent", Command: &commandRequest{Mode: "list", Path: path}}
		if w := actionCall(s, token, "commands", b); w.Code != 400 || f.lists != 0 || f.calls != 0 {
			t.Fatalf("invalid path accepted: status %d lists %d calls %d", w.Code, f.lists, f.calls)
		}
	}
}

func TestSpeedPickerUsesBoundCommitAndReportsVerifiedSuccess(t *testing.T) {
	s, _, token := controlServer(t)
	f := &commandsFake{revision: "current", picker: true}
	s.control.commands = f
	b := actionRequest{Session: "parent", Command: &commandRequest{Mode: "prepare", Path: "fast", Revision: "current", Choice: "choice"}}
	w := actionCall(s, token, "commands", b)
	var prepared struct{ Confirmation string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &prepared) != nil || prepared.Confirmation == "" || f.calls != 0 {
		t.Fatal("prepare sent or lost the highlighted speed", w.Body)
	}
	b.Confirmation = prepared.Confirmation
	b.Command = &commandRequest{Mode: "commit"}
	w = actionCall(s, token, "commands", b)
	var result struct{ Message string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Message != "Session speed changed. Applies to subsequent turns." || f.calls != 1 {
		t.Fatal("speed commit missing or success still pending", w.Body)
	}
	if w = actionCall(s, token, "commands", b); w.Code != 409 || f.calls != 1 {
		t.Fatal("speed token could be replayed")
	}
}

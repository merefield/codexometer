package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionActivityParallelCommands(t *testing.T) {
	s := sessionActivityState{prose: "Checking the build"}
	s.command("a", "go test ./...", "inProgress", false)
	s.command("b", "go vet ./...", "inProgress", false)
	if a := s.snapshot(); a.Prose != "Checking the build" || a.Command != "go vet ./..." || a.RunningCommands != 2 {
		t.Fatal(a)
	}
	s.command("b", "", "completed", true)
	if a := s.snapshot(); a.Command != "go test ./..." || a.CommandStatus != "running" || a.RunningCommands != 1 {
		t.Fatal(a)
	}
	s.command("a", "", "failed", true)
	if a := s.snapshot(); a.CommandStatus != "failed" || a.RunningCommands != 0 || a.Prose != "Checking the build" {
		t.Fatal(a)
	}
	for _, status := range []string{"completed", "failed", "declined", "unknown"} {
		s.command("c", "echo done", status, true)
		if s.snapshot().CommandStatus != status {
			t.Fatal(s.snapshot())
		}
	}
}

func TestSessionActivityBoundedAndSanitized(t *testing.T) {
	var s sessionActivityState
	s.command("missing-status", "echo unknown", "", false)
	if a := s.snapshot(); a.CommandStatus != "unknown" || a.RunningCommands != 0 {
		t.Fatal(a)
	}
	for i := range 100 {
		s.command(fmt.Sprint(i), "echo \x1b[31mhello\x1b[0m", "inProgress", false)
	}
	if a := s.snapshot(); a.RunningCommands != 64 || !a.RunningLimited || a.Command != "echo hello" {
		t.Fatal(a)
	}
	s.command("0", "echo hello", "inProgress", false)
	if s.snapshot().RunningCommands != 64 {
		t.Fatal("replay duplicated a command")
	}
}

func TestDaemonWorkingContextLifecycle(t *testing.T) {
	states := map[string]*daemonContextState{}
	emit := func(method, id, body string) {
		daemonContextEvent(states, method, json.RawMessage(id), json.RawMessage(body), time.Now())
	}
	emit("item/completed", "", `{"threadId":"root","turnId":"t","item":{"type":"agentMessage","text":"First explanation","phase":"commentary"}}`)
	emit("item/started", "", `{"threadId":"root","turnId":"t","item":{"id":"a","type":"commandExecution","command":"go test ./...","status":"inProgress"}}`)
	if a := states["root"].latest.Activity; a.Prose != "First explanation" || a.Command != "go test ./..." || a.CommandStatus != "running" {
		t.Fatal(a)
	}
	emit("item/completed", "", `{"threadId":"root","turnId":"t","item":{"type":"agentMessage","text":"Updated explanation","phase":"commentary"}}`)
	emit("item/commandExecution/requestApproval", "1", `{"threadId":"root","turnId":"t","itemId":"a","reason":"Allow?","command":"go test ./..."}`)
	if c := daemonContextSnapshot(states, []string{"root"}, map[string]sessionRuntimeStatus{"root": sessionRuntimeApproval})["root"]; c.Kind != SessionContextApproval || c.Activity.Command != "" {
		t.Fatal(c)
	}
	emit("serverRequest/resolved", "", `{"threadId":"root","requestId":1}`)
	emit("item/completed", "", `{"threadId":"root","turnId":"t","item":{"id":"a","type":"commandExecution","status":"completed","exitCode":1}}`)
	if a := states["root"].latest.Activity; a.Prose != "Updated explanation" || a.CommandStatus != "failed" || a.RunningCommands != 0 {
		t.Fatal(a)
	}
	emit("item/completed", "", `{"threadId":"root","turnId":"t","item":{"type":"agentMessage","text":"Finished","phase":"final_answer"}}`)
	emit("item/completed", "", `{"threadId":"root","turnId":"t","item":{"id":"a","type":"commandExecution","command":"go test ./...","status":"completed"}}`)
	if c := states["root"].latest; c.Kind != SessionContextReply || c.Text != "Finished" || c.Activity != (SessionActivity{}) {
		t.Fatal(c)
	}
	emit("turn/started", "", `{"threadId":"root"}`)
	emit("item/started", "", `{"threadId":"root","item":{"id":"b","type":"commandExecution","command":"pwd","status":"inProgress"}}`)
	if a := states["root"].latest.Activity; a.Prose != "" || a.Command != "pwd" {
		t.Fatal(a)
	}
	emit("turn/interrupted", "", `{"threadId":"root"}`)
	if states["root"].latest.Activity != (SessionActivity{}) {
		t.Fatal("interrupted turn retains running command")
	}
}

func TestLocalWorkingContextLifecycle(t *testing.T) {
	cursor := &rolloutCursor{threadID: "root"}
	emit := func(line string) {
		t.Helper()
		c, ok := rolloutContextRecord([]byte(line), cursor)
		if !ok {
			t.Fatal("record ignored", line)
		}
		cursor.preview = c
	}
	emit(`{"type":"event_msg","payload":{"type":"agent_message","message":"Checking"}}`)
	emit(`{"type":"event_msg","payload":{"type":"exec_command_begin","call_id":"a","command":"go test"}}`)
	emit(`{"type":"event_msg","payload":{"type":"exec_command_end","call_id":"a","exit_code":0}}`)
	if a := cursor.preview.Activity; a.Prose != "Checking" || a.Command != "go test" || a.CommandStatus != "completed" {
		t.Fatal(a)
	}
	emit(`{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"Done"}}`)
	emit(`{"type":"event_msg","payload":{"type":"exec_command_end","call_id":"a","command":"go test","exit_code":0}}`)
	if cursor.preview.Kind != SessionContextReply || cursor.preview.Activity != (SessionActivity{}) {
		t.Fatal(cursor.preview)
	}
	emit(`{"type":"event_msg","payload":{"type":"task_started"}}`)
	emit(`{"type":"event_msg","payload":{"type":"exec_command_begin","call_id":"b","command":"pwd"}}`)
	if cursor.preview.Activity.Prose != "" {
		t.Fatal("prose leaked across turns")
	}
}

func TestLocalWorkingContextBootstrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := "{\"type\":\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":\"Checking\"}}\n" +
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"exec_command_begin\",\"call_id\":\"a\",\"command\":\"go test\"}}\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cursor := &rolloutCursor{threadID: "root", preview: SessionContext{Kind: SessionContextApproval, Text: "old request"}}
	cursor.preview = latestSessionContext(path, cursor)
	if a := cursor.preview.Activity; a.Prose != "Checking" || a.Command != "go test" {
		t.Fatal(a)
	}
	c, ok := rolloutContextRecord([]byte(`{"type":"event_msg","payload":{"type":"exec_command_end","call_id":"a","exit_code":0}}`), cursor)
	if !ok || c.Activity.Command != "go test" || c.Activity.CommandStatus != "completed" {
		t.Fatal(c)
	}
}

func TestDaemonCommandStatusFallbacks(t *testing.T) {
	for _, startStatus := range []string{"", "unknown", "inProgress"} {
		for _, finish := range []struct{ fields, want string }{
			{`"exitCode":0`, "completed"},
			{`"exitCode":2`, "failed"},
			{`"status":"declined","exitCode":0`, "declined"},
			{`"status":"failed","exitCode":0`, "failed"},
			{`"status":"completed"`, "completed"},
			{`"status":"unknown"`, "unknown"},
		} {
			states := map[string]*daemonContextState{}
			start := fmt.Sprintf(`{"threadId":"root","item":{"type":"commandExecution","id":"a","command":"pwd","status":%q}}`, startStatus)
			daemonContextEvent(states, "item/started", nil, json.RawMessage(start), time.Now())
			if a := states["root"].latest.Activity; a.CommandStatus != "running" || a.RunningCommands != 1 {
				t.Fatal(a)
			}
			end := `{"threadId":"root","item":{"type":"commandExecution","id":"a",` + finish.fields + `}}`
			daemonContextEvent(states, "item/completed", nil, json.RawMessage(end), time.Now())
			if a := states["root"].latest.Activity; a.CommandStatus != finish.want || a.RunningCommands != 0 || a.Command != "pwd" {
				t.Fatal(a, finish)
			}
		}
	}
}

func TestDaemonTurnEndDropsCommandOnlyText(t *testing.T) {
	for _, method := range []string{"turn/completed", "turn/interrupted"} {
		for _, prose := range []string{"", "Checking"} {
			states := map[string]*daemonContextState{}
			emit := func(method, body string) { daemonContextEvent(states, method, nil, json.RawMessage(body), time.Now()) }
			if prose != "" {
				emit("item/completed", `{"threadId":"root","item":{"type":"agentMessage","phase":"commentary","text":"Checking"}}`)
			}
			emit("item/started", `{"threadId":"root","item":{"type":"commandExecution","id":"a","command":"pwd"}}`)
			emit(method, `{"threadId":"root","turn":{"status":"completed"}}`)
			if c := states["root"].latest; c.Text != prose || c.Activity != (SessionActivity{}) {
				t.Fatal(c)
			}
		}
	}
}

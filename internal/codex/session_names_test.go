package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionNamesFollowRenamesAndIndexReplacement(t *testing.T) {
	home := t.TempDir()
	r := &LiveUsageReader{SessionsRoot: filepath.Join(home, "sessions")}
	path := filepath.Join(home, "session_index.jsonl")
	write := func(data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		r.refreshSessionNames()
	}
	write("{\"id\":\"one\",\"thread_name\":\"First name\"}\n{\"id\":\"child\",\"thread_name\":\"Agent name\"}\n")
	if r.sessionNames["one"] != "First name" {
		t.Fatal(r.sessionNames)
	}
	write("{\"id\":\"one\",\"thread_name\":\"First name\"}\ninvalid\n{\"id\":\"one\",\"thread_name\":\"New name\\nline\"}\n")
	if r.sessionNames["one"] != "New name line" {
		t.Fatal(r.sessionNames)
	}
	write("{\"id\":\"one\",\"thread_name\":\"Final\"}\n")
	if r.sessionNames["one"] != "Final" || r.sessionNames["child"] != "" {
		t.Fatal(r.sessionNames)
	}
}

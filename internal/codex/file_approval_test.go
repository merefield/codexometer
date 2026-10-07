package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testFilePatch = `[{"path":"/work/a.go","kind":{"type":"update","move_path":null},"diff":"@@ -3,2 +3,2 @@\n-old\n+new\n context\n"}]`

func fileApprovalFixture(t *testing.T, patch string, overrides map[string]any) (map[string]*daemonContextState, SessionContext) {
	t.Helper()
	states := map[string]*daemonContextState{}
	daemonContextEvent(states, "item/started", nil, json.RawMessage(`{"threadId":"root","turnId":"turn","item":{"type":"fileChange","id":"patch","changes":`+patch+`}}`), time.Now())
	p := map[string]any{"threadId": "root", "turnId": "turn", "itemId": "patch", "reason": "Update the implementation"}
	for k, v := range overrides {
		p[k] = v
	}
	raw, _ := json.Marshal(p)
	daemonContextEvent(states, "item/fileChange/requestApproval", json.RawMessage(`"req"`), raw, time.Now())
	return states, states["root"].requests[`"req"`]
}

func TestFileApprovalCorrelationAndSafety(t *testing.T) {
	_, c := fileApprovalFixture(t, testFilePatch, nil)
	if c.ApprovalToken == "" || c.FileChanges == "" || c.ApprovalBlocked != "" {
		t.Fatalf("blocked complete patch: %+v", c)
	}
	for i, want := range []string{"accept", "decline", "cancel"} {
		if c.ApprovalOptions[i].Wire != `"`+want+`"` {
			t.Fatal(c.ApprovalOptions)
		}
	}
	for name, p := range map[string]string{
		"missing": "null", "empty": "[]", "invalid": "{}",
		"no diff":       `[{"path":"a","kind":{"type":"add"}}]`,
		"null diff":     `[{"path":"a","kind":{"type":"add"},"diff":null}]`,
		"unknown kind":  strings.Replace(testFilePatch, `"update"`, `"future"`, 1),
		"unknown field": strings.Replace(testFilePatch, `"path":`, `"permissions":true,"path":`, 1),
		"escape":        strings.Replace(testFilePatch, "old", `\u001b[31mold`, 1),
		"bidi":          strings.Replace(testFilePatch, "old", `\u202eold`, 1),
		"large":         strings.Replace(testFilePatch, "old", strings.Repeat("x", fileApprovalLimit), 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, c := fileApprovalFixture(t, p, nil)
			if c.ApprovalToken != "" || c.FileChanges != "" {
				t.Fatal("unsafe patch actionable", c)
			}
		})
	}
	for _, fields := range []map[string]any{{"turnId": "other"}, {"itemId": "other"}, {"grantRoot": "/work"}, {"reason": "hidden\x1b[31m"}} {
		_, c := fileApprovalFixture(t, testFilePatch, fields)
		if c.ApprovalToken != "" {
			t.Fatal("unsafe request actionable", c)
		}
	}
}

func TestFileApprovalPatchChangeAndLifecycle(t *testing.T) {
	states, c := fileApprovalFixture(t, testFilePatch, nil)
	changed := strings.Replace(testFilePatch, "new", "replacement", 1)
	daemonContextEvent(states, "item/fileChange/patchUpdated", nil, json.RawMessage(`{"threadId":"root","turnId":"turn","itemId":"patch","changes":`+changed+`}`), time.Now())
	if got := states["root"].requests[`"req"`]; got.ApprovalToken != "" || got.FileChanges == c.FileChanges {
		t.Fatal("old grant survived patch change", got)
	}
	for _, method := range []string{"turn/started", "turn/completed", "turn/interrupted", "item/completed", "thread/closed"} {
		states, _ := fileApprovalFixture(t, testFilePatch, nil)
		daemonContextEvent(states, method, nil, json.RawMessage(`{"threadId":"root","turnId":"turn","turn":{"id":"next"},"item":{"id":"patch","type":"fileChange"}}`), time.Now())
		if s := states["root"]; s != nil && (len(s.requests) != 0 || len(s.patches) != 0) {
			t.Fatal(method, "retained patch")
		}
	}
}

func TestFileDiffNumbering(t *testing.T) {
	lines := FileDiffLines(testFilePatch)
	if len(lines) != 5 || lines[2].Old != 3 || lines[2].Kind != "removal" || lines[3].New != 3 || lines[3].Kind != "addition" || lines[4].Old != 4 || lines[4].New != 4 {
		t.Fatalf("bad numbers: %+v", lines)
	}
	for _, kind := range []string{"add", "delete"} {
		lines := FileDiffLines(`[{"path":"a","kind":{"type":"` + kind + `"},"diff":"+literal\n\tindented\n"}]`)
		if len(lines) != 3 || !strings.Contains(lines[1].Text, "+literal") || !strings.Contains(lines[2].Text, "    indented") {
			t.Fatal(lines)
		}
		if kind == "add" && lines[2].New != 2 || kind == "delete" && lines[2].Old != 2 {
			t.Fatal(lines)
		}
	}
}

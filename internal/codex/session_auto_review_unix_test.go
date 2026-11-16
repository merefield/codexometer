//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func reviewNotification(id, status string, at int64) json.RawMessage {
	method := map[string]any{"threadId": "one", "turnId": "turn", "reviewId": id, "startedAtMs": at, "review": map[string]any{"status": status, "riskLevel": "high", "userAuthorization": "low", "rationale": "Writes outside the project."}, "action": map[string]any{"type": "command", "source": "unifiedExec", "command": "touch /outside/result", "cwd": "/work"}}
	if status != "inProgress" {
		method["completedAtMs"] = at + 1
		method["decisionSource"] = "agent"
	}
	data, _ := json.Marshal(method)
	return data
}

func TestNativeAutoReviewLifecycle(t *testing.T) {
	p := &daemonStatusProvider{}
	p.autoReviewNotificationLocked("item/autoApprovalReview/started", reviewNotification("first", "inProgress", 1))
	if !p.autoReviews["one"].latest.view.Active() {
		t.Fatal("review not active")
	}
	p.autoReviewNotificationLocked("item/autoApprovalReview/completed", reviewNotification("first", "denied", 1))
	p.autoReviewNotificationLocked("item/autoApprovalReview/started", reviewNotification("first", "inProgress", 1))
	if p.autoReviews["one"].latest.view.Status != "denied" || len(p.autoReviews["one"].denials) != 1 {
		t.Fatal("late start revived completed review")
	}
	p.autoReviewNotificationLocked("item/autoApprovalReview/started", reviewNotification("new", "inProgress", 100))
	p.autoReviewNotificationLocked("item/autoApprovalReview/completed", reviewNotification("old", "denied", 2))
	if p.autoReviews["one"].latest.view.ID != "new" {
		t.Fatal("old completion replaced new active review")
	}
	for i := 3; i < 18; i++ {
		p.autoReviewNotificationLocked("item/autoApprovalReview/completed", reviewNotification(fmt.Sprint(i), "denied", int64(i)))
	}
	if len(p.autoReviews["one"].denials) != autoReviewLimit {
		t.Fatal("unbounded denial inventory")
	}
	p.autoReviewNotificationLocked("thread/closed", json.RawMessage(`{"threadId":"one"}`))
	if len(p.autoReviews) != 0 {
		t.Fatal("closed session retained reviews")
	}
}

func TestNativeConcurrentReviewsRemainActive(t *testing.T) {
	p := &daemonStatusProvider{}
	p.autoReviewNotificationLocked("item/autoApprovalReview/started", reviewNotification("first", "inProgress", 1))
	p.autoReviewNotificationLocked("item/autoApprovalReview/started", reviewNotification("second", "inProgress", 2))
	p.autoReviewNotificationLocked("item/autoApprovalReview/completed", reviewNotification("second", "approved", 2))
	if r := p.autoReviews["one"].display(); r.ID != "first" || !r.Active() {
		t.Fatalf("completed newer review hid running assessment: %+v", r)
	}
	p.autoReviewNotificationLocked("item/autoApprovalReview/completed", reviewNotification("first", "denied", 1))
	if p.autoReviews["one"].display().Active() {
		t.Fatal("completed reviews still active")
	}
}

func TestNativeDenialReviewNeverUsesClippedTelemetry(t *testing.T) {
	p := &daemonStatusProvider{}
	var n map[string]any
	_ = json.Unmarshal(reviewNotification("long", "denied", 1), &n)
	rationale := strings.Repeat("Important context. ", 400) + "END OF COMPLETE RATIONALE"
	n["review"].(map[string]any)["rationale"] = rationale
	data, _ := json.Marshal(n)
	p.autoReviewNotificationLocked("item/autoApprovalReview/completed", data)
	menu, err := p.autoReviewMenu(commandThread{ID: "one"}, "approve/long")
	menu.Path = "approve/long"
	menu = normalizeCommandMenu(menu)
	if err != nil || len(menu.Choices) != 1 || !menu.Choices[0].Action || !strings.Contains(menu.Choices[0].Help, rationale) {
		t.Fatalf("full retry review lost text: %+v %v", menu, err)
	}
	if len(p.autoReviews["one"].latest.view.Rationale) >= len(rationale) {
		t.Fatal("ordinary telemetry no longer bounded")
	}
}

func TestNativeAutoReviewExactRetryAndReplay(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			ctx := context.Background()
			if err := p.ensureConnected(ctx); err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			p.autoReviewNotificationLocked("item/autoApprovalReview/completed", reviewNotification("denial", "denied", 1))
			p.mu.Unlock()
			menu, err := p.SessionCommands(ctx, "one", "approve/denial")
			if err != nil || len(menu.Choices) != 1 || !menu.Choices[0].Action {
				t.Fatalf("menu: %+v %v", menu, err)
			}
			if menu.Choices[0].Reversible {
				t.Fatal("retry consent treated as reversible setting")
			}
			f.mu.Lock()
			if fail {
				f.fail = "approve"
			}
			f.mu.Unlock()
			err = p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[0].ID)
			if (err != nil) != fail {
				t.Fatalf("write: %v", err)
			}
			p.mu.Lock()
			p.autoReviewNotificationLocked("item/autoApprovalReview/completed", reviewNotification("denial", "denied", 1))
			p.mu.Unlock()
			if err := p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[0].ID); err == nil {
				t.Fatal("replayed spent consent")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.denialWrites) != 1 || len(f.writes) != 0 {
				t.Fatalf("wrong mutations: %+v %+v", f.denialWrites, f.writes)
			}
			data, _ := json.Marshal(f.denialWrites[0]["event"])
			var event map[string]any
			_ = json.Unmarshal(data, &event)
			if event["id"] != "denial" || event["turn_id"] != "turn" || event["status"] != "denied" || event["decision_source"] != "agent" {
				t.Fatalf("not a core GuardianAssessmentEvent: %s", data)
			}
			a := event["action"].(map[string]any)
			if a["source"] != "unified_exec" || a["command"] != "touch /outside/result" {
				t.Fatalf("action changed: %s", data)
			}
		})
	}
}

func TestNativeReviewActionVariants(t *testing.T) {
	for _, tc := range []struct{ api, core, field, value string }{
		{`{"type":"command","source":"shell","command":"ls","cwd":"/work"}`, "command", "command", "ls"},
		{`{"type":"execve","source":"unifiedExec","program":"/bin/ls","argv":["ls"],"cwd":"/work"}`, "execve", "source", "unified_exec"},
		{`{"type":"writeStdin","approvalId":"a","processId":"p","stdin":"yes\n","cwd":"/work"}`, "write_stdin", "cwd", "file:///work"},
		{`{"type":"applyPatch","cwd":"/work","files":["/work/a"]}`, "apply_patch", "cwd", "/work"},
		{`{"type":"networkAccess","target":"url","host":"example.test","protocol":"socks5Tcp","port":80}`, "network_access", "protocol", "socks5_tcp"},
		{`{"type":"mcpToolCall","server":"s","toolName":"tool","connectorId":null}`, "mcp_tool_call", "tool_name", "tool"},
		{`{"type":"requestPermissions","reason":"build","permissions":{"network":{"enabled":true},"fileSystem":{"read":["/work"],"write":["/tmp"]}}}`, "request_permissions", "reason", "build"},
	} {
		t.Run(tc.core, func(t *testing.T) {
			a, err := nativeReviewAction(json.RawMessage(tc.api))
			if err != nil || a["type"] != tc.core || a[tc.field] != tc.value {
				t.Fatalf("conversion %+v %v", a, err)
			}
		})
	}
	for _, raw := range []string{`{"type":"future"}`, `{"type":"command","futurePermission":true}`, `{"type":"writeStdin","cwd":"relative"}`, `{"type":"requestPermissions","permissions":{"future":true}}`} {
		if _, err := nativeReviewAction(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted unknown action: %s", raw)
		}
	}
}

func configureNativePermissions(f *quotaDaemonFixture) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.permissions = map[string]string{"one": ":workspace"}
	f.reviewers = map[string]string{"one": "user"}
	f.approvalPolicies = map[string]any{"one": "on-request"}
	f.catalogues = map[string]map[string]any{
		"permissionProfile/list":   {"data": []any{map[string]any{"id": ":workspace", "description": "Write inside the project.", "allowed": true}}},
		"configRequirements/read":  {"requirements": nil},
		"experimentalFeature/list": {"data": []any{map[string]any{"name": "guardian_approval", "enabled": true}}},
	}
}

func TestNativePermissionsInlineAndVerification(t *testing.T) {
	p, f := newQuotaDaemon(t)
	configureNativePermissions(f)
	ctx := context.Background()
	menu, err := p.SessionCommands(ctx, "one", `permissions :workspace --approval-policy=on-request --reviewer auto_review`)
	if err != nil || len(menu.Choices) != 1 || !menu.Choices[0].Action {
		t.Fatalf("menu %+v %v", menu, err)
	}
	if err := p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[0].ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.writes) != 1 || f.writes[0]["approvalsReviewer"] != "auto_review" || f.writes[0]["approvalPolicy"] != "on-request" || f.writes[0]["permissions"] != ":workspace" {
		t.Fatalf("wrong native settings: %+v", f.writes)
	}
	f.ignorePermissions = true
	f.mu.Unlock()
	menu, err = p.SessionCommands(ctx, "one", "permissions/reviewer")
	if err != nil {
		t.Fatal(err)
	}
	limited, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if err := p.ExecuteSessionCommand(limited, "one", menu.Path, menu.Revision, menu.Choices[0].ID); err == nil || !strings.Contains(err.Error(), "unconfirmed") {
		t.Fatalf("ignored reviewer reported success: %v", err)
	}
}

func TestNativePermissionsManagedAndStale(t *testing.T) {
	p, f := newQuotaDaemon(t)
	configureNativePermissions(f)
	ctx := context.Background()
	menu, err := p.SessionCommands(ctx, "one", "permissions/reviewer")
	if err != nil || len(menu.Choices) != 2 {
		t.Fatalf("reviewers %+v %v", menu, err)
	}
	f.mu.Lock()
	f.reviewers["one"] = "auto_review"
	f.mu.Unlock()
	if err := p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[0].ID); err == nil {
		t.Fatal("accepted stale reviewed settings")
	}
	f.mu.Lock()
	f.catalogues["configRequirements/read"] = map[string]any{"requirements": map[string]any{"allowedApprovalsReviewers": []string{}}}
	f.mu.Unlock()
	menu, err = p.SessionCommands(ctx, "one", "permissions/reviewer")
	if err != nil || len(menu.Choices) != 0 {
		t.Fatalf("empty allowlist became unrestricted: %+v %v", menu, err)
	}
	if _, err = p.SessionCommands(ctx, "one", `permissions :workspace --reviewer=user`); err == nil {
		t.Fatal("inline bypassed managed reviewer requirements")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 0 {
		t.Fatal("validation wrote settings")
	}
}

func TestNativeRetryDoesNotAnswerHumanCheckpoint(t *testing.T) {
	p, f := newQuotaDaemon(t)
	ctx := context.Background()
	if err := p.ensureConnected(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.autoReviewNotificationLocked("item/autoApprovalReview/completed", reviewNotification("denial", "denied", 1))
	p.contexts = map[string]*daemonContextState{"one": {requests: map[string]SessionContext{"human": {RequiredHumanApproval: true}}}}
	p.mu.Unlock()
	menu, err := p.SessionCommands(ctx, "one", "approve/denial")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ExecuteSessionCommand(ctx, "one", menu.Path, menu.Revision, menu.Choices[0].ID); err == nil {
		t.Fatal("retry consent crossed a mandatory human checkpoint")
	}
	p.mu.Lock()
	retained := len(p.autoReviews["one"].denials)
	p.mu.Unlock()
	if retained != 1 {
		t.Fatal("blocked request consumed denial")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.denialWrites) != 0 {
		t.Fatal("sent marker while human approval pending")
	}
}

func TestNativeReviewSnapshotDoesNotReplaceConversation(t *testing.T) {
	p := &daemonStatusProvider{contexts: map[string]*daemonContextState{}, permissionSettings: map[string]string{"one": "auto_review"}}
	daemonContextEvent(p.contexts, "turn/started", nil, json.RawMessage(`{"threadId":"one","turn":{"id":"turn"}}`), time.Now())
	daemonContextEvent(p.contexts, "item/completed", nil, json.RawMessage(`{"threadId":"one","turnId":"turn","item":{"type":"agentMessage","id":"reply","text":"Reviewing the project."}}`), time.Now())
	p.autoReviewNotificationLocked("item/autoApprovalReview/started", reviewNotification("review", "inProgress", 1))
	c := p.statusSnapshotLocked([]string{"one"}).Contexts["one"]
	if c.Text != "Reviewing the project." || c.Reviewer != "auto_review" || !c.AutoReview.Active() {
		t.Fatalf("review lost prose/settings: %+v", c)
	}
	p.autoReviewNotificationLocked("turn/completed", json.RawMessage(`{"threadId":"one","turn":{"id":"turn"}}`))
	if p.autoReviews["one"].latest.view.Active() {
		t.Fatal("review stayed active after turn ended")
	}
}

func TestNativePermissionArguments(t *testing.T) {
	for _, args := range []string{`:workspace --approval-policy=on-request --reviewer=auto_review`, `':workspace' --reviewer user --approval-policy '{"granular":{"sandbox_approval":true,"rules":false,"mcp_elicitations":true}}'`} {
		path, err := permissionCommandPath("permissions " + args)
		if err != nil {
			t.Fatal(err)
		}
		parent, value, ok := SessionCommandInputValue(path)
		if !ok || parent != "permissions" || value != args {
			t.Fatalf("lost editable native args: %s", path)
		}
	}
	for _, args := range []string{`:workspace extra`, `:workspace --reviewer=custom`, `:workspace --reviewer=user --reviewer=auto_review`, `:workspace --approval-policy=maybe`, `:workspace --reviewer`, `:workspace --approval-policy '{"granular":{"rules":true}}'`} {
		if _, err := permissionCommandPath("permissions " + args); err == nil {
			t.Errorf("accepted invalid args %s", args)
		}
	}
}

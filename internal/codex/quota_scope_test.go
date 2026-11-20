package codex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuotaResponsePreservesPermissionAndWorkspaceWithoutPublishingIdentity(t *testing.T) {
	var scopes []string
	for _, tc := range []struct {
		raw       string
		reported  bool
		allowed   *bool
		workspace bool
	}{
		{raw: `{}`},
		{raw: `{"ordinaryUsageAllowed":null}`, reported: true},
		{raw: `{"accountId":"first-workspace","ordinaryUsageAllowed":false}`, reported: true, allowed: new(false), workspace: true},
		{raw: `{"accountId":"second-workspace","ordinaryUsageAllowed":true}`, reported: true, allowed: new(true), workspace: true},
	} {
		var response quotaResponse
		if err := json.Unmarshal([]byte(tc.raw), &response); err != nil {
			t.Fatal(err)
		}
		snapshot, err := response.snapshot("same-email", "")
		if err != nil || snapshot.QuotaPermissionReported != tc.reported ||
			(snapshot.OrdinaryUsageAllowed == nil) != (tc.allowed == nil) ||
			tc.allowed != nil && *snapshot.OrdinaryUsageAllowed != *tc.allowed {
			t.Fatalf("%s: permission not preserved: %+v (%v)", tc.raw, snapshot, err)
		}
		scope := snapshot.QuotaAccount()
		if IsWorkspaceQuotaAccount(scope) != tc.workspace || !tc.workspace && scope != "same-email" {
			t.Fatal("wrong workspace scope", tc.raw)
		}
		if tc.workspace {
			scopes = append(scopes, scope)
		}
		raw, err := json.Marshal(snapshot)
		if err != nil || strings.Contains(string(raw), "workspace") || strings.Contains(string(raw), "same-email") {
			t.Fatal("account metadata leaked into projected snapshot", err)
		}
	}
	if scopes[0] == scopes[1] {
		t.Fatal("workspaces with the same email matched")
	}
	if _, err := (quotaResponse{WorkspaceID: "previous"}).snapshot("same-email", "routed"); err == nil {
		t.Fatal("accepted quota from a different workspace than the authenticated route")
	}
	if fromRoute, err := (quotaResponse{}).snapshot("same-email", "routed"); err != nil || !IsWorkspaceQuotaAccount(fromRoute.QuotaAccount()) {
		t.Fatal("did not use a verified routing workspace when the quota omitted it", err)
	}
}

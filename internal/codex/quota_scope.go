package codex

import (
	"encoding/json"
	"errors"
	"strings"
)

const quotaWorkspacePrefix = "workspace:"

// ErrScheduledQuotaUnverified guarantees that no prompt was sent.
var ErrScheduledQuotaUnverified = errors.New("scheduled quota account could not be verified")

func quotaAccount(account, workspace string) string {
	if account == "" || workspace == "" {
		return account
	}
	return quotaWorkspacePrefix + commandDigest([]string{account, workspace})
}

func IsWorkspaceQuotaAccount(account string) bool {
	return strings.HasPrefix(account, quotaWorkspacePrefix)
}

func workspaceRoutingID(raw json.RawMessage) string {
	var routing struct {
		Workspace struct {
			AccountID string `json:"chatgptAccountId"`
		} `json:"workspaceRouting"`
	}
	if json.Unmarshal(raw, &routing) != nil {
		return ""
	}
	return routing.Workspace.AccountID
}

type quotaResponse struct {
	Snapshot
	WorkspaceID string          `json:"accountId"`
	Permission  json.RawMessage `json:"ordinaryUsageAllowed"`
}

func (r quotaResponse) snapshot(account, routedWorkspace string) (Snapshot, error) {
	if r.WorkspaceID != "" && routedWorkspace != "" && r.WorkspaceID != routedWorkspace {
		return Snapshot{}, errors.New("Codex workspace changed during the quota read")
	}
	if r.WorkspaceID == "" {
		r.WorkspaceID = routedWorkspace
	}
	s := r.Snapshot
	s.AccountFingerprint, s.quotaWorkspace = account, r.WorkspaceID
	s.QuotaPermissionReported = len(r.Permission) != 0
	if s.QuotaPermissionReported {
		if err := json.Unmarshal(r.Permission, &s.OrdinaryUsageAllowed); err != nil {
			return Snapshot{}, err
		}
	}
	return s, nil
}

func workspaceRoutingReady(raw json.RawMessage) bool {
	var event struct {
		Reason string `json:"reason"`
	}
	return json.Unmarshal(raw, &event) == nil && event.Reason == "workspaceRoutingReady"
}

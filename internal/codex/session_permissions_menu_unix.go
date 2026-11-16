//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/google/shlex"
	"github.com/gorilla/websocket"
)

// Keep permission provenance distinct from model/threshold settings. Missing
// reviewer metadata means an older server, not an implicit human reviewer.
type sessionPermissions = sessionPermissionSnapshot

func (p *daemonStatusProvider) readPermissions(ctx context.Context, conn *websocket.Conn, id string) (sessionPermissions, error) {
	var s sessionPermissions
	err := p.requestOn(ctx, conn, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &s)
	if err == nil && s.Thread.ID != id {
		err = ErrSessionCommand
	}
	return s, err
}

func equalPermissionPolicy(a, b any) bool {
	decode := func(v any) any {
		data, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var out any
		if json.Unmarshal(data, &out) != nil {
			return nil
		}
		return out
	}
	return reflect.DeepEqual(decode(a), decode(b))
}

func permissionCommandPath(path string) (string, error) {
	args, inline := strings.CutPrefix(path, "permissions ")
	if !inline {
		return path, nil
	}
	if _, err := parsePermissionArgs(args); err != nil {
		return "", err
	}
	return "permissions/apply/" + url.PathEscape(args), nil
}

func parsePermissionArgs(args string) (map[string]any, error) {
	words, err := shlex.Split(args)
	if err != nil || len(words) == 0 {
		return nil, ErrSessionCommand
	}
	params := map[string]any{}
	for i := 0; i < len(words); i++ {
		word := words[i]
		if strings.HasPrefix(word, "--") {
			key, value, equals := strings.Cut(word, "=")
			if !equals {
				i++
				if i == len(words) {
					return nil, ErrSessionCommand
				}
				value = words[i]
			}
			field := map[string]string{"--approval-policy": "approvalPolicy", "--reviewer": "approvalsReviewer"}[key]
			if field == "" || params[field] != nil {
				return nil, ErrSessionCommand
			}
			if field == "approvalPolicy" {
				policy, err := permissionPolicy(value)
				if err != nil {
					return nil, err
				}
				params[field] = policy
			} else {
				if value != "user" && value != "auto_review" {
					return nil, ErrSessionCommand
				}
				params[field] = value
			}
		} else {
			if params["permissions"] != nil || word == "" {
				return nil, ErrSessionCommand
			}
			params["permissions"] = word
		}
	}
	if params["permissions"] == nil {
		return nil, ErrSessionCommand
	}
	return params, nil
}

func permissionPolicy(value string) (json.RawMessage, error) {
	switch value {
	case "untrusted", "on-request", "never":
		data, _ := json.Marshal(value)
		return data, nil
	}
	var policy struct {
		Granular map[string]bool `json:"granular"`
	}
	if !json.Valid([]byte(value)) {
		return nil, ErrSessionCommand
	}
	d := json.NewDecoder(strings.NewReader(value))
	d.DisallowUnknownFields()
	if err := d.Decode(&policy); err != nil || policy.Granular == nil {
		return nil, ErrSessionCommand
	}
	var raw struct {
		Granular map[string]json.RawMessage `json:"granular"`
	}
	_ = json.Unmarshal([]byte(value), &raw)
	for _, v := range raw.Granular {
		if string(v) != "true" && string(v) != "false" {
			return nil, ErrSessionCommand
		}
	}
	allowed := map[string]bool{"sandbox_approval": true, "rules": true, "skill_approval": true, "request_permissions": true, "mcp_elicitations": true}
	for key := range policy.Granular {
		if !allowed[key] {
			return nil, ErrSessionCommand
		}
	}
	for _, key := range []string{"sandbox_approval", "rules", "mcp_elicitations"} {
		if _, ok := policy.Granular[key]; !ok {
			return nil, ErrSessionCommand
		}
	}
	// Match the two serde(default) switches in Codex's GranularApprovalConfig.
	for _, key := range []string{"skill_approval", "request_permissions"} {
		if _, ok := policy.Granular[key]; !ok {
			policy.Granular[key] = false
		}
	}
	data, err := json.Marshal(policy)
	return data, err
}

func (p *daemonStatusProvider) permissionMenu(ctx context.Context, t commandThread, path string) (SessionCommandMenu, error) {
	p.mu.Lock()
	conn := p.connection
	p.mu.Unlock()
	s, err := p.readPermissions(ctx, conn, t.ID)
	if err != nil {
		return SessionCommandMenu{}, err
	}
	if s.Cwd != t.Cwd {
		return SessionCommandMenu{}, ErrSessionCommand
	}
	m := SessionCommandMenu{Title: "/permissions", Help: "Change the native permission profile, approval policy or reviewer for subsequent turns. Auto-review is Codex's reviewer, not a Codexometer decision engine. Managed policy remains authoritative.", Choices: []SessionCommandChoice{}}
	m.permissions = &s
	m.Revision = commandDigest(s)
	if s.Profile != nil {
		m.Help += "\nCurrent profile: " + s.Profile.ID
	}
	if s.Reviewer != "" {
		m.Help += "\nCurrent reviewer: " + s.Reviewer + "\nCurrent approval policy: " + string(s.Policy)
	}
	rows, err := p.commandRows(ctx, "permissionProfile/list", map[string]any{"cwd": t.Cwd})
	if err != nil {
		return m, err
	}
	profiles := map[string]string{}
	for _, raw := range rows {
		var v struct {
			ID, Description string
			Allowed         bool
		}
		if json.Unmarshal(raw, &v) == nil && v.Allowed && commandSafe(v.ID) && strings.TrimSpace(v.Description) != "" {
			profiles[v.ID] = v.Description
			if path == "permissions" {
				m.Choices = append(m.Choices, SessionCommandChoice{Label: v.ID, Help: v.Description, method: "thread/settings/update", params: map[string]any{"threadId": t.ID, "permissions": v.ID}, Selected: s.Profile != nil && s.Profile.ID == v.ID})
			}
		}
	}
	if s.Reviewer == "" || len(s.Policy) == 0 {
		if path != "permissions" {
			return m, fmt.Errorf("native reviewer settings unavailable; update the shared Codex app-server")
		}
		return m, nil
	}
	var response struct{ Requirements json.RawMessage }
	if err := p.requestOn(ctx, conn, "configRequirements/read", map[string]any{}, &response); err != nil {
		return m, err
	}
	if len(response.Requirements) == 0 {
		return m, ErrSessionCommand
	}
	var requirements struct {
		Policies  *[]json.RawMessage `json:"allowedApprovalPolicies"`
		Reviewers *[]string          `json:"allowedApprovalsReviewers"`
	}
	if string(response.Requirements) != "null" {
		if err := json.Unmarshal(response.Requirements, &requirements); err != nil {
			return m, err
		}
	}
	features, err := p.commandRows(ctx, "experimentalFeature/list", map[string]any{"threadId": t.ID})
	if err != nil {
		return m, err
	}
	auto := false
	for _, raw := range features {
		var f struct {
			Name    string
			Enabled bool
		}
		if json.Unmarshal(raw, &f) == nil && f.Name == "guardian_approval" {
			auto = f.Enabled
		}
	}
	allowed := func(params map[string]any) bool {
		if r, ok := params["approvalsReviewer"].(string); ok {
			if r != "user" && r != "auto_review" || r == "auto_review" && !auto {
				return false
			}
			if requirements.Reviewers != nil {
				found := false
				for _, v := range *requirements.Reviewers {
					found = found || v == r
				}
				if !found {
					return false
				}
			}
		}
		if policy, ok := params["approvalPolicy"]; ok && requirements.Policies != nil {
			found := false
			for _, v := range *requirements.Policies {
				found = found || equalPermissionPolicy(v, policy)
			}
			if !found {
				return false
			}
		}
		return true
	}
	add := func(label, help string, params map[string]any) {
		if allowed(params) {
			params["threadId"] = t.ID
			m.Choices = append(m.Choices, SessionCommandChoice{Label: label, Help: help, method: "thread/settings/update", params: params})
		}
	}
	switch path {
	case "permissions":
		m.Choices = append(m.Choices, SessionCommandChoice{Label: "Approval policy", Help: "When native Codex asks for approval.", Next: "permissions/policy"}, SessionCommandChoice{Label: "Reviewer", Help: "Who reviews eligible escalations. Does not change the sandbox.", Next: "permissions/reviewer"})
	case "permissions/policy":
		for _, v := range []string{"untrusted", "on-request", "never"} {
			policy, _ := permissionPolicy(v)
			add(v, "Native approval policy: "+v+". Sandbox and reviewer are unchanged.", map[string]any{"approvalPolicy": policy})
		}
		if requirements.Policies != nil {
			for _, v := range *requirements.Policies {
				if len(v) > 0 && v[0] == '{' {
					add(string(v), "Managed granular policy.", map[string]any{"approvalPolicy": v})
				}
			}
		}
	case "permissions/reviewer":
		if !auto {
			m.Help += "\nAuto-review is unavailable because Codex's guardian_approval feature is disabled. Enable it in Codex first; this menu does not edit global configuration."
		}
		add("Human", "Codex asks you to review eligible approval requests.", map[string]any{"approvalsReviewer": "user"})
		add("Auto-review", "Codex's native reviewer assesses eligible requests. Adds model usage; mandatory human checkpoints remain human. Does not promise only low-risk actions will pass.", map[string]any{"approvalsReviewer": "auto_review"})
	default:
		encoded, ok := strings.CutPrefix(path, "permissions/apply/")
		if !ok {
			return m, ErrSessionCommand
		}
		data, err := url.PathUnescape(encoded)
		if err != nil {
			return m, err
		}
		params, err := parsePermissionArgs(data)
		if err != nil {
			return m, err
		}
		profile, ok := params["permissions"].(string)
		if !ok || profiles[profile] == "" {
			return m, ErrSessionCommand
		}
		if !allowed(params) {
			return m, ErrSessionCommand
		}
		add("Apply native permissions", profiles[profile]+"\n"+data, params)
	}
	return m, nil
}

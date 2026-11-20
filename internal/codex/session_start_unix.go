//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
)

type projectTrustConfig struct {
	Projects map[string]struct {
		TrustLevel string `json:"trust_level"`
	}
}

func (p *daemonStatusProvider) newSessionTrust(ctx context.Context, conn *websocket.Conn, cwd string) bool {
	var result struct {
		Config projectTrustConfig
		Layers []struct {
			Name           struct{ Type string }
			Config         projectTrustConfig
			DisabledReason *string
		}
	}
	// An explicit cwd can make thread/start persist a previously undecided
	// folder as trusted. Require an exact, effective decision from Codex's
	// user configuration. Never infer trust from a parent or a project layer.
	params := map[string]any{"cwd": cwd, "includeLayers": true}
	if p.requestOn(ctx, conn, "config/read", params, &result) != nil || result.Config.Projects[cwd].TrustLevel != "trusted" {
		return false
	}
	trusted := false
	for _, layer := range result.Layers {
		if layer.Name.Type == "project" && layer.DisabledReason != nil {
			return false
		}
		if layer.Name.Type == "user" && layer.DisabledReason == nil && layer.Config.Projects[cwd].TrustLevel == "trusted" {
			trusted = true
		}
	}
	return trusted
}

func (p *daemonStatusProvider) StartSession(ctx context.Context, scope, cwd, prompt string) (string, error) {
	conn, generation, account, err := p.savedSessionAction(ctx, scope)
	if err != nil {
		return "", ErrSessionStartUnavailable
	}
	if !p.newSessionTrust(ctx, conn, cwd) {
		return "", ErrSessionStartTrust
	}
	var result struct {
		Cwd, ApprovalsReviewer string
		Thread                 struct{ ID string }
	}
	// The outer action deadline still bounds all preflights and both writes.
	err = p.requestOnWithin(ctx, conn, "thread/start", map[string]string{"cwd": cwd}, &result, &generation, 9*time.Second)
	var rejected *daemonResponseError
	switch {
	case errors.Is(err, errDaemonWriteGuardRejected):
		return "", ErrSessionStartUnavailable
	case errors.As(err, &rejected):
		return "", ErrSessionStartRejected
	}
	id := result.Thread.ID
	if err != nil || id == "" || len(id) > 512 || singleLine(id) != id || filepath.Clean(result.Cwd) != cwd {
		return "", ErrSessionStartUncertain
	}
	if current, err := p.savedAccount(ctx, scope); err != nil || current != account {
		return "", ErrSessionStartUncertain
	}
	p.mu.Lock()
	valid := p.connection == conn && p.savedScope == scope && p.commandGeneration == generation
	if valid {
		p.rememberResumedThreadLocked(id, SessionContext{})
		p.recordThreadSubscriptionLocked(id, result.Cwd, result.ApprovalsReviewer)
		p.lastStatusAt = time.Time{}
	}
	p.mu.Unlock()
	if !valid {
		return "", ErrSessionStartUncertain
	}
	var started struct{ Turn struct{ ID string } }
	err = p.requestOnWithin(ctx, conn, "turn/start", map[string]any{
		"threadId": id,
		"input":    []any{map[string]string{"type": "text", "text": literalSessionText(prompt)}},
	}, &started, &generation, 5*time.Second)
	var wire struct{ Code int }
	// InvalidRequest is a definite rejection. Treat internal errors
	// conservatively instead of making possibly submitted work retryable.
	if errors.As(err, &rejected) && json.Unmarshal([]byte(rejected.payload), &wire) == nil && wire.Code == -32600 {
		return id, ErrSessionFirstPrompt
	}
	if err != nil || started.Turn.ID == "" {
		return id, ErrSessionStartUncertain
	}
	return id, nil
}

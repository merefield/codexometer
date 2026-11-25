package codex

import (
	"context"
	"time"

	"github.com/merefield/codexometer/internal/prose"
)

type SessionLinkPreview struct {
	Root            SavedSession  `json:"root"`
	Agent           *SessionAgent `json:"agent,omitempty"`
	Turns           []SavedTurn   `json:"turns,omitempty"`
	UnverifiedTurns bool          `json:"unverifiedTurns,omitempty"`
}

type SessionLinkClient interface {
	PreviewSessionLink(context.Context, string, string) (SessionLinkPreview, error)
}

type sessionLinkProvider interface {
	previewSessionLink(context.Context, *LiveUsageReader, string, string) (SessionLinkPreview, error)
}

func (c Client) PreviewSessionLink(ctx context.Context, scope, id string) (SessionLinkPreview, error) {
	if c.LiveUsage != nil && scope != "" && len(scope) <= 128 && prose.ThreadID("codex://threads/"+id) != "" {
		if p, ok := c.LiveUsage.statusProvider.(sessionLinkProvider); ok {
			return p.previewSessionLink(ctx, c.LiveUsage, scope, id)
		}
	}
	return SessionLinkPreview{}, ErrSavedSessions
}

// Only locally attributable agents are eligible; a missing or ambiguous spawn
// must not be treated as an ordinary transcript containing inherited history.
func (r *LiveUsageReader) sessionLinkTarget(id string) (root, name string, since time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byID := r.sessionAgentRolloutsLocked()
	child := byID[id]
	if child == nil || child.parentThreadID == "" && !child.nonRoot {
		return id, "", time.Time{}
	}
	root, unlinked := rolloutRoot(child, byID)
	if unlinked || child.guardianReviewer || child.startedAt.IsZero() {
		return "", "", time.Time{}
	}
	name = child.agentName
	if name == "" {
		name = r.sessionNames[id]
	}
	return root, singleLine(name), child.startedAt
}

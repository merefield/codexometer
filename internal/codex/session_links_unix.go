//go:build unix

package codex

import "context"

func (p *daemonStatusProvider) previewSessionLink(ctx context.Context, reader *LiveUsageReader, scope, id string) (SessionLinkPreview, error) {
	root, name, since := reader.sessionLinkTarget(id)
	if root == "" {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	conn, generation, account, err := p.savedSessionAction(ctx, scope)
	if err != nil {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	var result struct{ Data []savedWireThread }
	// The default source filter matches interactive roots only. Exact ID and
	// account visibility must both be proven before requesting any transcript.
	if err := p.requestOn(ctx, conn, "thread/list", map[string]any{
		"includedThreadIds": []string{root}, "currentAccountOnly": true, "archived": false,
		"modelProviders": []string{}, "limit": 1, "useStateDbOnly": true,
	}, &result); err != nil || len(result.Data) != 1 || result.Data[0].ID != root {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	current := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.connection == conn && p.commandGeneration == generation && p.savedScope == scope
	}
	if !current() {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	turns, unverified, err := p.readSessionHistoryOn(ctx, conn, generation, id, since)
	if err != nil {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	if verified, err := p.savedAccount(ctx, scope); err != nil || account != verified || !current() {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	if target, _, at := reader.sessionLinkTarget(id); target != root || at != since {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	reader.protectSessionHistory(id, turns)
	page := SavedPage{}
	page.addWireThread(result.Data[0])
	if len(page.Sessions) != 1 {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	preview := SessionLinkPreview{Root: page.Sessions[0], Turns: savedTurns(turns), UnverifiedTurns: unverified}
	if root != id {
		preview.Agent = &SessionAgent{ID: id, Name: name}
	}
	return preview, nil
}

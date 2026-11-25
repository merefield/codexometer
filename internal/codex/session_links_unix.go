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
	p.mu.Lock()
	exact := p.savedExactIDs
	p.mu.Unlock()
	current := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.connection == conn && p.commandGeneration == generation && p.savedScope == scope
	}
	if !current() {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	var page SavedPage
	if exact {
		// The default source filter matches interactive roots only.
		page, err = p.readSavedPage(ctx, conn, map[string]any{
			"includedThreadIds": []string{root}, "currentAccountOnly": true, "archived": false,
			"modelProviders": []string{}, "limit": 1, "useStateDbOnly": true,
		})
	} else {
		// Codex 0.170 has account scoping, but silently ignores includedThreadIds.
		page, err = savedPageFor(ctx, p, scope, "", root)
	}
	if err != nil || exact && len(page.Sessions) != 1 || !current() {
		return SessionLinkPreview{}, ErrSavedSessions
	}
	var row SavedSession
	for _, candidate := range page.Sessions {
		if candidate.ID == root {
			row = candidate
			break
		}
	}
	if row.ID == "" {
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
	preview := SessionLinkPreview{Root: row, Turns: savedTurns(turns), UnverifiedTurns: unverified}
	if root != id {
		preview.Agent = &SessionAgent{ID: id, Name: name}
	}
	return preview, nil
}

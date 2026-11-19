package codex

import (
	"context"
	"errors"
	"strings"
)

// Saved sessions are deliberately a separate, display-only type. A thread
// discovered in history must never become a live control or telemetry target.
type SavedSession struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Directory string `json:"directory"`
}

type SavedTurn struct {
	Task       string `json:"task"`
	Reply      string `json:"reply"`
	Restricted bool   `json:"restricted,omitempty"`
}

type SavedPage struct {
	Sessions []SavedSession `json:"sessions,omitempty"`
	Next     string         `json:"next,omitempty"`
	Turns    []SavedTurn    `json:"turns,omitempty"`
}

type SavedSessionClient interface {
	BrowseSavedSessions(ctx context.Context, scope, cursor, id string) (SavedPage, error)
}

var ErrSavedSessions = errors.New("account-scoped saved sessions unavailable")

type savedSessionProvider interface {
	listSaved(context.Context, string, string) (SavedPage, string, error)
	savedAccount(context.Context, string) (string, error)
}

func singleLine(s string) string { return strings.Join(strings.Fields(SanitizeSessionContext(s)), " ") }

func SessionMatches(query, id, name, directory string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	return q == "" || strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(directory), q) || strings.Contains(strings.ToLower(id), q)
}

func (c Client) BrowseSavedSessions(ctx context.Context, scope, cursor, id string) (SavedPage, error) {
	if c.LiveUsage == nil || scope == "" || len(scope) > 128 || len(cursor) > 4096 || len(id) > 512 {
		return SavedPage{}, ErrSavedSessions
	}
	p, ok := c.LiveUsage.statusProvider.(savedSessionProvider)
	if !ok {
		return SavedPage{}, ErrSavedSessions
	}
	page, account, err := p.listSaved(ctx, scope, cursor)
	if err != nil {
		return SavedPage{}, ErrSavedSessions
	}
	if id != "" {
		found := false
		for _, row := range page.Sessions {
			if row.ID == id {
				found = true
				break
			}
		}
		if !found {
			return SavedPage{}, ErrSavedSessions
		}
		turns, err := c.SessionHistory(ctx, id)
		if err != nil {
			return SavedPage{}, ErrSavedSessions
		}
		page = SavedPage{}
		for _, t := range turns {
			t = HistoryExcerpt(t)
			page.Turns = append(page.Turns, SavedTurn{Task: t.CurrentTask, Reply: t.Text, Restricted: t.ReplyRestricted})
		}
	}
	if current, err := p.savedAccount(ctx, scope); err != nil || current != account {
		return SavedPage{}, ErrSavedSessions
	}
	return page, nil
}

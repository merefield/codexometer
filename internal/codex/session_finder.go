package codex

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Saved sessions are deliberately a separate, display-only type. A thread
// discovered in history must never become a live control or telemetry target.
type SavedSession struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Directory  string `json:"directory"`
	UpdatedAt  int64  `json:"updatedAt,omitempty"`  // Unix seconds, from Codex.
	ListCursor string `json:"listCursor,omitempty"` // Origin page of a search result.
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

type SavedSessionSearchClient interface {
	SearchSavedSessions(ctx context.Context, scope, cursor, query string) (SavedPage, error)
}

type SavedSessionResumeClient interface {
	ResumeSavedSession(ctx context.Context, scope, cursor, id string) error
}

type savedSessionNavigation interface {
	searchSaved(context.Context, string, string, string) (SavedPage, error)
	resumeSaved(context.Context, string, string, string) error
}

func (c Client) SearchSavedSessions(ctx context.Context, scope, cursor, query string) (SavedPage, error) {
	query = strings.TrimSpace(query)
	if c.LiveUsage == nil || scope == "" || len(scope) > 128 || len(cursor) > 4096 || len(query) > 1024 || SanitizeSessionContext(query) != query {
		return SavedPage{}, ErrSavedSessions
	}
	if p, ok := c.LiveUsage.statusProvider.(savedSessionNavigation); ok {
		return p.searchSaved(ctx, scope, cursor, query)
	}
	return SavedPage{}, ErrSavedSessions
}

func (c Client) ResumeSavedSession(ctx context.Context, scope, cursor, id string) error {
	if c.LiveUsage != nil && scope != "" && len(scope) <= 128 && len(cursor) <= 4096 && id != "" && len(id) <= 512 && singleLine(id) == id {
		if p, ok := c.LiveUsage.statusProvider.(savedSessionNavigation); ok {
			return p.resumeSaved(ctx, scope, cursor, id)
		}
	}
	return ErrSavedSessions
}

// A cold resume can load history and project settings. Give it a separate
// action budget from routine observation requests.
const SavedResumeTimeout = 12 * time.Second

var (
	ErrSavedSessions        = errors.New("account-scoped saved sessions unavailable")
	ErrSavedResumeUncertain = errors.New("resume outcome unknown")
)

type savedSessionProvider interface {
	listSaved(ctx context.Context, scope, cursor, sortKey string) (SavedPage, error)
	savedAccount(context.Context, string) (string, error)
}

func singleLine(s string) string { return strings.Join(strings.Fields(SanitizeSessionContext(s)), " ") }

func SessionMatches(query, id, name, directory string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	return q == "" || strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(directory), q) || strings.Contains(strings.ToLower(id), q)
}

var savedNameFold = cases.Fold()

func savedNameKey(text string) string {
	return savedNameFold.String(norm.NFC.String(text))
}

func savedNameQuery(query string) string {
	return savedNameKey(strings.Join(strings.Fields(query), " "))
}

// SavedSessionNameFilter is deliberately narrower than the live filter. Fold
// Unicode case and normalise accents without replacing an accent with ASCII.
func SavedSessionNameFilter(query string) func(SavedSession) bool {
	needle := savedNameQuery(query)
	return func(row SavedSession) bool {
		return needle == "" || strings.Contains(savedNameKey(row.Name), needle)
	}
}

func (c Client) BrowseSavedSessions(ctx context.Context, scope, cursor, id string) (SavedPage, error) {
	if c.LiveUsage == nil || scope == "" || len(scope) > 128 || len(cursor) > 4096 || len(id) > 512 {
		return SavedPage{}, ErrSavedSessions
	}
	p, ok := c.LiveUsage.statusProvider.(savedSessionProvider)
	if !ok {
		return SavedPage{}, ErrSavedSessions
	}
	account, err := p.savedAccount(ctx, scope)
	if err != nil || account == "" {
		return SavedPage{}, ErrSavedSessions
	}
	sameAccount := func() bool {
		current, err := p.savedAccount(ctx, scope)
		return err == nil && current == account
	}
	page, err := savedPageFor(ctx, p, scope, cursor, id)
	if err != nil {
		return SavedPage{}, ErrSavedSessions
	}
	if id != "" {
		if !sameAccount() {
			return SavedPage{}, ErrSavedSessions
		}
		turns, err := c.SessionHistory(ctx, id)
		if err != nil {
			return SavedPage{}, ErrSavedSessions
		}
		page = SavedPage{Turns: savedTurns(turns)}
	}
	if !sameAccount() {
		return SavedPage{}, ErrSavedSessions
	}
	return page, nil
}

func savedTurns(turns []SessionContext) []SavedTurn {
	var result []SavedTurn
	for _, t := range turns {
		t = HistoryExcerpt(t)
		result = append(result, SavedTurn{Task: t.CurrentTask, Reply: t.Text, Restricted: t.ReplyRestricted})
	}
	return result
}

// The cursor is a fast path, not an authorization grant: updated_at ordering
// can change between list and click. On a miss or a stale cursor, walk fresh
// account-scoped pages in creation order so activity cannot move our target.
func savedPageFor(ctx context.Context, p savedSessionProvider, scope, cursor, id string) (SavedPage, error) {
	page, err := p.listSaved(ctx, scope, cursor, "updated_at")
	matches := func(row SavedSession) bool { return row.ID == id }
	if id == "" || (err == nil && slices.ContainsFunc(page.Sessions, matches)) || (err != nil && cursor == "") {
		return page, err
	}
	seen := map[string]bool{}
	for cursor = ""; ctx.Err() == nil && !seen[cursor]; cursor = page.Next {
		seen[cursor] = true
		page, err = p.listSaved(ctx, scope, cursor, "created_at")
		if err != nil {
			break
		}
		if slices.ContainsFunc(page.Sessions, matches) {
			return page, nil
		}
		if page.Next == "" {
			break
		}
	}
	return SavedPage{}, ErrSavedSessions
}

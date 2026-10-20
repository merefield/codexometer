package codex

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	SessionRenameInputLimit    = 512
	SessionDirectoryInputLimit = 1024
	// One UTF-8 rune can take four bytes, each percent-encoded as three bytes.
	SessionCommandPathLimit = len("rename/") + SessionDirectoryInputLimit*utf8.UTFMax*3
)

// ValidSessionCommandPath shares the encoded transport bound and decoded editor
// limits between the daemon adapter and browser control. Other command families
// keep their existing 2 KiB transport limit.
func ValidSessionCommandPath(path string) bool {
	if len(path) > SessionCommandPathLimit || !utf8.ValidString(path) {
		return false
	}
	command := strings.TrimPrefix(strings.TrimSpace(path), "/")
	for _, family := range []struct {
		name  string
		limit int
	}{
		{"rename", SessionRenameInputLimit},
		{"cd", SessionDirectoryInputLimit},
	} {
		value, encoded := strings.CutPrefix(command, family.name+"/")
		if !encoded {
			var direct bool
			value, direct = strings.CutPrefix(command, family.name+" ")
			if !direct {
				continue
			}
		} else {
			var err error
			value, err = url.PathUnescape(value)
			if err != nil {
				return false
			}
		}
		return utf8.ValidString(value) && utf8.RuneCountInString(value) <= family.limit
	}
	return len(path) <= 2048
}

// Command catalogues are interface-neutral, live and memory-only. Front ends
// render metadata; they never manufacture RPC methods or permission profiles.
type SessionCommandChoice struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Help     string `json:"help"`
	Next     string `json:"next,omitempty"`
	Action   bool   `json:"action,omitempty"`
	Selected bool   `json:"selected,omitempty"`
	// Unavailable explains a blocked action; an empty reason with no Action is genuine browse-only help.
	Unavailable string `json:"unavailable,omitempty"`
	method      string
	params      map[string]any
}

type SessionCommandMenu struct {
	Title    string `json:"title"`
	Help     string `json:"help"`
	Path     string `json:"path"`
	Revision string `json:"revision"`
	Multiple bool   `json:"multiple,omitempty"`
	// Picker keeps choices visible; the highlighted option is applied explicitly.
	Picker     bool                   `json:"picker,omitempty"`
	Input      bool                   `json:"input,omitempty"`
	InputLabel string                 `json:"inputLabel,omitempty"`
	InputLimit int                    `json:"inputLimit,omitempty"`
	Value      string                 `json:"value,omitempty"`
	Choices    []SessionCommandChoice `json:"choices"`
}

// LocalSessionCommands are owned by the interface and never become app-server RPCs.
func LocalSessionCommands() []SessionCommandChoice {
	return []SessionCommandChoice{
		{ID: "local-statusline", Label: "/statusline", Help: "Choose fields for Codexometer’s detail footer. Saved display preferences only.", Next: "statusline"},
		{ID: "local-usage", Label: "/usage", Help: "Open Codexometer’s usage history. Existing quota and reset views remain available.", Next: "usage"},
		{ID: "local-copy", Label: "/copy", Help: "Copy the selected session’s observed reply or activity using Codexometer’s copy control.", Next: "copy"},
		{ID: "local-theme", Label: "/theme", Help: "Choose a Codexometer display theme. Saved local display preferences only.", Next: "theme"},
		{ID: "local-exit", Label: "/exit", Help: "Leave this Codexometer interface. Codex sessions continue running.", Next: "exit"},
		{ID: "local-quit", Label: "/quit", Help: "Alias for /exit. Codex sessions continue running.", Next: "quit"},
	}
}

func IsLocalSessionCommand(path string) bool {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	for _, choice := range LocalSessionCommands() {
		if choice.Next == path {
			return true
		}
	}
	return false
}

type SessionCommandsClient interface {
	SessionCommands(context.Context, string, string) (SessionCommandMenu, error)
	ExecuteSessionCommand(context.Context, string, string, string, string) error
}

// Optional browsing fast path. Fresh reads and writes keep their existing
// server-side revision checks; cached menus contain display metadata only.
type CachedSessionCommandsClient interface {
	CachedSessionCommands(string, string) (SessionCommandMenu, bool)
	SessionCommandsFresh(context.Context, string, string) (SessionCommandMenu, error)
}

var ErrSessionCommand = errors.New("command unavailable or session/options changed; reopen the command menu")

func IsSessionCommand(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "//")
}

func literalSessionText(text string) string {
	if strings.HasPrefix(strings.TrimSpace(text), "//") {
		return strings.Replace(text, "//", "/", 1)
	}
	return text
}

func (c Client) commandClient() SessionCommandsClient {
	if c.LiveUsage != nil {
		p, _ := c.LiveUsage.statusProvider.(SessionCommandsClient)
		return p
	}
	return nil
}
func (c Client) SessionCommands(ctx context.Context, id, path string) (SessionCommandMenu, error) {
	if cache := c.sessionCommandMenus(); cache != nil {
		return cache.load(ctx, id, path, false)
	}
	return SessionCommandMenu{}, ErrSessionCommand
}

func (c Client) sessionCommandMenus() *sessionCommandCache {
	source := c.commandClient()
	if source == nil {
		return nil
	}
	r := c.LiveUsage
	r.commandMu.Lock()
	defer r.commandMu.Unlock()
	if r.commandMenus == nil {
		r.commandMenus = newSessionCommandCache(source)
	}
	return r.commandMenus
}

func (c Client) CachedSessionCommands(id, path string) (SessionCommandMenu, bool) {
	if cache := c.sessionCommandMenus(); cache != nil {
		return cache.peek(id, path)
	}
	return SessionCommandMenu{}, false
}

func (c Client) SessionCommandsFresh(ctx context.Context, id, path string) (SessionCommandMenu, error) {
	if cache := c.sessionCommandMenus(); cache != nil {
		return cache.load(ctx, id, path, true)
	}
	return SessionCommandMenu{}, ErrSessionCommand
}

func (c Client) warmSessionCommands(usage LiveUsageSnapshot) {
	if cache := c.sessionCommandMenus(); cache != nil {
		cache.observe(usage)
	}
}

func (c Client) ExecuteSessionCommand(ctx context.Context, id, path, revision, choice string) error {
	if p := c.commandClient(); p != nil {
		if cache := c.sessionCommandMenus(); cache != nil {
			cache.invalidate(id)
			defer cache.invalidate(id)
		}
		return p.ExecuteSessionCommand(ctx, id, path, revision, choice)
	}
	return ErrSessionCommand
}

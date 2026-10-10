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
	method   string
	params   map[string]any
}

type SessionCommandMenu struct {
	Title      string                 `json:"title"`
	Help       string                 `json:"help"`
	Path       string                 `json:"path"`
	Revision   string                 `json:"revision"`
	Multiple   bool                   `json:"multiple,omitempty"`
	Input      bool                   `json:"input,omitempty"`
	InputLabel string                 `json:"inputLabel,omitempty"`
	InputLimit int                    `json:"inputLimit,omitempty"`
	Value      string                 `json:"value,omitempty"`
	Choices    []SessionCommandChoice `json:"choices"`
}

type SessionCommandsClient interface {
	SessionCommands(context.Context, string, string) (SessionCommandMenu, error)
	ExecuteSessionCommand(context.Context, string, string, string, string) error
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
	if p := c.commandClient(); p != nil {
		return p.SessionCommands(ctx, id, path)
	}
	return SessionCommandMenu{}, ErrSessionCommand
}
func (c Client) ExecuteSessionCommand(ctx context.Context, id, path, revision, choice string) error {
	if p := c.commandClient(); p != nil {
		return p.ExecuteSessionCommand(ctx, id, path, revision, choice)
	}
	return ErrSessionCommand
}

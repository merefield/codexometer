package codex

import (
	"context"
	"errors"
	"strings"
)

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

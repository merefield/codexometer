package codex

import (
	"context"
	"errors"
)

var ErrSessionTerminals = errors.New("background terminals unavailable; connect to the session's shared app-server")

type SessionTerminal struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	Directory string `json:"directory"`
	PID       uint32 `json:"pid,omitempty"`
}

type TerminalInspection struct {
	Terminals []SessionTerminal `json:"terminals"`
	More      bool              `json:"more,omitempty"`
}

type SessionTerminalClient interface {
	InspectSessionTerminals(context.Context, string) (TerminalInspection, error)
}

func (c Client) InspectSessionTerminals(ctx context.Context, id string) (TerminalInspection, error) {
	if id != "" && len(id) <= 512 && singleLine(id) == id {
		if p, ok := sessionCapability[SessionTerminalClient](c); ok {
			return p.InspectSessionTerminals(ctx, id)
		}
	}
	return TerminalInspection{}, ErrSessionTerminals
}

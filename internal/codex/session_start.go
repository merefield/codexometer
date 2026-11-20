package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

type SessionStartClient interface {
	StartSession(ctx context.Context, scope, directory, prompt string) (string, error)
}

var (
	ErrSessionStartUnavailable = errors.New("new session unavailable; check the shared Codex connection")
	ErrSessionStartDirectory   = errors.New("choose an existing absolute working directory")
	ErrSessionStartPrompt      = errors.New("enter a first message, not a slash command")
	ErrSessionStartUncertain   = errors.New("could not verify the new session or its first message; check Codex before trying again")
)

func (c Client) StartSession(ctx context.Context, scope, directory, prompt string) (string, error) {
	if c.LiveUsage == nil || scope == "" || len(scope) > 128 {
		return "", ErrSessionStartUnavailable
	}
	p, ok := c.LiveUsage.statusProvider.(SessionStartClient)
	if !ok {
		return "", ErrSessionStartUnavailable
	}
	if !validPromptAnswers(SessionPromptOffer{}, []string{prompt}) || IsSessionCommand(prompt) {
		return "", ErrSessionStartPrompt
	}
	if !filepath.IsAbs(directory) || len(directory) > 4096 || SanitizeSessionContext(directory) != directory {
		return "", ErrSessionStartDirectory
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return "", ErrSessionStartDirectory
	}
	return p.StartSession(ctx, scope, filepath.Clean(directory), prompt)
}

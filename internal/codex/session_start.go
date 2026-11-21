package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type SessionStartClient interface {
	StartSession(ctx context.Context, scope, directory, prompt string) (string, error)
}

// A new thread may load project configuration and warm up model connections.
// This is the total action budget, not the timeout for routine discovery RPCs.
const SessionStartTimeout = 12 * time.Second

var (
	ErrSessionStartUnavailable = errors.New("new session unavailable; check the shared Codex connection")
	ErrSessionStartDirectory   = errors.New("choose an existing absolute working directory")
	ErrSessionStartTrust       = errors.New("Codex has not verified this folder as trusted; choose an already-trusted folder or open it in Codex to review folder trust")
	ErrSessionStartPrompt      = errors.New("enter a first message with at most 4096 characters and no unsupported control characters")
	ErrSessionStartCommand     = errors.New("this slash command is not available before starting a session")
)

// Distinguish failures before the first prompt, a definite refusal on a
// known thread, and a response whose side effects could not be verified.
var (
	ErrSessionStartRejected  = errors.New("Codex rejected the new session; check the directory and Codex configuration")
	ErrSessionFirstPrompt    = errors.New("session created, but Codex rejected the first message; open the session to try again")
	ErrSessionStartUncertain = errors.New("could not verify the new session or its first message; check Codex before trying again")
)

func (c Client) StartSession(ctx context.Context, scope, directory, prompt string) (string, error) {
	if c.LiveUsage == nil || scope == "" || len(scope) > 128 {
		return "", ErrSessionStartUnavailable
	}
	p, ok := c.LiveUsage.statusProvider.(SessionStartClient)
	if !ok {
		return "", ErrSessionStartUnavailable
	}
	if IsSessionCommand(prompt) {
		return "", ErrSessionStartCommand
	}
	// Validate the submitted text, not its display projection. In particular,
	// the renderer expands tabs, but indentation is part of the prompt.
	if strings.TrimSpace(prompt) == "" || utf8.RuneCountInString(prompt) > sessionContextLimit || !safePatchText(prompt) {
		return "", ErrSessionStartPrompt
	}
	if !filepath.IsAbs(directory) || len(directory) > 4096 || SanitizeSessionContext(directory) != directory {
		return "", ErrSessionStartDirectory
	}
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil || SanitizeSessionContext(directory) != directory {
		return "", ErrSessionStartDirectory
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return "", ErrSessionStartDirectory
	}
	return p.StartSession(ctx, scope, directory, prompt)
}

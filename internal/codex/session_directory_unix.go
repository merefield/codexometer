//go:build unix

package codex

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
)

// Resume without overrides reads the running configuration of a loaded thread.
// thread/read.cwd is captured metadata and need not reflect a settings update.
func (p *daemonStatusProvider) commandDirectory(ctx context.Context, conn *websocket.Conn, id string) (string, error) {
	var response struct {
		Cwd    string
		Thread struct{ ID string }
	}
	if err := p.requestOn(ctx, conn, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &response); err != nil {
		return "", err
	}
	if response.Thread.ID != id || !filepath.IsAbs(response.Cwd) {
		return "", errors.New("running session directory unavailable; use Codex")
	}
	p.mu.Lock()
	if p.connection != conn {
		p.mu.Unlock()
		return "", ErrSessionCommand
	}
	if p.threadDirectories == nil {
		p.threadDirectories = map[string]string{}
	}
	p.threadDirectories[id] = response.Cwd
	p.mu.Unlock()
	return response.Cwd, nil
}

func (p *daemonStatusProvider) commandDirectoryMenu(ctx context.Context, t commandThread, path string) (SessionCommandMenu, error) {
	m := SessionCommandMenu{Title: "/" + strings.Split(path, "/")[0], Choices: []SessionCommandChoice{}}
	p.mu.Lock()
	conn := p.connection
	p.mu.Unlock()
	cwd, err := p.commandDirectory(ctx, conn, t.ID)
	if err != nil {
		return m, err
	}
	m.Revision = commandDigest(cwd)
	if path == "cwd" || path == "pwd" {
		m.Help = "Current working directory from this session's running Codex configuration. /cwd and /pwd are aliases."
		m.Choices = append(m.Choices, SessionCommandChoice{Label: strconv.Quote(cwd), Help: "Working directory: " + strconv.Quote(cwd)})
		return m, nil
	}
	if path != "cd" && !strings.HasPrefix(path, "cd/") {
		return m, ErrSessionCommand
	}
	m.Help = "Change this session's directory for subsequent turns. History and session ID are retained. The server adjusts its directory workspace root and retains other roots and permission settings. This does not perform the native CLI's project-configuration reload or conversation fork. Review and confirm while idle, with no queued messages."
	for _, env := range t.Environments {
		if env.EnvironmentID != "local" {
			return m, errors.New("directory changes for remote or multiple execution environments require Codex")
		}
	}
	if len(t.Environments) > 1 {
		return m, ErrSessionCommand
	}
	if path == "cd" {
		m.Input, m.InputLabel, m.InputLimit, m.Value = true, "Working directory", 1024, cwd
		return m, nil
	}
	input, err := url.PathUnescape(strings.TrimPrefix(path, "cd/"))
	if err != nil || input == "" || strings.Contains(input, "\x00") {
		return m, ErrSessionCommand
	}
	// The review retains the supplied path; resolving it again at confirmation
	// invalidates the choice if a symlink has been retargeted in the meantime.
	target := input
	if target == "~" || strings.HasPrefix(target, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return m, err
		}
		target = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(target, "~"), "/"))
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, target)
	}
	target, err = filepath.EvalSymlinks(filepath.Clean(target))
	if err != nil {
		return m, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return m, err
	}
	if !info.IsDir() {
		return m, errors.New("target is not a directory")
	}
	help := "Current directory: " + strconv.Quote(cwd) + "\nNew directory: " + strconv.Quote(target)
	choice := SessionCommandChoice{Label: "Change working directory", Help: help}
	if t.Status.Type == "idle" {
		choice.method, choice.params = "thread/settings/update", map[string]any{"threadId": t.ID, "cwd": target}
	} else {
		choice.Help += "\nAvailable when this session is idle."
	}
	m.Choices = append(m.Choices, choice)
	return m, nil
}

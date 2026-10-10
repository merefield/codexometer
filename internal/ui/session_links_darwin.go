package ui

import (
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// SSH sessions retain terminal-owned opening on the user's machine.
func localSessionWebLinkOpening() bool {
	return localSessionWebLinkEnvironment(os.Getenv)
}
func localSessionWebLinkEnvironment(getenv func(string) string) bool {
	return getenv("SSH_TTY") == "" && getenv("SSH_CONNECTION") == "" && getenv("SSH_CLIENT") == ""
}
func sessionWebLinkCommand(target string) *exec.Cmd {
	return exec.Command("/usr/bin/open", target)
}
func openSessionWebLink(target string) tea.Cmd {
	return func() tea.Msg { return sessionWebLinkOpenedMsg{err: sessionWebLinkCommand(target).Run()} }
}

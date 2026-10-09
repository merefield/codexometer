package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// Apple Terminal gained true colour in macOS 26, but existing profiles can
// still advertise xterm-256color. Respect explicit colour preferences and
// avoid inferring the capabilities of a remote host or terminal multiplexer.
func terminalProgramOptions(environ []string, appleTrueColor bool) []tea.ProgramOption {
	env := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	if !appleTrueColor || env["TERM_PROGRAM"] != "Apple_Terminal" ||
		env["TERM"] != "xterm-256color" || env["COLORTERM"] != "" ||
		env["NO_COLOR"] != "" || env["CLICOLOR"] == "0" ||
		env["TMUX"] != "" || env["STY"] != "" ||
		env["SSH_TTY"] != "" || env["SSH_CONNECTION"] != "" {
		return nil
	}
	return []tea.ProgramOption{tea.WithColorProfile(colorprofile.TrueColor)}
}

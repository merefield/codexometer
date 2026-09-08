package ui

import (
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const repositoryURL = "https://github.com/merefield/codexometer"

var headerLogo = []string{
	"█▀▀ █▀█ █▀▄ █▀▀ ▀▄▀ █▀█ █▀▄▀█ █▀▀ ▀█▀ █▀▀ █▀█",
	"█▄▄ █▄█ █▄▀ ██▄ █ █ █▄█ █ ▀ █ ██▄  █  ██▄ █▀▄",
}

// Match the visible header, including truncation by the right-hand labels.
func (m Model) headerActionAt(x, y int) string {
	x, y = x-2, y-1
	g := m.dashboardLayout()
	colors := paletteFor(m.theme)
	header := strings.Split(ansi.Strip(renderHeader(g.contentWidth, m.phase, m.renderSignalStatus(g.contentWidth, colors), m.renderAccount(colors), m.appVersion, colors)), "\n")
	if x < 0 || x >= g.contentWidth || y < 0 || y >= len(header) {
		return ""
	}
	if y < len(header)-1 {
		title := "▰ CODEXOMETER ▰"
		if g.contentWidth >= 64 {
			title = headerLogo[y]
		}
		visible := 0
		actual := []rune(header[y])
		for i, r := range []rune(title) {
			if i >= len(actual) || actual[i] != r {
				break
			}
			visible += lipgloss.Width(string(r))
		}
		if x < visible {
			return "home"
		}
		return ""
	}
	version := displayedHeaderVersion(m.appVersion)
	if i := strings.Index(header[y], version); i >= 0 {
		start := lipgloss.Width(header[y][:i])
		if x >= start && x < start+lipgloss.Width(version) {
			return "repository"
		}
	}
	return ""
}

// Use fixed arguments, never a shell or session-supplied URL.
func openRepository() tea.Msg {
	name, args := "xdg-open", []string{repositoryURL}
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", repositoryURL}
	}
	return repositoryOpenedMsg{err: exec.Command(name, args...).Run()}
}

type repositoryOpenedMsg struct{ err error }

func displayedHeaderVersion(version string) string {
	version = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(version, "v")))
	if version == "" {
		return "DEVELOPMENT"
	}
	return version
}

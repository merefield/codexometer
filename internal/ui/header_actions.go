package ui

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const repositoryURL = "https://github.com/merefield/codexometer"

var releaseVersionPattern = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)(?:$|[-+])`)

// Development builds link to their base release rather than a nonexistent
// Git-description tag. Unknown identities fall back to the releases list.
func versionHighlightsURL(version string) string {
	if match := releaseVersionPattern.FindStringSubmatch(strings.TrimSpace(version)); match != nil {
		return repositoryURL + "/releases/tag/v" + match[1]
	}
	return repositoryURL + "/releases"
}

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

// OSC 8 lets the terminal open the URL on the user's machine, including when
// Codexometer runs remotely or in WSL. Never invoke a host browser process.
func linkHeaderVersion(header, version string, hovered bool, colors palette) string {
	lines := strings.Split(header, "\n")
	i := len(lines) - 1
	if strings.Contains(lines[i], version) {
		label := version
		if hovered {
			label = colors.dimmed().Underline(true).Render(version)
		}
		lines[i] = strings.Replace(lines[i], version, ansi.SetHyperlink(versionHighlightsURL(version))+label+ansi.ResetHyperlink(), 1)
	}
	return strings.Join(lines, "\n")
}

func displayedHeaderVersion(version string) string {
	version = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(version, "v")))
	if version == "" {
		return "DEVELOPMENT"
	}
	return version
}

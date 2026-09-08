package ui

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const repositoryURL = "https://github.com/merefield/codexometer"

var releaseVersionPattern = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?)$`)
var developmentVersionSuffix = regexp.MustCompile(`(?:-[0-9]+-g[0-9a-f]+|-dev\+[0-9a-f]+(?:\.dirty)?)$`)

// Development builds link to their base release rather than a nonexistent
// Git-description tag. Unknown identities fall back to the releases list.
func versionHighlightsURL(version string) string {
	version = developmentVersionSuffix.ReplaceAllString(strings.TrimSuffix(strings.TrimSpace(version), "-dirty"), "")
	if match := releaseVersionPattern.FindStringSubmatch(version); match != nil {
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
	if x < 0 || y < 0 || y >= 3 {
		return ""
	}
	g := m.dashboardLayout()
	if x >= g.contentWidth || g.contentWidth < 64 && y >= 2 {
		return ""
	}
	colors := paletteFor(m.theme)
	account := ""
	if !m.loading || len(m.snapshot.Meters()) > 0 {
		account = m.renderAccount(colors)
	}
	header := strings.Split(ansi.Strip(renderHeader(g.contentWidth, m.phase, m.renderSignalStatus(g.contentWidth, colors), account, m.appVersion, colors)), "\n")
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
			return "version"
		}
	}
	return ""
}

// OSC 8 lets the terminal open the URL on the user's machine, including when
// Codexometer runs remotely or in WSL. Never invoke a host browser process.
func linkHeaderVersion(line, version string, hovered bool, colors palette) string {
	displayed := displayedHeaderVersion(version)
	if !strings.Contains(line, displayed) {
		return line
	}
	label := displayed
	if hovered {
		label = colors.dimmed().Underline(true).Render(displayed)
	}
	return strings.Replace(line, displayed, ansi.SetHyperlink(versionHighlightsURL(version))+label+ansi.ResetHyperlink(), 1)
}

func displayedHeaderVersion(version string) string {
	version = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(version, "v")))
	if version == "" {
		return "DEVELOPMENT"
	}
	return version
}

package ui

import "github.com/charmbracelet/lipgloss"

type themeID int

const (
	themeHacker themeID = iota
	themeRust
	themeBlueSteel
	themeUltraviolet
	themeNightshade
	themeCount
)

func (t themeID) next() themeID {
	return (t + 1) % themeCount
}

type meterStyleID int

const (
	styleBars meterStyleID = iota
	styleRotary
	stylePie
	styleTachometer
	styleCrashBar
	styleFuel
	styleFuse
	stylePac
	styleBoat
	styleCount
)

func (s meterStyleID) next() meterStyleID {
	return (s + 1) % styleCount
}

func (s meterStyleID) name() string {
	return [...]string{
		"BARS",
		"ROTARY",
		"PIE",
		"REV METER",
		"CRASH BAR",
		"FUEL TANK",
		"BURNING FUSE",
		"PELLET RUN",
		"SINKING SHIP",
	}[s]
}

type palette struct {
	name       string
	primary    lipgloss.Color
	dim        lipgloss.Color
	accent     lipgloss.Color
	warning    lipgloss.Color
	danger     lipgloss.Color
	background lipgloss.Color
}

func paletteFor(theme themeID) palette {
	switch theme {
	case themeRust:
		return palette{
			name:       "RUST",
			primary:    lipgloss.Color("#FFB454"),
			dim:        lipgloss.Color("#8A5428"),
			accent:     lipgloss.Color("#FFD38A"),
			warning:    lipgloss.Color("#FF8A3D"),
			danger:     lipgloss.Color("#FF5F56"),
			background: lipgloss.Color("#1A0E05"),
		}
	case themeBlueSteel:
		return palette{
			name:       "BLUE STEEL",
			primary:    lipgloss.Color("#76B7E5"),
			dim:        lipgloss.Color("#425B76"),
			accent:     lipgloss.Color("#B7E3FF"),
			warning:    lipgloss.Color("#E8C46A"),
			danger:     lipgloss.Color("#FF6B83"),
			background: lipgloss.Color("#08111B"),
		}
	case themeUltraviolet:
		return palette{
			name:       "ULTRAVIOLET",
			primary:    lipgloss.Color("#C084FC"),
			dim:        lipgloss.Color("#68428F"),
			accent:     lipgloss.Color("#F0ABFC"),
			warning:    lipgloss.Color("#F9A8D4"),
			danger:     lipgloss.Color("#FF668F"),
			background: lipgloss.Color("#12081C"),
		}
	case themeNightshade:
		return palette{
			name:       "NIGHTSHADE",
			primary:    lipgloss.Color("#A970FF"),
			dim:        lipgloss.Color("#6840A6"),
			accent:     lipgloss.Color("#D0A7FF"),
			warning:    lipgloss.Color("#8F7CFF"),
			danger:     lipgloss.Color("#FF668F"),
			background: lipgloss.Color("#180D29"),
		}
	case themeHacker:
		return palette{
			name:       "HACKER",
			primary:    lipgloss.Color("#57FF8A"),
			dim:        lipgloss.Color("#238F4A"),
			accent:     lipgloss.Color("#67E8F9"),
			warning:    lipgloss.Color("#FFCA58"),
			danger:     lipgloss.Color("#FF5F6D"),
			background: lipgloss.Color("#07130B"),
		}
	default:
		return paletteFor(themeHacker)
	}
}

func (p palette) header() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(p.primary)
}

func (p palette) dimmed() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.dim)
}

func (p palette) label() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(p.accent)
}

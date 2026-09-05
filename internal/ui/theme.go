package ui

import (
	imagecolor "image/color"

	"charm.land/lipgloss/v2"
)

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

type meterViewID int

const (
	viewBars meterViewID = iota
	viewMonitor
	viewPie
	viewConsumptionPace
	viewFuel
	viewBenchmark
	viewUsage
	viewCount
)

var quotaViewOrder = [...]meterViewID{
	viewBars,
	viewConsumptionPace,
	viewPie,
	viewFuel,
}

func (s meterViewID) isQuota() bool {
	for _, view := range quotaViewOrder {
		if s == view {
			return true
		}
	}
	return false
}

func (s meterViewID) nextQuota() meterViewID {
	for index, view := range quotaViewOrder {
		if s == view {
			return quotaViewOrder[(index+1)%len(quotaViewOrder)]
		}
	}
	return viewBars
}

func (s meterViewID) name() string {
	return [...]string{
		"BARS",
		"MONITOR",
		"PIE",
		"CONSUMPTION PACE",
		"FUEL TANK",
		"BENCHMARK",
		"USAGE",
	}[s]
}

type palette struct {
	name       string
	primary    imagecolor.Color
	dim        imagecolor.Color
	accent     imagecolor.Color
	success    imagecolor.Color
	successDim imagecolor.Color
	warning    imagecolor.Color
	warningDim imagecolor.Color
	danger     imagecolor.Color
	background imagecolor.Color
}

func paletteFor(theme themeID) palette {
	switch theme {
	case themeRust:
		return palette{
			name:       "RUST",
			primary:    lipgloss.Color("#FFB454"),
			dim:        lipgloss.Color("#8A5428"),
			accent:     lipgloss.Color("#FFD38A"),
			success:    lipgloss.Color("#67D391"),
			successDim: lipgloss.Color("#285B3B"),
			warning:    lipgloss.Color("#FF8A3D"),
			warningDim: lipgloss.Color("#713817"),
			danger:     lipgloss.Color("#FF5F56"),
			background: lipgloss.Color("#1A0E05"),
		}
	case themeBlueSteel:
		return palette{
			name:       "BLUE STEEL",
			primary:    lipgloss.Color("#76B7E5"),
			dim:        lipgloss.Color("#425B76"),
			accent:     lipgloss.Color("#B7E3FF"),
			success:    lipgloss.Color("#57D68D"),
			successDim: lipgloss.Color("#25583D"),
			warning:    lipgloss.Color("#E8C46A"),
			warningDim: lipgloss.Color("#6D5B2F"),
			danger:     lipgloss.Color("#FF6B83"),
			background: lipgloss.Color("#08111B"),
		}
	case themeUltraviolet:
		return palette{
			name:       "ULTRAVIOLET",
			primary:    lipgloss.Color("#C084FC"),
			dim:        lipgloss.Color("#68428F"),
			accent:     lipgloss.Color("#F0ABFC"),
			success:    lipgloss.Color("#65D98B"),
			successDim: lipgloss.Color("#28583A"),
			warning:    lipgloss.Color("#F9A8D4"),
			warningDim: lipgloss.Color("#754C63"),
			danger:     lipgloss.Color("#FF668F"),
			background: lipgloss.Color("#12081C"),
		}
	case themeNightshade:
		return palette{
			name:       "NIGHTSHADE",
			primary:    lipgloss.Color("#A970FF"),
			dim:        lipgloss.Color("#6840A6"),
			accent:     lipgloss.Color("#D0A7FF"),
			success:    lipgloss.Color("#62D990"),
			successDim: lipgloss.Color("#28583C"),
			warning:    lipgloss.Color("#8F7CFF"),
			warningDim: lipgloss.Color("#493F80"),
			danger:     lipgloss.Color("#FF668F"),
			background: lipgloss.Color("#180D29"),
		}
	case themeHacker:
		return palette{
			name:       "HACKER",
			primary:    lipgloss.Color("#57FF8A"),
			dim:        lipgloss.Color("#238F4A"),
			accent:     lipgloss.Color("#67E8F9"),
			success:    lipgloss.Color("#00C853"),
			successDim: lipgloss.Color("#006B2D"),
			warning:    lipgloss.Color("#FFCA58"),
			warningDim: lipgloss.Color("#765D26"),
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

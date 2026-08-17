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
	}[s]
}

type palette struct {
	name       string
	primary    imagecolor.Color
	dim        imagecolor.Color
	accent     imagecolor.Color
	warning    imagecolor.Color
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

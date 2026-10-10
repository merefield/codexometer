package ui

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	startupFrameInterval = time.Second / 30
	startupEntrance      = 900 * time.Millisecond
	startupHold          = 180 * time.Millisecond
	startupDock          = 800 * time.Millisecond
	startupDuration      = startupEntrance + startupHold + startupDock
)

type startupState struct {
	pending bool
	started time.Time
	elapsed time.Duration
}

func (s startupState) active() bool { return !s.started.IsZero() }

type startupFrameMsg struct{ started, at time.Time }

func startupTick(started time.Time) tea.Cmd {
	return tea.Tick(startupFrameInterval, func(now time.Time) tea.Msg {
		return startupFrameMsg{started: started, at: now}
	})
}

func (m *Model) beginStartup(now time.Time) tea.Cmd {
	// Resize recomputes geometry, but never restarts or adds a second tick chain.
	if m.inline || m.width < 68 || m.height < 8 {
		if m.width > 0 && m.height > 0 {
			m.startup = startupState{}
		}
		return nil
	}
	if !m.startup.pending {
		return nil
	}
	m.startup = startupState{started: now}
	return startupTick(now)
}

func (m Model) updateStartup(message tea.Msg) (Model, tea.Cmd, bool) {
	if tick, ok := message.(startupFrameMsg); ok {
		if !m.startup.active() || tick.started != m.startup.started {
			return m, nil, true
		}
		// Wall-clock progress catches up after slow frames instead of delaying startup.
		m.startup.elapsed = max(m.startup.elapsed, tick.at.Sub(m.startup.started))
		if m.startup.elapsed >= startupDuration {
			m.startup = startupState{}
			return m, nil, true
		}
		return m, startupTick(m.startup.started), true
	}
	if !m.startup.active() {
		return m, nil, false
	}
	switch message := message.(type) {
	case tea.KeyPressMsg:
		m.startup = startupState{}
		if message.String() == "ctrl+c" || strings.EqualFold(message.String(), "q") {
			m.cancelBenchmark()
			return m, tea.Quit, true
		}
		// Consume the skip key so it cannot activate an unseen dashboard action.
		return m, nil, true
	case tea.PasteMsg:
		m.startup = startupState{}
		return m, nil, true
	case tea.MouseMsg:
		if click, ok := message.(tea.MouseClickMsg); ok && click.Mouse().Button == tea.MouseLeft {
			m.startup = startupState{}
		}
		return m, nil, true
	}
	// Fetches, telemetry, timers and initial view loading keep their normal handlers.
	return m, nil, false
}

// Decode the existing two-row half-block lettering into four pixel rows. The
// same bitmap scales up for the entrance and returns exactly to the header art.
func startupLogoPixels() [][]bool {
	width := len([]rune(headerLogo[0]))
	pixels := make([][]bool, len(headerLogo)*2)
	for row, line := range headerLogo {
		pixels[row*2], pixels[row*2+1] = make([]bool, width), make([]bool, width)
		for col, cell := range []rune(line) {
			pixels[row*2][col] = cell == '█' || cell == '▀'
			pixels[row*2+1][col] = cell == '█' || cell == '▄'
		}
	}
	return pixels
}

var startupLogoBitmap = startupLogoPixels()

type startupGeometry struct{ x, y, width, pixelHeight int }

func (m Model) startupGeometry() startupGeometry {
	width := max(m.width, 1)
	baseWidth := len(startupLogoBitmap[0])
	pixels := min(max(int(math.Round(float64(len(startupLogoBitmap))*float64(width)/float64(baseWidth))), 4), max((m.height-2)*2, 4))
	g := startupGeometry{width: width, pixelHeight: pixels, y: max((m.height-(pixels+1)/2)/2, 0)}
	elapsed := m.startup.elapsed
	if elapsed < startupEntrance {
		progress := float64(max(elapsed, 0)) / float64(startupEntrance)
		g.x = int(math.Round(float64(width) * math.Pow(1-progress, 3)))
	} else if elapsed > startupEntrance+startupHold {
		progress := min(float64(elapsed-startupEntrance-startupHold)/float64(startupDock), 1)
		eased := progress * progress * (3 - 2*progress)
		g.width = int(math.Round(float64(width) + (float64(baseWidth)-float64(width))*eased))
		g.pixelHeight = int(math.Round(float64(pixels) + (float64(len(startupLogoBitmap))-float64(pixels))*eased))
		g.x = int(math.Round(2 * eased))
		g.y = int(math.Round(float64(g.y) + (1-float64(g.y))*eased))
	}
	return g
}

func scaledStartupLogo(width, pixelHeight int) []string {
	width, pixelHeight = max(width, 1), max(pixelHeight, 1)
	sourceHeight, sourceWidth := len(startupLogoBitmap), len(startupLogoBitmap[0])
	rows := make([]string, (pixelHeight+1)/2)
	for row := range rows {
		var line strings.Builder
		for col := range width {
			sourceX := col * sourceWidth / width
			top := startupLogoBitmap[(row*2)*sourceHeight/pixelHeight][sourceX]
			bottom := row*2+1 < pixelHeight && startupLogoBitmap[(row*2+1)*sourceHeight/pixelHeight][sourceX]
			switch {
			case top && bottom:
				line.WriteRune('█')
			case top:
				line.WriteRune('▀')
			case bottom:
				line.WriteRune('▄')
			default:
				line.WriteByte(' ')
			}
		}
		rows[row] = line.String()
	}
	return rows
}

func (m Model) renderStartup() string {
	g := m.startupGeometry()
	logo := scaledStartupLogo(g.width, g.pixelHeight)
	rows := make([]string, max(m.height, 1))
	colors := paletteFor(m.theme)
	for row := range rows {
		rows[row] = strings.Repeat(" ", max(m.width, 1))
		logoRow := row - g.y
		if logoRow < 0 || logoRow >= len(logo) || g.x >= m.width {
			continue
		}
		// Each block occupies one terminal cell. Clip before styling to prevent any
		// incoming frame wrapping onto the next row or leaving stray background cells.
		cells := []rune(logo[logoRow])
		visible := min(len(cells), m.width-g.x)
		rows[row] = strings.Repeat(" ", g.x) + colors.header().Render(string(cells[:visible])) + strings.Repeat(" ", m.width-g.x-visible)
	}
	return strings.Join(rows, "\n")
}

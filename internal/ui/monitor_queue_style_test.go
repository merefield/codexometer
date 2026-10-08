package ui

import (
	"image"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestQueuePanelThemeAndGeometry(t *testing.T) {
	m, _ := queueTestModel(false)
	for theme := themeID(0); theme < themeCount; theme++ {
		colors := paletteFor(theme)
		for _, width := range []int{34, 60, 120} {
			m.monitorQueue.focused = true
			m.monitorQueue.offset = 0
			m.monitorContextHover = ""
			output := m.renderMonitorQueue(width, 3, colors)
			lines := strings.Split(ansi.Strip(output), "\n")
			if len(lines) != 3 || !strings.HasPrefix(lines[0], "─ FOLLOW-UPS") || !strings.HasPrefix(lines[1], "› QUEUED") {
				t.Fatal(lines)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != width-4 {
					t.Fatal("panel width changed", width, line)
				}
			}
			cells := uv.NewScreenBuffer(width-4, 3)
			uv.NewStyledString(output).Draw(&cells, image.Rect(0, 0, width-4, 3))
			for y := 0; y < 3; y++ {
				tint := monitorTintBackground(colors, 6)
				if y == 1 {
					tint = monitorTintBackground(colors, 18)
				}
				for x := 0; x < width-4; x++ {
					cell := cells.CellAt(x, y)
					if cell == nil || cell.Style.Bg == nil {
						t.Fatalf("untinted cell %d,%d", x, y)
					}
					r, g, b, a := cell.Style.Bg.RGBA()
					wr, wg, wb, wa := tint.RGBA()
					if r != wr || g != wg || b != wb || a != wa {
						t.Fatalf("wrong background theme %d at %d,%d", theme, x, y)
					}
				}
				// The queue stays readable at full theme colour without giving
				// every pending item the weight of the selected row.
				cell := cells.CellAt(0, y)
				if bold := cell.Style.Attrs&uv.AttrBold != 0; bold != (y == 1) {
					t.Fatalf("wrong queue emphasis theme %d row %d", theme, y)
				}
				foreground := colors.accent
				if y < 2 {
					foreground = colors.primary
				}
				if cell.Style.Fg == nil {
					t.Fatal("queue text lost its theme colour")
				}
				r, g, b, a := cell.Style.Fg.RGBA()
				wr, wg, wb, wa := foreground.RGBA()
				if r != wr || g != wg || b != wb || a != wa {
					t.Fatalf("queue text was dimmed theme %d row %d", theme, y)
				}
			}
			entry := m.followupEntries()[0]
			buttons := followupButtons(entry, width)
			for _, button := range buttons {
				if got := ansi.Cut(lines[1], button.x, button.x+ansi.StringWidth(button.text)); got != button.text {
					t.Fatalf("button moved: %q vs %q", got, button.text)
				}
			}
		}
	}
}

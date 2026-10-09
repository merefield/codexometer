package ui

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
	"testing/synctest"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

// Use the same screen-buffer path as Bubble Tea to inspect the actual cells.
func backgroundTestLines(text string) []uv.Line {
	width, height := lipgloss.Width(text), lipgloss.Height(text)
	screen := uv.NewScreenBuffer(width, height)
	uv.NewStyledString(text).Draw(&screen, image.Rect(0, 0, width, height))
	plainLines := strings.Split(ansi.Strip(text), "\n")
	lines := make([]uv.Line, height)
	for y := 0; y < height; y++ {
		for x := 0; x < ansi.StringWidth(plainLines[y]); x++ {
			if x > 0 && screen.CellAt(x-1, y).Width > 1 {
				continue
			}
			lines[y] = append(lines[y], *screen.CellAt(x, y))
		}
	}
	return lines
}

func assertBackground(t *testing.T, got, want color.Color) {
	t.Helper()
	if got == nil || want == nil {
		t.Fatalf("missing background: got %v want %v", got, want)
	}
	r, g, b, a := got.RGBA()
	wr, wg, wb, wa := want.RGBA()
	if r != wr || g != wg || b != wb || a != wa {
		t.Fatalf("background = %v, want %v", got, want)
	}
}

func TestPanelBackgroundSurvivesNestedStyles(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		c := paletteFor(theme)
		body := c.label().Render("label") + "  " + c.dimmed().Render("value") + " tail"
		out := frameSized(36, 6, "PANEL", body, c.primary, c)
		for _, line := range backgroundTestLines(out) {
			for _, cell := range line {
				assertBackground(t, cell.Style.Bg, c.background)
			}
		}
	}
}

func TestBackgroundFillPreservesStylesAndLinks(t *testing.T) {
	c := paletteFor(themeHacker)
	link := ansi.SetHyperlink("https://example.com", "")
	input := c.label().Render("A") + " " + link +
		c.label().Background(c.primary).Render("B") + ansi.ResetHyperlink() +
		"\x1b[0;38;2;0;49;0mC\x1b[49mD\x1b[48:2::0:49:0mE\x1b[mF"
	out := withDefaultBackground(input, c.background)
	if ansi.Strip(out) != "A BCDEF" {
		t.Fatal("background fill changed text", ansi.Strip(out))
	}
	cells := backgroundTestLines(out)[0]
	for _, index := range []int{0, 1, 3, 4, 6} {
		assertBackground(t, cells[index].Style.Bg, c.background)
	}
	assertBackground(t, cells[2].Style.Bg, c.primary)
	assertBackground(t, cells[5].Style.Bg, color.RGBA{0, 49, 0, 255})
	if cells[0].Style.Attrs&uv.AttrBold == 0 || cells[2].Link.URL != "https://example.com" || cells[3].Link.URL != "" {
		t.Fatal("background fill changed text attributes or hyperlink boundaries")
	}
	following := backgroundTestLines(out + "Z")[0]
	if following[len(following)-1].Style.Bg != nil {
		t.Fatal("background leaked outside rendered component")
	}
}

func TestDashboardCanvasBackgroundAndInlineBounds(t *testing.T) {
	// Compare identical observations: two renders must not cross a real-clock
	// countdown boundary and masquerade as an inline padding regression.
	synctest.Test(t, func(t *testing.T) {
		for theme := themeHacker; theme < themeCount; theme++ {
			for _, size := range [][2]int{{40, 16}, {80, 24}, {120, 40}} {
				for view := viewBars; view < viewCount; view++ {
					m := Model{snapshot: codex.DemoSnapshot(), width: size[0], height: size[1], theme: theme, meterView: view}
					output := m.View().Content
					lines := backgroundTestLines(output)
					if len(lines) != m.height || lipgloss.Width(output) != m.width {
						t.Fatalf("canvas geometry changed: theme %d view %d at %v", theme, view, size)
					}
					for y, line := range lines {
						for x, cell := range line {
							if cell.Style.Bg == nil {
								t.Fatalf("unpainted canvas cell: theme %d view %d at %d,%d", theme, view, x, y)
							}
						}
					}
					m.inline = true
					if ansi.Strip(m.View().Content) != ansi.Strip(m.render()) {
						t.Fatalf("inline rendering gained full-screen padding: theme %d view %d at %v", theme, view, size)
					}
					if m.View().BackgroundColor != nil {
						t.Fatal("rendering changed the terminal profile background")
					}
				}
			}
		}
	})
}

func TestMonitorButtonBlankRowsHaveBackground(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		c := paletteFor(theme)
		for _, flashed := range []bool{false, true} {
			m := Model{}
			if flashed {
				m.flashedButton = footerButtonMonitorReset
			}
			lines := backgroundTestLines(m.renderMonitorButton(14, 6, "RESET", footerButtonMonitorReset, true, c))
			for y, line := range lines {
				for x, cell := range line {
					want := c.background
					if flashed && y > 0 && y < len(lines)-1 && x > 0 && x < len(line)-1 {
						want = c.primary
					}
					assertBackground(t, cell.Style.Bg, want)
				}
			}
		}
	}
}

func TestApprovalBadgeOpensVisibleDecisionControls(t *testing.T) {
	for theme := themeHacker; theme < themeCount; theme++ {
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
			m := approvalTestModel()
			m.theme, m.width, m.height = theme, 100, 30
			m.monitorSessionData[0].attention = codex.SessionAttentionApproval
			m.setRowContext("root-one", contextSplit)
			m.openMonitorAttention("attention:root-one")
			if m.monitorContextDetail != "root-one" || m.monitorApprovalConfirm != "" || m.monitorApprovalBusy {
				t.Fatal("badge did not open review without approving")
			}
			buttons := m.visibleMonitorApprovalButtons()
			if len(buttons) == 0 {
				t.Fatal("badge opened a review without decision controls")
			}
			var converted bytes.Buffer
			writer := colorprofile.Writer{Forward: &converted, Profile: profile}
			if _, err := writer.Write([]byte(m.View().Content)); err != nil {
				t.Fatal(err)
			}
			lines := backgroundTestLines(converted.String())
			for _, b := range buttons {
				found := false
				for _, line := range lines {
					var plain strings.Builder
					for _, cell := range line {
						plain.WriteString(cell.Content)
					}
					index := strings.Index(plain.String(), b.label)
					if index < 0 {
						continue
					}
					found = true
					x := ansi.StringWidth(plain.String()[:index])
					for _, cell := range line[x : x+ansi.StringWidth(b.label)] {
						assertBackground(t, cell.Style.Bg, profile.Convert(paletteFor(theme).primary))
						if cell.Style.Fg == nil {
							t.Fatal("decision label has no foreground")
						}
						fr, fg, fb, _ := cell.Style.Fg.RGBA()
						br, bg, bb, _ := cell.Style.Bg.RGBA()
						if fr == br && fg == bg && fb == bb {
							t.Fatal("decision label blends into its background")
						}
					}
				}
				if !found {
					t.Fatalf("decision %s missing from review", b.label)
				}
			}
		}
	}
}

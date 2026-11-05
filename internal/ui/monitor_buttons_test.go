package ui

import (
	"slices"
	"strings"
	"testing"
)

func TestMonitorButtonRowsPreservePositionsAndVisibleWidths(t *testing.T) {
	if got := renderMonitorButtonRows(nil, palette{}, nil); got != "" {
		t.Fatalf("empty controls = %q", got)
	}
	buttons := []monitorApprovalButton{
		{action: "first", label: "界", x: 1, y: 0},
		{action: "second", label: "B", x: 6, y: 0},
		{action: "third", label: "C", x: 2, y: 2},
	}
	var actions []string
	got := renderMonitorButtonRows(buttons, palette{}, func(action, label string, _ palette) string {
		actions = append(actions, action)
		return "\x1b[31m" + label + "\x1b[0m"
	})
	want := " \x1b[31m界\x1b[0m   \x1b[31mB\x1b[0m\n\n  \x1b[31mC\x1b[0m"
	if got != want || !slices.Equal(actions, []string{"first", "second", "third"}) {
		t.Fatalf("positioned controls = %q, actions = %v; want %q", got, actions, want)
	}
}

func TestMonitorButtonRowsKeepApprovalAndProfileRenderers(t *testing.T) {
	approval := approvalTestModel()
	profile, _ := profileTestModel(t)
	for _, theme := range []themeID{themeHacker, themeRust, themeBlueSteel, themeUltraviolet, themeNightshade} {
		colors := paletteFor(theme)
		for _, width := range []int{20, 50, 120} {
			for _, tc := range []struct {
				buttons []monitorApprovalButton
				render  func(string, string, palette) string
				output  string
			}{
				{approval.monitorApprovalButtons(width, 40), approval.renderMonitorApprovalAction, approval.renderMonitorApprovalControls(width, 40, colors)},
				{profile.profileButtons(width, 40, profile.monitorSessionData[0]), profile.renderContextAction, profile.renderProfileControls(profile.profileButtons(width, 40, profile.monitorSessionData[0]), colors)},
			} {
				if width == 120 && len(tc.buttons) == 0 {
					t.Fatal("wide fixture must exercise actual controls")
				}
				if len(tc.buttons) == 0 && tc.output != "" {
					t.Fatal("hidden controls rendered")
				}
				for _, button := range tc.buttons {
					if !strings.Contains(tc.output, tc.render(button.action, button.label, colors)) {
						t.Fatal("button lost its original renderer", theme, width, button.action)
					}
				}
			}
		}
	}
}

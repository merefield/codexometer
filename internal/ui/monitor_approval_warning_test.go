package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorApprovalWarningTransition(t *testing.T) {
	for _, failed := range []bool{false, true} {
		m := approvalTestModel()
		m.setRowContext("root-one", contextWide)
		assertWarning := func(model Model, want bool) {
			t.Helper()
			got := false
			for _, b := range model.expandedContextNavigation(160, 30, model.monitorSessionData[0]) {
				got = got || b.action == "detail:root-one"
			}
			if got != want {
				t.Fatalf("warning = %v, want %v (failed send: %v)", got, want, failed)
			}
		}
		assertWarning(m, false)
		m, _, _ = m.monitorApprovalAction("decision:0")
		assertWarning(m, false) // confirmation still fits
		previous := m
		m, cmd, _ := m.monitorApprovalAction("decision:0")
		assertWarning(m, false) // sending
		result := cmd().(monitorApprovalResult)
		// Even an older UI snapshot must not offer approval for the consumed
		// request, including when the session is no longer the selected target.
		assertWarning(previous, false)
		previous.monitorContextExpanded = ""
		assertWarning(previous, false)
		if failed {
			result.err = errors.New("ambiguous write")
		}
		next, _ := m.Update(result)
		m = next.(Model)
		assertWarning(m, false)
		// A genuinely new request must not be hidden by the previous outcome.
		m.fetcher.(*approvalTestClient).token = "replacement"
		m.monitorSessionData[0].preview.ApprovalToken = "replacement"
		m.monitorSessionData[0].preview.Text = strings.Repeat("long request\n", 100)
		assertWarning(m, true)
		// Tokenless local/unsupported approvals retain their diagnostic link.
		m.monitorSessionData[0].preview.ApprovalToken = ""
		m.monitorSessionData[0].preview.Source = "LOCAL"
		assertWarning(m, true)
	}
}

func warningRowLayout(m Model) (int, int, int) {
	g := m.dashboardLayout()
	a := layoutMonitorArea(g.contentWidth, g.meterHeight)
	_, heights, _ := m.monitorSessionPage(a.graphHeight)
	mw, cw, _ := monitorSessionColumnWidths(a.width)
	return cw, heights[0], mw + 3
}

func TestMonitorApprovalWarningResponsiveClicks(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		m := approvalTestModel()
		m.width = width
		m.setRowContext("root-one", contextWide)
		m.monitorSessionData[0].preview.Text = strings.Repeat("long command justification\n", 100)
		cw, h, x0 := warningRowLayout(m)
		s := m.monitorSessionData[0]
		buttons := m.expandedContextNavigation(cw, h, s)
		if len(buttons) == 0 || buttons[0].action != "detail:root-one" {
			t.Fatalf("width %d missing warning", width)
		}
		warning := buttons[0]
		out := ansi.Strip(m.render())
		if !strings.Contains(out, warning.label) || lipgloss.Width(out) > width {
			t.Fatalf("warning missing/overflow at %d", width)
		}
		g := m.dashboardLayout()
		a := layoutMonitorArea(g.contentWidth, g.meterHeight)
		y := g.meterY + a.topHeight + a.gap - 1
		for dx := 0; dx < warning.rect.width; dx++ {
			x := x0 + warning.rect.x + dx
			if got := m.monitorContextAt(x, y); got != warning.action {
				t.Fatalf("warning click %d,%d = %q", x, y, got)
			}
			n, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			next := n.(Model)
			if cmd != nil || next.monitorContextDetail != "root-one" || next.monitorApprovalConfirm != "" {
				t.Fatal("warning did not safely open detail")
			}
		}
		m.monitorSessionData[0].preview.Kind = codex.SessionContextReply
		for _, b := range m.expandedContextNavigation(cw, h, m.monitorSessionData[0]) {
			if strings.HasPrefix(b.action, "detail:") {
				t.Fatal("ordinary reply has approval warning")
			}
		}
	}
}

func TestMonitorApprovalWarningOpensMissingContext(t *testing.T) {
	m := approvalTestModel()
	m.setRowContext("root-one", contextWide)
	m.monitorSessionData[0].preview.Text = ""
	m.monitorSelectedID = "root-two"
	m.monitorContextExpanded = ""
	m.openMonitorContext("root-one")
	if m.monitorContextDetail != "root-one" {
		t.Fatal("approval with missing text could not open its diagnostic detail")
	}
}

func TestMonitorApprovalButtonsReappearAfterDismissal(t *testing.T) {
	// Find a real responsive layout where removing one row gives this request
	// enough room; translated button labels may require different heights.
	var m Model
	found := false
	for height := 24; height <= 64; height++ {
		candidate := approvalTestModel()
		candidate.width = 180
		candidate.height = height
		candidate.monitorSessionData = candidate.monitorSessionData[:2]
		candidate.monitorSessionData[0].preview.Text = strings.Repeat("Justification line\n", 6) + "Command: git push\nDirectory: /work"
		candidate.setRowContext("root-one", contextWide)
		cw, h, _ := warningRowLayout(candidate)
		if len(candidate.expandedApprovalButtons(cw, h, candidate.monitorSessionData[0])) != 0 {
			continue
		}
		larger := candidate
		larger.monitorSessionData = larger.monitorSessionData[:1]
		cw, h, _ = warningRowLayout(larger)
		if len(larger.expandedApprovalButtons(cw, h, larger.monitorSessionData[0])) > 0 {
			m = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no threshold-crossing layout found")
	}
	cw, h, _ := warningRowLayout(m)
	if m.expandedContextNavigation(cw, h, m.monitorSessionData[0])[0].action != "detail:root-one" {
		t.Fatal("initial warning absent")
	}
	clicked := false
	for y, line := range strings.Split(ansi.Strip(m.render()), "\n") {
		pos := strings.Index(line, monitorDismissLabel)
		if pos < 0 {
			continue
		}
		x := lipgloss.Width(line[:pos])
		if id, ok := m.monitorSessionDismissAt(x, y); ok && id == "root-two" {
			n, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			m = n.(Model)
			// Dismiss waits for its button flash before removing the row.
			n, _ = m.Update(monitorSessionDismissMsg{id: "root-two", sequence: m.monitorDismissSeq})
			m = n.(Model)
			clicked = true
			break
		}
	}
	if !clicked {
		t.Fatal("could not click the other session's dismiss button")
	}
	cw, h, _ = warningRowLayout(m)
	buttons := m.expandedApprovalButtons(cw, h, m.monitorSessionData[0])
	if len(buttons) == 0 {
		t.Fatal("approval controls did not reappear after dismissal")
	}
	for _, b := range m.expandedContextNavigation(cw, h, m.monitorSessionData[0]) {
		if strings.HasPrefix(b.action, "detail:") {
			t.Fatal("warning retained despite available controls")
		}
	}
	out := ansi.Strip(m.render())
	for _, b := range buttons {
		found := false
		for y, line := range strings.Split(out, "\n") {
			pos := strings.Index(line, b.label)
			if pos < 0 {
				continue
			}
			found = true
			x := lipgloss.Width(line[:pos])
			for dx := 0; dx < lipgloss.Width(b.label); dx++ {
				if m.monitorContextAt(x+dx, y) != b.action {
					t.Fatal("resized approval hit target is stale")
				}
			}
		}
		if !found {
			t.Fatal("approval button not rendered")
		}
	}
	// Shrinking the window immediately restores the warning and hides controls.
	n, _ := m.Update(tea.WindowSizeMsg{Width: m.width, Height: 16})
	m = n.(Model)
	cw, h, _ = warningRowLayout(m)
	if len(m.expandedApprovalButtons(cw, h, m.monitorSessionData[0])) != 0 || m.expandedContextNavigation(cw, h, m.monitorSessionData[0])[0].action != "detail:root-one" {
		t.Fatal("shrink did not restore safety warning")
	}
}

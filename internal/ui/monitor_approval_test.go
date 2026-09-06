package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

type approvalTestClient struct{ token, decision string }

func (c *approvalTestClient) Fetch(context.Context) (codex.Snapshot, error) {
	return codex.DemoSnapshot(), nil
}
func (c *approvalTestClient) SessionApprovalPending(token string) bool {
	return token != "" && token == c.token
}
func (c *approvalTestClient) RespondSessionApproval(_ context.Context, token, decision string) error {
	c.decision = decision
	c.token = ""
	return nil
}

func approvalTestModel() Model {
	m := contextTestModel()
	m.fetcher = &approvalTestClient{token: "live"}
	m.monitorSessionData[0].preview = codex.SessionContext{Kind: codex.SessionContextApproval, Text: "Command: git push\nDirectory: /work", Source: "LIVE", ApprovalToken: "live"}
	m.openMonitorContext(m.monitorSessionData[0].id)
	return m
}

func TestMonitorApprovalRenderedTargets(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 180} {
		for _, height := range []int{12, 24, 40} {
			for _, confirmed := range []bool{false, true} {
				m := approvalTestModel()
				m.width = width
				m.height = height
				if confirmed {
					m.monitorApprovalConfirm = "live"
				}
				out := m.render()
				if lipgloss.Width(out) > width || lipgloss.Height(out) > height {
					t.Fatalf("overflow %dx%d", width, height)
				}
				a, b := m.monitorApprovalLabels()
				g := m.dashboardLayout()
				shown := m.monitorApprovalControls(g.contentWidth, g.meterHeight)
				for label, action := range map[string]string{a: "approve", b: "decline"} {
					found := false
					for y, line := range strings.Split(ansi.Strip(out), "\n") {
						i := strings.Index(line, label)
						if i < 0 {
							continue
						}
						found = true
						x := lipgloss.Width(line[:i])
						for dx := 0; dx < lipgloss.Width(label); dx++ {
							if got := m.monitorContextAt(x+dx, y); got != action {
								t.Fatalf("%s miss at %d,%d got %q\n%s", action, x+dx, y, got, ansi.Strip(out))
							}
						}
						updated, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
						u := updated.(Model)
						if action == "approve" && !confirmed {
							if cmd != nil || u.monitorApprovalConfirm != "live" {
								t.Fatal("first click must only confirm")
							}
						} else if cmd == nil || !u.monitorApprovalBusy {
							t.Fatal("decision was not queued")
						}
					}
					if found != shown {
						t.Fatalf("render/hit visibility mismatch %dx%d", width, height)
					}
				}
			}
		}
	}
}

func TestMonitorApprovalStaleAndKeyboardSafety(t *testing.T) {
	m := approvalTestModel()
	m, cmd, _ := m.monitorApprovalAction("approve")
	if cmd != nil {
		t.Fatal("first click sent decision")
	}
	client := m.fetcher.(*approvalTestClient)
	client.token = "replacement"
	m, cmd, _ = m.monitorApprovalAction("approve")
	if cmd != nil || m.monitorApprovalConfirm != "" {
		t.Fatal("stale confirmation submitted")
	}
	client.token = "live"
	for _, key := range []string{"a", "y", "enter"} {
		copy := m
		_, cmd, _ := copy.updateMonitorContextKey(key)
		if cmd != nil || client.decision != "" {
			t.Fatal("keyboard approved")
		}
	}
	m.monitorContextHidden = true
	if m.monitorApprovalToken() != "" {
		t.Fatal("hidden preview actionable")
	}
	m.monitorContextHidden = false
	m.monitorSessionData[0].preview.ApprovalToken = ""
	if m.monitorApprovalToken() != "" {
		t.Fatal("local request actionable")
	}
}

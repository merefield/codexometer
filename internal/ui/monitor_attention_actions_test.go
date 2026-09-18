package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type profileApprovalTestFetcher struct {
	*quotaStepTestFetcher
	approvalTestClient
}

func (f *profileApprovalTestFetcher) Fetch(ctx context.Context) (codex.Snapshot, error) {
	return f.quotaStepTestFetcher.Fetch(ctx)
}

func TestSessionPillsStackAndOpenDistinctReviews(t *testing.T) {
	m, quota := profileTestModel(t)
	f := &profileApprovalTestFetcher{quotaStepTestFetcher: quota, approvalTestClient: approvalTestClient{token: "live"}}
	m.fetcher = f
	m.width, m.height = 240, 65
	m.monitorSessionData[0].attention = codex.SessionAttentionApproval
	m.monitorSessionData[0].preview = approvalTestModel().monitorSessionData[0].preview
	items := m.monitorAttentionSessions()
	if len(items) != 3 || items[0].action() != "attention:one" || items[1].action() != "attention-profile:one" || items[2].action() != "attention-profile:two" {
		t.Fatalf("expected distinct native and profile pills: %#v", items)
	}

	click := func(action string) {
		t.Helper()
		g := m.dashboardLayout()
		summary, _, buttons := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
		for _, b := range buttons {
			if b.action != action {
				continue
			}
			x, y := 2+b.rect.x, g.meterY+summary+b.rect.y
			if got := m.monitorContextAt(x, y); got != action {
				t.Fatalf("hit %q, want %q", got, action)
			}
			next, cmd, handled := m.updateMonitorContextMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if cmd != nil || !handled {
				t.Fatal("navigation sent an action")
			}
			m = next
			return
		}
		t.Fatalf("missing pill %q", action)
	}

	click("attention-profile:one")
	if !m.hasSessionProfile(m.monitorSessionData[0]) || m.monitorApprovalToken() != "" || len(m.visibleMonitorApprovalButtons()) != 0 {
		t.Fatal("quota pill must show quota controls only")
	}
	if !strings.Contains(strings.Join(m.contextDetailLines(200), "\n"), i18n.Format("Your %d%% quota threshold has been reached. Approve the profile switch below for this session's next turns, or skip it.", 80)) {
		t.Fatal("wrong document")
	}
	buttons, _ := m.monitorAttentionButtons(236, 1)
	for _, b := range buttons {
		if strings.HasPrefix(b.label, ">") != (b.action == "attention-profile:one") {
			t.Fatalf("wrong active marker: %#v", b)
		}
	}
	m, _, _ = m.updateProfileKey("1")
	if !m.profileArmed("one") {
		t.Fatal("quota confirmation not armed")
	}
	click("attention:one")
	if m.profileArmed("one") || m.hasSessionProfile(m.monitorSessionData[0]) || m.monitorApprovalToken() != "live" {
		t.Fatal("native pill did not switch document/disarm quota confirmation")
	}
	if len(m.visibleProfileButtons()) != 0 || len(m.visibleMonitorApprovalButtons()) == 0 {
		t.Fatal("wrong controls for Codex approval")
	}
	if !strings.Contains(strings.Join(m.contextDetailLines(200), "\n"), "git push") {
		t.Fatal("approval command missing")
	}
	if quota.updates != 0 || f.decision != "" {
		t.Fatal("switching pills wrote settings or approved a command")
	}
}

func TestSessionPillCompletionSuppressionAndIndependentResolution(t *testing.T) {
	m, _ := profileTestModel(t)
	m.monitorSessionData = m.monitorSessionData[:1]
	m.monitorSessionData[0].attention = codex.SessionAttentionComplete
	if items := m.monitorAttentionSessions(); len(items) != 1 || !items[0].profile {
		t.Fatal("completion duplicates outstanding quota review")
	}
	m.monitorSessionData[0].attention = codex.SessionAttentionInput
	items := m.monitorAttentionSessions()
	if len(items) != 2 || items[0].profile || !items[1].profile {
		t.Fatal("input and quota should coexist")
	}
	m.openMonitorAttention("attention-profile:one")
	m.setRowContext("one", contextWide)
	if !m.hasSessionProfile(m.monitorSessionData[0]) {
		t.Fatal("leaving full detail lost chosen review")
	}
	m.declineQuotaSession("one")
	if items = m.monitorAttentionSessions(); len(items) != 1 || items[0].profile {
		t.Fatal("skipping quota removed native input pill")
	}
	m.monitorSessionData[0].attention = codex.SessionAttentionComplete
	if items = m.monitorAttentionSessions(); len(items) != 1 || items[0].profile {
		t.Fatal("completion did not return after quota was handled")
	}
}

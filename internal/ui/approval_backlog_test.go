package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func backlogModel() Model {
	m := attentionTestModel()
	m.monitorSessionData = []monitorSession{
		{id: "one", displayed: true, attention: codex.SessionAttentionApproval, preview: codex.SessionContext{PendingApprovals: 1, At: time.Unix(1, 0)}},
		{id: "two", displayed: true, attention: codex.SessionAttentionApproval, preview: codex.SessionContext{PendingApprovals: 3, At: time.Unix(2, 0)}},
	}
	return m
}

func TestApprovalBacklogLabelsAndGeometry(t *testing.T) {
	m := backlogModel()
	for _, width := range []int{40, 90, 180} {
		buttons, _ := m.monitorAttentionButtons(width, 2)
		if len(buttons) < 2 || buttons[0].action != "attention:two" {
			t.Fatalf("backlog order at width %d: %+v", width, buttons)
		}
		if !strings.Contains(buttons[0].label, "×3") || strings.Contains(buttons[1].label, "×1") {
			t.Fatalf("counts: %+v", buttons)
		}
		for _, b := range buttons {
			if b.rect.x+b.rect.width > width || !b.rect.contains(b.rect.x+b.rect.width-1, b.rect.y) {
				t.Fatalf("invalid click geometry: %+v", b)
			}
		}
	}
}

func TestApprovalBacklogOrderSettlesAndFreezes(t *testing.T) {
	m := backlogModel()
	now := time.Unix(100, 0)
	m.settleMonitorApprovalOrder(now)
	m.monitorSessionData[0].preview.PendingApprovals = 4
	m.settleMonitorApprovalOrder(now.Add(time.Second))
	m.settleMonitorApprovalOrder(now.Add(4 * time.Second))
	if m.monitorAttentionSessions()[0].id != "two" {
		t.Fatal("reordered too early")
	}
	m.monitorContextHover = "attention:two"
	m.settleMonitorApprovalOrder(now.Add(8 * time.Second))
	if m.monitorAttentionSessions()[0].id != "two" {
		t.Fatal("reordered under pointer")
	}
	m.monitorContextHover = ""
	m.settleMonitorApprovalOrder(now.Add(12 * time.Second))
	if m.monitorAttentionSessions()[0].id != "two" {
		t.Fatal("did not retain hover grace period")
	}
	m.settleMonitorApprovalOrder(now.Add(13 * time.Second))
	if m.monitorAttentionSessions()[0].id != "one" {
		t.Fatal("did not settle")
	}
	m.monitorSessionData[0].attention = codex.SessionAttentionNone
	if m.monitorAttentionSessions()[0].id != "two" {
		t.Fatal("resolved request retained")
	}
}

func TestApprovalBacklogConfirmationFreezeAndTieBreak(t *testing.T) {
	m := backlogModel()
	m.monitorSessionData[0].preview.PendingApprovals = 3
	if m.monitorAttentionSessions()[0].id != "one" {
		t.Fatal("equal counts must prefer oldest")
	}
	now := time.Unix(100, 0)
	m.settleMonitorApprovalOrder(now)
	m.monitorSessionData[1].preview.PendingApprovals = 4
	m.settleMonitorApprovalOrder(now.Add(time.Second))
	m.monitorApprovalConfirm = "decision:0"
	m.settleMonitorApprovalOrder(now.Add(10 * time.Second))
	if m.monitorAttentionSessions()[0].id != "one" {
		t.Fatal("moved during confirmation")
	}
	m.monitorApprovalConfirm = ""
	m.settleMonitorApprovalOrder(now.Add(15 * time.Second))
	if m.monitorAttentionSessions()[0].id != "two" {
		t.Fatal("did not resume ordering")
	}
}

func TestApprovalBacklogClickTargetsAfterReorderAndResize(t *testing.T) {
	m := backlogModel()
	now := time.Unix(100, 0)
	m.settleMonitorApprovalOrder(now)
	m.monitorSessionData[0].preview.PendingApprovals = 4
	m.settleMonitorApprovalOrder(now.Add(time.Second))
	m.settleMonitorApprovalOrder(now.Add(6 * time.Second))
	for _, full := range []bool{false, true} {
		m.monitorContextDetail = ""
		if full {
			m.setRowContext("one", contextFull)
		}
		for _, width := range []int{60, 120, 200} {
			m.width, m.height = width, 65
			g := m.dashboardLayout()
			a := m.monitorArea(g.contentWidth, g.meterHeight)
			buttons, y := a.attention, g.meterY+a.topHeight
			if full {
				summary, _, detailButtons := m.monitorDetailHeader(g.contentWidth, g.meterHeight)
				buttons, y = detailButtons, g.meterY+summary
			}
			if len(buttons) == 0 {
				t.Fatal("no buttons")
			}
			for _, b := range buttons {
				for _, x := range []int{b.rect.x, b.rect.x + b.rect.width - 1} {
					if got := m.monitorContextAt(2+x, y+b.rect.y); got != b.action {
						t.Fatalf("click target drift: got %q want %q", got, b.action)
					}
				}
			}
		}
	}
}

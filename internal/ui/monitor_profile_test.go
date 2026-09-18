package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func profileTestModel(t *testing.T) (Model, *quotaStepTestFetcher) {
	m, f := quotaTestModel(t)
	m.meterView = viewMonitor
	m.monitorState = monitorRunning
	for _, s := range f.sessions {
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{id: s.ID, displayed: true, active: true, workingDirectory: "/projects/test"})
	}
	m.setRowContext("one", contextFull)
	return m, f
}

func TestProfileReviewsAreIndividual(t *testing.T) {
	m, f := profileTestModel(t)
	pills, _ := m.monitorAttentionButtons(160, 1)
	if len(pills) != 2 || !strings.Contains(pills[0].label, i18n.Text("QUOTA THRESHOLD")) {
		t.Fatalf("pills %#v", pills)
	}
	m, cmd, handled := m.updateProfileKey("1")
	if !handled || cmd != nil || !m.profileArmed("one") {
		t.Fatal("first action did not arm")
	}
	m, cmd, _ = m.updateProfileKey("c")
	if cmd == nil {
		t.Fatal("confirmation missing")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if f.updates != 1 || len(f.targets) != 1 || f.targets[0].ID != "one" {
		t.Fatalf("wrong target: %#v", f.targets)
	}
	if got := m.quotaCandidates(); len(got) != 1 || got[0].ID != "two" {
		t.Fatalf("other session lost review: %#v", got)
	}
	if m.quota.notices["one"] != "" || m.quota.notices["two"] != "" {
		t.Fatal("successful profile update left stale feedback")
	}
	m.setRowContext("two", contextFull)
	m, cmd, _ = m.updateProfileKey("2")
	if cmd != nil || len(m.quotaCandidates()) != 0 || f.updates != 1 {
		t.Fatal("skip wrote settings")
	}
}

func TestProfileReviewExplainsThresholdAndChoice(t *testing.T) {
	m, _ := profileTestModel(t)
	var text []string
	for _, line := range m.profileDocument(m.monitorSessionData[0], 200) {
		text = append(text, line.text)
	}
	want := i18n.Format("Your %d%% quota threshold has been reached. Approve the profile switch below for this session's next turns, or skip it.", 80)
	if !strings.Contains(strings.Join(text, "\n"), want) {
		t.Fatal("review must explain the configured threshold and the user's choice")
	}
	for _, profile := range []string{"large / high / " + i18n.Text("unset"), "small / medium / " + i18n.Text("speed unchanged")} {
		if !strings.Contains(strings.Join(text, "\n"), i18n.Text("MODEL / REASONING LEVEL / SPEED")+"\n"+profile) {
			t.Fatal("each profile must have a field heading and remain left aligned")
		}
	}
	var headings []string
	warnings := 0
	for index, line := range m.profileDocument(m.monitorSessionData[0], 200) {
		if line.kind == "heading" {
			if index == 0 || text[index-1] != "" {
				t.Fatal("section needs a blank line")
			}
			headings = append(headings, strings.TrimRight(line.text, " ─"))
		}
		if line.kind == "warning" {
			warnings++
			if line.text != i18n.Text("Changes remain after Codexometer closes.") {
				t.Fatal("ordinary review text should not use warning colour")
			}
		}
	}
	wantHeadings := []string{i18n.Text("WHY THIS CHANGE"), i18n.Text("CURRENT PROFILE"), i18n.Text("PROPOSED PROFILE"), i18n.Text("PLEASE NOTE")}
	if strings.Join(headings, "|") != strings.Join(wantHeadings, "|") || warnings != 1 {
		t.Fatalf("incorrect sections or warning emphasis: %v, %d", headings, warnings)
	}
}

func TestProfileNativeAttentionAndHiddenControls(t *testing.T) {
	for _, kind := range []string{"approval", "input", "question", "graph", "short", "paused", "error", "stale", "focused"} {
		t.Run(kind, func(t *testing.T) {
			m, f := profileTestModel(t)
			switch kind {
			case "approval":
				m.monitorSessionData[0].preview.Kind = codex.SessionContextApproval
			case "input":
				m.monitorSessionData[0].attention = codex.SessionAttentionInput
			case "question":
				m.monitorSessionData[0].preview.Kind = codex.SessionContextQuestion
			case "graph":
				m.setRowContext("one", contextGraph)
			case "short":
				m.height = 14
			case "paused":
				m.monitorState = monitorPaused
			case "error":
				m.monitorError = "disconnected"
			case "stale":
				m.snapshot.FetchedAt = time.Now().Add(-time.Hour)
			case "focused":
				m.monitorPrompt.input = newMonitorEditor()
				m.monitorPrompt.input.Focus()
			}
			if len(m.visibleProfileButtons()) != 0 {
				t.Fatal("unsafe controls visible")
			}
			_, cmd, _ := m.updateProfileKey("1")
			if cmd != nil || f.updates != 0 {
				t.Fatal("unexpected write")
			}
		})
	}
}

func TestProfileConfirmationCannotFollowSelection(t *testing.T) {
	m, f := profileTestModel(t)
	m, _, _ = m.updateProfileKey("1")
	m.setRowContext("two", contextFull)
	m, cmd, _ := m.updateProfileKey("c")
	if cmd != nil || f.updates != 0 || m.profileArmed("one") {
		t.Fatal("confirmation survived navigation")
	}
	m, _, _ = m.updateProfileKey("1")
	m.quotaStepConfirmUntil = time.Now().Add(-time.Second)
	_, cmd, _ = m.updateProfileKey("c")
	if cmd != nil {
		t.Fatal("expired confirmation submitted")
	}
}

func TestUnverifiedProfileNoticeClosesAfterAuthoritativeRescan(t *testing.T) {
	m, f := profileTestModel(t)
	target := m.quotaCandidates()[0]
	f.sessions[0].Model = "small"
	f.sessions[0].Effort = "medium"
	message := quotaStepResult{
		revision: m.quota.revision, step: *m.quotaStepPending, window: m.quotaStepWindow,
		targets: []codex.QuotaSession{target}, err: errors.New("settings queued but application not verified"),
	}
	next, cmd := m.Update(message)
	m = next.(Model)
	if cmd == nil || m.quota.notices["one"] == "" {
		t.Fatal("uncertain result was not scheduled for reconciliation")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.quota.notices["one"] != "" || f.updates != 0 {
		t.Fatal("verified rescan retained notice or resent the write")
	}
}

func TestProfileZeroThresholdAndNoQuotaChrome(t *testing.T) {
	m, _ := profileTestModel(t)
	m.quotaStepPending.Threshold = 0
	m.quotaSteps[0].Threshold = 0
	if len(m.quotaCandidates()) != 2 {
		t.Fatal("zero threshold hid unhandled sessions")
	}
	m.meterView = viewBars
	if strings.Contains(ansi.Strip(m.render()), i18n.Text("QUOTA THRESHOLD")) {
		t.Fatal("profile leaked into quota view")
	}
}

func TestProfileThresholdChangesBeforeRescan(t *testing.T) {
	m, _ := profileTestModel(t)
	m, _, _ = m.updateProfileKey("1")
	m.snapshot.RateLimits.Secondary.UsedPercent = 96
	if m.quotaFresh() {
		t.Fatal("old threshold remained actionable before inventory refresh")
	}
	_, cmd, _ := m.updateProfileKey("c")
	if cmd != nil {
		t.Fatal("applied outdated profile")
	}
}

func TestProfilePillOrderAndNativeRequestDocument(t *testing.T) {
	m, _ := profileTestModel(t)
	m.monitorSessionData = append(m.monitorSessionData,
		monitorSession{id: "approval", displayed: true, attention: codex.SessionAttentionApproval},
		monitorSession{id: "input", displayed: true, attention: codex.SessionAttentionInput},
		monitorSession{id: "complete", displayed: true, attention: codex.SessionAttentionComplete})
	got := m.monitorAttentionSessions()
	if len(got) != 5 || got[0].id != "approval" || got[1].id != "input" || got[2].id != "one" || got[4].id != "complete" {
		t.Fatalf("order %#v", got)
	}
	m.monitorSessionData[0].preview = codex.SessionContext{Kind: codex.SessionContextApproval, Text: "Real command approval"}
	if m.hasSessionProfile(m.monitorSessionData[0]) {
		t.Fatal("quota hides Codex approval")
	}
	if !strings.Contains(strings.Join(m.contextDetailLines(100), "\n"), "Real command approval") {
		t.Fatal("real approval text lost")
	}
}

func TestProfileDoesNotInterruptComposer(t *testing.T) {
	m, _ := profileTestModel(t)
	m.monitorPrompt.session = "one"
	m.monitorPrompt.input = newMonitorEditor()
	m.monitorPrompt.input.Focus()
	if m.hasSessionProfile(m.monitorSessionData[0]) {
		t.Fatal("profile replaced active composer")
	}
	m.monitorPrompt.input.Blur()
	if !m.hasSessionProfile(m.monitorSessionData[0]) {
		t.Fatal("profile did not return after editing")
	}
}

// Also run under every supported startup locale: every rendered button cell
// must hit the same session/action after both confirmation and resize.
func TestQuotaLocalisedControls(t *testing.T) {
	count := 0
	for _, size := range [][2]int{{40, 18}, {80, 40}, {120, 65}, {200, 100}} {
		for _, mode := range []int{contextSplit, contextWide, contextFull} {
			for _, armed := range []bool{false, true} {
				m, _ := profileTestModel(t)
				m.width, m.height = size[0], size[1]
				m.setRowContext("one", mode)
				if armed {
					m, _ = m.pressQuotaSession("one", false)
				}
				g := m.monitorDashboardLayout()
				width, height, x, y := g.contentWidth, g.meterHeight, 2, g.meterY
				if mode != contextFull {
					a := m.monitorArea(g.contentWidth, g.meterHeight)
					sessions, heights, _ := m.monitorSessionPage(a.graphHeight)
					if len(sessions) == 0 {
						continue
					}
					mw, cw, _ := monitorSessionColumnWidths(a.width)
					if mode == contextSplit {
						_, cw, _ = m.contextColumns(a.width, sessions[0])
					}
					width, height, x, y = cw, heights[0], 2+mw+1, g.meterY+a.topHeight+a.gap-1
				}
				buttons := m.profileButtons(width, height, m.monitorSessionData[0])
				if len(buttons) == 0 {
					continue
				}
				count++
				_, _, cy := monitorContextBodyLayout(height, buttons[len(buttons)-1].y+1)
				rendered := strings.Split(ansi.Strip(m.render()), "\n")
				for _, b := range buttons {
					if !strings.Contains(rendered[y+cy+b.y], b.label) {
						t.Fatalf("%s missing rendered %q", i18n.Code(), b.label)
					}
					for col := 0; col < lipgloss.Width(b.label); col++ {
						if hit := m.monitorContextAt(x+2+b.x+col, y+cy+b.y); hit != b.action {
							t.Fatalf("%s %v mode %d: %s got %s", i18n.Code(), size, mode, b.action, hit)
						}
					}
				}
				// Mouse uses the same two-step confirmation, never the neighbouring row.
				b := buttons[0]
				next, cmd, _ := m.updateMonitorContextMouse(tea.MouseClickMsg{X: x + 2 + b.x, Y: y + cy + b.y, Button: tea.MouseLeft})
				if armed && cmd == nil || !armed && (cmd != nil || !next.profileArmed("one")) {
					t.Fatal("mouse confirmation mismatch")
				}
			}
		}
	}
	if count == 0 {
		t.Fatal("no profile buttons tested")
	}
}

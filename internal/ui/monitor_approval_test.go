package ui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorDecisionOutcomeDoesNotAskForAnotherReply(t *testing.T) {
	for _, failed := range []bool{false, true} {
		m := approvalTestModel()
		m, _, _ = m.monitorApprovalAction("decision:0")
		m, cmd, _ := m.monitorApprovalAction("decision:0")
		if strings.Contains(strings.Join(m.contextDetailLines(120), "\n"), i18n.Text("REPLY IN CODEX")) {
			t.Fatal("sending decision asks for another reply")
		}
		result := cmd().(monitorApprovalResult)
		if failed {
			result.err = errors.New("ambiguous write")
		}
		n, _ := m.Update(result)
		m = n.(Model)
		if strings.Contains(strings.Join(m.contextDetailLines(120), "\n"), i18n.Text("REPLY IN CODEX")) {
			t.Fatal("outcome asks for another reply")
		}
		if !m.monitorApprovalHasOutcome() {
			t.Fatal("outcome was not retained")
		}
		m.monitorSessionData[0].preview.ApprovalToken = "replacement"
		if m.monitorApprovalHasOutcome() {
			t.Fatal("old result hides new request")
		}
		if !strings.Contains(strings.Join(m.contextDetailLines(120), "\n"), i18n.Text("REPLY IN CODEX")) {
			t.Fatal("unavailable replacement diagnostic hidden")
		}
		n, _ = m.Update(result)
		m = n.(Model)
		if m.monitorApprovalNotice != "" {
			t.Fatal("late old outcome applied to replacement")
		}
	}
}

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
	m.SetConfirmationMode(confirmation.Require)
	m.fetcher = &approvalTestClient{token: "live"}
	m.monitorSessionData[0].preview = codex.SessionContext{Kind: codex.SessionContextApproval, Text: "Command: git push\nDirectory: /work", Source: "LIVE", ApprovalToken: "live"}
	m.monitorSessionData[0].preview.ApprovalOptions = [8]codex.ApprovalOption{
		{Kind: "accept", Value: "accept"}, {Kind: "decline", Value: "decline"}, {Kind: "cancel", Value: "cancel"}, {Kind: "acceptForSession", Value: "acceptForSession"}, {Kind: "acceptWithExecpolicyAmendment", Value: `{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["pwd"]}}`},
	}
	m.openMonitorContext(m.monitorSessionData[0].id)
	return m
}

func TestConfirmationNoneStillRequiresAnExplicitApprovalAndLiveCapability(t *testing.T) {
	for _, stale := range []bool{false, true} {
		m := approvalTestModel()
		m.SetConfirmationMode(confirmation.None)
		client := m.fetcher.(*approvalTestClient)
		if stale {
			client.token = "replacement"
		}
		n, cmd, _ := m.monitorApprovalAction("decision:0")
		if (cmd != nil) == stale || n.monitorApprovalConfirm != "" {
			t.Fatal("wrong live approval/confirmation handling", stale)
		}
		if cmd != nil {
			if client.decision != "" {
				t.Fatal("decision executed before explicit action dispatch")
			}
			cmd()
			if client.decision != "accept" {
				t.Fatal("chosen approval was not sent")
			}
		}
	}
}

func TestMonitorApprovalRenderedTargets(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 180} {
		for _, height := range []int{12, 24, 40} {
			for _, confirmed := range []bool{false, true} {
				m := approvalTestModel()
				m.width = width
				m.height = height
				if confirmed {
					m.monitorApprovalConfirm = "live/decision:0"
					m.monitorApprovalConfirmUntil = time.Now().Add(monitorApprovalConfirmDuration)
				}
				out := m.render()
				plain := ansi.Strip(out)
				lines := strings.Split(plain, "\n")
				if lipgloss.Width(out) > width || lipgloss.Height(out) > height {
					t.Fatalf("overflow %dx%d", width, height)
				}
				g := m.dashboardLayout()
				shown := m.monitorApprovalControls(g.contentWidth, g.meterHeight)
				for i, option := range m.monitorSessionData[0].preview.ApprovalOptions {
					if option.Kind == "" {
						continue
					}
					action := "decision:" + strconv.Itoa(i)
					label := m.approvalButtonLabel(option.Kind, confirmed && i == 0, i)
					found := false
					for y, line := range lines {
						pos := strings.Index(line, label)
						if pos < 0 {
							continue
						}
						found = true
						x := lipgloss.Width(line[:pos])
						for dx := 0; dx < lipgloss.Width(label); dx++ {
							if got := m.monitorContextAt(x+dx, y); got != action {
								t.Fatalf("%s miss at %d,%d got %q\n%s", action, x+dx, y, got, ansi.Strip(out))
							}
						}
						updated, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
						u := updated.(Model)
						if m.confirmations.Required(confirmation.Approval(option.Kind)) && !(confirmed && i == 0) {
							if cmd != nil || u.monitorApprovalConfirm != "live/"+action {
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
	m, cmd, _ := m.monitorApprovalAction("decision:0")
	if cmd != nil {
		t.Fatal("first click sent decision")
	}
	client := m.fetcher.(*approvalTestClient)
	client.token = "replacement"
	m, cmd, _ = m.monitorApprovalAction("decision:0")
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
	m.setRowContext("root-one", contextWide)
	m.cycleMonitorContext()
	m.cycleMonitorContext()
	if m.monitorApprovalToken() != "" {
		t.Fatal("hidden preview actionable")
	}
	m.cycleMonitorContext()
	m.monitorSessionData[0].preview.ApprovalToken = ""
	if m.monitorApprovalToken() != "" {
		t.Fatal("local request actionable")
	}
}

func TestMonitorApprovalNumberSelectsCConfirms(t *testing.T) {
	for _, index := range []int{0, 3, 4} {
		m := approvalTestModel()
		m, cmd, _ := m.updateMonitorContextKey("c")
		if cmd != nil || m.monitorApprovalBusy {
			t.Fatal("C granted without a selection")
		}
		for range 2 {
			m, cmd, _ = m.updateMonitorContextKey(strconv.Itoa(index + 1))
			if cmd != nil || m.monitorApprovalBusy || m.monitorApprovalConfirm != "live/decision:"+strconv.Itoa(index) {
				t.Fatal("number did not exclusively arm requested choice")
			}
		}
		if !strings.Contains(ansi.Strip(m.render()), m.approvalButtonLabel(m.monitorSessionData[0].preview.ApprovalOptions[index].Kind, true, index)) {
			t.Fatal("C confirmation shortcut missing")
		}
		m, cmd, _ = m.updateMonitorContextKey("c")
		if cmd == nil || !m.monitorApprovalBusy {
			t.Fatal("C did not submit armed grant")
		}
		cmd()
		if got := m.fetcher.(*approvalTestClient).decision; got != m.monitorSessionData[0].preview.ApprovalOptions[index].Value {
			t.Fatal("wrong grant", got)
		}
	}
	for _, index := range []int{1, 2} {
		m := approvalTestModel()
		m.SetConfirmationMode(confirmation.Balanced)
		m, cmd, _ := m.updateMonitorContextKey(strconv.Itoa(index + 1))
		if cmd == nil || !m.monitorApprovalBusy {
			t.Fatal("negative decision shortcut did not match its button")
		}
	}
}

func TestMonitorApprovalShortcutsHaveOneVisibleSession(t *testing.T) {
	m := approvalTestModel()
	m.width, m.height = 180, 60
	for i := range m.monitorSessionData {
		m.monitorSessionData[i].preview = m.monitorSessionData[0].preview
	}
	for _, detail := range []bool{false, true} {
		m.monitorContextDetail, m.monitorContextExpanded = "", "root-one"
		if detail {
			m.monitorContextDetail, m.monitorContextExpanded = "root-one", ""
		}
		buttons := m.visibleMonitorApprovalButtons()
		if len(buttons) == 0 {
			t.Fatal("fixture has no visible buttons")
		}
		out := ansi.Strip(m.render())
		for _, b := range buttons {
			if strings.Count(out, b.label) != 1 {
				t.Fatal("approval choice shown for multiple sessions", b.label)
			}
		}
		m, _, _ = m.updateMonitorContextKey("1")
		if m.monitorApprovalConfirm != "live/decision:0" {
			t.Fatal("visible keyboard choice failed")
		}
	}
	m.monitorContextDetail = ""
	m.monitorContextExpanded = "root-one"
	m.monitorSessionData[0].preview.Text = strings.Repeat("too long\n", 200)
	for _, k := range []string{"1", "c", "2"} {
		_, cmd, _ := m.updateMonitorContextKey(k)
		if cmd != nil {
			t.Fatal("invisible inline button actionable")
		}
	}
	m.monitorSelectedID = "root-one"
	m.selectMonitorSession(1)
	if m.monitorApprovalConfirm != "" {
		t.Fatal("selection retained old confirmation")
	}
	_, cmd, _ := m.updateMonitorContextKey("c")
	if cmd != nil {
		t.Fatal("C acted on previous session")
	}
}

func TestMonitorApprovalExplainsUnavailableControls(t *testing.T) {
	m := approvalTestModel()
	s := &m.monitorSessionData[0]
	s.preview.ApprovalToken = ""
	s.preview.ApprovalBlocked = "permissions"
	lines := m.contextDetailLines(300)
	if !strings.Contains(strings.Join(lines, "\n"), i18n.Text("Additional permissions require approval in Codex.")) {
		t.Fatal("missing explanation", lines)
	}
	for _, line := range m.contextDetailLines(20) {
		if lipgloss.Width(line) > 20 {
			t.Fatal("diagnostic overflow", line)
		}
	}
	s.preview.ApprovalBlocked = ""
	s.preview.Source = "LOCAL"
	if got := m.monitorApprovalBlockReason(s.preview); got != i18n.Text("Local observation cannot answer live approvals.") {
		t.Fatal(got)
	}
	s.preview.Source = "LIVE"
	s.preview.ApprovalToken = "expired"
	if got := m.monitorApprovalBlockReason(s.preview); got != i18n.Text("Request resolved, expired, or disconnected.") {
		t.Fatal(got)
	}
	s.preview.ApprovalToken = "live"
	m.width = 30
	if got := strings.Join(m.contextDetailLines(100), "\n"); !strings.Contains(got, i18n.Text("Enlarge the terminal to show approval controls.")) {
		t.Fatal(got)
	}
}

func TestMonitorGrantsConfirmExactChoiceAndStableLayout(t *testing.T) {
	for _, index := range []int{0, 3, 4} {
		m := approvalTestModel()
		m.width = 180
		m.height = 40
		g := m.dashboardLayout()
		before := m.monitorApprovalButtons(g.contentWidth, g.meterHeight)
		action := "decision:" + strconv.Itoa(index)
		m, cmd, _ := m.monitorApprovalAction(action)
		if cmd != nil {
			t.Fatal("grant sent without confirmation")
		}
		after := m.monitorApprovalButtons(g.contentWidth, g.meterHeight)
		for i := range before {
			if before[i].x != after[i].x || before[i].y != after[i].y {
				t.Fatal("confirmation moved decision targets")
			}
		}
		m, cmd, _ = m.monitorApprovalAction(action)
		if cmd == nil {
			t.Fatal("confirmed grant not queued")
		}
		cmd()
		if got := m.fetcher.(*approvalTestClient).decision; got != m.monitorSessionData[0].preview.ApprovalOptions[index].Value {
			t.Fatal("wrong decision sent", got)
		}
	}
	m := approvalTestModel()
	m, _, _ = m.monitorApprovalAction("decision:0")
	m, cmd, _ := m.monitorApprovalAction("decision:3")
	if cmd != nil || m.monitorApprovalConfirm != "live/decision:3" {
		t.Fatal("one-time confirmation authorised session grant")
	}
}

func TestMonitorApprovalFooterPinnedBelowScrollingText(t *testing.T) {
	m := approvalTestModel()
	m.width = 120
	m.height = 30
	m.monitorSessionData[0].preview.Text = strings.Repeat("request detail\n", 100) + "END OF REQUEST"
	g := m.dashboardLayout()
	n := m.monitorApprovalControlRows(g.contentWidth, g.meterHeight)
	textRows, gap, y := monitorContextBodyLayout(g.meterHeight, n)
	if n == 0 || gap != 1 || textRows < 3 {
		t.Fatal("footer allocation", n, textRows, gap)
	}
	for _, offset := range []int{0, 10000} {
		m.scrollMonitorContext(offset)
		out := strings.Split(ansi.Strip(m.renderMonitorContextDetail(g.contentWidth, g.meterHeight, paletteFor(m.theme))), "\n")
		if strings.Trim(out[y-1], " │") != "" {
			t.Fatal("missing spacer", out[y-1])
		}
		for _, b := range m.monitorApprovalButtons(g.contentWidth, g.meterHeight) {
			if !strings.Contains(out[y+b.y], b.label) {
				t.Fatal("button moved while scrolling")
			}
			if got := m.monitorContextAt(4+b.x, g.meterY+y+b.y); got != b.action {
				t.Fatal("footer hit miss", got)
			}
		}
		if offset > 0 && !strings.Contains(strings.Join(out[:y], "\n"), "END OF REQUEST") {
			t.Fatal("last text row inaccessible")
		}
	}
	if rows, gap, _ := monitorContextBodyLayout(6, 1); rows != 3 || gap != 0 {
		t.Fatal("short terminal did not drop padding first")
	}
}

func TestApprovalButtonsKeepEvenVisibleGapsAndStableConfirmationPositions(t *testing.T) {
	for _, mode := range []confirmation.Mode{confirmation.Balanced, confirmation.None, confirmation.Require} {
		for _, middle := range []string{"decline", "acceptForSession"} {
			m := approvalTestModel()
			m.SetConfirmationMode(mode)
			m.monitorSessionData[0].preview.ApprovalOptions = [8]codex.ApprovalOption{{Kind: "accept", Value: "accept"}, {Kind: middle, Value: middle}, {Kind: "cancel", Value: "cancel"}}
			before := m.monitorApprovalButtons(140, 20)
			if len(before) != 3 {
				t.Fatal("three choices were not visible")
			}
			for armed := -1; armed < len(before); armed++ {
				if armed >= 0 && !m.confirmations.Required(confirmation.Approval(m.monitorSessionData[0].preview.ApprovalOptions[armed].Kind)) {
					continue
				}
				m.monitorApprovalConfirm = ""
				if armed >= 0 {
					m.monitorApprovalConfirm = "live/decision:" + strconv.Itoa(armed)
					m.monitorApprovalConfirmUntil = time.Now().Add(monitorApprovalConfirmDuration)
				}
				buttons := m.monitorApprovalButtons(140, 20)
				rendered := ansi.Strip(m.renderMonitorApprovalControls(140, 20, paletteFor(m.theme)))
				for i, button := range buttons {
					if button.x != before[i].x || button.y != before[i].y {
						t.Fatal("arming moved a neighbouring decision")
					}
					if lipgloss.Width(button.label) != button.slot {
						t.Fatal("reserved space was left between buttons")
					}
					if i > 0 && buttons[i-1].y == button.y {
						previous := buttons[i-1]
						end := strings.Index(rendered, previous.label) + len(previous.label)
						start := strings.Index(rendered, button.label)
						if rendered[end:start] != "  " {
							t.Fatalf("mode %s choice %d has uneven gap: %q", mode, i+1, rendered[end:start])
						}
					}
					_, _, controlY := monitorContextBodyLayout(20, buttons[len(buttons)-1].y+1)
					if monitorButtonAt(buttons, 20, 2+button.x+button.slot-2, controlY+button.y) != button.action {
						t.Fatal("painted button padding does not belong to its button")
					}
				}
			}
		}
	}
}

func TestMCPApprovalFourButtonsAndConfirmationPolicy(t *testing.T) {
	for _, mode := range []confirmation.Mode{confirmation.Balanced, confirmation.None, confirmation.Require} {
		for index, kind := range []string{"accept", "acceptForSession", "acceptMcpAlways", "cancelMcpTool"} {
			m := approvalTestModel()
			m.SetConfirmationMode(mode)
			m.monitorSessionData[0].preview = codex.SessionContext{Kind: codex.SessionContextApproval, Source: "LIVE", ThreadID: "root-one", Text: "Allow Calendar to create an event?", ApprovalToken: "live", MCPTool: codex.MCPToolApprovalDetails{Server: "codex_apps", Title: "Create Event", Message: "Allow Calendar to create an event?", Arguments: `{"title":"Demo"}`}, ApprovalOptions: [8]codex.ApprovalOption{{Kind: "accept", Value: "accept"}, {Kind: "acceptForSession", Value: "acceptForSession"}, {Kind: "acceptMcpAlways", Value: "acceptMcpAlways"}, {Kind: "cancelMcpTool", Value: "cancelMcpTool"}}}
			buttons := m.visibleMonitorApprovalButtons()
			if len(buttons) != 4 {
				t.Fatalf("mode %s: four MCP choices not visible", mode)
			}
			doc := ansi.Strip(m.render())
			for _, text := range []string{i18n.Text("MCP TOOL APPROVAL"), "codex_apps", "Create Event", "Demo", strings.Trim(i18n.Text("[ CANCEL TOOL CALL ]"), "[] ")} {
				if !strings.Contains(doc, strings.TrimSuffix(text, " ]")) {
					t.Fatalf("missing MCP review text %q", text)
				}
			}
			if strings.Contains(doc, strings.Trim(i18n.Text("[ REJECT & STOP TURN ]"), "[] ")) {
				t.Fatal("tool cancellation labelled as turn rejection")
			}
			next, cmd, _ := m.updateMonitorApprovalKey(strconv.Itoa(index + 1))
			m = next
			guarded := mode == confirmation.Require || mode == confirmation.Balanced && (index == 1 || index == 2)
			if guarded {
				if cmd != nil || m.monitorApprovalConfirm == "" {
					t.Fatal("reusable MCP grant bypassed review")
				}
				m, cmd, _ = m.updateMonitorApprovalKey("c")
			}
			if cmd == nil {
				t.Fatal("explicit MCP decision not sent")
			}
			cmd()
			if m.fetcher.(*approvalTestClient).decision != kind {
				t.Fatal("wrong MCP choice")
			}
		}
	}
}

func TestNetworkApprovalButtonsAndPolicy(t *testing.T) {
	for _, mode := range []confirmation.Mode{confirmation.Balanced, confirmation.None, confirmation.Require} {
		for index, kind := range []string{"accept", "acceptForSession", "allowNetworkHost", "denyNetworkHost", "decline", "cancel"} {
			m := approvalTestModel()
			m.SetConfirmationMode(mode)
			ctx := codex.SessionContext{Kind: codex.SessionContextApproval, Source: "LIVE", ThreadID: "root-one", ApprovalToken: "live", Text: "Allow network access?", ActionDetails: codex.ApprovalActionDetails{Kind: "network", Target: "example.com", Protocol: "https"}}
			for i, k := range []string{"accept", "acceptForSession", "allowNetworkHost", "denyNetworkHost", "decline", "cancel"} {
				ctx.ApprovalOptions[i] = codex.ApprovalOption{Kind: k, Value: k}
			}
			m.monitorSessionData[0].preview = ctx
			if len(m.visibleMonitorApprovalButtons()) != 6 {
				t.Fatal("network choices missing", mode)
			}
			doc := ansi.Strip(m.render())
			for _, text := range []string{i18n.Text("NETWORK APPROVAL"), "example.com", "https"} {
				if !strings.Contains(doc, text) {
					t.Fatal("network review missing", text)
				}
			}
			next, cmd, _ := m.updateMonitorApprovalKey(strconv.Itoa(index + 1))
			m = next
			guarded := mode == confirmation.Require || mode == confirmation.Balanced && index >= 1 && index <= 3
			if guarded {
				if cmd != nil || m.monitorApprovalConfirm == "" {
					t.Fatal("persistent host rule bypassed review")
				}
				m, cmd, _ = m.updateMonitorApprovalKey("c")
			}
			if cmd == nil {
				t.Fatal("network decision not sent")
			}
			cmd()
			if m.fetcher.(*approvalTestClient).decision != kind {
				t.Fatal("wrong network decision")
			}
		}
	}
}

func TestStdinApprovalControlsAndEscapedReview(t *testing.T) {
	for _, mode := range []confirmation.Mode{confirmation.Balanced, confirmation.None, confirmation.Require} {
		for index, decision := range []string{"accept", "cancel"} {
			m := approvalTestModel()
			m.SetConfirmationMode(mode)
			input := "yes\n\x03\u202e"
			m.monitorSessionData[0].preview = codex.SessionContext{Kind: codex.SessionContextApproval, Source: "LIVE", ThreadID: "root-one", Text: "Review terminal input", ApprovalToken: "live", ActionDetails: codex.ApprovalActionDetails{Kind: "stdin", Target: "123", Input: input, Directory: "/terminal/start"}, ApprovalOptions: [8]codex.ApprovalOption{{Kind: "acceptStdin", Value: "accept"}, {Kind: "cancelStdin", Value: "cancel"}}}
			if len(m.visibleMonitorApprovalButtons()) != 2 {
				t.Fatal("stdin choices missing")
			}
			doc := ansi.Strip(m.render())
			for _, text := range []string{i18n.Text("TERMINAL INPUT APPROVAL"), "123", strconv.QuoteToASCII(input), "/terminal/start"} {
				if !strings.Contains(doc, text) {
					t.Fatal("stdin review missing", text)
				}
			}
			next, cmd, _ := m.updateMonitorApprovalKey(strconv.Itoa(index + 1))
			m = next
			if mode == confirmation.Require {
				if cmd != nil || m.monitorApprovalConfirm == "" {
					t.Fatal("strict input skipped review")
				}
				m, cmd, _ = m.updateMonitorApprovalKey("c")
			}
			if cmd == nil {
				t.Fatal("stdin decision not sent")
			}
			cmd()
			if m.fetcher.(*approvalTestClient).decision != decision {
				t.Fatal("input decision changed")
			}
		}
	}
}

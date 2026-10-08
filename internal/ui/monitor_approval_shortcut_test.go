package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestApprovalShortcutAfterFocusedComposer(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, enhanced := range []bool{false, true} {
			m := approvalTestModel()
			m.keyboardEventTypes = enhanced
			if wide {
				m.width, m.height = 180, 60
				m.monitorSessionData = m.monitorSessionData[:1]
				m.setRowContext("root-one", contextWide)
			}
			m.monitorPrompt = monitorPromptState{session: "root-one", offer: codex.SessionPromptOffer{Token: "previous", TurnID: "turn"}, input: newMonitorEditor()}
			m.monitorPrompt.input.SetValue("Keep my unsent draft")
			m.monitorPrompt.input.Focus()
			// Even an old armed confirmation must not make the transition
			// key grant permission immediately.
			m.monitorApprovalConfirm = "live/decision:0"
			m.monitorApprovalConfirmUntil = time.Now().Add(time.Minute)
			m.monitorApprovalNumberReleased = true
			m, cmd := approvalEvent(m, key('1'))
			if cmd != nil || m.monitorApprovalBusy || m.monitorApprovalConfirm != "live/decision:0" || m.monitorPrompt.input.Focused() || m.monitorApprovalNumberReleased {
				t.Fatalf("wide=%v enhanced=%v: first number must only select approval", wide, enhanced)
			}
			if m.monitorDrafts["root-one"] != "Keep my unsent draft" {
				t.Fatal("approval transition lost ordinary draft")
			}
			if enhanced {
				m, _ = approvalEvent(m, tea.KeyReleaseMsg{Code: '1'})
				m, cmd = approvalEvent(m, key('1'))
			} else {
				m, cmd = approvalEvent(m, key('c'))
			}
			if cmd == nil || !m.monitorApprovalBusy {
				t.Fatal("separate confirmation did not submit")
			}
			cmd()
			if m.fetcher.(*approvalTestClient).decision != "accept" {
				t.Fatal("wrong decision sent")
			}
		}
	}
}

func TestApprovalComposerTransitionDoesNotReinterpretOtherInput(t *testing.T) {
	for _, scenario := range []string{"decline", "cancel", "confirm", "repeat", "different-session", "clipped", "paste", "secret"} {
		t.Run(scenario, func(t *testing.T) {
			m := approvalTestModel()
			m.monitorPrompt = monitorPromptState{session: "root-one", offer: codex.SessionPromptOffer{Token: "previous", TurnID: "turn"}, input: newMonitorEditor()}
			m.monitorPrompt.input.SetValue("draft")
			m.monitorPrompt.input.Focus()
			var msg tea.Msg = key('1')
			switch scenario {
			case "decline":
				msg = key('2')
			case "cancel":
				msg = key('3')
			case "confirm":
				msg = key('c')
			case "repeat":
				msg = tea.KeyPressMsg{Code: '1', IsRepeat: true}
			case "different-session":
				m.monitorPrompt.session = "root-two"
			case "clipped":
				m.width, m.height = 40, 12
			case "paste":
				msg = tea.PasteMsg{Content: "1"}
			case "secret":
				m.monitorPrompt.input.secret = true
			}
			m, cmd := approvalEvent(m, msg)
			if cmd != nil || m.monitorApprovalBusy || m.monitorApprovalConfirm != "" {
				t.Fatal("unrelated input became an approval action")
			}
			if scenario == "secret" && len(m.monitorDrafts) != 0 {
				t.Fatal("secret answer persisted as an ordinary draft")
			}
		})
	}
}

func approvalEvent(m Model, msg tea.Msg) (Model, tea.Cmd) {
	n, cmd := m.Update(msg)
	return n.(Model), cmd
}

func enhancedApprovalModel() Model {
	m, _ := approvalEvent(approvalTestModel(), tea.KeyboardEnhancementsMsg{Flags: ansi.KittyReportEventTypes})
	return m
}

func TestApprovalNumberRequiresRelease(t *testing.T) {
	for _, wide := range []bool{false, true} {
		m := enhancedApprovalModel()
		if wide {
			m.width, m.height = 180, 60
			m.monitorSessionData = m.monitorSessionData[:1]
			m.setRowContext("root-one", contextWide)
		}
		m, _ = approvalEvent(m, key('1'))
		if !strings.Contains(m.render(), "(1)") || m.monitorApprovalConfirm == "" {
			t.Fatal("number did not arm confirmation")
		}
		for _, msg := range []tea.Msg{tea.KeyPressMsg{Code: '1', IsRepeat: true}, key('1')} {
			var cmd tea.Cmd
			m, cmd = approvalEvent(m, msg)
			if cmd != nil || m.monitorApprovalBusy {
				t.Fatal("held/repeated number granted without release")
			}
		}
		m, _ = approvalEvent(m, tea.KeyReleaseMsg{Code: '1'})
		m, cmd := approvalEvent(m, key('1'))
		if cmd == nil || !m.monitorApprovalBusy {
			t.Fatal("fresh second press did not grant one-time approval")
		}
		cmd()
		if m.fetcher.(*approvalTestClient).decision != "accept" {
			t.Fatal("wrong decision")
		}
	}
}

func TestApprovalBroaderGrantsAndExpiry(t *testing.T) {
	for _, digit := range []rune{'4', '5'} {
		m := enhancedApprovalModel()
		m, _ = approvalEvent(m, key(digit))
		m, _ = approvalEvent(m, tea.KeyReleaseMsg{Code: digit})
		m, cmd := approvalEvent(m, key(digit))
		if cmd != nil || m.monitorApprovalBusy {
			t.Fatal("number confirmed broader grant")
		}
		m, cmd = approvalEvent(m, key('c'))
		if cmd == nil || !m.monitorApprovalBusy {
			t.Fatal("C did not confirm broader grant")
		}
	}
	for _, confirm := range []rune{'1', 'c'} {
		m := enhancedApprovalModel()
		m, _ = approvalEvent(m, key('1'))
		m, _ = approvalEvent(m, tea.KeyReleaseMsg{Code: '1'})
		m.monitorApprovalConfirmUntil = time.Now().Add(-time.Second)
		m, cmd := approvalEvent(m, key(confirm))
		if cmd != nil || m.monitorApprovalBusy {
			t.Fatal("expired confirmation granted permission")
		}
	}
	m := enhancedApprovalModel()
	m, _ = approvalEvent(m, key('1'))
	m, cmd := approvalEvent(m, key('c'))
	if cmd == nil || !m.monitorApprovalBusy {
		t.Fatal("C alternative for one-time approval was lost")
	}
}

func TestApprovalRepeatFallbackAndCancellation(t *testing.T) {
	m := approvalTestModel() // no event-type support
	m, _ = approvalEvent(m, key('1'))
	m, _ = approvalEvent(m, tea.KeyReleaseMsg{Code: '1'})
	m, cmd := approvalEvent(m, key('1'))
	if cmd != nil || m.monitorApprovalBusy || !strings.Contains(m.render(), "(C)") {
		t.Fatal("legacy terminal did not retain safe C confirmation")
	}
	m = enhancedApprovalModel()
	m, _ = approvalEvent(m, key('1'))
	m, _ = approvalEvent(m, tea.KeyReleaseMsg{Code: '1'})
	m.monitorSessionData[0].preview.ApprovalToken = "replacement"
	m.fetcher.(*approvalTestClient).token = "replacement"
	m, cmd = approvalEvent(m, key('1'))
	if cmd != nil || m.monitorApprovalBusy {
		t.Fatal("previous confirmation approved replacement request")
	}
	next, _ := m.pressViewTab(viewBars)
	m = next.(Model)
	if m.monitorApprovalConfirm != "" {
		t.Fatal("changing tabs retained confirmation")
	}
}

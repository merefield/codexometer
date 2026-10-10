package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMacSessionWebLinkOpeningIsLocalAndLiteral(t *testing.T) {
	for _, remote := range []string{"SSH_TTY", "SSH_CONNECTION", "SSH_CLIENT"} {
		if localSessionWebLinkEnvironment(func(key string) string {
			if key == remote {
				return "remote"
			}
			return ""
		}) {
			t.Fatal("remote Mac launched host browser")
		}
	}
	if !localSessionWebLinkEnvironment(func(string) string { return "" }) {
		t.Fatal("local opening unavailable")
	}
	target := "https://example.com/?q=$(touch%20should-not-run)&x=1"
	command := sessionWebLinkCommand(target)
	if command.Path != "/usr/bin/open" || !reflect.DeepEqual(command.Args, []string{"/usr/bin/open", target}) {
		t.Fatal("URL was passed through shell or modified")
	}
}
func TestMacWebLinkClickDoesNotNavigateOrApprove(t *testing.T) {
	for _, key := range []string{"SSH_TTY", "SSH_CONNECTION", "SSH_CLIENT"} {
		t.Setenv(key, "")
	}
	target := "https://example.com/" + strings.Repeat("path-", 12)
	m := contextTestModel()
	m.width, m.height = 100, 40
	m.setRowContext("root-one", contextFull)
	m.monitorSessionData[0].preview = codex.SessionContext{Kind: codex.SessionContextReply, Text: "[release](" + target + ")", ThreadID: "root-one"}
	for y, line := range strings.Split(m.View().Content, "\n") {
		for x := 0; x < ansi.StringWidth(line); x++ {
			if sessionWebLinkAt(line, x, 0) != target {
				continue
			}
			next, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			n := next.(Model)
			// Do not execute cmd: regression tests must never launch a real browser.
			if cmd == nil || n.monitorContextDetail != m.monitorContextDetail || n.monitorApprovalConfirm != "" || n.monitorPrompt.input.Focused() {
				t.Fatal("link click navigated, approved, or focused composer")
			}
			return
		}
	}
	t.Fatal("no clickable session link")
}

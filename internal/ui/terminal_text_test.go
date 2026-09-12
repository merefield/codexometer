package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/merefield/codexometer/internal/codex"
)

func TestSessionDisplayRejectsTerminalControlsWithoutChangingRequests(t *testing.T) {
	attack := "\x1b]52;c;CLIPBOARD_PAYLOAD\a\x1b[2J\x1b]8;;https://evil.invalid\aLINK\x1b]8;;\a\u202e"
	m := contextTestModel()
	s := &m.monitorSessionData[0]
	s.id = attack + "session"
	s.workingDirectory = "/projects/" + attack + "日本語"
	s.preview.ThreadID = s.id
	s.preview.Source = attack + "LOCAL"
	s.preview.Text = attack + "Reply\nSecond line"
	s.preview.CommandDetails = codex.ApprovalCommandDetails{Command: attack + "git status", Directory: s.workingDirectory, Justification: attack + "Check status"}
	before := *s
	m.monitorSelectedID, m.monitorContextExpanded = s.id, s.id
	m.monitorContextDetail = s.id
	client := &promptTestClient{offer: codex.SessionPromptOffer{Token: "prompt", ThreadID: s.id}}
	m.fetcher = client
	if m.monitorPromptOffer().Token != "prompt" {
		t.Fatal("composer did not route using the original session ID")
	}
	outputs := []string{
		m.renderMonitorSessionMetrics(120, 20, *s, "", paletteFor(themeHacker)),
		m.renderMonitorContextRow(120, 20, "", *s, paletteFor(themeHacker)),
		strings.Join(expandedContextLines(120, *s), "\n"),
		strings.Join(m.contextDetailLines(120), "\n"),
		m.renderMonitorPrompt(120, 20, paletteFor(themeHacker)),
	}
	for _, output := range outputs {
		for _, forbidden := range []string{"\x1b]", "\x1b[2J", "\u202e", "CLIPBOARD_PAYLOAD", "evil.invalid"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("unsafe %q in %q", forbidden, output)
			}
		}
		if !utf8.ValidString(output) {
			t.Fatal("invalid UTF-8 display")
		}
	}
	if s.id != before.id || s.workingDirectory != before.workingDirectory || s.preview != before.preview {
		t.Fatal("display sanitation mutated routing or command data")
	}
	if client.offer.ThreadID != before.id || m.monitorPromptOffer().Token != "prompt" {
		t.Fatal("composer display changed the raw routing ID")
	}
}

func TestTerminalLabelPreservesOrdinaryTextAndUnicodeIDs(t *testing.T) {
	for _, text := range []string{"/work/my project", "LOCAL", "日本語の作業", "répertoire"} {
		if terminalLabel(text) != text {
			t.Fatalf("changed ordinary text %q", text)
		}
	}
	if terminalLabel("one\ntwo") != "one two" {
		t.Fatal("label retained extra lines")
	}
	if id := shortSessionID("日本語の作業"); !utf8.ValidString(id) || len([]rune(id)) != 5 {
		t.Fatalf("split unicode ID: %q", id)
	}
}

func TestApprovalCommandDisplaySanitization(t *testing.T) {
	m := contextTestModel()
	s := &m.monitorSessionData[0]
	attack := "\x1b]52;c;CLIPBOARD_PAYLOAD\a\x1b[2J\x1b]8;;https://evil.invalid\a\x1b]8;;\a\u202e"
	s.preview.Kind = codex.SessionContextApproval
	s.preview.CommandDetails = codex.ApprovalCommandDetails{
		Command:       attack + "git status",
		Justification: attack + "Check working tree",
		Directory:     attack + "/work/project",
	}
	before := s.preview
	m.monitorSelectedID, m.monitorContextDetail = s.id, s.id
	var command, document strings.Builder
	for _, line := range m.contextDetailDocument(120) {
		document.WriteString(line.text)
		if line.kind == "command" {
			command.WriteString(line.text)
		}
	}
	if !strings.Contains(command.String(), "git status") {
		t.Fatal("structured command branch not rendered")
	}
	for _, want := range []string{"Check working tree", "/work/project"} {
		if !strings.Contains(document.String(), want) {
			t.Fatalf("missing structured field %q", want)
		}
	}
	for _, forbidden := range []string{"\x1b", "\u202e", "CLIPBOARD_PAYLOAD", "evil.invalid"} {
		if strings.Contains(document.String(), forbidden) {
			t.Fatalf("unsafe structured field %q", forbidden)
		}
	}
	if s.preview != before {
		t.Fatal("rendering changed original approval request")
	}
}

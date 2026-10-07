package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestCurrentTaskAndStreamingDetail(t *testing.T) {
	m := approvalTestModel()
	c := codex.SessionContext{Kind: codex.SessionContextReply, Text: "Partial reply", CurrentTask: "Original task", LatestGuidance: "New guidance", Streaming: true}
	m.monitorSessionData[0].preview = c
	text := strings.Join(m.contextDetailLines(80), "\n")
	for _, want := range []string{"CURRENT TASK", "Original task", "LATEST GUIDANCE", "New guidance", "REPLY // STREAMING", "Partial reply"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	if strings.Index(text, "Original task") > strings.Index(text, "Partial reply") {
		t.Fatal("task not above reply")
	}
	wide := strings.Join(expandedContextLines(80, m.monitorSessionData[0]), "\n")
	if !strings.Contains(wide, "Original task") || !strings.Contains(wide, "New guidance") {
		t.Fatal(wide)
	}
	c.Text = strings.Repeat("reply line\n", 100)
	m.monitorSessionData[0].preview = c
	m.monitorContextScroll = m.monitorContextScrollLimit()
	old := m.monitorContextScroll
	s := m.monitorSessionData[0]
	c.Text += strings.Repeat("more lines\n", 10)
	usage := codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: s.id, TotalTokens: s.latest, Active: true, Working: true, Context: c}}}
	m.syncMonitorSessions(usage, time.Now())
	if m.monitorContextScroll <= old || m.monitorContextScroll != m.monitorContextScrollLimit() {
		t.Fatal("did not follow growing reply")
	}
	m.scrollMonitorContext(-5)
	old = m.monitorContextScroll
	usage.Sessions[0].Context.Text += strings.Repeat("more\n", 10)
	m.syncMonitorSessions(usage, time.Now())
	if m.monitorContextScroll != old {
		t.Fatal("reading position stolen")
	}
}

func TestCompletedReplyRetainsTaskWithRoom(t *testing.T) {
	m := approvalTestModel()
	s := m.monitorSessionData[0]
	s.preview = codex.SessionContext{Kind: codex.SessionContextReply, Text: "Finished reply", CurrentTask: "Original task", LatestGuidance: "Use Go"}
	s.attention = codex.SessionAttentionComplete
	m.monitorSessionData[0] = s
	doc := strings.Join(m.contextDetailLines(80), "\n")
	if !strings.Contains(doc, "Original task") || !strings.Contains(doc, "Use Go") || strings.Contains(doc, "CURRENT TASK") || contextTaskTitle(s.preview) != "TASK" {
		t.Fatal(doc)
	}
	tall := m.renderExpandedContext(80, 24, s, paletteFor(m.theme))
	if !strings.Contains(tall, "Original task") || !strings.Contains(tall, "Finished reply") {
		t.Fatal(tall)
	}
	short := m.renderExpandedContext(80, 4, s, paletteFor(m.theme))
	if strings.Contains(short, "Original task") || !strings.Contains(short, "Finished reply") {
		t.Fatal(short)
	}
}

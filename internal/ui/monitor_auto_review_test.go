package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestMonitorNativeReviewPreservesConversationAndHumanApproval(t *testing.T) {
	m := contextTestModel()
	s := &m.monitorSessionData[0]
	s.working, s.active, s.attention = true, true, codex.SessionAttentionNone
	s.preview.Reviewer = "auto_review"
	s.preview.AutoReview = codex.SessionAutoReview{ID: "review", Status: "inProgress", Action: "ls /work", Rationale: "Inspect repository", Risk: "low", Authorization: "high"}
	m.monitorContextDetail = s.id
	badge := ansi.Strip(m.renderMonitorSessionBadge(*s, 80, paletteFor(m.theme)))
	if !strings.Contains(badge, i18n.Text("AUTO REVIEWING")) {
		t.Fatal(badge)
	}
	if strings.Contains(badge, "APPROVAL") {
		t.Fatal("native reviewer became human request")
	}
	doc := strings.Join(m.contextDetailLines(120), "\n")
	for _, text := range []string{s.preview.Text, "AUTO REVIEW", "ls /work", "Inspect repository"} {
		if !strings.Contains(doc, text) {
			t.Fatalf("lost context/review %q: %s", text, doc)
		}
	}
	s.attention = codex.SessionAttentionApproval
	badge = ansi.Strip(m.renderMonitorSessionBadge(*s, 80, paletteFor(m.theme)))
	if !strings.Contains(badge, monitorSessionAttentionLabel(*s)) || strings.Contains(badge, "AUTO REVIEWING") {
		t.Fatal("native review hid human checkpoint", badge)
	}
	metrics := ansi.Strip(m.renderMonitorSessionMetrics(48, 16, *s, "", paletteFor(m.theme)))
	if !strings.Contains(metrics, i18n.Text("REVIEW // AUTO")) {
		t.Fatal("missing reviewer metadata", metrics)
	}
}

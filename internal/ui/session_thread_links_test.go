package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
)

const previewID = "550e8400-e29b-41d4-a716-446655440000"

type threadLinkProbe struct{ Fetcher }

func (threadLinkProbe) PreviewSessionLink(_ context.Context, scope, id string) (codex.SessionLinkPreview, error) {
	if scope != "scope" || id != previewID {
		return codex.SessionLinkPreview{}, codex.ErrSavedSessions
	}
	return codex.SessionLinkPreview{Root: codex.SavedSession{ID: id, Name: "Target work"}, Turns: []codex.SavedTurn{{Reply: "Other transcript"}}}, nil
}

func TestThreadLinkResumeUsesExistingReview(t *testing.T) {
	m := contextTestModel()
	probe := &resumeFinderProbe{}
	m.fetcher, m.confirmations = probe, confirmation.Policy{Mode: confirmation.Require}
	m.finder.scope = "scope"
	m.threadLinks = &threadLinkPanel{scope: "scope", id: previewID, preview: codex.SessionLinkPreview{Root: codex.SavedSession{ID: previewID}}}
	if cmd := m.openThreadLinkTarget(); cmd != nil || probe.resumed != "" || m.actionReview == nil || m.actionReview.action != "resume-saved" || m.finder.detail != previewID {
		t.Fatal("separate open action did not reuse saved-session confirmation", m.actionReview, m.finder.detail)
	}
	next, cmd := m.Update(key('c'))
	m = next.(Model)
	if cmd == nil || probe.resumed != "" {
		t.Fatal("confirmation did not return a guarded resume command")
	}
	_, _ = m.Update(cmd())
	if probe.resumed != previewID {
		t.Fatal("confirmed resume did not target the previewed session", probe.resumed)
	}
}

func TestThreadLinkClickAndKeyboardPreviewPreserveSource(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		m := contextTestModel()
		m.fetcher = threadLinkProbe{}
		m.finder.scope = "scope"
		root := m.monitorSessionData[0].id
		m.monitorSessionData[0].preview = codex.SessionContext{ThreadID: root, Kind: codex.SessionContextReply, Text: "See [Earlier work](codex://threads/" + previewID + ")."}
		m.setRowContext(root, contextFull)
		m.monitorPrompt.session = root
		m.monitorPrompt.input = newMonitorEditor()
		m.monitorPrompt.input.SetValue("Unsent draft")
		m.monitorContextScroll = 0
		if view := m.View().Content; strings.Contains(view, threadLinkMarker) || strings.Contains(view, "codex://threads/") || !strings.Contains(ansi.Strip(view), "Earlier work") {
			t.Fatal("ordinary View should render the link label without any internal href")
		}
		var cmd tea.Cmd
		apply := func(msg tea.Msg) { next, task := m.Update(msg); m, cmd = next.(Model), task }
		if keyboard {
			apply(key('l'))
			if m.threadLinks == nil || len(m.threadLinks.choices) != 1 {
				t.Fatal("keyboard did not open link picker")
			}
			apply(specialKey(tea.KeyEnter))
		} else {
			x, y := renderedTextStart(t, m, "Earlier work")
			apply(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		}
		if cmd == nil || m.threadLinks.id != previewID {
			t.Fatal("link activation did not request preview", m.threadLinks)
		}
		apply(cmd())
		if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Other transcript") || m.monitorContextDetail != root {
			t.Fatal("preview retargeted source or did not display transcript", view)
		}
		apply(specialKey(tea.KeyEscape))
		if m.threadLinks != nil || m.monitorContextDetail != root || m.monitorPrompt.input.Value() != "Unsent draft" {
			t.Fatal("escape did not restore source")
		}
	}
}

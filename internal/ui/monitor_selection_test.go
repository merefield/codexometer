package ui

import (
	"slices"
	"testing"
	"time"
)

func TestMonitorListSelectionUsesCurrentFilterAndVisibility(t *testing.T) {
	for _, tc := range []struct {
		filter monitorStatusFilter
		ids    []string
	}{
		{monitorFilterAll, []string{"work", "approval", "input", "check", "complete", "idle"}},
		{monitorFilterActive, []string{"work", "approval", "input", "complete"}},
		{monitorFilterWorking, []string{"work"}},
		{monitorFilterApproval, []string{"approval"}},
		{monitorFilterInput, []string{"input"}},
		{monitorFilterErrors, nil},
		{monitorFilterCheck, []string{"check"}},
	} {
		for _, selected := range []string{"", "work", "approval", "input", "check", "complete", "idle", "dismissed", "undisplayed", "absent"} {
			m := statusFilterTestModel()
			m.monitorFilter, m.monitorSelectedID = tc.filter, selected
			m.monitorSummaryFocused, m.monitorScroll = true, 3
			ids, index := m.monitorListSelection()
			if !slices.Equal(ids, tc.ids) || index != slices.Index(tc.ids, selected) {
				t.Fatalf("filter=%d selected=%q: got %v/%d, want %v/%d", tc.filter, selected, ids, index, tc.ids, slices.Index(tc.ids, selected))
			}
			if m.monitorSelectedID != selected || m.monitorFilter != tc.filter || !m.monitorSummaryFocused || m.monitorScroll != 3 || m.monitorRecordedTokens() != 625 {
				t.Fatal("list projection changed selection, focus, scrolling or accounting")
			}
		}
	}
}

func TestMonitorListSelectionKeepsNavigationAndDismissalScoped(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		filter                  monitorStatusFilter
		selected, next, dismiss string
		dismisses               bool
	}{
		{"middle row", monitorFilterAll, "approval", "input", "input", true},
		{"last row", monitorFilterAll, "idle", "idle", "complete", true},
		{"only matching row", monitorFilterApproval, "approval", "approval", "", true},
		{"selection outside filter", monitorFilterApproval, "work", "approval", "", false},
		{"no matching rows", monitorFilterErrors, "work", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := statusFilterTestModel()
			m.monitorFilter, m.monitorSelectedID = tc.filter, tc.selected
			m.selectMonitorSession(1)
			if m.monitorSelectedID != tc.next || m.monitorFilter != tc.filter || len(m.monitorDismissed) != 1 {
				t.Fatal("navigation changed filtering or dismissed a session")
			}
			if tc.filter == monitorFilterErrors && !m.monitorSummaryFocused {
				t.Fatal("empty-list navigation did not return focus to the summary")
			}
			m = statusFilterTestModel()
			m.monitorFilter, m.monitorSelectedID = tc.filter, tc.selected
			m.dismissSelectedMonitorSession()
			_, dismissed := m.monitorDismissed[tc.selected]
			if m.monitorSelectedID != tc.dismiss || dismissed != tc.dismisses || m.monitorFilter != tc.filter || m.monitorRecordedTokens() != 625 {
				t.Fatal("dismissal did not preserve filtered selection and accounting")
			}
			if !tc.dismisses && m.monitorSummaryFocused {
				t.Fatal("invalid dismissal unexpectedly moved focus")
			}
		})
	}
}

func TestMonitorListNavigationPreservesDraftAndDismissalWatermark(t *testing.T) {
	m := statusFilterTestModel()
	m.monitorFilter, m.monitorSelectedID, m.monitorContextExpanded = monitorFilterActive, "input", "input"
	m.monitorApprovalConfirm = "old approval"
	m.monitorPrompt = monitorPromptState{session: "input", input: newMonitorEditor()}
	m.monitorPrompt.input.SetValue("keep this draft")
	m.selectMonitorSession(1)
	if m.monitorSelectedID != "complete" || m.monitorContextExpanded != "" || m.monitorApprovalConfirm != "" || m.monitorPrompt.session != "" || m.monitorDrafts["input"] != "keep this draft" {
		t.Fatal("navigation lost the draft or retained old-session approval/editor state")
	}
	m.monitorSessionData[4].latest = 42
	m.monitorSessionData[4].callSequence, m.monitorSessionData[4].turnSequence = 9, 2
	m.monitorSessionData[4].lastActivity = time.Unix(100, 0)
	m.dismissSelectedMonitorSession()
	watermark := m.monitorDismissed["complete"]
	if m.monitorSelectedID != "input" || watermark.latest != 42 || watermark.callSequence != 9 || watermark.turnSequence != 2 || !watermark.lastActivity.Equal(time.Unix(100, 0)) || m.monitorDrafts["input"] != "keep this draft" {
		t.Fatal("dismissal lost activity watermarks, filtered selection or the saved draft")
	}
}

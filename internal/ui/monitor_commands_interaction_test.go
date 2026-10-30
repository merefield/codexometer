package ui

import (
	"fmt"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/merefield/codexometer/internal/codex"
)

func TestMonitorPickerClicksRespectConfirmationPolicy(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, width := range []int{60, 120, 180} {
			for _, reversible := range []bool{false, true} {
				for _, noConfirm := range []bool{false, true} {
					t.Run(fmt.Sprintf("inline=%t/width=%d/reversible=%t/noConfirm=%t", inline, width, reversible, noConfirm), func(t *testing.T) {
						m, f := speedCommandTestModel(t)
						m.width = width
						m.SetNoConfirm(noConfirm)
						m.monitorCommands.menu.Choices[0].Reversible = reversible
						if inline {
							m.monitorCommands.inline = true
							m.monitorCommands.prefix = "/fast "
							m.monitorPrompt.input.SetValue("/fast ")
							m.monitorPrompt.input.Focus()
						}
						x, y := renderedTextStart(t, m, "Standard (default)")
						next, _ := m.Update(tea.MouseMotionMsg{X: x, Y: y})
						m = next.(Model)
						if m.monitorCommands.busy || f.calls != 0 || m.monitorCommands.selected != 2 {
							t.Fatal("hover selected or applied an option")
						}
						next, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
						m = next.(Model)
						if m.monitorCommands.selected != 0 || f.calls != 0 {
							t.Fatal("click did not select the rendered option or sent before command execution")
						}
						if direct := reversible || noConfirm; (cmd != nil) != direct || m.monitorCommands.busy != direct {
							t.Fatal("picker click ignored the confirmation policy")
						}
						if cmd == nil {
							if m.monitorCommands.reviewedChoice != "" || m.monitorCommands.detail {
								t.Fatal("guarded picker click did more than highlight")
							}
							return
						}
						_, duplicate := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
						if duplicate != nil {
							t.Fatal("busy picker permitted a duplicate submission")
						}
						cmd()
						if f.calls != 1 || f.choice != "default" {
							t.Fatal("picker did not apply exactly the clicked choice")
						}
					})
				}
			}
		}
	}
}

func TestMonitorCommandRightRepeatKeepsTextCursorMovement(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, path := range []string{"rename", "cd", "session/name", "model"} {
			if !inline && path == "model" {
				continue // Modal choices use Right to choose, not edit a filter.
			}
			t.Run(fmt.Sprintf("inline=%t/path=%s", inline, path), func(t *testing.T) {
				m, f := commandTestModel(t)
				p := &m.monitorCommands
				p.menu.Path = path
				p.menu.Input = path != "model"
				p.input = textinput.New()
				p.input.SetValue("abc")
				p.input.Focus()
				p.input.CursorEnd()
				if inline {
					p.inline = true
					p.prefix = "/" + path + " "
					m.monitorPrompt.input.SetValue(p.prefix + "abc")
					m.monitorPrompt.input.Focus()
					m.monitorPrompt.input.CursorEnd()
				}
				for _, k := range []tea.KeyPressMsg{
					{Code: tea.KeyLeft}, {Code: tea.KeyLeft},
					{Code: tea.KeyRight, IsRepeat: true}, key('X'),
				} {
					next, _ := m.Update(k)
					m = next.(Model)
				}
				got := m.monitorCommands.input.Value()
				want := "abXc"
				if inline {
					got = m.monitorPrompt.input.Value()
					want = m.monitorCommands.prefix + want
				}
				if got != want || f.calls != 0 {
					t.Fatalf("held Right lost cursor movement: got %q, want %q (calls=%d)", got, want, f.calls)
				}
				next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, IsRepeat: true})
				if cmd != nil || next.(Model).monitorCommands.busy || f.calls != 0 {
					t.Fatal("held Enter submitted text or opened a review")
				}
			})
		}
	}
}

func TestMonitorCommandRepeatGuardsOnlySubmissionKeys(t *testing.T) {
	m, f := commandTestModel(t)
	m.SetNoConfirm(true)
	for _, code := range []rune{tea.KeyEnter, tea.KeyRight} {
		next, cmd := m.Update(tea.KeyPressMsg{Code: code, IsRepeat: true})
		m = next.(Model)
		if cmd != nil || m.monitorCommands.busy || f.calls != 0 {
			t.Fatal("held choice activation sent a command")
		}
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if cmd == nil {
		t.Fatal("ordinary Right no longer activates a choice")
	}

	for _, inline := range []bool{false, true} {
		m, f := commandTestModel(t)
		m.monitorCommands.inline = inline
		m.monitorCommands.menu.Multiple = true
		m.monitorCommands.menu.Choices = append(m.monitorCommands.menu.Choices, codex.SessionCommandChoice{ID: "second", Label: "Second"})
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight, IsRepeat: true})
		m = next.(Model)
		if cmd != nil || f.calls != 0 || m.monitorCommands.selected != 1 || m.monitorCommands.menu.Choices[1].ID != "choice" {
			t.Fatal("held Right blocked local checkbox reordering", inline)
		}
	}
}

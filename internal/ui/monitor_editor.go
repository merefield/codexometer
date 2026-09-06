package ui

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// The textarea owns wrapping and cursor scrolling. Password questions keep
// the dedicated masked widget: textarea does not provide password echo.
type monitorEditor struct {
	area     textarea.Model
	password textinput.Model
	secret   bool
	ready    bool
}

func newMonitorEditor() monitorEditor {
	a := textarea.New()
	a.ShowLineNumbers = false
	a.Prompt = "> "
	a.CharLimit = 4096
	a.MaxWidth = 0
	a.DynamicHeight = true
	a.MinHeight = 1
	a.MaxContentHeight = 8192 // viewport cap must not truncate the draft
	a.SetHeight(1)
	a.KeyMap.InsertNewline.SetEnabled(false) // Enter is handled as submit
	p := textinput.New()
	p.CharLimit = 4096
	p.EchoMode = textinput.EchoPassword
	return monitorEditor{area: a, password: p, ready: true}
}

func (e *monitorEditor) configure(width, height int) {
	if !e.ready {
		return
	}
	e.area.MaxHeight = max(height-8, 1)
	e.area.SetWidth(max(width-4, 1))
	e.password.SetWidth(max(width-6, 1))
}
func (e *monitorEditor) setSecret(secret bool) {
	if e.secret == secret {
		return
	}
	focused := e.Focused()
	e.Reset()
	e.Blur()
	e.secret = secret
	if focused {
		e.Focus()
	}
}
func (e monitorEditor) Value() string {
	if !e.ready {
		return ""
	}
	if e.secret {
		return e.password.Value()
	}
	return e.area.Value()
}
func (e *monitorEditor) SetValue(s string) {
	if e.secret {
		e.password.SetValue(s)
	} else {
		e.area.SetValue(s)
	}
}
func (e *monitorEditor) Reset() {
	if !e.ready {
		return
	}
	e.area.Reset()
	e.password.Reset()
}
func (e monitorEditor) Focused() bool {
	if !e.ready {
		return false
	}
	if e.secret {
		return e.password.Focused()
	}
	return e.area.Focused()
}
func (e *monitorEditor) Focus() tea.Cmd {
	if e.secret {
		return e.password.Focus()
	}
	return e.area.Focus()
}
func (e *monitorEditor) Blur() {
	if !e.ready {
		return
	}
	e.area.Blur()
	e.password.Blur()
}
func (e *monitorEditor) CursorEnd() {
	if e.secret {
		e.password.CursorEnd()
	} else {
		e.area.CursorEnd()
	}
}
func (e monitorEditor) Height() int {
	if !e.ready || e.secret {
		return 1
	}
	return max(e.area.Height(), 1)
}
func (e monitorEditor) Update(msg tea.Msg) (monitorEditor, tea.Cmd) {
	var cmd tea.Cmd
	if e.secret {
		e.password, cmd = e.password.Update(msg)
	} else {
		e.area, cmd = e.area.Update(msg)
	}
	return e, cmd
}
func (e monitorEditor) View(colors palette) string {
	if e.secret {
		s := e.password.Styles()
		s.Focused.Text = colors.label()
		s.Focused.Prompt = colors.label().Foreground(colors.primary)
		s.Cursor.Color = colors.primary
		e.password.SetStyles(s)
		return e.password.View()
	}
	s := e.area.Styles()
	s.Focused.Text = colors.label()
	s.Focused.Prompt = colors.label().Foreground(colors.primary)
	s.Focused.CursorLine = colors.label()
	s.Cursor.Color = colors.primary
	e.area.SetStyles(s)
	return e.area.View()
}

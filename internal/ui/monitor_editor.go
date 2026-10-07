package ui

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"image/color"
)

// The textarea owns wrapping and cursor scrolling. Password questions keep
// the dedicated masked widget: textarea does not provide password echo.
type monitorEditor struct {
	area          *textarea.Model
	password      *textinput.Model
	secret        bool
	ready         bool
	width, height int
	styleName     string
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
	return monitorEditor{area: &a, password: &p, ready: true}
}

// Keep inactive editors cheap to carry through Bubble Tea's value models.
// Detach the widget before mutation, preserving the previous value-copy
// semantics without copying both large widgets on every layout query.
func (e *monitorEditor) editArea() *textarea.Model {
	a := *e.area
	e.area = &a
	return e.area
}

func (e *monitorEditor) editPassword() *textinput.Model {
	p := *e.password
	e.password = &p
	return e.password
}

func (e *monitorEditor) configure(width, height int) {
	if !e.ready {
		return
	}
	if e.width == width && e.height == height {
		return
	}
	e.width, e.height = width, height
	e.editArea()
	e.editPassword()
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
		e.editPassword().SetValue(s)
	} else {
		e.editArea().SetValue(s)
	}
}
func (e *monitorEditor) Reset() {
	if !e.ready {
		return
	}
	e.editArea().Reset()
	e.editPassword().Reset()
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
		return e.editPassword().Focus()
	}
	return e.editArea().Focus()
}
func (e *monitorEditor) Blur() {
	if !e.ready {
		return
	}
	e.editArea().Blur()
	e.editPassword().Blur()
}
func (e *monitorEditor) CursorEnd() {
	if e.secret {
		e.editPassword().CursorEnd()
	} else {
		e.editArea().CursorEnd()
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
		p, command := e.password.Update(msg)
		e.password, cmd = &p, command
	} else {
		a, command := e.area.Update(msg)
		e.area, cmd = &a, command
	}
	return e, cmd
}
func (e *monitorEditor) style(colors palette) {
	if !e.ready || e.styleName == colors.name {
		return
	}
	e.styleName = colors.name
	e.editArea()
	e.editPassword()
	s := e.password.Styles()
	s.Focused.Text = colors.label()
	s.Focused.Prompt = colors.label().Foreground(colors.primary)
	s.Cursor.Color = colors.primary
	e.password.SetStyles(s)
	areaStyles := e.area.Styles()
	tint := monitorComposerBackground(colors)
	areaStyles.Focused.Base = areaStyles.Focused.Base.Background(tint)
	areaStyles.Blurred.Base = areaStyles.Blurred.Base.Background(tint)
	areaStyles.Focused.Text = colors.label().Background(tint)
	areaStyles.Focused.Prompt = colors.label().Foreground(colors.primary)
	areaStyles.Focused.CursorLine = colors.label().Background(tint)
	areaStyles.Cursor.Color = colors.primary
	e.area.SetStyles(areaStyles)
}

func (e monitorEditor) View(colors palette) string {
	e.style(colors)
	if e.secret {
		return e.password.View()
	}
	return e.area.View()
}

func monitorComposerBackground(colors palette) color.RGBA {
	pr, pg, pb, _ := colors.primary.RGBA()
	br, bg, bb, _ := colors.background.RGBA()
	return color.RGBA{uint8((pr + br*9) / 10 >> 8), uint8((pg + bg*9) / 10 >> 8), uint8((pb + bb*9) / 10 >> 8), 255}
}

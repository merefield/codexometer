package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

type Fetcher interface {
	Fetch(context.Context) (codex.Snapshot, error)
}

type Model struct {
	fetcher       Fetcher
	refreshEvery  time.Duration
	snapshot      codex.Snapshot
	err           error
	width         int
	height        int
	loading       bool
	lastRefresh   time.Time
	nextRefresh   time.Time
	phase         int
	theme         themeID
	meterStyle    meterStyleID
	hoveredButton footerButtonID
	flashedButton footerButtonID
	flashSequence uint64
}

type fetchedMsg struct {
	snapshot codex.Snapshot
	err      error
}

type secondMsg time.Time
type refreshMsg time.Time

type footerButtonFlashExpiredMsg struct {
	button   footerButtonID
	sequence uint64
}

const footerButtonFlashDuration = 150 * time.Millisecond

type footerButtonID int

const (
	footerButtonNone footerButtonID = iota
	footerButtonTheme
	footerButtonStyle
	footerButtonRefresh
	footerButtonQuit
)

type footerButton struct {
	id      footerButtonID
	label   string
	compact string
}

var footerButtonDefinitions = []footerButton{
	{id: footerButtonTheme, label: "[ (T)HEME ]", compact: "[T]"},
	{id: footerButtonStyle, label: "[ (S)TYLE ]", compact: "[S]"},
	{id: footerButtonRefresh, label: "[ (R)EFRESH ]", compact: "[R]"},
	{id: footerButtonQuit, label: "[ (Q)UIT ]", compact: "[Q]"},
}

func New(fetcher Fetcher, refreshEvery time.Duration) Model {
	if refreshEvery <= 0 {
		refreshEvery = time.Minute
	}
	return Model{
		fetcher:      fetcher,
		refreshEvery: refreshEvery,
		loading:      true,
		nextRefresh:  time.Now().Add(refreshEvery),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetch(), secondTick(), refreshTick(m.refreshEvery))
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch strings.ToLower(message.String()) {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "t":
			return m.pressFooterButton(footerButtonTheme)
		case "s":
			return m.pressFooterButton(footerButtonStyle)
		case "r":
			return m.pressFooterButton(footerButtonRefresh)
		case "q":
			return m.pressFooterButton(footerButtonQuit)
		}
	case tea.MouseMsg:
		button := m.footerButtonAt(message.X, message.Y)
		m.hoveredButton = button
		if message.Button == tea.MouseButtonLeft && message.Action == tea.MouseActionPress && button != footerButtonNone {
			return m.pressFooterButton(button)
		}
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
	case fetchedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.snapshot = message.snapshot
			m.lastRefresh = time.Now()
		}
	case secondMsg:
		m.phase++
		return m, secondTick()
	case refreshMsg:
		m.loading = true
		m.nextRefresh = time.Now().Add(m.refreshEvery)
		return m, tea.Batch(m.fetch(), refreshTick(m.refreshEvery))
	case footerButtonFlashExpiredMsg:
		if message.sequence == m.flashSequence && message.button == m.flashedButton {
			m.flashedButton = footerButtonNone
		}
	}
	return m, nil
}

func (m Model) pressFooterButton(button footerButtonID) (tea.Model, tea.Cmd) {
	m.flashSequence++
	m.flashedButton = button
	sequence := m.flashSequence
	if button == footerButtonQuit {
		return m, tea.Tick(footerButtonFlashDuration, func(time.Time) tea.Msg { return tea.Quit() })
	}

	updated, action := m.activateFooterButton(button)
	expire := tea.Tick(footerButtonFlashDuration, func(time.Time) tea.Msg {
		return footerButtonFlashExpiredMsg{button: button, sequence: sequence}
	})
	if action == nil {
		return updated, expire
	}
	return updated, tea.Batch(action, expire)
}

func (m Model) activateFooterButton(button footerButtonID) (Model, tea.Cmd) {
	switch button {
	case footerButtonTheme:
		m.theme = m.theme.next()
	case footerButtonStyle:
		m.meterStyle = m.meterStyle.next()
	case footerButtonRefresh:
		if !m.loading {
			m.loading = true
			return m, m.fetch()
		}
	}
	return m, nil
}

func (m Model) footerButtonAt(x, y int) footerButtonID {
	if x < 0 || y < 0 {
		return footerButtonNone
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if y >= len(lines) {
		return footerButtonNone
	}
	buttons, _ := footerButtonLayout(m.contentWidth())
	for _, button := range buttons {
		start := strings.Index(lines[y], button.label)
		if start >= 0 && x >= start && x < start+len(button.label) {
			return button.id
		}
	}
	return footerButtonNone
}

func (m Model) contentWidth() int {
	width := m.width
	if width == 0 {
		width = 80
	}
	return max(width-4, 1)
}

func footerButtonLayout(width int) ([]footerButton, string) {
	buttons := make([]footerButton, len(footerButtonDefinitions))
	copy(buttons, footerButtonDefinitions)
	separator := "  "
	total := len(separator) * (len(buttons) - 1)
	for _, button := range buttons {
		total += len(button.label)
	}
	if total <= width {
		return buttons, separator
	}
	separator = " "
	for index := range buttons {
		buttons[index].label = buttons[index].compact
	}
	return buttons, separator
}

func (m Model) fetch() tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.fetcher.Fetch(context.Background())
		return fetchedMsg{snapshot: snapshot, err: err}
	}
}

func secondTick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return secondMsg(now) })
}

func refreshTick(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(now time.Time) tea.Msg { return refreshMsg(now) })
}

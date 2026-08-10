package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/merefield/codexometer/internal/codex"
)

type Fetcher interface {
	Fetch(context.Context) (codex.Snapshot, error)
}

type Model struct {
	fetcher      Fetcher
	refreshEvery time.Duration
	snapshot     codex.Snapshot
	err          error
	width        int
	height       int
	loading      bool
	lastRefresh  time.Time
	nextRefresh  time.Time
	phase        int
	theme        themeID
	meterStyle   meterStyleID
}

type fetchedMsg struct {
	snapshot codex.Snapshot
	err      error
}

type secondMsg time.Time
type refreshMsg time.Time

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
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "t":
			m.theme = m.theme.next()
		case "s":
			m.meterStyle = m.meterStyle.next()
		case "r":
			if !m.loading {
				m.loading = true
				return m, m.fetch()
			}
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
	}
	return m, nil
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

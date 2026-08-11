package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/merefield/codexometer/internal/codex"
)

type Fetcher interface {
	Fetch(context.Context) (codex.Snapshot, error)
}

type TokenUsageFetcher interface {
	FetchTokenUsage(context.Context) (codex.LiveUsageSnapshot, error)
}

type FreshTokenUsageFetcher interface {
	FetchTokenUsageFresh(context.Context) (codex.LiveUsageSnapshot, error)
}

type Model struct {
	fetcher       Fetcher
	usageFetcher  TokenUsageFetcher
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

	stopwatchState        stopwatchState
	stopwatchStartedAt    time.Time
	stopwatchStoppedAt    time.Time
	stopwatchBaseline     int64
	stopwatchLatest       int64
	stopwatchSamples      []stopwatchSample
	stopwatchRequest      uint64
	stopwatchFetchActive  bool
	stopwatchNextSample   time.Time
	stopwatchGraphStart   int64
	stopwatchLastActivity time.Time
	stopwatchSessions     int
	stopwatchError        string
}

type fetchedMsg struct {
	snapshot codex.Snapshot
	err      error
}

type secondMsg time.Time
type refreshMsg time.Time

type stopwatchState int

const (
	stopwatchIdle stopwatchState = iota
	stopwatchStarting
	stopwatchRunning
	stopwatchStopping
	stopwatchStopped
)

type stopwatchFetchKind int

const (
	stopwatchFetchStart stopwatchFetchKind = iota
	stopwatchFetchSample
	stopwatchFetchStop
)

type stopwatchSample struct {
	at             time.Time
	intervalTokens int64
}

type stopwatchFetchedMsg struct {
	kind     stopwatchFetchKind
	sequence uint64
	usage    codex.LiveUsageSnapshot
	err      error
	at       time.Time
}

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
	footerButtonStopwatchGo
	footerButtonStopwatchStop
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
	model := Model{
		fetcher:      fetcher,
		refreshEvery: refreshEvery,
		loading:      true,
		nextRefresh:  time.Now().Add(refreshEvery),
	}
	if usageFetcher, ok := fetcher.(TokenUsageFetcher); ok {
		model.usageFetcher = usageFetcher
	}
	return model
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
		case "g":
			if m.meterStyle == styleStopwatch {
				return m.pressFooterButton(footerButtonStopwatchGo)
			}
		case "p":
			if m.meterStyle == styleStopwatch {
				return m.pressFooterButton(footerButtonStopwatchStop)
			}
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
		commands := []tea.Cmd{secondTick()}
		if m.stopwatchState == stopwatchRunning {
			now := time.Time(message)
			if !m.stopwatchNextSample.IsZero() && !now.Before(m.stopwatchNextSample) {
				delta := max(m.stopwatchLatest-m.stopwatchGraphStart, int64(0))
				m.stopwatchSamples = append(m.stopwatchSamples, stopwatchSample{at: now, intervalTokens: delta})
				m.stopwatchGraphStart = m.stopwatchLatest
				for !m.stopwatchNextSample.After(now) {
					m.stopwatchNextSample = m.stopwatchNextSample.Add(stopwatchSampleInterval)
				}
			}
			if !m.stopwatchFetchActive {
				m.stopwatchRequest++
				m.stopwatchFetchActive = true
				commands = append(commands, m.stopwatchFetch(stopwatchFetchSample, m.stopwatchRequest))
			}
		}
		return m, tea.Batch(commands...)
	case refreshMsg:
		m.loading = true
		m.nextRefresh = time.Now().Add(m.refreshEvery)
		return m, tea.Batch(m.fetch(), refreshTick(m.refreshEvery))
	case stopwatchFetchedMsg:
		if message.sequence != m.stopwatchRequest {
			return m, nil
		}
		m.stopwatchFetchActive = false
		return m.applyStopwatchFetch(message)
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
	case footerButtonStopwatchGo:
		if m.meterStyle == styleStopwatch && (m.stopwatchState == stopwatchIdle || m.stopwatchState == stopwatchStopped) {
			m.stopwatchState = stopwatchStarting
			m.stopwatchStartedAt = time.Time{}
			m.stopwatchStoppedAt = time.Time{}
			m.stopwatchSamples = nil
			m.stopwatchLastActivity = time.Time{}
			m.stopwatchSessions = 0
			m.stopwatchError = ""
			m.stopwatchRequest++
			m.stopwatchFetchActive = true
			return m, m.stopwatchFetch(stopwatchFetchStart, m.stopwatchRequest)
		}
	case footerButtonStopwatchStop:
		if m.meterStyle == styleStopwatch && m.stopwatchState == stopwatchRunning {
			m.stopwatchState = stopwatchStopping
			m.stopwatchRequest++
			m.stopwatchFetchActive = true
			return m, m.stopwatchFetch(stopwatchFetchStop, m.stopwatchRequest)
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
	if m.meterStyle == styleStopwatch {
		if button := m.stopwatchButtonAt(x, y); button != footerButtonNone {
			return button
		}
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

const stopwatchSampleInterval = 30 * time.Second

func (m Model) stopwatchFetch(kind stopwatchFetchKind, sequence uint64) tea.Cmd {
	return func() tea.Msg {
		if m.usageFetcher == nil {
			return stopwatchFetchedMsg{
				kind: kind, sequence: sequence, err: errors.New("local Codex session telemetry unavailable"), at: time.Now(),
			}
		}
		var usage codex.LiveUsageSnapshot
		var err error
		if kind == stopwatchFetchStop {
			if freshFetcher, ok := m.usageFetcher.(FreshTokenUsageFetcher); ok {
				usage, err = freshFetcher.FetchTokenUsageFresh(context.Background())
			} else {
				usage, err = m.usageFetcher.FetchTokenUsage(context.Background())
			}
		} else {
			usage, err = m.usageFetcher.FetchTokenUsage(context.Background())
		}
		return stopwatchFetchedMsg{
			kind: kind, sequence: sequence, usage: usage, err: err, at: time.Now(),
		}
	}
}

func (m Model) applyStopwatchFetch(message stopwatchFetchedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.stopwatchError = message.err.Error()
		if message.kind == stopwatchFetchStop {
			m.stopwatchState = stopwatchStopped
			m.stopwatchStoppedAt = message.at
		} else if message.kind == stopwatchFetchStart {
			m.stopwatchState = stopwatchStopped
		}
		return m, nil
	}
	switch message.kind {
	case stopwatchFetchStart:
		m.stopwatchBaseline = message.usage.TotalTokens
		m.stopwatchLatest = message.usage.TotalTokens
		m.stopwatchGraphStart = message.usage.TotalTokens
		m.stopwatchStartedAt = message.at
		m.stopwatchState = stopwatchRunning
		m.stopwatchSessions = message.usage.SessionCount
		m.stopwatchError = ""
		m.stopwatchNextSample = message.at.Add(stopwatchSampleInterval)
	case stopwatchFetchSample, stopwatchFetchStop:
		if message.usage.TotalTokens < m.stopwatchLatest {
			m.stopwatchError = "local Codex token counter moved backwards"
		} else {
			if message.usage.TotalTokens > m.stopwatchLatest {
				m.stopwatchLastActivity = message.usage.LastActivity
			}
			m.stopwatchLatest = message.usage.TotalTokens
			m.stopwatchSessions = message.usage.SessionCount
			m.stopwatchError = ""
		}
		if message.kind == stopwatchFetchStop {
			m.stopwatchState = stopwatchStopped
			m.stopwatchStoppedAt = message.at
		}
	}
	return m, nil
}

func secondTick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return secondMsg(now) })
}

func refreshTick(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(now time.Time) tea.Msg { return refreshMsg(now) })
}

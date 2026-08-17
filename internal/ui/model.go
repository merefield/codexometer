package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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

type BenchmarkRunner interface {
	BenchmarkCombinationCount(context.Context) (int, error)
	RunBenchmarkSuite(context.Context, []codex.BenchmarkTaskID, func(codex.BenchmarkEvent))
}

type Model struct {
	fetcher         Fetcher
	usageFetcher    TokenUsageFetcher
	refreshEvery    time.Duration
	snapshot        codex.Snapshot
	err             error
	width           int
	height          int
	inline          bool
	loading         bool
	lastRefresh     time.Time
	nextRefresh     time.Time
	phase           int
	theme           themeID
	meterView       meterViewID
	quotaMeterView  meterViewID
	hoveredMainTab  mainTabID
	mainTabHovered  bool
	hoveredView     meterViewID
	viewHovered     bool
	flashedView     meterViewID
	viewFlashing    bool
	viewSequence    uint64
	hoveredButton   footerButtonID
	flashedButton   footerButtonID
	flashSequence   uint64
	benchmarkRunner BenchmarkRunner
	preferenceStore PreferenceStore

	benchmarkState           benchmarkState
	benchmarkResults         []codex.BenchmarkResult
	benchmarkTotal           int
	benchmarkCompleted       int
	benchmarkCurrentModel    string
	benchmarkCurrentEffort   string
	benchmarkCurrentTask     string
	benchmarkError           string
	benchmarkPlanning        bool
	benchmarkCombinations    int
	benchmarkSelectedTask    int
	benchmarkFilter          benchmarkResultFilter
	benchmarkAllArmed        bool
	benchmarkConfirmSequence uint64
	benchmarkScroll          int
	benchmarkSort            benchmarkSortColumn
	benchmarkSortDescending  bool
	benchmarkSortHovered     bool
	benchmarkHoveredSort     benchmarkSortColumn
	benchmarkRankMode        benchmarkRankMode
	benchmarkEvents          <-chan codex.BenchmarkEvent
	benchmarkCancel          context.CancelFunc

	monitorState        monitorState
	monitorStartedAt    time.Time
	monitorStoppedAt    time.Time
	monitorBaseline     int64
	monitorLatest       int64
	monitorSamples      []monitorSample
	monitorRequest      uint64
	monitorFetchActive  bool
	monitorNextSample   time.Time
	monitorBoundaryDue  bool
	monitorGraphStart   int64
	monitorLastActivity time.Time
	monitorSessions     int
	monitorSessionData  []monitorSession
	monitorDismissed    map[string]monitorSessionDismissal
	monitorDismissHover string
	monitorDismissFlash string
	monitorDismissSeq   uint64
	monitorScroll       int
	monitorError        string
	monitorQuotaWindows []monitorQuotaWindow
	monitorQuotaError   string

	quotaAPIAnchors        map[string]quotaAPIAnchor
	quotaAPIEvidence       []quotaAPISample
	quotaAPIIssues         map[string]string
	quotaAPIAccount        string
	quotaAPITelemetryIssue string
}

type fetchedMsg struct {
	snapshot codex.Snapshot
	err      error
	usage    codex.LiveUsageSnapshot
	usageErr error
	at       time.Time
}

type secondMsg time.Time
type refreshMsg time.Time

type monitorState int

const (
	monitorIdle monitorState = iota
	monitorStarting
	monitorRunning
	monitorStopping
	monitorStopped
)

type monitorFetchKind int

const (
	monitorFetchStart monitorFetchKind = iota
	monitorFetchSample
	monitorFetchBoundary
	monitorFetchStop
)

type monitorSample struct {
	at             time.Time
	duration       time.Duration
	intervalTokens int64
}

type monitorSessionDismissal struct {
	latest           int64
	lastActivity     time.Time
	callSequence     uint64
	turnSequence     uint64
	inactiveObserved bool
}

type monitorSession struct {
	id               string
	workingDirectory string
	baseline         int64
	latest           int64
	graphStart       int64
	startedAt        time.Time
	lastActivity     time.Time
	agentCount       int
	active           bool
	attention        codex.SessionAttention
	displayed        bool
	unattributed     bool
	samples          []monitorSample
	callSequence     uint64
	turnSequence     uint64
	modelCalls       int
	lastCallAt       time.Time
	latestOutput     int64
	peakOutput       int64
	latestOutputOK   bool
	peakOutputOK     bool
	latestTTFT       time.Duration
	peakTTFT         time.Duration
	latestTTFTOK     bool
	peakTTFTOK       bool
}

type monitorQuotaWindow struct {
	key           string
	label         string
	baselineUsed  int
	latestUsed    int
	latestReset   *int64
	resetDetected bool
	partial       bool
	stale         bool
}

type monitorFetchedMsg struct {
	kind     monitorFetchKind
	sequence uint64
	usage    codex.LiveUsageSnapshot
	err      error
	quota    codex.Snapshot
	quotaErr error
	at       time.Time
}

type footerButtonFlashExpiredMsg struct {
	button   footerButtonID
	sequence uint64
}

type viewTabFlashExpiredMsg struct {
	view     meterViewID
	sequence uint64
}

type monitorSessionDismissMsg struct {
	id       string
	sequence uint64
}

type benchmarkState int

const (
	benchmarkIdle benchmarkState = iota
	benchmarkRunning
	benchmarkFinished
)

type benchmarkEventMsg struct {
	event  codex.BenchmarkEvent
	events <-chan codex.BenchmarkEvent
	ok     bool
}

type benchmarkPlanMsg struct {
	combinations int
	err          error
}

type benchmarkConfirmExpiredMsg struct {
	sequence uint64
}

type benchmarkResultFilter int

const (
	benchmarkFilterAll benchmarkResultFilter = iota
	benchmarkFilterPass
	benchmarkFilterFail
)

type benchmarkRankMode int

const (
	benchmarkRankBalanced benchmarkRankMode = iota
	benchmarkRankCost
	benchmarkRankSpeed
)

const footerButtonFlashDuration = 150 * time.Millisecond

type footerButtonID int

const (
	footerButtonNone footerButtonID = iota
	footerButtonTheme
	footerButtonView
	footerButtonRefresh
	footerButtonQuit
	footerButtonMonitorGo
	footerButtonMonitorStop
	footerButtonBenchmarkPrevious
	footerButtonBenchmarkNext
	footerButtonBenchmarkSelected
	footerButtonBenchmarkAll
	footerButtonBenchmarkFilterAll
	footerButtonBenchmarkFilterPass
	footerButtonBenchmarkFilterFail
	footerButtonBenchmarkRankCost
	footerButtonBenchmarkRankBalanced
	footerButtonBenchmarkRankSpeed
)

type footerButton struct {
	id      footerButtonID
	label   string
	compact string
}

var footerButtonDefinitions = []footerButton{
	{id: footerButtonTheme, label: "[ (T)HEME ]", compact: "[T]"},
	{id: footerButtonRefresh, label: "[ (R)EFRESH ]", compact: "[R]"},
	{id: footerButtonQuit, label: "[ (Q)UIT ]", compact: "[Q]"},
}

var quotaFooterButtonDefinitions = []footerButton{
	{id: footerButtonTheme, label: "[ (T)HEME ]", compact: "[T]"},
	{id: footerButtonView, label: "[ (V)IEW ]", compact: "[V]"},
	{id: footerButtonRefresh, label: "[ (R)EFRESH ]", compact: "[R]"},
	{id: footerButtonQuit, label: "[ (Q)UIT ]", compact: "[Q]"},
}

func New(fetcher Fetcher, refreshEvery time.Duration) Model {
	if refreshEvery <= 0 {
		refreshEvery = time.Minute
	}
	model := Model{
		fetcher:           fetcher,
		refreshEvery:      refreshEvery,
		loading:           true,
		nextRefresh:       time.Now().Add(refreshEvery),
		benchmarkRankMode: benchmarkRankBalanced,
		quotaAPIAnchors:   make(map[string]quotaAPIAnchor),
		quotaAPIIssues:    make(map[string]string),
	}
	if usageFetcher, ok := fetcher.(TokenUsageFetcher); ok {
		model.usageFetcher = usageFetcher
	}
	if runner, ok := fetcher.(BenchmarkRunner); ok {
		model.benchmarkRunner = runner
	}
	return model
}

// SetInline controls whether the dashboard renders in the current terminal
// buffer instead of Bubble Tea's alternate screen.
func (m *Model) SetInline(inline bool) {
	m.inline = inline
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetch(), secondTick(), refreshTick(m.refreshEvery))
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyPressMsg:
		switch strings.ToLower(message.String()) {
		case "ctrl+c", "esc":
			m.cancelBenchmark()
			return m, tea.Quit
		case "t":
			return m.pressFooterButton(footerButtonTheme)
		case "s":
			if m.meterView == viewMonitor {
				return m.pressFooterButton(footerButtonMonitorGo)
			}
		case "v":
			if m.meterView.isQuota() {
				return m.pressFooterButton(footerButtonView)
			}
		case "tab":
			return m.pressMainTab(m.currentMainTab().next())
		case "shift+tab":
			return m.pressMainTab(m.currentMainTab().previous())
		case "r":
			return m.pressFooterButton(footerButtonRefresh)
		case "q":
			return m.pressFooterButton(footerButtonQuit)
		case "b":
			if m.meterView == viewBenchmark {
				return m.pressFooterButton(footerButtonBenchmarkSelected)
			}
		case "a":
			if m.meterView == viewBenchmark {
				return m.pressFooterButton(footerButtonBenchmarkAll)
			}
		case "left", "[":
			if m.meterView == viewBenchmark {
				return m.pressFooterButton(footerButtonBenchmarkPrevious)
			}
		case "right", "]":
			if m.meterView == viewBenchmark {
				return m.pressFooterButton(footerButtonBenchmarkNext)
			}
		case "f":
			if m.meterView == viewBenchmark {
				m.setBenchmarkFilter((m.benchmarkFilter + 1) % 3)
				return m, nil
			}
		case "w":
			if m.meterView == viewBenchmark {
				return m.pressFooterButton(benchmarkRankButton(m.benchmarkRankMode.next()))
			}
		case "pgup":
			if m.meterView == viewBenchmark {
				m.scrollBenchmarkPage(1)
				return m, nil
			}
			if m.meterView == viewMonitor {
				m.scrollMonitorPage(-1)
				return m, nil
			}
		case "pgdown":
			if m.meterView == viewBenchmark {
				m.scrollBenchmarkPage(-1)
				return m, nil
			}
			if m.meterView == viewMonitor {
				m.scrollMonitorPage(1)
				return m, nil
			}
		case "p":
			if m.meterView == viewMonitor {
				return m.pressFooterButton(footerButtonMonitorStop)
			}
		}
	case tea.MouseMsg:
		mouse := message.Mouse()
		_, clicked := message.(tea.MouseClickMsg)
		if tab, ok := m.mainTabAt(mouse.X, mouse.Y); ok {
			m.mainTabHovered = true
			m.hoveredMainTab = tab
			m.viewHovered = false
			m.hoveredButton = footerButtonNone
			if mouse.Button == tea.MouseLeft && clicked {
				return m.pressMainTab(tab)
			}
			return m, nil
		}
		m.mainTabHovered = false
		if view, ok := m.quotaViewTabAt(mouse.X, mouse.Y); ok {
			m.viewHovered = true
			m.hoveredView = view
			m.hoveredButton = footerButtonNone
			if mouse.Button == tea.MouseLeft && clicked {
				return m.pressViewTab(view)
			}
			return m, nil
		}
		m.viewHovered = false
		if column, ok := m.benchmarkHeaderAt(mouse.X, mouse.Y); ok {
			m.benchmarkSortHovered = true
			m.benchmarkHoveredSort = column
			m.hoveredButton = footerButtonNone
			if mouse.Button == tea.MouseLeft && clicked {
				m.sortBenchmarkBy(column)
			}
			return m, nil
		}
		m.benchmarkSortHovered = false
		if id, ok := m.monitorSessionDismissAt(mouse.X, mouse.Y); ok {
			m.monitorDismissHover = id
			m.hoveredButton = footerButtonNone
			if mouse.Button == tea.MouseLeft && clicked {
				return m.pressMonitorSessionDismiss(id)
			}
			return m, nil
		}
		m.monitorDismissHover = ""
		if m.meterView == viewMonitor {
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.scrollMonitorRows(-1)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollMonitorRows(1)
				return m, nil
			}
		}
		if m.meterView == viewBenchmark {
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.scrollBenchmarkRows(3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollBenchmarkRows(-3)
				return m, nil
			}
		}
		button := m.footerButtonAt(mouse.X, mouse.Y)
		m.hoveredButton = button
		if mouse.Button == tea.MouseLeft && clicked && button != footerButtonNone {
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
			if message.usageErr == nil && m.usageFetcher != nil {
				m.quotaAPITelemetryIssue = ""
				m.observeQuotaAPIEq(message.snapshot, message.usage, message.at)
			} else if message.usageErr != nil {
				if errors.Is(message.usageErr, errQuotaObservationChanged) {
					m.quotaAPITelemetryIssue = "OBSERVATION DEFERRED"
				} else {
					m.quotaAPITelemetryIssue = "LOCAL TELEMETRY UNAVAILABLE"
				}
			}
			if m.monitorState == monitorRunning || m.monitorState == monitorStopping {
				m.syncMonitorQuotaSnapshot(message.snapshot)
				m.monitorQuotaError = ""
			}
		} else if m.monitorState == monitorRunning || m.monitorState == monitorStopping {
			m.monitorQuotaError = message.err.Error()
		}
	case secondMsg:
		m.phase++
		commands := []tea.Cmd{secondTick()}
		if m.monitorState == monitorRunning {
			now := time.Time(message)
			if !m.monitorNextSample.IsZero() && !now.Before(m.monitorNextSample) {
				if !m.monitorBoundaryDue {
					m.monitorBoundaryDue = true
				}
				for !m.monitorNextSample.After(now) {
					m.monitorNextSample = m.monitorNextSample.Add(monitorSampleInterval)
				}
			}
			if !m.monitorFetchActive {
				m.monitorRequest++
				m.monitorFetchActive = true
				kind := monitorFetchSample
				if m.monitorBoundaryDue {
					kind = monitorFetchBoundary
				}
				commands = append(commands, m.monitorFetch(kind, m.monitorRequest))
			}
		}
		return m, tea.Batch(commands...)
	case refreshMsg:
		m.loading = true
		m.nextRefresh = time.Now().Add(m.refreshEvery)
		return m, tea.Batch(m.fetch(), refreshTick(m.refreshEvery))
	case monitorFetchedMsg:
		if message.sequence != m.monitorRequest {
			return m, nil
		}
		m.monitorFetchActive = false
		updated, command, accepted := m.applyMonitorFetch(message)
		m = updated.(Model)
		if accepted && message.kind == monitorFetchBoundary && m.monitorState == monitorRunning {
			m.captureMonitorSamples(message.at)
			m.monitorBoundaryDue = false
		}
		if m.monitorState == monitorRunning && m.monitorBoundaryDue && !m.monitorFetchActive && message.kind != monitorFetchBoundary {
			m.monitorRequest++
			m.monitorFetchActive = true
			boundary := m.monitorFetch(monitorFetchBoundary, m.monitorRequest)
			if command == nil {
				return m, boundary
			}
			return m, tea.Batch(command, boundary)
		}
		return m, command
	case footerButtonFlashExpiredMsg:
		if message.sequence == m.flashSequence && message.button == m.flashedButton {
			m.flashedButton = footerButtonNone
		}
	case viewTabFlashExpiredMsg:
		if message.sequence == m.viewSequence && message.view == m.flashedView {
			m.viewFlashing = false
		}
	case monitorSessionDismissMsg:
		if message.sequence == m.monitorDismissSeq && message.id == m.monitorDismissFlash {
			m.dismissMonitorSession(message.id)
			m.monitorDismissFlash = ""
			m.monitorDismissHover = ""
		}
	case benchmarkEventMsg:
		if !message.ok {
			m.benchmarkState = benchmarkFinished
			m.benchmarkEvents = nil
			m.benchmarkCancel = nil
			return m, nil
		}
		event := message.event
		m.benchmarkTotal = event.Total
		m.benchmarkCompleted = event.Completed
		m.benchmarkCurrentModel = event.CurrentModel
		m.benchmarkCurrentEffort = event.CurrentEffort
		m.benchmarkCurrentTask = event.CurrentTask
		if event.Combinations > 0 {
			m.benchmarkCombinations = event.Combinations
		}
		if event.Result != nil {
			if m.benchmarkScroll > 0 {
				m.benchmarkScroll++
			}
			m.benchmarkResults = append(m.benchmarkResults, *event.Result)
		}
		if event.Err != nil {
			m.benchmarkError = event.Err.Error()
		}
		if event.Done {
			m.benchmarkState = benchmarkFinished
			m.benchmarkEvents = nil
			m.benchmarkCancel = nil
			return m, nil
		}
		return m, waitBenchmarkEvent(message.events)
	case benchmarkPlanMsg:
		m.benchmarkPlanning = false
		if message.err != nil {
			m.benchmarkError = message.err.Error()
		} else {
			m.benchmarkError = ""
			m.benchmarkCombinations = message.combinations
		}
	case benchmarkConfirmExpiredMsg:
		if message.sequence == m.benchmarkConfirmSequence {
			m.benchmarkAllArmed = false
		}
	}
	return m, nil
}

func (m Model) pressViewTab(view meterViewID) (tea.Model, tea.Cmd) {
	if view < viewBars || view >= viewCount {
		return m, nil
	}
	m.meterView = view
	if view.isQuota() {
		m.quotaMeterView = view
	}
	m.persistPreferences()
	m.viewSequence++
	m.flashedView = view
	m.viewFlashing = true
	sequence := m.viewSequence
	commands := []tea.Cmd{tea.Tick(footerButtonFlashDuration, func(time.Time) tea.Msg {
		return viewTabFlashExpiredMsg{view: view, sequence: sequence}
	})}
	if view == viewBenchmark && m.benchmarkRunner != nil && m.benchmarkCombinations == 0 && !m.benchmarkPlanning {
		m.benchmarkPlanning = true
		commands = append(commands, planBenchmark(m.benchmarkRunner))
	}
	return m, tea.Batch(commands...)
}

func (m Model) pressFooterButton(button footerButtonID) (tea.Model, tea.Cmd) {
	m.flashSequence++
	m.flashedButton = button
	sequence := m.flashSequence
	if button == footerButtonQuit {
		m.cancelBenchmark()
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

func (m Model) pressMonitorSessionDismiss(id string) (tea.Model, tea.Cmd) {
	if m.monitorSessionIndex(id) < 0 {
		return m, nil
	}
	m.monitorDismissSeq++
	m.monitorDismissFlash = id
	sequence := m.monitorDismissSeq
	return m, tea.Tick(footerButtonFlashDuration, func(time.Time) tea.Msg {
		return monitorSessionDismissMsg{id: id, sequence: sequence}
	})
}

func (m Model) activateFooterButton(button footerButtonID) (Model, tea.Cmd) {
	switch button {
	case footerButtonTheme:
		m.theme = m.theme.next()
		m.persistPreferences()
	case footerButtonView:
		if m.meterView.isQuota() {
			m.meterView = m.meterView.nextQuota()
			m.quotaMeterView = m.meterView
			m.persistPreferences()
		}
	case footerButtonRefresh:
		if !m.loading {
			m.loading = true
			return m, m.fetch()
		}
	case footerButtonMonitorGo:
		if m.meterView == viewMonitor && (m.monitorState == monitorIdle || m.monitorState == monitorStopped) {
			m.monitorState = monitorStarting
			m.monitorStartedAt = time.Time{}
			m.monitorStoppedAt = time.Time{}
			m.monitorSamples = nil
			m.monitorSessionData = nil
			m.monitorDismissed = nil
			m.monitorDismissHover = ""
			m.monitorDismissFlash = ""
			m.monitorDismissSeq++
			m.monitorScroll = 0
			m.monitorBoundaryDue = false
			m.monitorLastActivity = time.Time{}
			m.monitorSessions = 0
			m.monitorError = ""
			m.monitorQuotaWindows = nil
			m.monitorQuotaError = ""
			m.monitorRequest++
			m.monitorFetchActive = true
			return m, m.monitorFetch(monitorFetchStart, m.monitorRequest)
		}
	case footerButtonMonitorStop:
		if m.meterView == viewMonitor && m.monitorState == monitorRunning {
			m.monitorState = monitorStopping
			m.monitorRequest++
			m.monitorFetchActive = true
			return m, m.monitorFetch(monitorFetchStop, m.monitorRequest)
		}
	case footerButtonBenchmarkPrevious:
		if m.benchmarkState != benchmarkRunning {
			m.selectBenchmarkTask(-1)
		}
	case footerButtonBenchmarkNext:
		if m.benchmarkState != benchmarkRunning {
			m.selectBenchmarkTask(1)
		}
	case footerButtonBenchmarkSelected:
		if m.meterView == viewBenchmark && m.benchmarkState != benchmarkRunning {
			m.benchmarkAllArmed = false
			tasks := m.benchmarkTasks()
			if len(tasks) > 0 {
				return m.startBenchmark([]codex.BenchmarkTaskID{tasks[m.benchmarkSelectedTask%len(tasks)].ID})
			}
		}
	case footerButtonBenchmarkAll:
		tasks := m.benchmarkTasks()
		if m.meterView == viewBenchmark && benchmarkRunAllAvailable(m.benchmarkState == benchmarkRunning, m.benchmarkCombinations, len(tasks)) {
			if !m.benchmarkAllArmed {
				m.benchmarkAllArmed = true
				m.benchmarkConfirmSequence++
				sequence := m.benchmarkConfirmSequence
				return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return benchmarkConfirmExpiredMsg{sequence: sequence} })
			}
			m.benchmarkAllArmed = false
			ids := make([]codex.BenchmarkTaskID, 0, len(tasks))
			for _, task := range tasks {
				ids = append(ids, task.ID)
			}
			return m.startBenchmark(ids)
		}
	case footerButtonBenchmarkFilterAll:
		m.setBenchmarkFilter(benchmarkFilterAll)
	case footerButtonBenchmarkFilterPass:
		m.setBenchmarkFilter(benchmarkFilterPass)
	case footerButtonBenchmarkFilterFail:
		m.setBenchmarkFilter(benchmarkFilterFail)
	case footerButtonBenchmarkRankCost:
		m.setBenchmarkRankMode(benchmarkRankCost)
	case footerButtonBenchmarkRankBalanced:
		m.setBenchmarkRankMode(benchmarkRankBalanced)
	case footerButtonBenchmarkRankSpeed:
		m.setBenchmarkRankMode(benchmarkRankSpeed)
	}
	return m, nil
}

func (m Model) footerButtonAt(x, y int) footerButtonID {
	if x < 0 || y < 0 {
		return footerButtonNone
	}
	if m.loading && len(m.snapshot.Meters()) == 0 {
		return footerButtonNone
	}
	if m.meterView == viewMonitor {
		if button := m.monitorButtonAt(x, y); button != footerButtonNone {
			return button
		}
	}
	if m.meterView == viewBenchmark {
		if button := m.benchmarkButtonAt(x, y); button != footerButtonNone {
			return button
		}
	}
	layout := m.dashboardLayout()
	if y != layout.footerY+1 {
		return footerButtonNone
	}
	localX := x - 2
	buttons, separator := footerButtonLayoutWithTheme(layout.contentWidth, paletteFor(m.theme).name, m.meterView.isQuota())
	buttonX := 0
	for _, button := range buttons {
		if localX >= buttonX && localX < buttonX+len(button.label) {
			return button.id
		}
		buttonX += len(button.label) + len(separator)
	}
	return footerButtonNone
}

type dashboardGeometry struct {
	contentWidth int
	tabsY        int
	quotaTabsY   int
	meterHeight  int
	meterY       int
	footerY      int
	headerSpacer bool
}

func (m Model) dashboardLayout() dashboardGeometry {
	width := m.width
	if width == 0 {
		width = 80
	}
	height := m.height
	if height == 0 {
		height = 24
	}
	contentWidth := max(width-4, 1)
	contentHeight := max(height-2, 1)
	headerHeight := 3
	if contentWidth < 64 {
		headerHeight = 2
	}
	statusHeight := 1
	tabsHeight := 1
	if m.meterView.isQuota() {
		tabsHeight++
	}
	const framedErrorHeight = 3
	const footerHeight = 2
	extraHeight := 0
	if m.err != nil {
		extraHeight += framedErrorHeight
	}
	meters := m.snapshot.Meters()
	if len(meters) == 0 {
		extraHeight += framedErrorHeight
	}
	if (m.meterView == viewBars || m.meterView == viewConsumptionPace || m.meterView == viewFuel) && len(meters) > 0 {
		minimumMeterHeight := len(meters) * 3
		meterHeightWithSpacer := contentHeight - headerHeight - statusHeight - tabsHeight - extraHeight - footerHeight
		meterHeightWithoutSpacer := meterHeightWithSpacer + statusHeight
		if meterHeightWithSpacer < minimumMeterHeight && meterHeightWithoutSpacer >= minimumMeterHeight {
			statusHeight = 0
		}
	}
	tabsY := 1 + headerHeight + statusHeight
	quotaTabsY := -1
	if m.meterView.isQuota() {
		quotaTabsY = tabsY + 1
	}
	meterY := tabsY + tabsHeight + extraHeight
	meterHeight := max(contentHeight-headerHeight-statusHeight-tabsHeight-extraHeight-footerHeight, 1)
	footerY := meterY
	if m.meterView == viewMonitor || m.meterView == viewBenchmark || len(m.snapshot.Meters()) > 0 {
		footerY += meterHeight
	}
	return dashboardGeometry{
		contentWidth: contentWidth,
		tabsY:        tabsY,
		quotaTabsY:   quotaTabsY,
		meterHeight:  meterHeight,
		meterY:       meterY,
		footerY:      footerY,
		headerSpacer: statusHeight > 0,
	}
}

func launchBenchmark(ctx context.Context, runner BenchmarkRunner, tasks []codex.BenchmarkTaskID, events chan codex.BenchmarkEvent) tea.Cmd {
	return func() tea.Msg {
		go func() {
			defer close(events)
			runner.RunBenchmarkSuite(ctx, tasks, func(event codex.BenchmarkEvent) {
				select {
				case events <- event:
				case <-ctx.Done():
				}
			})
		}()
		event, ok := <-events
		return benchmarkEventMsg{event: event, events: events, ok: ok}
	}
}

func planBenchmark(runner BenchmarkRunner) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		combinations, err := runner.BenchmarkCombinationCount(ctx)
		return benchmarkPlanMsg{combinations: combinations, err: err}
	}
}

func waitBenchmarkEvent(events <-chan codex.BenchmarkEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		return benchmarkEventMsg{event: event, events: events, ok: ok}
	}
}

func (m *Model) cancelBenchmark() {
	if m.benchmarkCancel != nil {
		m.benchmarkCancel()
		m.benchmarkCancel = nil
	}
}

func (m Model) startBenchmark(tasks []codex.BenchmarkTaskID) (Model, tea.Cmd) {
	if m.benchmarkRunner == nil {
		m.benchmarkState = benchmarkFinished
		m.benchmarkError = "benchmark runner unavailable"
		return m, nil
	}
	m.benchmarkResults = nil
	m.benchmarkTotal = 0
	m.benchmarkCompleted = 0
	m.benchmarkCurrentModel = ""
	m.benchmarkCurrentEffort = ""
	m.benchmarkCurrentTask = ""
	m.benchmarkError = ""
	m.benchmarkScroll = 0
	m.benchmarkState = benchmarkRunning
	ctx, cancel := context.WithCancel(context.Background())
	m.benchmarkCancel = cancel
	events := make(chan codex.BenchmarkEvent, 2)
	m.benchmarkEvents = events
	return m, launchBenchmark(ctx, m.benchmarkRunner, tasks, events)
}

func (m *Model) selectBenchmarkTask(direction int) {
	tasks := m.benchmarkTasks()
	if len(tasks) == 0 {
		return
	}
	m.benchmarkSelectedTask = (m.benchmarkSelectedTask + direction + len(tasks)) % len(tasks)
	m.benchmarkAllArmed = false
}

func (m Model) benchmarkTasks() []codex.BenchmarkTask {
	return codex.BenchmarkTasks()
}

func (m *Model) setBenchmarkFilter(filter benchmarkResultFilter) {
	m.benchmarkFilter = filter
	m.benchmarkScroll = 0
	m.persistPreferences()
}

func (m *Model) setBenchmarkRankMode(mode benchmarkRankMode) {
	if mode < benchmarkRankBalanced || mode > benchmarkRankSpeed {
		return
	}
	m.benchmarkRankMode = mode
	m.benchmarkScroll = 0
	m.persistPreferences()
}

func benchmarkRankButton(mode benchmarkRankMode) footerButtonID {
	switch mode {
	case benchmarkRankCost:
		return footerButtonBenchmarkRankCost
	case benchmarkRankSpeed:
		return footerButtonBenchmarkRankSpeed
	default:
		return footerButtonBenchmarkRankBalanced
	}
}

func (mode benchmarkRankMode) next() benchmarkRankMode {
	switch mode {
	case benchmarkRankCost:
		return benchmarkRankBalanced
	case benchmarkRankBalanced:
		return benchmarkRankSpeed
	default:
		return benchmarkRankCost
	}
}

func (m Model) benchmarkPageSize() int {
	layout := m.dashboardLayout()
	geometry := layoutBenchmarkArea(layout.contentWidth, layout.meterHeight)
	bodyHeight := max(geometry.tableHeight-3, 1)
	return max(bodyHeight-3, 1)
}

func (m *Model) scrollBenchmarkPage(direction int) {
	m.scrollBenchmarkRows(direction * m.benchmarkPageSize())
}

func (m *Model) scrollBenchmarkRows(rows int) {
	maximum := max(len(filterBenchmarkResults(m.benchmarkResults, m.benchmarkFilter))-m.benchmarkPageSize(), 0)
	m.benchmarkScroll = min(max(m.benchmarkScroll+rows, 0), maximum)
}

func (m *Model) sortBenchmarkBy(column benchmarkSortColumn) {
	if m.benchmarkSort == column {
		m.benchmarkSortDescending = !m.benchmarkSortDescending
	} else {
		m.benchmarkSort = column
		m.benchmarkSortDescending = false
	}
	m.benchmarkScroll = 0
}

func (m Model) contentWidth() int {
	width := m.width
	if width == 0 {
		width = 80
	}
	return max(width-4, 1)
}

func footerButtonLayout(width int, quota bool) ([]footerButton, string) {
	definitions := footerButtonDefinitions
	if quota {
		definitions = quotaFooterButtonDefinitions
	}
	buttons := make([]footerButton, len(definitions))
	copy(buttons, definitions)
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

func footerButtonLayoutWithTheme(width int, themeName string, quota bool) ([]footerButton, string) {
	themeWidth := len("THEME // ") + len(themeName)
	available := width - themeWidth - 1
	if available <= 0 {
		return nil, " "
	}
	buttons, separator := footerButtonLayout(available, quota)
	visible := buttons[:0]
	used := 0
	for _, button := range buttons {
		required := len(button.label)
		if len(visible) > 0 {
			required += len(separator)
		}
		if used+required > available {
			break
		}
		visible = append(visible, button)
		used += required
	}
	return visible, separator
}

func (m Model) fetch() tea.Cmd {
	return func() tea.Msg {
		var before codex.LiveUsageSnapshot
		var beforeErr error
		if m.usageFetcher != nil {
			before, beforeErr = m.usageFetcher.FetchTokenUsage(context.Background())
		}
		snapshot, err := m.fetcher.Fetch(context.Background())
		message := fetchedMsg{snapshot: snapshot, err: err, at: time.Now()}
		if err == nil && m.usageFetcher != nil {
			after, afterErr := m.usageFetcher.FetchTokenUsage(context.Background())
			switch {
			case beforeErr != nil:
				message.usageErr = beforeErr
			case afterErr != nil:
				message.usageErr = afterErr
			case !quotaAPIAccountingEqual(before, after) || after.APIEqPendingCalls > 0:
				message.usageErr = errQuotaObservationChanged
			default:
				message.usage = after
			}
			if !snapshot.FetchedAt.IsZero() {
				message.at = snapshot.FetchedAt
			}
		}
		return message
	}
}

const monitorSampleInterval = 30 * time.Second

func (m Model) monitorFetch(kind monitorFetchKind, sequence uint64) tea.Cmd {
	return func() tea.Msg {
		quota := m.snapshot
		var quotaErr error
		fetchQuota := func() {
			if m.fetcher == nil {
				return
			}
			if freshQuota, err := m.fetcher.Fetch(context.Background()); err != nil {
				quotaErr = err
			} else {
				quota = freshQuota
			}
		}
		if m.usageFetcher == nil {
			return monitorFetchedMsg{
				kind: kind, sequence: sequence, err: errors.New("local Codex session telemetry unavailable"),
				quota: quota, quotaErr: quotaErr, at: time.Now(),
			}
		}
		var usage codex.LiveUsageSnapshot
		var err error
		if kind == monitorFetchStart {
			// Bracket the measured local-token interval inside the account-quota
			// interval: quota first at Start, local telemetry first at Stop.
			fetchQuota()
			usage, err = m.usageFetcher.FetchTokenUsage(context.Background())
		} else if kind == monitorFetchStop {
			if freshFetcher, ok := m.usageFetcher.(FreshTokenUsageFetcher); ok {
				usage, err = freshFetcher.FetchTokenUsageFresh(context.Background())
			} else {
				usage, err = m.usageFetcher.FetchTokenUsage(context.Background())
			}
			observedAt := time.Now()
			fetchQuota()
			return monitorFetchedMsg{
				kind: kind, sequence: sequence, usage: usage, err: err,
				quota: quota, quotaErr: quotaErr, at: observedAt,
			}
		} else {
			usage, err = m.usageFetcher.FetchTokenUsage(context.Background())
		}
		observedAt := time.Now()
		return monitorFetchedMsg{
			kind: kind, sequence: sequence, usage: usage, err: err,
			quota: quota, quotaErr: quotaErr, at: observedAt,
		}
	}
}

func (m Model) applyMonitorFetch(message monitorFetchedMsg) (tea.Model, tea.Cmd, bool) {
	if message.kind == monitorFetchStop {
		if message.quotaErr == nil {
			m.syncMonitorQuotaSnapshot(message.quota)
		}
		m.applyMonitorQuotaResult(message)
	}
	if message.err != nil {
		m.monitorError = message.err.Error()
		if message.kind == monitorFetchStop {
			m.monitorState = monitorStopped
			m.monitorStoppedAt = message.at
		} else if message.kind == monitorFetchStart {
			m.monitorState = monitorStopped
		}
		return m, nil, false
	}
	accepted := true
	switch message.kind {
	case monitorFetchStart:
		m.monitorBaseline = message.usage.TotalTokens
		m.monitorLatest = message.usage.TotalTokens
		m.monitorGraphStart = message.usage.TotalTokens
		m.monitorStartedAt = message.at
		m.monitorState = monitorRunning
		m.startMonitorQuotaSnapshot(message.quota)
		if message.quotaErr != nil {
			for index := range m.monitorQuotaWindows {
				m.monitorQuotaWindows[index].partial = true
			}
		}
		m.applyMonitorQuotaResult(message)
		m.startMonitorSessions(message.usage, message.at)
		m.monitorSessions = m.visibleMonitorSessionCount()
		m.monitorError = ""
		m.monitorNextSample = message.at.Add(monitorSampleInterval)
	case monitorFetchSample, monitorFetchBoundary, monitorFetchStop:
		if m.monitorUsageMovesBackwards(message.usage) {
			m.monitorError = "local Codex token counter moved backwards"
			accepted = false
		} else {
			if message.usage.TotalTokens > m.monitorLatest {
				m.monitorLastActivity = message.usage.LastActivity
			}
			m.monitorLatest = message.usage.TotalTokens
			m.monitorError = ""
			m.syncMonitorSessions(message.usage, message.at)
			m.monitorSessions = m.visibleMonitorSessionCount()
		}
		if message.kind == monitorFetchStop {
			m.monitorState = monitorStopped
			m.monitorStoppedAt = message.at
		}
	}
	return m, nil, accepted
}

func (m Model) monitorUsageMovesBackwards(usage codex.LiveUsageSnapshot) bool {
	if usage.TotalTokens < m.monitorLatest {
		return true
	}
	for _, update := range usage.Sessions {
		if index := m.monitorSessionIndex(update.ID); index >= 0 && update.TotalTokens < m.monitorSessionData[index].latest {
			return true
		}
	}
	return false
}

func (m *Model) applyMonitorQuotaResult(message monitorFetchedMsg) {
	if message.quotaErr != nil {
		m.monitorQuotaError = message.quotaErr.Error()
		return
	}
	m.monitorQuotaError = ""
	if len(message.quota.Meters()) > 0 {
		m.snapshot = message.quota
		m.lastRefresh = message.quota.FetchedAt
		if m.lastRefresh.IsZero() {
			m.lastRefresh = message.at
		}
		m.err = nil
	}
}

func (m *Model) startMonitorQuotaSnapshot(snapshot codex.Snapshot) {
	m.monitorQuotaWindows = nil
	for _, meter := range snapshot.Meters() {
		reset := cloneInt64(meter.Window.ResetsAt)
		m.monitorQuotaWindows = append(m.monitorQuotaWindows, monitorQuotaWindow{
			key: monitorQuotaKey(meter), label: monitorQuotaLabel(meter),
			baselineUsed: meter.Window.UsedPercent, latestUsed: meter.Window.UsedPercent,
			latestReset: reset,
		})
	}
}

func (m *Model) syncMonitorQuotaSnapshot(snapshot codex.Snapshot) {
	seen := make(map[string]bool)
	for _, meter := range snapshot.Meters() {
		key := monitorQuotaKey(meter)
		seen[key] = true
		index := -1
		for candidate := range m.monitorQuotaWindows {
			if m.monitorQuotaWindows[candidate].key == key {
				index = candidate
				break
			}
		}
		if index < 0 {
			m.monitorQuotaWindows = append(m.monitorQuotaWindows, monitorQuotaWindow{
				key: key, label: monitorQuotaLabel(meter),
				baselineUsed: meter.Window.UsedPercent, latestUsed: meter.Window.UsedPercent,
				latestReset: cloneInt64(meter.Window.ResetsAt),
				partial:     true,
			})
			continue
		}
		window := &m.monitorQuotaWindows[index]
		if !sameOptionalInt64(window.latestReset, meter.Window.ResetsAt) || meter.Window.UsedPercent < window.latestUsed {
			window.resetDetected = true
		}
		window.latestUsed = meter.Window.UsedPercent
		window.latestReset = cloneInt64(meter.Window.ResetsAt)
		window.stale = false
	}
	for index := range m.monitorQuotaWindows {
		if !seen[m.monitorQuotaWindows[index].key] {
			m.monitorQuotaWindows[index].stale = true
		}
	}
}

func monitorQuotaKey(meter codex.Meter) string {
	duration := int64(-1)
	if meter.Window.WindowDurationMins != nil {
		duration = *meter.Window.WindowDurationMins
	}
	return meter.Bucket + "\x00" + meter.Name + "\x00" + fmt.Sprint(duration)
}

func monitorQuotaLabel(meter codex.Meter) string {
	if meter.Bucket == "codex" {
		return meter.Name
	}
	return codex.DisplayName(meter.Bucket) + " " + meter.Name
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (m *Model) startMonitorSessions(usage codex.LiveUsageSnapshot, observedAt time.Time) {
	m.monitorSessionData = nil
	m.monitorDismissed = nil
	for _, session := range usage.Sessions {
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{
			id: session.ID, workingDirectory: session.WorkingDirectory,
			baseline: session.TotalTokens, latest: session.TotalTokens, graphStart: session.TotalTokens,
			startedAt:    observedAt,
			lastActivity: session.LastActivity, agentCount: session.AgentCount,
			active: session.Active, attention: session.Attention,
			displayed: session.Active, unattributed: session.Unattributed,
			callSequence: latestModelCallSequence(session.ModelCalls),
			turnSequence: latestTurnTimingSequence(session.TurnTimings),
		})
	}
	if len(m.monitorSessionData) == 0 && usage.SessionCount > 0 {
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{
			id: "local", baseline: usage.TotalTokens, latest: usage.TotalTokens,
			graphStart: usage.TotalTokens, startedAt: observedAt, lastActivity: usage.LastActivity,
			active: true, displayed: true, unattributed: true,
		})
	}
}

func (m *Model) syncMonitorSessions(usage codex.LiveUsageSnapshot, observedAt time.Time) {
	for index := range m.monitorSessionData {
		m.monitorSessionData[index].active = false
		m.monitorSessionData[index].attention = codex.SessionAttentionNone
	}
	for _, update := range usage.Sessions {
		index := m.monitorSessionIndex(update.ID)
		if index < 0 {
			startedAt := observedAt
			if sessionStart := update.StartedAt; !sessionStart.IsZero() && sessionStart.After(m.monitorStartedAt) && sessionStart.Before(observedAt) {
				startedAt = sessionStart
			}
			created := monitorSession{
				id: update.ID, workingDirectory: update.WorkingDirectory,
				latest: update.TotalTokens, graphStart: 0, startedAt: startedAt,
				lastActivity: update.LastActivity, agentCount: update.AgentCount,
				active: update.Active, attention: update.Attention,
				displayed:    update.Active || update.TotalTokens > 0 || len(update.ModelCalls) > 0 || len(update.TurnTimings) > 0,
				unattributed: update.Unattributed,
			}
			applyMonitorSessionTelemetry(&created, update, m.monitorStartedAt)
			m.monitorSessionData = append(m.monitorSessionData, created)
			continue
		}
		session := &m.monitorSessionData[index]
		if update.TotalTokens < session.latest {
			m.monitorError = "local Codex session token counter moved backwards"
			continue
		}
		session.latest = update.TotalTokens
		session.lastActivity = update.LastActivity
		session.agentCount = max(session.agentCount, update.AgentCount)
		session.active = update.Active
		session.attention = update.Attention
		session.displayed = session.displayed || update.Active || update.TotalTokens > session.baseline ||
			len(update.ModelCalls) > 0 || len(update.TurnTimings) > 0
		if update.WorkingDirectory != "" {
			session.workingDirectory = update.WorkingDirectory
		}
		applyMonitorSessionTelemetry(session, update, time.Time{})
		m.restoreMonitorSessionOnActivity(session)
	}
	if len(usage.Sessions) == 0 && len(m.monitorSessionData) == 1 && m.monitorSessionData[0].id == "local" {
		session := &m.monitorSessionData[0]
		session.latest = usage.TotalTokens
		session.lastActivity = usage.LastActivity
		session.active = usage.SessionCount > 0
		session.displayed = true
		m.restoreMonitorSessionOnActivity(session)
	}
	for index := range m.monitorSessionData {
		session := &m.monitorSessionData[index]
		dismissal, dismissed := m.monitorDismissed[session.id]
		if dismissed && !session.active {
			dismissal.inactiveObserved = true
			m.monitorDismissed[session.id] = dismissal
		}
	}
}

func (m *Model) dismissMonitorSession(id string) {
	index := m.monitorSessionIndex(id)
	if index < 0 {
		return
	}
	if m.monitorDismissed == nil {
		m.monitorDismissed = make(map[string]monitorSessionDismissal)
	}
	session := m.monitorSessionData[index]
	m.monitorDismissed[id] = monitorSessionDismissal{
		latest:       session.latest,
		lastActivity: session.lastActivity,
		callSequence: session.callSequence,
		turnSequence: session.turnSequence,
	}
	m.monitorSessions = m.visibleMonitorSessionCount()
	maximumScroll := max(m.monitorSessions-m.monitorPageSize(), 0)
	m.monitorScroll = min(m.monitorScroll, maximumScroll)
}

func (m *Model) restoreMonitorSessionOnActivity(session *monitorSession) {
	dismissal, dismissed := m.monitorDismissed[session.id]
	if !dismissed {
		return
	}
	if session.attention != codex.SessionAttentionNone ||
		session.latest > dismissal.latest ||
		session.lastActivity.After(dismissal.lastActivity) ||
		session.callSequence > dismissal.callSequence ||
		session.turnSequence > dismissal.turnSequence ||
		(dismissal.inactiveObserved && session.active) {
		delete(m.monitorDismissed, session.id)
	}
}

func (m Model) monitorSessionVisible(session monitorSession) bool {
	if !session.displayed {
		return false
	}
	_, dismissed := m.monitorDismissed[session.id]
	return !dismissed || session.attention != codex.SessionAttentionNone
}

func applyMonitorSessionTelemetry(session *monitorSession, update codex.LiveUsageSession, since time.Time) {
	for _, call := range update.ModelCalls {
		if call.Sequence <= session.callSequence {
			continue
		}
		session.callSequence = call.Sequence
		if !since.IsZero() && !call.At.IsZero() && call.At.Before(since) {
			continue
		}
		session.modelCalls++
		session.latestOutputOK = call.OutputAvailable
		if call.OutputAvailable {
			session.latestOutput = max(call.OutputTokens, int64(0))
			session.peakOutput = max(session.peakOutput, session.latestOutput)
			session.peakOutputOK = true
		}
		if call.At.After(session.lastCallAt) {
			session.lastCallAt = call.At
		}
	}
	for _, timing := range update.TurnTimings {
		if timing.Sequence <= session.turnSequence {
			continue
		}
		session.turnSequence = timing.Sequence
		if !since.IsZero() && !timing.At.IsZero() && timing.At.Before(since) {
			continue
		}
		session.latestTTFTOK = timing.Available
		if timing.Available {
			session.latestTTFT = max(timing.TimeToFirstToken, time.Duration(0))
			session.peakTTFT = max(session.peakTTFT, session.latestTTFT)
			session.peakTTFTOK = true
		}
	}
}

func latestModelCallSequence(calls []codex.LiveModelCall) uint64 {
	var latest uint64
	for _, call := range calls {
		latest = max(latest, call.Sequence)
	}
	return latest
}

func latestTurnTimingSequence(timings []codex.LiveTurnTiming) uint64 {
	var latest uint64
	for _, timing := range timings {
		latest = max(latest, timing.Sequence)
	}
	return latest
}

func (m *Model) captureMonitorSamples(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	start := m.monitorStartedAt
	if count := len(m.monitorSamples); count > 0 {
		start = m.monitorSamples[count-1].at
	}
	duration := max(at.Sub(start), time.Duration(0))
	delta := max(m.monitorLatest-m.monitorGraphStart, int64(0))
	m.monitorSamples = append(m.monitorSamples, monitorSample{at: at, duration: duration, intervalTokens: delta})
	m.monitorGraphStart = m.monitorLatest

	for index := range m.monitorSessionData {
		session := &m.monitorSessionData[index]
		if !session.displayed {
			continue
		}
		start := session.startedAt
		if count := len(session.samples); count > 0 {
			start = session.samples[count-1].at
		}
		duration := max(at.Sub(start), time.Duration(0))
		delta := max(session.latest-session.graphStart, int64(0))
		session.samples = append(session.samples, monitorSample{at: at, duration: duration, intervalTokens: delta})
		session.graphStart = session.latest
	}
}

func (m *Model) monitorSessionIndex(id string) int {
	for index := range m.monitorSessionData {
		if m.monitorSessionData[index].id == id {
			return index
		}
	}
	return -1
}

func (m Model) visibleMonitorSessionCount() int {
	count := 0
	for _, session := range m.monitorSessionData {
		if m.monitorSessionVisible(session) {
			count++
		}
	}
	return count
}

func (m Model) monitorPageSize() int {
	dashboard := m.dashboardLayout()
	layout := layoutMonitorArea(dashboard.contentWidth, dashboard.meterHeight)
	return max(layout.graphHeight/3, 1)
}

func (m *Model) scrollMonitorPage(direction int) {
	m.scrollMonitorRows(direction * m.monitorPageSize())
}

func (m *Model) scrollMonitorRows(rows int) {
	maximum := max(m.visibleMonitorSessionCount()-m.monitorPageSize(), 0)
	m.monitorScroll = min(max(m.monitorScroll+rows, 0), maximum)
}

func secondTick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return secondMsg(now) })
}

func refreshTick(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(now time.Time) tea.Msg { return refreshMsg(now) })
}

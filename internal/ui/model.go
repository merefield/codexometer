package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/version"
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

type ScopedBenchmarkRunner interface {
	BenchmarkRunner
	BenchmarkPlan(context.Context) (codex.BenchmarkPlan, error)
	RunBenchmarkSuiteScoped(context.Context, []codex.BenchmarkTaskID, codex.BenchmarkScope, func(codex.BenchmarkEvent))
}

type BenchmarkTaskProvider interface {
	BenchmarkTasks() []codex.BenchmarkTask
}

type Model struct {
	resetHovered                        bool
	resetBusy                           bool
	resetKey, resetAccount, resetNotice string
	resetConfirmUntil                   time.Time
	resetRevision                       uint64
	fetcher                             Fetcher
	usageFetcher                        TokenUsageFetcher
	refreshEvery                        time.Duration
	snapshot                            codex.Snapshot
	err                                 error
	width                               int
	height                              int
	inline                              bool
	loading                             bool
	lastRefresh                         time.Time
	nextRefresh                         time.Time
	phase                               int
	theme                               themeID
	meterView                           meterViewID
	quotaMeterView                      meterViewID
	hoveredMainTab                      mainTabID
	mainTabHovered                      bool
	hoveredView                         meterViewID
	viewHovered                         bool
	flashedView                         meterViewID
	viewFlashing                        bool
	viewSequence                        uint64
	hoveredButton                       footerButtonID
	flashedButton                       footerButtonID
	flashSequence                       uint64
	benchmarkRunner                     BenchmarkRunner
	benchmarkScopedRunner               ScopedBenchmarkRunner
	preferenceStore                     PreferenceStore
	appVersion                          string

	benchmarkState           benchmarkState
	benchmarkResults         []codex.BenchmarkResult
	benchmarkResultIndexes   map[string]int
	benchmarkRunResults      []codex.BenchmarkResult
	benchmarkRunIndexes      map[string]int
	benchmarkTotal           int
	benchmarkCompleted       int
	benchmarkCurrentModel    string
	benchmarkCurrentEffort   string
	benchmarkCurrentTask     string
	benchmarkError           string
	benchmarkPlanning        bool
	benchmarkCombinations    int
	benchmarkPlan            codex.BenchmarkPlan
	benchmarkScope           codex.BenchmarkScope
	benchmarkScopeBeforeEdit codex.BenchmarkScope
	benchmarkScopeOpen       bool
	benchmarkScopeCursor     int
	benchmarkScopeScroll     int
	benchmarkScopeKeyboard   bool
	benchmarkSelectedSuite   int
	benchmarkScopeTasks      map[codex.BenchmarkSuiteID][]codex.BenchmarkTaskID
	benchmarkTasksBeforeEdit map[codex.BenchmarkSuiteID][]codex.BenchmarkTaskID
	benchmarkPairsBeforeEdit int
	benchmarkFilter          benchmarkResultFilter
	benchmarkAllArmed        bool
	benchmarkSelectedArmed   bool
	benchmarkConfirmSequence uint64
	benchmarkScroll          int
	benchmarkSort            benchmarkSortColumn
	benchmarkSortDescending  bool
	benchmarkSortHovered     bool
	benchmarkHoveredSort     benchmarkSortColumn
	benchmarkRankMode        benchmarkRankMode
	benchmarkEvents          <-chan codex.BenchmarkEvent
	benchmarkCancel          context.CancelFunc
	benchmarkActive          *codex.BenchmarkResult
	benchmarkActiveSince     time.Time
	benchmarkSelectedRun     string
	benchmarkHoveredRun      string
	benchmarkRunHovered      bool
	benchmarkDetail          *codex.BenchmarkResult
	benchmarkDetailActive    bool
	benchmarkDetailScroll    int
	benchmarkDetailCache     benchmarkDetailTranscriptCache
	benchmarkQuotaAccounting benchmarkQuotaAccounting

	monitorState            monitorState
	monitorAutoStart        bool
	monitorStartedAt        time.Time
	monitorStoppedAt        time.Time
	monitorBaseline         int64
	monitorLatest           int64
	monitorSamples          []monitorSample
	monitorRequest          uint64
	monitorFetchActive      bool
	monitorNextFetch        time.Time
	monitorResetPaused      bool
	monitorNextSample       time.Time
	monitorBoundaryDue      bool
	monitorGraphStart       int64
	monitorLastActivity     time.Time
	monitorSessions         int
	monitorSessionData      []monitorSession
	monitorCodexStatusKnown bool
	monitorCodexUp          bool
	monitorCodexWorking     bool
	monitorSelectedID       string
	monitorDismissed        map[string]monitorSessionDismissal
	monitorDismissHover     string
	monitorDismissFlash     string
	monitorDismissSeq       uint64
	monitorScroll           int
	monitorError            string
	monitorQuotaWindows     []monitorQuotaWindow
	monitorQuotaError       string

	quotaAPIAnchors        map[string]quotaAPIAnchor
	quotaAPIEvidence       []quotaAPISample
	quotaAPIIssues         map[string]string
	quotaAPIAccount        string
	quotaAPITelemetryIssue string
}

type fetchedMsg struct {
	resetRevision          uint64
	snapshot               codex.Snapshot
	err                    error
	usage                  codex.LiveUsageSnapshot
	usageErr               error
	at                     time.Time
	benchmarkQuotaRevision uint64
}

type secondMsg time.Time
type refreshMsg time.Time

type monitorState int

const (
	monitorIdle monitorState = iota
	monitorStarting
	monitorRunning
	monitorPausing
	monitorPaused
	monitorResuming
	monitorResetting
)

type monitorFetchKind int

const (
	monitorFetchStart monitorFetchKind = iota
	monitorFetchSample
	monitorFetchBoundary
	monitorFetchPause
	monitorFetchResume
	monitorFetchReset
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
	attention        codex.SessionAttention
	attentionCleared bool
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
	benchmarkStopping
	benchmarkStopped
	benchmarkFinished
)

type benchmarkEventMsg struct {
	event  codex.BenchmarkEvent
	events <-chan codex.BenchmarkEvent
	ok     bool
}

type benchmarkPlanMsg struct {
	plan         codex.BenchmarkPlan
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
	footerButtonMonitorPause
	footerButtonMonitorReset
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
	footerButtonBenchmarkCopy
	footerButtonBenchmarkClearAll
	footerButtonBenchmarkClose
	footerButtonBenchmarkDone
	footerButtonBenchmarkScope
	footerButtonBenchmarkStop
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
		monitorAutoStart:  true,
		loading:           true,
		nextRefresh:       time.Now().Add(refreshEvery),
		benchmarkRankMode: benchmarkRankBalanced,
		quotaAPIAnchors:   make(map[string]quotaAPIAnchor),
		quotaAPIIssues:    make(map[string]string),
		appVersion:        version.Current(),
	}
	if usageFetcher, ok := fetcher.(TokenUsageFetcher); ok {
		model.usageFetcher = usageFetcher
	}
	if runner, ok := fetcher.(BenchmarkRunner); ok {
		model.benchmarkRunner = runner
		model.benchmarkQuotaAccounting = benchmarkQuotaAccountingFor(runner)
	}
	if runner, ok := fetcher.(ScopedBenchmarkRunner); ok {
		model.benchmarkScopedRunner = runner
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
	case quotaResetResult:
		m.resetBusy = false
		if message.err != nil {
			m.resetNotice = "Reset unconfirmed: " + message.err.Error() + ". Retry uses the same attempt."
		} else {
			m.resetKey, m.resetAccount = "", ""
			switch message.outcome {
			case "reset", "alreadyRedeemed":
				m.resetNotice = "Quota reset. Refreshing limits…"
			case "nothingToReset":
				m.resetNotice = "Nothing eligible to reset; credit was not consumed."
			case "noCredit":
				m.resetNotice = "No reset credits available."
			}
		}
		m.loading = true
		return m, m.fetch()
	case tea.KeyPressMsg:
		if m.meterView == viewBenchmark && m.benchmarkScopeOpen {
			m.benchmarkScopeKeyboard = true
		}
		switch strings.ToLower(message.String()) {
		case "ctrl+c":
			m.cancelBenchmark()
			return m, tea.Quit
		case "esc":
			if !m.resetConfirmUntil.IsZero() || (m.meterView.isQuota() && m.resetNotice != "" && !m.resetBusy) {
				m.resetConfirmUntil = time.Time{}
				m.resetNotice = ""
				return m, nil
			}
			if m.meterView == viewBenchmark && m.benchmarkScopeOpen {
				m.cancelBenchmarkScope()
				return m, nil
			}
			if m.meterView == viewBenchmark && m.benchmarkDetail != nil {
				m.closeBenchmarkDetail()
				return m, nil
			}
			m.cancelBenchmark()
			return m, tea.Quit
		case "t":
			return m.pressFooterButton(footerButtonTheme)
		case "s":
			if m.meterView == viewMonitor {
				return m.pressFooterButton(footerButtonMonitorReset)
			}
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && m.benchmarkDetail == nil && len(m.benchmarkPlan.Models) > 0 {
				updated, command := m.pressFooterButton(footerButtonBenchmarkScope)
				next := updated.(Model)
				next.benchmarkScopeKeyboard = next.benchmarkScopeOpen
				return next, command
			}
		case "x":
			if m.meterView == viewMonitor && m.monitorSelectedID != "" {
				m.dismissSelectedMonitorSession()
				return m, nil
			}
			if m.meterView == viewBenchmark {
				if m.benchmarkScopeOpen {
					return m.pressFooterButton(footerButtonBenchmarkClose)
				}
				if m.benchmarkDetail != nil {
					return m.pressFooterButton(footerButtonBenchmarkClose)
				}
				return m.pressFooterButton(footerButtonBenchmarkStop)
			}
		case "d":
			if m.meterView == viewBenchmark && m.benchmarkScopeOpen {
				return m.pressFooterButton(footerButtonBenchmarkDone)
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
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && m.benchmarkDetail == nil {
				return m.pressFooterButton(footerButtonBenchmarkSelected)
			}
		case "c":
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && (m.benchmarkDetail != nil || m.benchmarkResultsCopyAvailable()) {
				return m.pressFooterButton(footerButtonBenchmarkCopy)
			}
		case "l":
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && m.benchmarkDetail == nil && !m.benchmarkRunActive() && len(m.benchmarkResults) > 0 {
				return m.pressFooterButton(footerButtonBenchmarkClearAll)
			}
		case "a":
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && m.benchmarkDetail == nil {
				return m.pressFooterButton(footerButtonBenchmarkAll)
			}
		case "enter":
			if m.meterView == viewBenchmark && m.benchmarkScopeOpen {
				m.toggleBenchmarkScopeCursor()
				return m, nil
			}
			if m.meterView == viewBenchmark && m.benchmarkDetail == nil {
				m.openSelectedBenchmarkDetail()
				return m, nil
			}
		case "space", " ":
			if m.meterView == viewBenchmark && m.benchmarkScopeOpen {
				m.toggleBenchmarkScopeCursor()
				return m, nil
			}
		case "up":
			if m.meterView == viewMonitor {
				m.selectMonitorSession(-1)
				return m, nil
			}
			if m.meterView == viewBenchmark {
				if m.benchmarkScopeOpen {
					m.moveBenchmarkScopeCursor(-1)
				} else if m.benchmarkDetail != nil {
					m.scrollBenchmarkDetail(-1)
				} else {
					m.selectBenchmarkRun(-1)
				}
				return m, nil
			}
		case "down":
			if m.meterView == viewMonitor {
				m.selectMonitorSession(1)
				return m, nil
			}
			if m.meterView == viewBenchmark {
				if m.benchmarkScopeOpen {
					m.moveBenchmarkScopeCursor(1)
				} else if m.benchmarkDetail != nil {
					m.scrollBenchmarkDetail(1)
				} else {
					m.selectBenchmarkRun(1)
				}
				return m, nil
			}
		case "left", "[":
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && m.benchmarkDetail == nil {
				return m.pressFooterButton(footerButtonBenchmarkPrevious)
			}
		case "right", "]":
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && m.benchmarkDetail == nil {
				return m.pressFooterButton(footerButtonBenchmarkNext)
			}
		case "f":
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && m.benchmarkDetail == nil {
				m.setBenchmarkFilter((m.benchmarkFilter + 1) % 3)
				return m, nil
			}
		case "w":
			if m.meterView == viewBenchmark && !m.benchmarkScopeOpen && m.benchmarkDetail == nil {
				return m.pressFooterButton(benchmarkRankButton(m.benchmarkRankMode.next()))
			}
		case "pgup":
			if m.meterView == viewBenchmark {
				if m.benchmarkScopeOpen {
					m.moveBenchmarkScopeCursor(-m.benchmarkScopePageSize())
				} else if m.benchmarkDetail != nil {
					m.scrollBenchmarkDetailPage(-1)
				} else {
					m.scrollBenchmarkPage(1)
				}
				return m, nil
			}
			if m.meterView == viewMonitor {
				m.scrollMonitorPage(-1)
				return m, nil
			}
		case "pgdown":
			if m.meterView == viewBenchmark {
				if m.benchmarkScopeOpen {
					m.moveBenchmarkScopeCursor(m.benchmarkScopePageSize())
				} else if m.benchmarkDetail != nil {
					m.scrollBenchmarkDetailPage(1)
				} else {
					m.scrollBenchmarkPage(-1)
				}
				return m, nil
			}
			if m.meterView == viewMonitor {
				m.scrollMonitorPage(1)
				return m, nil
			}
		case "home":
			if m.meterView == viewBenchmark && m.benchmarkDetail != nil {
				m.benchmarkDetailScroll = 0
				return m, nil
			}
		case "end":
			if m.meterView == viewBenchmark && m.benchmarkDetail != nil {
				m.prepareBenchmarkDetailTranscript()
				m.benchmarkDetailScroll = m.benchmarkDetailMaximumScroll()
				return m, nil
			}
		case "p":
			if m.meterView == viewMonitor {
				return m.pressFooterButton(footerButtonMonitorPause)
			}
		}
	case tea.MouseMsg:
		mouse := message.Mouse()
		_, clicked := message.(tea.MouseClickMsg)
		m.resetHovered = m.resetAt(mouse.X, mouse.Y)
		if m.resetAt(mouse.X, mouse.Y) && clicked && mouse.Button == tea.MouseLeft {
			return m.pressQuotaReset()
		}
		if m.meterView == viewBenchmark && m.benchmarkScopeOpen {
			m.benchmarkScopeKeyboard = false
		}
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
		if m.meterView == viewBenchmark && m.benchmarkScopeOpen {
			if item, ok := m.benchmarkScopeItemAt(mouse.X, mouse.Y); ok {
				m.hoveredButton = footerButtonNone
				if mouse.Button == tea.MouseLeft && clicked {
					m.toggleBenchmarkScopeItem(item)
				}
				return m, nil
			}
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.moveBenchmarkScopeCursor(-1)
				return m, nil
			case tea.MouseWheelDown:
				m.moveBenchmarkScopeCursor(1)
				return m, nil
			}
		}
		if m.meterView == viewBenchmark && m.benchmarkDetail != nil {
			m.benchmarkRunHovered = false
			m.benchmarkHoveredRun = ""
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.scrollBenchmarkDetail(-3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollBenchmarkDetail(3)
				return m, nil
			}
		}
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
		if row, ok := m.benchmarkRunAt(mouse.X, mouse.Y); ok {
			m.benchmarkRunHovered = true
			m.benchmarkHoveredRun = benchmarkRunKey(row.result)
			m.hoveredButton = footerButtonNone
			if mouse.Button == tea.MouseLeft && clicked {
				m.openBenchmarkDetail(row.result, row.active)
			}
			return m, nil
		}
		m.benchmarkRunHovered = false
		m.benchmarkHoveredRun = ""
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
		m.prepareBenchmarkDetailTranscript()
	case fetchedMsg:
		if m.resetBusy || message.resetRevision != m.resetRevision {
			return m, nil
		}
		if message.err == nil && m.resetNotice == "Quota reset. Refreshing limits…" {
			m.resetNotice = "Quota reset. Limits refreshed."
		}
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.snapshot = message.snapshot
			m.lastRefresh = time.Now()
			if message.benchmarkQuotaRevision != m.benchmarkQuotaAccounting.revision || m.benchmarkQuotaAccounting.active {
				m.quotaAPITelemetryIssue = "OBSERVATION DEFERRED"
			} else if message.usageErr == nil && m.usageFetcher != nil {
				m.quotaAPITelemetryIssue = ""
				m.observeQuotaAPIEq(message.snapshot, message.usage, message.at)
				m.benchmarkQuotaAccounting.settle()
			} else if message.usageErr != nil {
				if errors.Is(message.usageErr, errQuotaObservationChanged) {
					m.quotaAPITelemetryIssue = "OBSERVATION DEFERRED"
				} else {
					m.quotaAPITelemetryIssue = "LOCAL TELEMETRY UNAVAILABLE"
				}
			}
			if m.monitorState == monitorRunning || m.monitorState == monitorPausing {
				m.syncMonitorQuotaSnapshot(message.snapshot)
				m.monitorQuotaError = ""
			}
		} else if m.monitorState == monitorRunning || m.monitorState == monitorPausing {
			m.monitorQuotaError = message.err.Error()
		}
		if m.monitorAutoStart && m.monitorState == monitorIdle {
			m.monitorAutoStart = false
			if message.err == nil && message.usageErr == nil && m.usageFetcher != nil {
				started, _, _ := m.applyMonitorFetch(monitorFetchedMsg{
					kind: monitorFetchStart, usage: message.usage, quota: message.snapshot, at: message.at,
				})
				m = started.(Model)
				return m, nil
			}
			return m.beginMonitorFetch(monitorFetchStart)
		}
	case secondMsg:
		if !m.resetConfirmUntil.IsZero() && time.Now().After(m.resetConfirmUntil) {
			m.resetConfirmUntil = time.Time{}
			m.resetNotice = ""
		}
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
			if !m.monitorFetchActive && (m.monitorNextFetch.IsZero() || !now.Before(m.monitorNextFetch)) {
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
		if m.monitorState == monitorRunning {
			m.monitorNextFetch = message.at.Add(m.monitorPollInterval())
		}
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
			if m.benchmarkActive != nil {
				m.benchmarkQuotaAccounting.abandonActiveResult()
			}
			if m.benchmarkState == benchmarkStopping {
				m.benchmarkState = benchmarkStopped
			} else {
				m.benchmarkState = benchmarkFinished
			}
			m.benchmarkActive = nil
			m.benchmarkActiveSince = time.Time{}
			m.benchmarkEvents = nil
			m.benchmarkCancel = nil
			if m.benchmarkQuotaAccounting.finish() {
				return m, m.fetch()
			}
			return m, nil
		}
		event := message.event
		if event.Total > 0 || !event.Stopped {
			m.benchmarkTotal = event.Total
		}
		m.benchmarkCompleted = event.Completed
		m.benchmarkCurrentModel = event.CurrentModel
		m.benchmarkCurrentEffort = event.CurrentEffort
		m.benchmarkCurrentTask = event.CurrentTask
		if event.Combinations > 0 {
			m.benchmarkCombinations = event.Combinations
		}
		if event.Active != nil {
			newRun := m.benchmarkActive == nil || benchmarkRunKey(*m.benchmarkActive) != benchmarkRunKey(*event.Active)
			if m.benchmarkActive == nil && m.benchmarkScroll > 0 {
				m.benchmarkScroll++
			}
			active := *event.Active
			m.benchmarkActive = &active
			if newRun {
				m.benchmarkActiveSince = time.Now().Add(-active.Duration)
			}
			if m.benchmarkDetailActive && m.benchmarkDetail != nil && benchmarkRunKey(*m.benchmarkDetail) == benchmarkRunKey(active) {
				m.benchmarkDetail = &active
				m.prepareBenchmarkDetailTranscript()
			}
		}
		if event.Result != nil {
			hadActive := m.benchmarkActive != nil && benchmarkRunKey(*m.benchmarkActive) == benchmarkRunKey(*event.Result)
			if !hadActive && m.benchmarkScroll > 0 {
				m.benchmarkScroll++
			}
			result := *event.Result
			m.benchmarkQuotaAccounting.observe(result)
			m.benchmarkResults, m.benchmarkResultIndexes = upsertBenchmarkResult(m.benchmarkResults, m.benchmarkResultIndexes, result)
			m.benchmarkRunResults, m.benchmarkRunIndexes = upsertBenchmarkResult(m.benchmarkRunResults, m.benchmarkRunIndexes, result)
			if m.benchmarkDetailActive && m.benchmarkDetail != nil && benchmarkRunKey(*m.benchmarkDetail) == benchmarkRunKey(result) {
				m.benchmarkDetail = &result
				m.benchmarkDetailActive = false
				m.prepareBenchmarkDetailTranscript()
			}
			m.benchmarkActive = nil
			m.benchmarkActiveSince = time.Time{}
		}
		if event.Err != nil {
			m.benchmarkError = event.Err.Error()
		}
		if event.Done {
			if m.benchmarkActive != nil && event.Result == nil {
				m.benchmarkQuotaAccounting.abandonActiveResult()
			}
			if event.Stopped {
				m.benchmarkState = benchmarkStopped
			} else {
				m.benchmarkState = benchmarkFinished
			}
			m.benchmarkActive = nil
			m.benchmarkActiveSince = time.Time{}
			m.benchmarkEvents = nil
			m.benchmarkCancel = nil
			if m.benchmarkQuotaAccounting.finish() {
				return m, m.fetch()
			}
			return m, nil
		}
		return m, waitBenchmarkEvent(message.events)
	case benchmarkPlanMsg:
		m.benchmarkPlanning = false
		if message.err != nil {
			m.benchmarkError = message.err.Error()
		} else {
			m.benchmarkError = ""
			if len(message.plan.Models) > 0 {
				m.benchmarkPlan = message.plan
				m.benchmarkScope = message.plan.AllScope()
			}
			m.benchmarkCombinations = message.combinations
		}
	case benchmarkConfirmExpiredMsg:
		if message.sequence == m.benchmarkConfirmSequence {
			m.benchmarkAllArmed = false
			m.benchmarkSelectedArmed = false
		}
	}
	return m, nil
}

func (m Model) pressViewTab(view meterViewID) (tea.Model, tea.Cmd) {
	m.resetConfirmUntil = time.Time{}
	if !m.resetBusy {
		m.resetNotice = ""
	}
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
	if view == viewBenchmark && m.benchmarkRunner != nil && m.benchmarkPlanNeeded() && !m.benchmarkPlanning {
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
		m.prepareBenchmarkDetailTranscript()
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
	case footerButtonMonitorPause:
		if m.meterView != viewMonitor {
			break
		}
		switch m.monitorState {
		case monitorRunning:
			m.monitorState = monitorPausing
			return m.beginMonitorFetch(monitorFetchPause)
		case monitorPaused:
			m.monitorState = monitorResuming
			return m.beginMonitorFetch(monitorFetchResume)
		}
	case footerButtonMonitorReset:
		if m.meterView == viewMonitor && (m.monitorState == monitorRunning || m.monitorState == monitorPaused) {
			m.monitorResetPaused = m.monitorState == monitorPaused
			m.monitorState = monitorResetting
			return m.beginMonitorFetch(monitorFetchReset)
		}
	case footerButtonBenchmarkPrevious:
		if !m.benchmarkRunActive() {
			m.selectBenchmarkSuite(-1)
		}
	case footerButtonBenchmarkNext:
		if !m.benchmarkRunActive() {
			m.selectBenchmarkSuite(1)
		}
	case footerButtonBenchmarkSelected:
		if m.meterView == viewBenchmark && m.benchmarkCanRunSelected() {
			tasks := m.benchmarkScopedTasks()
			if m.benchmarkSelectedSuiteExternal() && !m.benchmarkSelectedArmed {
				m.benchmarkSelectedArmed = true
				m.benchmarkConfirmSequence++
				sequence := m.benchmarkConfirmSequence
				return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return benchmarkConfirmExpiredMsg{sequence: sequence} })
			}
			m.benchmarkSelectedArmed = false
			m.benchmarkAllArmed = false
			return m.startBenchmark(tasks, m.benchmarkScope, false)
		}
	case footerButtonBenchmarkAll:
		tasks := m.benchmarkAllTasks()
		allScope := m.benchmarkPlan.AllScope()
		allCombinations := m.benchmarkPlan.CombinationCount(allScope)
		if m.benchmarkScopedRunner == nil {
			allCombinations = m.benchmarkCombinations
		}
		if m.meterView == viewBenchmark && benchmarkRunAllAvailable(m.benchmarkRunActive(), allCombinations, len(tasks)) {
			if !m.benchmarkAllArmed {
				m.benchmarkAllArmed = true
				m.benchmarkConfirmSequence++
				sequence := m.benchmarkConfirmSequence
				return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return benchmarkConfirmExpiredMsg{sequence: sequence} })
			}
			m.benchmarkAllArmed = false
			return m.startBenchmark(tasks, allScope, true)
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
	case footerButtonBenchmarkCopy:
		if m.meterView == viewBenchmark {
			if m.benchmarkDetail != nil {
				return m, tea.SetClipboard(m.benchmarkDetailClipboardText())
			}
			if !m.benchmarkScopeOpen && m.benchmarkResultsCopyAvailable() {
				return m, tea.SetClipboard(m.benchmarkResultsClipboardText())
			}
		}
	case footerButtonBenchmarkClearAll:
		if m.meterView == viewBenchmark && !m.benchmarkRunActive() && len(m.benchmarkResults) > 0 {
			m.clearBenchmarkResults()
		}
	case footerButtonBenchmarkClose:
		if m.meterView == viewBenchmark {
			if m.benchmarkScopeOpen {
				m.cancelBenchmarkScope()
			} else if m.benchmarkDetail != nil {
				m.closeBenchmarkDetail()
			}
		}
	case footerButtonBenchmarkDone:
		if m.meterView == viewBenchmark && m.benchmarkScopeOpen {
			m.finishBenchmarkScope()
		}
	case footerButtonBenchmarkScope:
		if m.meterView == viewBenchmark && !m.benchmarkRunActive() && len(m.benchmarkPlan.Models) > 0 {
			m.openBenchmarkScope()
		}
	case footerButtonBenchmarkStop:
		if m.meterView == viewBenchmark && m.benchmarkState == benchmarkRunning {
			m.benchmarkState = benchmarkStopping
			m.benchmarkAllArmed = false
			m.benchmarkSelectedArmed = false
			m.cancelBenchmark()
		}
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
	if m.resetOwnRow(contentWidth) {
		tabsHeight++
	}
	const framedErrorHeight = 3
	const footerHeight = 2
	extraHeight := 0
	extraHeight += m.resetNoticeHeight(contentWidth)
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
		if m.resetOwnRow(contentWidth) {
			quotaTabsY++
		}
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

func launchBenchmark(ctx context.Context, runner BenchmarkRunner, tasks []codex.BenchmarkTaskID, scope codex.BenchmarkScope, events chan codex.BenchmarkEvent) tea.Cmd {
	return func() tea.Msg {
		go func() {
			defer close(events)
			emit := func(event codex.BenchmarkEvent) {
				if event.Stopped {
					events <- event
					return
				}
				select {
				case events <- event:
				case <-ctx.Done():
				}
			}
			if scoped, ok := runner.(ScopedBenchmarkRunner); ok {
				scoped.RunBenchmarkSuiteScoped(ctx, tasks, scope, emit)
			} else {
				runner.RunBenchmarkSuite(ctx, tasks, emit)
			}
		}()
		event, ok := <-events
		return benchmarkEventMsg{event: event, events: events, ok: ok}
	}
}

func planBenchmark(runner BenchmarkRunner) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if scoped, ok := runner.(ScopedBenchmarkRunner); ok {
			plan, err := scoped.BenchmarkPlan(ctx)
			return benchmarkPlanMsg{plan: plan, combinations: plan.CombinationCount(plan.AllScope()), err: err}
		}
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

func (m Model) benchmarkRunActive() bool {
	return m.benchmarkState == benchmarkRunning || m.benchmarkState == benchmarkStopping
}

func (m Model) benchmarkCanRunSelected() bool {
	if m.benchmarkRunActive() || m.benchmarkCombinations == 0 {
		return false
	}
	return len(m.benchmarkScopedTasks()) > 0 && (!m.benchmarkSelectedSuiteExternal() || len(m.benchmarkScope.Games) > 0)
}

func (m Model) benchmarkPlanNeeded() bool {
	if m.benchmarkScopedRunner != nil {
		return len(m.benchmarkPlan.Models) == 0
	}
	return m.benchmarkCombinations == 0
}

func (m Model) startBenchmark(tasks []codex.BenchmarkTaskID, scope codex.BenchmarkScope, clearAll bool) (Model, tea.Cmd) {
	if m.benchmarkRunner == nil {
		m.benchmarkState = benchmarkFinished
		m.benchmarkError = "benchmark runner unavailable"
		return m, nil
	}
	if clearAll {
		m.benchmarkResults = nil
	} else {
		m.benchmarkResults = benchmarkResultsOutsideScope(m.benchmarkResults, tasks, scope)
	}
	m.benchmarkResultIndexes = indexBenchmarkResults(m.benchmarkResults)
	m.benchmarkRunResults = make([]codex.BenchmarkResult, 0)
	m.benchmarkRunIndexes = make(map[string]int)
	combinations := m.benchmarkPlan.CombinationCount(scope)
	if m.benchmarkScopedRunner == nil {
		combinations = m.benchmarkCombinations
	}
	m.benchmarkTotal = combinations * len(tasks)
	if len(tasks) == 1 && tasks[0] == codex.BenchmarkDigBench {
		m.benchmarkTotal = combinations * len(scope.Games)
	}
	m.benchmarkCompleted = 0
	m.benchmarkCurrentModel = ""
	m.benchmarkCurrentEffort = ""
	m.benchmarkCurrentTask = ""
	m.benchmarkError = ""
	m.benchmarkScroll = 0
	m.benchmarkActive = nil
	m.benchmarkActiveSince = time.Time{}
	m.benchmarkSelectedRun = ""
	m.benchmarkHoveredRun = ""
	m.benchmarkRunHovered = false
	m.benchmarkDetail = nil
	m.benchmarkDetailActive = false
	m.benchmarkDetailScroll = 0
	m.benchmarkDetailCache = benchmarkDetailTranscriptCache{}
	m.benchmarkScopeOpen = false
	m.benchmarkScopeKeyboard = false
	m.benchmarkState = benchmarkRunning
	m.benchmarkQuotaAccounting.start()
	m.benchmarkAllArmed = false
	m.benchmarkSelectedArmed = false
	ctx, cancel := context.WithCancel(context.Background())
	m.benchmarkCancel = cancel
	events := make(chan codex.BenchmarkEvent, 2)
	m.benchmarkEvents = events
	return m, launchBenchmark(ctx, m.benchmarkRunner, tasks, scope, events)
}

func (m *Model) clearBenchmarkResults() {
	m.benchmarkState = benchmarkIdle
	m.benchmarkResults = nil
	m.benchmarkResultIndexes = nil
	m.benchmarkRunResults = nil
	m.benchmarkRunIndexes = nil
	m.benchmarkTotal = 0
	m.benchmarkCompleted = 0
	m.benchmarkCurrentModel = ""
	m.benchmarkCurrentEffort = ""
	m.benchmarkCurrentTask = ""
	m.benchmarkError = ""
	m.benchmarkScroll = 0
	m.benchmarkActive = nil
	m.benchmarkActiveSince = time.Time{}
	m.benchmarkSelectedRun = ""
	m.benchmarkHoveredRun = ""
	m.benchmarkRunHovered = false
	m.benchmarkDetail = nil
	m.benchmarkDetailActive = false
	m.benchmarkDetailScroll = 0
	m.benchmarkDetailCache = benchmarkDetailTranscriptCache{}
	m.benchmarkAllArmed = false
	m.benchmarkSelectedArmed = false
}

func upsertBenchmarkResult(results []codex.BenchmarkResult, indexes map[string]int, result codex.BenchmarkResult) ([]codex.BenchmarkResult, map[string]int) {
	if indexes == nil {
		indexes = indexBenchmarkResults(results)
	}
	key := benchmarkRunKey(result)
	if index, ok := indexes[key]; ok {
		results[index] = result
		return results, indexes
	}
	indexes[key] = len(results)
	return append(results, result), indexes
}

func indexBenchmarkResults(results []codex.BenchmarkResult) map[string]int {
	indexes := make(map[string]int, len(results))
	for index, result := range results {
		indexes[benchmarkRunKey(result)] = index
	}
	return indexes
}

func benchmarkResultsOutsideScope(results []codex.BenchmarkResult, tasks []codex.BenchmarkTaskID, scope codex.BenchmarkScope) []codex.BenchmarkResult {
	taskSet := make(map[codex.BenchmarkTaskID]bool, len(tasks))
	for _, task := range tasks {
		taskSet[task] = true
	}
	modelSet := lowerStringSet(scope.Models)
	effortSet := lowerStringSet(scope.Efforts)
	gameSet := lowerStringSet(scope.Games)
	kept := make([]codex.BenchmarkResult, 0, len(results))
	for _, result := range results {
		model := result.Model
		if model == "" {
			model = result.DisplayName
		}
		matches := taskSet[result.TaskID]
		if len(modelSet) > 0 {
			matches = matches && modelSet[strings.ToLower(model)]
		}
		if len(effortSet) > 0 {
			matches = matches && effortSet[strings.ToLower(result.Effort)]
		}
		if matches && result.TaskID == codex.BenchmarkDigBench {
			game := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(result.TaskName), "digbench"))
			matches = len(gameSet) == 0 || gameSet[game]
		}
		if !matches {
			kept = append(kept, result)
		}
	}
	return kept
}

func lowerStringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[strings.ToLower(value)] = true
	}
	return set
}

func (m *Model) selectBenchmarkSuite(direction int) {
	suites := m.benchmarkSuites()
	if len(suites) == 0 {
		return
	}
	m.benchmarkSelectedSuite = (m.benchmarkSelectedSuite + direction + len(suites)) % len(suites)
	m.benchmarkScopeCursor = 0
	m.benchmarkScopeScroll = 0
	m.benchmarkAllArmed = false
	m.benchmarkSelectedArmed = false
}

func (m Model) benchmarkTasks() []codex.BenchmarkTask {
	if provider, ok := m.benchmarkRunner.(BenchmarkTaskProvider); ok {
		return provider.BenchmarkTasks()
	}
	return codex.BenchmarkTasks()
}

func (m Model) benchmarkTasksForSuite(suite codex.BenchmarkSuiteID) []codex.BenchmarkTask {
	var tasks []codex.BenchmarkTask
	for _, task := range m.benchmarkTasks() {
		if task.Suite == suite {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func (m Model) benchmarkSuites() []benchmarkSuite {
	suites := []benchmarkSuite{
		{id: codex.BenchmarkSuiteCore, name: "CODEXOMETER CORE"},
		{id: codex.BenchmarkSuiteExtended, name: "CODEXOMETER EXTENDED"},
	}
	for _, task := range m.benchmarkTasks() {
		if task.ID == codex.BenchmarkDigBench && task.External {
			return append(suites, benchmarkSuite{id: codex.BenchmarkSuiteDigBench, name: "DIGBENCH", external: true})
		}
	}
	return suites
}

func (m Model) benchmarkSelectedSuiteOption() benchmarkSuite {
	suites := m.benchmarkSuites()
	if len(suites) == 0 {
		return benchmarkSuite{name: "NO SUITES"}
	}
	return suites[m.benchmarkSelectedSuite%len(suites)]
}

func (m Model) benchmarkSelectedSuiteExternal() bool {
	return m.benchmarkSelectedSuiteOption().external
}

func (m Model) benchmarkSelectedTaskIDs() []codex.BenchmarkTaskID {
	suite := m.benchmarkSelectedSuiteOption().id
	if tasks, ok := m.benchmarkScopeTasks[suite]; ok {
		return append([]codex.BenchmarkTaskID(nil), tasks...)
	}
	ids := make([]codex.BenchmarkTaskID, 0, len(m.benchmarkTasksForSuite(suite)))
	for _, task := range m.benchmarkTasksForSuite(suite) {
		ids = append(ids, task.ID)
	}
	return ids
}

func (m *Model) setBenchmarkSelectedTaskIDs(tasks []codex.BenchmarkTaskID) {
	selected := make(map[codex.BenchmarkSuiteID][]codex.BenchmarkTaskID, len(m.benchmarkScopeTasks)+1)
	for suite, existing := range m.benchmarkScopeTasks {
		selected[suite] = append([]codex.BenchmarkTaskID(nil), existing...)
	}
	selected[m.benchmarkSelectedSuiteOption().id] = append([]codex.BenchmarkTaskID(nil), tasks...)
	m.benchmarkScopeTasks = selected
}

func (m *Model) selectAllBenchmarkTasks() {
	var tasks []codex.BenchmarkTaskID
	for _, task := range m.benchmarkTasksForSuite(m.benchmarkSelectedSuiteOption().id) {
		tasks = append(tasks, task.ID)
	}
	m.setBenchmarkSelectedTaskIDs(tasks)
}

func (m Model) benchmarkScopedTasks() []codex.BenchmarkTaskID {
	if m.benchmarkSelectedSuiteExternal() {
		return []codex.BenchmarkTaskID{codex.BenchmarkDigBench}
	}
	selected := benchmarkTaskIDSet(m.benchmarkSelectedTaskIDs())
	var tasks []codex.BenchmarkTaskID
	for _, task := range m.benchmarkTasksForSuite(m.benchmarkSelectedSuiteOption().id) {
		if selected[task.ID] {
			tasks = append(tasks, task.ID)
		}
	}
	return tasks
}

func (m Model) benchmarkAllTasks() []codex.BenchmarkTaskID {
	if m.benchmarkSelectedSuiteExternal() {
		return []codex.BenchmarkTaskID{codex.BenchmarkDigBench}
	}
	var tasks []codex.BenchmarkTaskID
	for _, task := range m.benchmarkTasksForSuite(m.benchmarkSelectedSuiteOption().id) {
		tasks = append(tasks, task.ID)
	}
	return tasks
}

func (m Model) benchmarkScopeTurnCount() int {
	items := len(m.benchmarkScopedTasks())
	if m.benchmarkSelectedSuiteExternal() {
		items = len(m.benchmarkScope.Games)
	}
	return m.benchmarkCombinations * items
}

func (m Model) benchmarkAllTurnCount() int {
	combinations := m.benchmarkPlan.CombinationCount(m.benchmarkPlan.AllScope())
	if m.benchmarkScopedRunner == nil {
		combinations = m.benchmarkCombinations
	}
	items := len(m.benchmarkAllTasks())
	if m.benchmarkSelectedSuiteExternal() {
		items = len(m.benchmarkPlan.Games)
	}
	return combinations * items
}

func (m *Model) setBenchmarkFilter(filter benchmarkResultFilter) {
	m.benchmarkFilter = filter
	m.benchmarkScroll = 0
	m.benchmarkSelectedRun = ""
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
	maximum := max(len(m.orderedBenchmarkResults())-m.benchmarkPageSize(), 0)
	m.benchmarkScroll = min(max(m.benchmarkScroll+rows, 0), maximum)
}

func (m *Model) selectBenchmarkRun(direction int) {
	results := m.orderedBenchmarkResults()
	if len(results) == 0 {
		m.benchmarkSelectedRun = ""
		return
	}
	selected := -1
	for index, result := range results {
		if benchmarkRunKey(result) == m.benchmarkSelectedRun {
			selected = index
			break
		}
	}
	if selected < 0 {
		selected = len(results) - 1
		if direction > 0 {
			selected = 0
		}
	} else {
		selected = min(max(selected+direction, 0), len(results)-1)
	}
	m.benchmarkSelectedRun = benchmarkRunKey(results[selected])
	m.revealBenchmarkRun(selected, len(results))
}

func (m *Model) revealBenchmarkRun(index, total int) {
	layout := m.dashboardLayout()
	geometry := layoutBenchmarkArea(layout.contentWidth, layout.meterHeight)
	bodyHeight := max(geometry.tableHeight-2, 1)
	baseLines := 1
	if bodyHeight > 1 {
		baseLines++
	}
	if bodyHeight > 2 {
		baseLines++
	}
	available := max(bodyHeight-baseLines, 0)
	start, end, banner := benchmarkVisibleResultRange(total, available, m.benchmarkScroll)
	pageSize := available
	if banner {
		pageSize--
	}
	pageSize = max(pageSize, 1)
	if index < start {
		m.benchmarkScroll = min(max(total-(index+pageSize), 0), max(total-pageSize, 0))
	} else if index >= end {
		m.benchmarkScroll = max(total-index-1, 0)
	}
}

func (m *Model) openSelectedBenchmarkDetail() {
	results := m.orderedBenchmarkResults()
	if len(results) == 0 {
		return
	}
	if m.benchmarkSelectedRun == "" {
		m.selectBenchmarkRun(-1)
	}
	for _, result := range results {
		if benchmarkRunKey(result) == m.benchmarkSelectedRun {
			active := m.benchmarkActive != nil && benchmarkRunKey(*m.benchmarkActive) == benchmarkRunKey(result)
			m.openBenchmarkDetail(result, active)
			return
		}
	}
}

func (m *Model) openBenchmarkDetail(result codex.BenchmarkResult, active ...bool) {
	m.benchmarkSelectedRun = benchmarkRunKey(result)
	m.benchmarkDetail = &result
	m.benchmarkDetailActive = len(active) > 0 && active[0]
	m.benchmarkDetailScroll = 0
	m.prepareBenchmarkDetailTranscript()
	m.benchmarkRunHovered = false
	m.benchmarkHoveredRun = ""
}

func (m *Model) closeBenchmarkDetail() {
	m.benchmarkDetail = nil
	m.benchmarkDetailActive = false
	m.benchmarkDetailScroll = 0
	m.benchmarkDetailCache = benchmarkDetailTranscriptCache{}
}

func (m *Model) scrollBenchmarkDetail(rows int) {
	if m.benchmarkDetail == nil {
		return
	}
	m.prepareBenchmarkDetailTranscript()
	maximum := m.benchmarkDetailMaximumScroll()
	m.benchmarkDetailScroll = min(max(m.benchmarkDetailScroll+rows, 0), maximum)
}

func (m *Model) prepareBenchmarkDetailTranscript() {
	if m.benchmarkDetail == nil {
		m.benchmarkDetailCache = benchmarkDetailTranscriptCache{}
		return
	}
	result, ok := m.benchmarkDetailResult()
	if !ok {
		m.benchmarkDetailCache = benchmarkDetailTranscriptCache{}
		return
	}
	layout := m.dashboardLayout()
	width := max(layout.contentWidth-4, 1)
	cache := &m.benchmarkDetailCache
	run := benchmarkDetailCacheKey(result)
	canAppend := cache.valid && cache.run == run && cache.width == width && cache.theme == m.theme && cache.correct == result.Correct && cache.interactionCount <= len(result.Interactions)
	if canAppend && cache.interactionCount > 0 && cache.lastInteraction != result.Interactions[cache.interactionCount-1] {
		canAppend = false
	}
	colors := paletteFor(m.theme)
	if !canAppend {
		cache.lines = buildBenchmarkDetailTranscriptLines(result.Interactions, 0, width, colors, result.Correct)
		cache.interactionCount = len(result.Interactions)
	} else if cache.interactionCount < len(result.Interactions) {
		cache.lines = append(cache.lines, buildBenchmarkDetailTranscriptLines(result.Interactions, cache.interactionCount, width, colors, result.Correct)...)
		cache.interactionCount = len(result.Interactions)
	}
	cache.valid = true
	cache.run = run
	cache.width = width
	cache.theme = m.theme
	cache.correct = result.Correct
	cache.lastInteraction = codex.BenchmarkInteraction{}
	if cache.interactionCount > 0 {
		cache.lastInteraction = result.Interactions[cache.interactionCount-1]
	}
}

func (m *Model) scrollBenchmarkDetailPage(direction int) {
	m.scrollBenchmarkDetail(direction * m.benchmarkDetailPageSize())
}

func (m *Model) sortBenchmarkBy(column benchmarkSortColumn) {
	if m.benchmarkSort == column {
		m.benchmarkSortDescending = !m.benchmarkSortDescending
	} else {
		m.benchmarkSort = column
		m.benchmarkSortDescending = false
	}
	m.benchmarkScroll = 0
	m.benchmarkSelectedRun = ""
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
			before = m.benchmarkQuotaAccounting.combine(before)
		}
		snapshot, err := m.fetcher.Fetch(context.Background())
		message := fetchedMsg{
			resetRevision: m.resetRevision,
			snapshot:      snapshot, err: err, at: time.Now(),
			benchmarkQuotaRevision: m.benchmarkQuotaAccounting.revision,
		}
		if err == nil && m.usageFetcher != nil {
			after, afterErr := m.usageFetcher.FetchTokenUsage(context.Background())
			after = m.benchmarkQuotaAccounting.combine(after)
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
const monitorActivePollInterval = time.Second
const monitorIdlePollInterval = 5 * time.Second
const monitorSampleHistoryMax = 4_096

func (m Model) beginMonitorFetch(kind monitorFetchKind) (Model, tea.Cmd) {
	switch kind {
	case monitorFetchStart:
		m.monitorState = monitorStarting
	case monitorFetchPause:
		m.monitorState = monitorPausing
	case monitorFetchResume:
		m.monitorState = monitorResuming
	case monitorFetchReset:
		m.monitorState = monitorResetting
	}
	m.monitorRequest++
	m.monitorFetchActive = true
	return m, m.monitorFetch(kind, m.monitorRequest)
}

func (m Model) monitorPollInterval() time.Duration {
	for _, session := range m.monitorSessionData {
		if session.active {
			return monitorActivePollInterval
		}
	}
	return monitorIdlePollInterval
}

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
		if kind == monitorFetchStart || kind == monitorFetchResume || kind == monitorFetchReset {
			// Bracket the measured local-token interval inside the account-quota
			// interval: quota first when starting a segment, local telemetry first
			// when pausing one.
			fetchQuota()
			usage, err = m.usageFetcher.FetchTokenUsage(context.Background())
		} else if kind == monitorFetchPause {
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
	if message.kind == monitorFetchPause {
		if message.quotaErr == nil {
			m.syncMonitorQuotaSnapshot(message.quota)
		}
		m.applyMonitorQuotaResult(message)
	}
	if message.err != nil {
		m.monitorError = message.err.Error()
		if message.kind == monitorFetchPause {
			m.monitorState = monitorPaused
			m.monitorStoppedAt = message.at
		} else if message.kind == monitorFetchStart || message.kind == monitorFetchResume {
			m.monitorState = monitorPaused
		} else if message.kind == monitorFetchReset {
			if m.monitorResetPaused {
				m.monitorState = monitorPaused
			} else {
				m.monitorState = monitorRunning
				m.monitorNextFetch = message.at.Add(m.monitorPollInterval())
			}
			m.monitorResetPaused = false
		}
		return m, nil, false
	}
	m.syncMonitorCodexStatus(message.usage)
	accepted := true
	switch message.kind {
	case monitorFetchStart:
		m.resetMonitorFromSnapshot(message, false)
	case monitorFetchReset:
		m.resetMonitorFromSnapshot(message, m.monitorResetPaused)
	case monitorFetchResume:
		if m.monitorUsageMovesBackwards(message.usage) {
			m.monitorError = "local Codex token counter moved backwards"
			m.monitorState = monitorPaused
			accepted = false
			break
		}
		if m.monitorStartedAt.IsZero() {
			m.resetMonitorFromSnapshot(message, false)
			break
		}
		if message.quotaErr == nil {
			m.resumeMonitorQuotaSnapshot(message.quota)
		}
		m.applyMonitorQuotaResult(message)
		m.resumeMonitorFromSnapshot(message)
	case monitorFetchSample, monitorFetchBoundary, monitorFetchPause:
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
		if message.kind == monitorFetchPause {
			m.monitorState = monitorPaused
			m.monitorStoppedAt = message.at
			m.monitorNextFetch = time.Time{}
		}
	}
	return m, nil, accepted
}

func (m *Model) syncMonitorCodexStatus(usage codex.LiveUsageSnapshot) {
	m.monitorCodexStatusKnown = usage.CodexStatusKnown
	m.monitorCodexUp = usage.CodexUp
	m.monitorCodexWorking = usage.CodexWorking
}

func (m Model) monitorHasVisibleWaitingSession() bool {
	for _, session := range m.monitorSessionData {
		if m.monitorSessionVisible(session) && session.attention != codex.SessionAttentionNone {
			return true
		}
	}
	return false
}

func (m *Model) resetMonitorFromSnapshot(message monitorFetchedMsg, paused bool) {
	m.monitorStartedAt = message.at
	m.monitorStoppedAt = time.Time{}
	m.monitorBaseline = message.usage.TotalTokens
	m.monitorLatest = message.usage.TotalTokens
	m.monitorGraphStart = message.usage.TotalTokens
	m.monitorSamples = nil
	m.monitorSessionData = nil
	m.monitorSelectedID = ""
	m.monitorDismissed = nil
	m.monitorDismissHover = ""
	m.monitorDismissFlash = ""
	m.monitorDismissSeq++
	m.monitorScroll = 0
	m.monitorBoundaryDue = false
	m.monitorLastActivity = time.Time{}
	m.monitorSessions = 0
	m.monitorError = ""
	m.monitorQuotaError = ""
	m.startMonitorQuotaSnapshot(message.quota)
	if message.quotaErr != nil {
		m.monitorQuotaError = message.quotaErr.Error()
		for index := range m.monitorQuotaWindows {
			m.monitorQuotaWindows[index].partial = true
		}
	}
	m.applyMonitorQuotaResult(message)
	m.startMonitorSessions(message.usage, message.at)
	m.monitorSessions = m.visibleMonitorSessionCount()
	m.monitorResetPaused = false
	if paused {
		m.monitorState = monitorPaused
		m.monitorStoppedAt = message.at
		m.monitorNextFetch = time.Time{}
		m.monitorNextSample = time.Time{}
		return
	}
	m.monitorState = monitorRunning
	m.monitorNextFetch = message.at.Add(m.monitorPollInterval())
	m.monitorNextSample = message.at.Add(monitorSampleInterval)
}

func (m *Model) resumeMonitorFromSnapshot(message monitorFetchedMsg) {
	pausedFor := time.Duration(0)
	if !m.monitorStoppedAt.IsZero() && message.at.After(m.monitorStoppedAt) {
		pausedFor = message.at.Sub(m.monitorStoppedAt)
		m.monitorStartedAt = m.monitorStartedAt.Add(pausedFor)
		for index := range m.monitorSamples {
			m.monitorSamples[index].at = m.monitorSamples[index].at.Add(pausedFor)
		}
	}
	recorded := m.monitorRecordedTokens()
	m.monitorBaseline = message.usage.TotalTokens - recorded
	m.monitorLatest = message.usage.TotalTokens
	m.monitorGraphStart = message.usage.TotalTokens
	m.resumeMonitorSessions(message.usage, message.at, pausedFor)
	m.monitorSessions = m.visibleMonitorSessionCount()
	m.monitorStoppedAt = time.Time{}
	m.monitorState = monitorRunning
	m.monitorError = ""
	m.monitorBoundaryDue = false
	m.monitorNextFetch = message.at.Add(m.monitorPollInterval())
	m.monitorNextSample = message.at.Add(monitorSampleInterval)
}

func (m *Model) resumeMonitorSessions(usage codex.LiveUsageSnapshot, observedAt time.Time, pausedFor time.Duration) {
	updates := make(map[string]codex.LiveUsageSession, len(usage.Sessions))
	for _, update := range usage.Sessions {
		updates[update.ID] = update
	}
	for index := range m.monitorSessionData {
		session := &m.monitorSessionData[index]
		if pausedFor > 0 {
			session.startedAt = session.startedAt.Add(pausedFor)
			for sampleIndex := range session.samples {
				session.samples[sampleIndex].at = session.samples[sampleIndex].at.Add(pausedFor)
			}
		}
		update, ok := updates[session.id]
		if !ok {
			session.active = false
			session.attention = codex.SessionAttentionNone
			continue
		}
		recorded := max(session.latest-session.baseline, int64(0))
		session.baseline = update.TotalTokens - recorded
		session.latest = update.TotalTokens
		session.graphStart = update.TotalTokens
		session.lastActivity = update.LastActivity
		session.agentCount = max(session.agentCount, update.AgentCount)
		session.active = update.Active
		session.attention = update.Attention
		session.callSequence = latestModelCallSequence(update.ModelCalls)
		session.turnSequence = latestTurnTimingSequence(update.TurnTimings)
		if update.WorkingDirectory != "" {
			session.workingDirectory = update.WorkingDirectory
		}
		if dismissal, ok := m.monitorDismissed[session.id]; ok {
			dismissal.latest = session.latest
			dismissal.lastActivity = session.lastActivity
			dismissal.callSequence = session.callSequence
			dismissal.turnSequence = session.turnSequence
			dismissal.attention = session.attention
			m.monitorDismissed[session.id] = dismissal
		}
		delete(updates, session.id)
	}
	for _, update := range updates {
		m.monitorSessionData = append(m.monitorSessionData, monitorSession{
			id: update.ID, workingDirectory: update.WorkingDirectory,
			baseline: update.TotalTokens, latest: update.TotalTokens, graphStart: update.TotalTokens,
			startedAt: observedAt, lastActivity: update.LastActivity, agentCount: update.AgentCount,
			active: update.Active, attention: update.Attention, displayed: update.Active,
			unattributed: update.Unattributed, callSequence: latestModelCallSequence(update.ModelCalls),
			turnSequence: latestTurnTimingSequence(update.TurnTimings),
		})
	}
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

// resumeMonitorQuotaSnapshot advances the quota baseline by any change observed
// while polling was paused, preserving only the percentage-point delta already
// recorded by the Monitor.
func (m *Model) resumeMonitorQuotaSnapshot(snapshot codex.Snapshot) {
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
				key: key, label: monitorQuotaLabel(meter), baselineUsed: meter.Window.UsedPercent,
				latestUsed: meter.Window.UsedPercent, latestReset: cloneInt64(meter.Window.ResetsAt), partial: true,
			})
			continue
		}
		window := &m.monitorQuotaWindows[index]
		delta := window.latestUsed - window.baselineUsed
		if !sameOptionalInt64(window.latestReset, meter.Window.ResetsAt) || meter.Window.UsedPercent < window.latestUsed {
			window.resetDetected = true
		}
		window.baselineUsed = meter.Window.UsedPercent - delta
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
		if !dismissed {
			continue
		}
		if !session.active {
			dismissal.inactiveObserved = true
		}
		if dismissal.attention != codex.SessionAttentionNone && session.attention == codex.SessionAttentionNone {
			dismissal.attentionCleared = true
		}
		m.monitorDismissed[session.id] = dismissal
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
		attention:    session.attention,
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
	newAttention := session.attention != codex.SessionAttentionNone &&
		(session.attention != dismissal.attention || dismissal.attentionCleared)
	if session.latest > dismissal.latest ||
		session.lastActivity.After(dismissal.lastActivity) ||
		session.callSequence > dismissal.callSequence ||
		session.turnSequence > dismissal.turnSequence ||
		newAttention ||
		(dismissal.inactiveObserved && session.active) {
		delete(m.monitorDismissed, session.id)
	}
}

func (m Model) monitorSessionVisible(session monitorSession) bool {
	if !session.displayed {
		return false
	}
	_, dismissed := m.monitorDismissed[session.id]
	return !dismissed
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
	m.monitorSamples = appendMonitorSample(m.monitorSamples, monitorSample{at: at, duration: duration, intervalTokens: delta})
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
		session.samples = appendMonitorSample(session.samples, monitorSample{at: at, duration: duration, intervalTokens: delta})
		session.graphStart = session.latest
	}
}

func appendMonitorSample(samples []monitorSample, sample monitorSample) []monitorSample {
	if len(samples) >= monitorSampleHistoryMax {
		copy(samples, samples[len(samples)-monitorSampleHistoryMax+1:])
		samples = samples[:monitorSampleHistoryMax-1]
	}
	return append(samples, sample)
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

func (m *Model) selectMonitorSession(direction int) {
	visible := make([]string, 0, len(m.monitorSessionData))
	selected := -1
	for _, session := range m.monitorSessionData {
		if !m.monitorSessionVisible(session) {
			continue
		}
		if session.id == m.monitorSelectedID {
			selected = len(visible)
		}
		visible = append(visible, session.id)
	}
	if len(visible) == 0 {
		m.monitorSelectedID = ""
		return
	}
	if selected < 0 {
		if direction < 0 {
			selected = len(visible) - 1
		} else {
			selected = 0
		}
	} else {
		selected = min(max(selected+direction, 0), len(visible)-1)
	}
	m.monitorSelectedID = visible[selected]
	pageSize := max(m.monitorPageSize(), 1)
	if selected < m.monitorScroll {
		m.monitorScroll = selected
	} else if selected >= m.monitorScroll+pageSize {
		m.monitorScroll = selected - pageSize + 1
	}
}

func (m *Model) dismissSelectedMonitorSession() {
	visible := make([]string, 0, len(m.monitorSessionData))
	selected := -1
	for _, session := range m.monitorSessionData {
		if !m.monitorSessionVisible(session) {
			continue
		}
		if session.id == m.monitorSelectedID {
			selected = len(visible)
		}
		visible = append(visible, session.id)
	}
	if selected < 0 {
		m.monitorSelectedID = ""
		return
	}
	m.dismissMonitorSession(m.monitorSelectedID)
	visible = append(visible[:selected], visible[selected+1:]...)
	if len(visible) == 0 {
		m.monitorSelectedID = ""
		return
	}
	m.monitorSelectedID = visible[min(selected, len(visible)-1)]
	maximum := max(len(visible)-max(m.monitorPageSize(), 1), 0)
	m.monitorScroll = min(m.monitorScroll, maximum)
}

func secondTick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return secondMsg(now) })
}

func refreshTick(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(now time.Time) tea.Msg { return refreshMsg(now) })
}

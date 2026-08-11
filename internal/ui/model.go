package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	loading         bool
	lastRefresh     time.Time
	nextRefresh     time.Time
	phase           int
	theme           themeID
	meterStyle      meterStyleID
	hoveredStyle    meterStyleID
	styleHovered    bool
	flashedStyle    meterStyleID
	styleFlashing   bool
	styleSequence   uint64
	hoveredButton   footerButtonID
	flashedButton   footerButtonID
	flashSequence   uint64
	benchmarkRunner BenchmarkRunner

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
	monitorGraphStart   int64
	monitorLastActivity time.Time
	monitorSessions     int
	monitorError        string
}

type fetchedMsg struct {
	snapshot codex.Snapshot
	err      error
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
	monitorFetchStop
)

type monitorSample struct {
	at             time.Time
	intervalTokens int64
}

type monitorFetchedMsg struct {
	kind     monitorFetchKind
	sequence uint64
	usage    codex.LiveUsageSnapshot
	err      error
	at       time.Time
}

type footerButtonFlashExpiredMsg struct {
	button   footerButtonID
	sequence uint64
}

type styleTabFlashExpiredMsg struct {
	style    meterStyleID
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

const footerButtonFlashDuration = 150 * time.Millisecond

type footerButtonID int

const (
	footerButtonNone footerButtonID = iota
	footerButtonTheme
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
	if runner, ok := fetcher.(BenchmarkRunner); ok {
		model.benchmarkRunner = runner
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
			m.cancelBenchmark()
			return m, tea.Quit
		case "t":
			return m.pressFooterButton(footerButtonTheme)
		case "s":
			if m.meterStyle == styleMonitor {
				return m.pressFooterButton(footerButtonMonitorGo)
			}
		case "tab":
			return m.pressStyleTab(m.meterStyle.next())
		case "shift+tab":
			return m.pressStyleTab(m.meterStyle.previous())
		case "r":
			return m.pressFooterButton(footerButtonRefresh)
		case "q":
			return m.pressFooterButton(footerButtonQuit)
		case "b":
			if m.meterStyle == styleBenchmark {
				return m.pressFooterButton(footerButtonBenchmarkSelected)
			}
		case "a":
			if m.meterStyle == styleBenchmark {
				return m.pressFooterButton(footerButtonBenchmarkAll)
			}
		case "left", "[":
			if m.meterStyle == styleBenchmark {
				return m.pressFooterButton(footerButtonBenchmarkPrevious)
			}
		case "right", "]":
			if m.meterStyle == styleBenchmark {
				return m.pressFooterButton(footerButtonBenchmarkNext)
			}
		case "f":
			if m.meterStyle == styleBenchmark {
				m.setBenchmarkFilter((m.benchmarkFilter + 1) % 3)
				return m, nil
			}
		case "pgup":
			if m.meterStyle == styleBenchmark {
				m.scrollBenchmarkPage(1)
				return m, nil
			}
		case "pgdown":
			if m.meterStyle == styleBenchmark {
				m.scrollBenchmarkPage(-1)
				return m, nil
			}
		case "p":
			if m.meterStyle == styleMonitor {
				return m.pressFooterButton(footerButtonMonitorStop)
			}
		}
	case tea.MouseMsg:
		if style, ok := m.styleTabAt(message.X, message.Y); ok {
			m.styleHovered = true
			m.hoveredStyle = style
			m.hoveredButton = footerButtonNone
			if message.Button == tea.MouseButtonLeft && message.Action == tea.MouseActionPress {
				return m.pressStyleTab(style)
			}
			return m, nil
		}
		m.styleHovered = false
		if column, ok := m.benchmarkHeaderAt(message.X, message.Y); ok {
			m.benchmarkSortHovered = true
			m.benchmarkHoveredSort = column
			m.hoveredButton = footerButtonNone
			if message.Button == tea.MouseButtonLeft && message.Action == tea.MouseActionPress {
				m.sortBenchmarkBy(column)
			}
			return m, nil
		}
		m.benchmarkSortHovered = false
		if m.meterStyle == styleBenchmark {
			switch message.Button {
			case tea.MouseButtonWheelUp:
				m.scrollBenchmarkRows(3)
				return m, nil
			case tea.MouseButtonWheelDown:
				m.scrollBenchmarkRows(-3)
				return m, nil
			}
		}
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
		if m.monitorState == monitorRunning {
			now := time.Time(message)
			if !m.monitorNextSample.IsZero() && !now.Before(m.monitorNextSample) {
				delta := max(m.monitorLatest-m.monitorGraphStart, int64(0))
				m.monitorSamples = append(m.monitorSamples, monitorSample{at: now, intervalTokens: delta})
				m.monitorGraphStart = m.monitorLatest
				for !m.monitorNextSample.After(now) {
					m.monitorNextSample = m.monitorNextSample.Add(monitorSampleInterval)
				}
			}
			if !m.monitorFetchActive {
				m.monitorRequest++
				m.monitorFetchActive = true
				commands = append(commands, m.monitorFetch(monitorFetchSample, m.monitorRequest))
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
		return m.applyMonitorFetch(message)
	case footerButtonFlashExpiredMsg:
		if message.sequence == m.flashSequence && message.button == m.flashedButton {
			m.flashedButton = footerButtonNone
		}
	case styleTabFlashExpiredMsg:
		if message.sequence == m.styleSequence && message.style == m.flashedStyle {
			m.styleFlashing = false
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

func (m Model) pressStyleTab(style meterStyleID) (tea.Model, tea.Cmd) {
	if style < styleBars || style >= styleCount {
		return m, nil
	}
	m.meterStyle = style
	m.styleSequence++
	m.flashedStyle = style
	m.styleFlashing = true
	sequence := m.styleSequence
	commands := []tea.Cmd{tea.Tick(footerButtonFlashDuration, func(time.Time) tea.Msg {
		return styleTabFlashExpiredMsg{style: style, sequence: sequence}
	})}
	if style == styleBenchmark && m.benchmarkRunner != nil && m.benchmarkCombinations == 0 && !m.benchmarkPlanning {
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

func (m Model) activateFooterButton(button footerButtonID) (Model, tea.Cmd) {
	switch button {
	case footerButtonTheme:
		m.theme = m.theme.next()
	case footerButtonRefresh:
		if !m.loading {
			m.loading = true
			return m, m.fetch()
		}
	case footerButtonMonitorGo:
		if m.meterStyle == styleMonitor && (m.monitorState == monitorIdle || m.monitorState == monitorStopped) {
			m.monitorState = monitorStarting
			m.monitorStartedAt = time.Time{}
			m.monitorStoppedAt = time.Time{}
			m.monitorSamples = nil
			m.monitorLastActivity = time.Time{}
			m.monitorSessions = 0
			m.monitorError = ""
			m.monitorRequest++
			m.monitorFetchActive = true
			return m, m.monitorFetch(monitorFetchStart, m.monitorRequest)
		}
	case footerButtonMonitorStop:
		if m.meterStyle == styleMonitor && m.monitorState == monitorRunning {
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
		if m.meterStyle == styleBenchmark && m.benchmarkState != benchmarkRunning {
			m.benchmarkAllArmed = false
			tasks := codex.BenchmarkTasks()
			if len(tasks) > 0 {
				return m.startBenchmark([]codex.BenchmarkTaskID{tasks[m.benchmarkSelectedTask%len(tasks)].ID})
			}
		}
	case footerButtonBenchmarkAll:
		tasks := codex.BenchmarkTasks()
		if m.meterStyle == styleBenchmark && benchmarkRunAllAvailable(m.benchmarkState == benchmarkRunning, m.benchmarkCombinations, len(tasks)) {
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
	if m.meterStyle == styleMonitor {
		if button := m.monitorButtonAt(x, y); button != footerButtonNone {
			return button
		}
	}
	if m.meterStyle == styleBenchmark {
		if button := m.benchmarkButtonAt(x, y); button != footerButtonNone {
			return button
		}
	}
	layout := m.dashboardLayout()
	if y != layout.footerY+1 {
		return footerButtonNone
	}
	localX := x - 2
	buttons, separator := footerButtonLayoutWithTheme(layout.contentWidth, paletteFor(m.theme).name)
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
	meterHeight  int
	meterY       int
	footerY      int
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
	const statusHeight = 1
	const tabsHeight = 1
	const framedErrorHeight = 3
	const footerHeight = 2
	extraHeight := 0
	if m.err != nil {
		extraHeight += framedErrorHeight
	}
	if len(m.snapshot.Meters()) == 0 {
		extraHeight += framedErrorHeight
	}
	tabsY := 1 + headerHeight + statusHeight
	meterY := tabsY + tabsHeight + extraHeight
	meterHeight := max(contentHeight-headerHeight-statusHeight-tabsHeight-extraHeight-footerHeight, 1)
	footerY := meterY
	if m.meterStyle == styleMonitor || m.meterStyle == styleBenchmark || len(m.snapshot.Meters()) > 0 {
		footerY += meterHeight
	}
	return dashboardGeometry{
		contentWidth: contentWidth,
		tabsY:        tabsY,
		meterHeight:  meterHeight,
		meterY:       meterY,
		footerY:      footerY,
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
	tasks := codex.BenchmarkTasks()
	if len(tasks) == 0 {
		return
	}
	m.benchmarkSelectedTask = (m.benchmarkSelectedTask + direction + len(tasks)) % len(tasks)
	m.benchmarkAllArmed = false
}

func (m *Model) setBenchmarkFilter(filter benchmarkResultFilter) {
	m.benchmarkFilter = filter
	m.benchmarkScroll = 0
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

func footerButtonLayoutWithTheme(width int, themeName string) ([]footerButton, string) {
	themeWidth := len("THEME // ") + len(themeName)
	available := width - themeWidth - 1
	if available <= 0 {
		return nil, " "
	}
	buttons, separator := footerButtonLayout(available)
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
		snapshot, err := m.fetcher.Fetch(context.Background())
		return fetchedMsg{snapshot: snapshot, err: err}
	}
}

const monitorSampleInterval = 30 * time.Second

func (m Model) monitorFetch(kind monitorFetchKind, sequence uint64) tea.Cmd {
	return func() tea.Msg {
		if m.usageFetcher == nil {
			return monitorFetchedMsg{
				kind: kind, sequence: sequence, err: errors.New("local Codex session telemetry unavailable"), at: time.Now(),
			}
		}
		var usage codex.LiveUsageSnapshot
		var err error
		if kind == monitorFetchStop {
			if freshFetcher, ok := m.usageFetcher.(FreshTokenUsageFetcher); ok {
				usage, err = freshFetcher.FetchTokenUsageFresh(context.Background())
			} else {
				usage, err = m.usageFetcher.FetchTokenUsage(context.Background())
			}
		} else {
			usage, err = m.usageFetcher.FetchTokenUsage(context.Background())
		}
		return monitorFetchedMsg{
			kind: kind, sequence: sequence, usage: usage, err: err, at: time.Now(),
		}
	}
}

func (m Model) applyMonitorFetch(message monitorFetchedMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.monitorError = message.err.Error()
		if message.kind == monitorFetchStop {
			m.monitorState = monitorStopped
			m.monitorStoppedAt = message.at
		} else if message.kind == monitorFetchStart {
			m.monitorState = monitorStopped
		}
		return m, nil
	}
	switch message.kind {
	case monitorFetchStart:
		m.monitorBaseline = message.usage.TotalTokens
		m.monitorLatest = message.usage.TotalTokens
		m.monitorGraphStart = message.usage.TotalTokens
		m.monitorStartedAt = message.at
		m.monitorState = monitorRunning
		m.monitorSessions = message.usage.SessionCount
		m.monitorError = ""
		m.monitorNextSample = message.at.Add(monitorSampleInterval)
	case monitorFetchSample, monitorFetchStop:
		if message.usage.TotalTokens < m.monitorLatest {
			m.monitorError = "local Codex token counter moved backwards"
		} else {
			if message.usage.TotalTokens > m.monitorLatest {
				m.monitorLastActivity = message.usage.LastActivity
			}
			m.monitorLatest = message.usage.TotalTokens
			m.monitorSessions = message.usage.SessionCount
			m.monitorError = ""
		}
		if message.kind == monitorFetchStop {
			m.monitorState = monitorStopped
			m.monitorStoppedAt = message.at
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

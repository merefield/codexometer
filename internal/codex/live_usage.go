package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	activeRolloutHorizon   = 24 * time.Hour
	discoveryEvery         = 5 * time.Second
	fullDiscoveryEvery     = 5 * time.Minute
	telemetryHistoryMax    = 4_096
	fallbackAttentionAfter = 3 * time.Minute
)

// LiveUsageSnapshot is a process-local accounting view built from token-count
// events appended to locally persisted Codex rollouts. Finalized API-equivalent
// totals increase monotonically; APIEqPendingCalls is an instantaneous count
// kept separate until daemon model resolution or requested-model fallback.
type LiveUsageSnapshot struct {
	TotalTokens        int64
	LastActivity       time.Time
	SessionCount       int
	Sessions           []LiveUsageSession
	CodexStatusKnown   bool
	CodexUp            bool
	CodexWorking       bool
	APIEqUSD           float64
	APIEqPricedCalls   int64
	APIEqUnpricedCalls int64
	APIEqPendingCalls  int64
}

// LiveUsageSession is one independently started local Codex session. Token
// usage from explicitly linked spawned descendants is folded into the root.
type LiveUsageSession struct {
	ID               string
	WorkingDirectory string
	StartedAt        time.Time
	TotalTokens      int64
	LastActivity     time.Time
	AgentCount       int
	Active           bool
	Attention        SessionAttention
	Unattributed     bool
	ModelCalls       []LiveModelCall
	TurnTimings      []LiveTurnTiming
}

// SessionAttention describes why a local Codex session may need the user.
// Input and approval are definite signals; Check is deliberately cautious and
// means only that an open fallback-observed session has remained quiet.
type SessionAttention int

const (
	SessionAttentionNone SessionAttention = iota
	SessionAttentionInput
	SessionAttentionApproval
	SessionAttentionCheck
)

// LiveModelCall is the small, content-free usage pulse persisted after one
// upstream model response.
type LiveModelCall struct {
	Sequence        uint64
	At              time.Time
	OutputTokens    int64
	OutputAvailable bool
	Model           string
	APIEqUSD        float64
	APIEqKnown      bool
	apiEqFinalized  bool
}

// LiveTurnTiming contains the persisted latency measurement for one completed
// Codex turn. Available is false for older rollouts that omit TTFT.
type LiveTurnTiming struct {
	Sequence         uint64
	At               time.Time
	TimeToFirstToken time.Duration
	Available        bool
}

// LiveUsageReader incrementally observes token telemetry written by local Codex
// sessions. It never interprets message, reasoning, command, or tool contents.
type LiveUsageReader struct {
	SessionsRoot    string
	WriterLocksRoot string
	statusProvider  sessionStatusProvider

	mu                        sync.Mutex
	initialized               bool
	startedAt                 time.Time
	lastDiscovery             time.Time
	lastFullDiscovery         time.Time
	files                     map[string]*rolloutCursor
	totalTokens               int64
	apiEqUSD                  float64
	apiEqPricedCalls          int64
	apiEqUnknownCalls         int64
	lastActivity              time.Time
	nextEventSequence         uint64
	daemonObservationSequence uint64
	resolvedObservations      []resolvedModelObservation
	pendingModelResolutions   []pendingModelResolution
	daemonSubscribedThreads   map[string]struct{}
}

type rolloutCursor struct {
	offset                      int64
	totalTokens                 int64
	observedTokens              int64
	lastModified                time.Time
	threadID                    string
	parentThreadID              string
	workingDirectory            string
	startedAt                   time.Time
	currentModel                string
	currentTurnID               string
	nonRoot                     bool
	subagentHistoryStartOrdinal *uint64
	modelCalls                  []LiveModelCall
	turnTimings                 []LiveTurnTiming
	attention                   sessionAttentionState
}

type pendingModelResolution struct {
	callSequence    uint64
	threadID        string
	turnID          string
	usage           BenchmarkUsage
	cumulativeTotal int64
}

type sessionAttentionState int

const (
	sessionAttentionUnknown sessionAttentionState = iota
	sessionAttentionWorking
	sessionAttentionIdle
	sessionAttentionInput
	sessionAttentionApproval
)

type rolloutEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Ordinal   *uint64   `json:"ordinal"`
	Type      string    `json:"type"`
	Payload   struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage rolloutTokenUsage `json:"total_token_usage"`
			LastTokenUsage  rolloutTokenUsage `json:"last_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

type rolloutTokenUsage struct {
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	CacheWriteInputTokens int64  `json:"cache_write_input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
}

func (u rolloutTokenUsage) benchmarkUsage() BenchmarkUsage {
	output := int64(0)
	if u.OutputTokens != nil {
		output = *u.OutputTokens
	}
	return BenchmarkUsage{
		InputTokens: u.InputTokens, CachedInputTokens: u.CachedInputTokens,
		CacheWriteInputTokens: u.CacheWriteInputTokens, OutputTokens: output,
		ReasoningOutputTokens: u.ReasoningOutputTokens, TotalTokens: u.TotalTokens,
	}
}

type rolloutModelEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Ordinal   *uint64   `json:"ordinal"`
	Type      string    `json:"type"`
	Payload   struct {
		Model string `json:"model"`
	} `json:"payload"`
}

type rolloutTurnIdentityEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Ordinal   *uint64   `json:"ordinal"`
	Type      string    `json:"type"`
	Payload   struct {
		Type   string `json:"type"`
		TurnID string `json:"turn_id"`
	} `json:"payload"`
}

type rolloutTurnEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Ordinal   *uint64   `json:"ordinal"`
	Type      string    `json:"type"`
	Payload   struct {
		Type               string `json:"type"`
		CompletedAt        *int64 `json:"completed_at"`
		TimeToFirstTokenMS *int64 `json:"time_to_first_token_ms"`
	} `json:"payload"`
}

type rolloutAttentionEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Ordinal   *uint64   `json:"ordinal"`
	Type      string    `json:"type"`
	Payload   struct {
		Type       string `json:"type"`
		IsBlocking *bool  `json:"isBlocking"`
	} `json:"payload"`
}

type rolloutMetadata struct {
	ID                          string
	ParentThreadID              string
	WorkingDirectory            string
	StartedAt                   time.Time
	NonRoot                     bool
	SubagentHistoryStartOrdinal *uint64
}

type rolloutMetadataEvent struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type rolloutMetadataPayload struct {
	ID                          string          `json:"id"`
	Timestamp                   time.Time       `json:"timestamp"`
	ParentThreadID              string          `json:"parent_thread_id"`
	WorkingDirectory            string          `json:"cwd"`
	Source                      json.RawMessage `json:"source"`
	ThreadSource                string          `json:"thread_source"`
	SubagentHistoryStartOrdinal *uint64         `json:"subagent_history_start_ordinal"`
}

// DefaultCodexHome follows the same CODEX_HOME convention as the Codex CLI.
func DefaultCodexHome() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("CODEX_HOME")); configured != "" {
		return filepath.Clean(configured), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

// NewLiveUsageReader creates a reader for a Codex home. An empty path resolves
// CODEX_HOME and then falls back to the current user's .codex directory.
func NewLiveUsageReader(codexHome string) (*LiveUsageReader, error) {
	if strings.TrimSpace(codexHome) == "" {
		var err error
		codexHome, err = DefaultCodexHome()
		if err != nil {
			return nil, err
		}
	}
	return &LiveUsageReader{
		SessionsRoot:    filepath.Join(codexHome, "sessions"),
		WriterLocksRoot: filepath.Join(codexHome, "thread-writer-locks"),
		statusProvider:  newSessionStatusProvider(codexHome),
	}, nil
}

// FetchTokenUsage discovers active rollout files and consumes only complete,
// newly appended JSONL records. The first call establishes baselines and never
// reports historical tokens as new activity.
func (r *LiveUsageReader) FetchTokenUsage(ctx context.Context) (LiveUsageSnapshot, error) {
	return r.fetchTokenUsage(ctx, false)
}

// FetchTokenUsageFresh forces a complete session discovery before consuming
// telemetry. The Monitor uses this for its final Pause reading so a recently
// resumed rollout in an older date directory cannot be missed.
func (r *LiveUsageReader) FetchTokenUsageFresh(ctx context.Context) (LiveUsageSnapshot, error) {
	return r.fetchTokenUsage(ctx, true)
}

func (r *LiveUsageReader) fetchTokenUsage(ctx context.Context, forceFullDiscovery bool) (LiveUsageSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := contextErr(ctx); err != nil {
		return LiveUsageSnapshot{}, err
	}

	now := time.Now()
	if !r.initialized {
		if err := r.initialize(ctx, now); err != nil {
			return LiveUsageSnapshot{}, err
		}
	} else if forceFullDiscovery || now.Sub(r.lastDiscovery) >= discoveryEvery {
		full := forceFullDiscovery || now.Sub(r.lastFullDiscovery) >= fullDiscoveryEvery
		if err := r.discover(ctx, now, full); err != nil {
			return LiveUsageSnapshot{}, err
		}
	}

	exactStatuses := map[string]sessionRuntimeStatus(nil)
	appServerUp := false
	r.daemonSubscribedThreads = nil
	if r.statusProvider != nil {
		if daemonSnapshot, exact := r.statusProvider.Fetch(ctx, r.observedThreadIDs(now)); exact {
			appServerUp = true
			exactStatuses = daemonSnapshot.Statuses
			r.ingestResolvedModelObservations(daemonSnapshot.ModelObservations)
			r.daemonSubscribedThreads = daemonSnapshot.SubscribedThreads
		}
	}
	r.reconcilePendingModelResolutions()

	var firstReadErr error
	readableFiles := 0
	for path, cursor := range r.files {
		if err := contextErr(ctx); err != nil {
			return LiveUsageSnapshot{}, err
		}
		if now.Sub(cursor.lastModified) > activeRolloutHorizon {
			continue
		}
		if err := r.consume(path, cursor); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				delete(r.files, path)
				continue
			}
			if firstReadErr == nil {
				firstReadErr = err
			}
			continue
		}
		readableFiles++
	}
	if firstReadErr != nil && readableFiles == 0 {
		return LiveUsageSnapshot{}, fmt.Errorf("read local Codex token telemetry: %w", firstReadErr)
	}

	liveWriters, writerLocksSupported := r.liveWriterThreads()
	sessions, activeSessions, sessionWorking := r.sessionSnapshots(now, liveWriters, writerLocksSupported, exactStatuses)
	codexStatusKnown, codexUp, codexWorking := codexRuntimeHealth(
		appServerUp, len(liveWriters) > 0, sessionWorking, writerLocksSupported,
	)
	return LiveUsageSnapshot{
		TotalTokens: r.totalTokens, LastActivity: r.lastActivity,
		SessionCount: activeSessions, Sessions: sessions,
		CodexStatusKnown: codexStatusKnown,
		CodexUp:          codexUp,
		CodexWorking:     codexWorking,
		APIEqUSD:         r.apiEqUSD, APIEqPricedCalls: r.apiEqPricedCalls,
		APIEqUnpricedCalls: r.apiEqUnknownCalls,
		APIEqPendingCalls:  int64(len(r.pendingModelResolutions)),
	}, nil
}

// codexRuntimeHealth combines the optional shared app-server heartbeat with
// locally held writer locks and reconciled session state. A held lock
// establishes that Codex is healthy without implying that its session is
// working. An unavailable optional socket alone is not evidence that Codex is
// down.
func codexRuntimeHealth(appServerUp, liveWriter, sessionWorking, writerLocksSupported bool) (known, up, working bool) {
	working = sessionWorking
	up = appServerUp || liveWriter || working
	known = up || writerLocksSupported
	return known, up, working
}

func (r *LiveUsageReader) observedThreadIDs(now time.Time) []string {
	seen := make(map[string]struct{}, len(r.files))
	threadIDs := make([]string, 0, len(r.files))
	for _, cursor := range r.files {
		if cursor.threadID == "" || now.Sub(cursor.lastModified) > activeRolloutHorizon {
			continue
		}
		if _, ok := seen[cursor.threadID]; ok {
			continue
		}
		seen[cursor.threadID] = struct{}{}
		threadIDs = append(threadIDs, cursor.threadID)
	}
	return threadIDs
}

func (r *LiveUsageReader) initialize(ctx context.Context, now time.Time) error {
	r.startedAt = now
	r.files = make(map[string]*rolloutCursor)
	if err := r.discover(ctx, now, true); err != nil {
		return err
	}
	r.initialized = true
	return nil
}

func (r *LiveUsageReader) discover(ctx context.Context, now time.Time, full bool) error {
	roots := []string{r.SessionsRoot}
	if !full {
		roots = roots[:0]
		for _, day := range []time.Time{now, now.Add(-24 * time.Hour)} {
			root := filepath.Join(r.SessionsRoot, day.Format("2006"), day.Format("01"), day.Format("02"))
			roots = append(roots, root)
		}
	}
	var firstFileErr error
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if errors.Is(walkErr, os.ErrNotExist) {
					return nil
				}
				if path == root {
					return walkErr
				}
				if firstFileErr == nil {
					firstFileErr = walkErr
				}
				return nil
			}
			if err := contextErr(ctx); err != nil {
				return err
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !isRolloutFile(entry.Name()) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				if firstFileErr == nil {
					firstFileErr = err
				}
				return nil
			}
			if cursor, exists := r.files[path]; exists {
				cursor.lastModified = info.ModTime()
				return nil
			}
			if err := r.addFile(path, info); err != nil {
				if firstFileErr == nil {
					firstFileErr = err
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("discover local Codex sessions: %w", err)
		}
	}
	if firstFileErr != nil && len(r.files) == 0 {
		return fmt.Errorf("read local Codex session telemetry: %w", firstFileErr)
	}
	r.lastDiscovery = now
	if full {
		r.lastFullDiscovery = now
	}
	return nil
}

func (r *LiveUsageReader) addFile(path string, info os.FileInfo) error {
	metadata, err := readRolloutMetadata(path)
	if err != nil {
		return err
	}
	cursor := &rolloutCursor{
		lastModified:                info.ModTime(),
		threadID:                    metadata.ID,
		parentThreadID:              metadata.ParentThreadID,
		workingDirectory:            metadata.WorkingDirectory,
		startedAt:                   metadata.StartedAt,
		nonRoot:                     metadata.NonRoot,
		subagentHistoryStartOrdinal: metadata.SubagentHistoryStartOrdinal,
	}
	if time.Since(info.ModTime()) <= activeRolloutHorizon {
		attention, err := latestSessionAttention(path, cursor)
		if err != nil {
			return err
		}
		cursor.attention = attention
	}
	if !r.initialized || !rolloutCreatedAfter(path, r.startedAt) {
		model, modelErr := latestRolloutModel(path, cursor)
		if modelErr != nil {
			return modelErr
		}
		cursor.currentModel = model
		turnID, turnErr := latestRolloutTurnID(path, cursor)
		if turnErr != nil {
			return turnErr
		}
		cursor.currentTurnID = turnID
		total, err := latestTokenTotal(path)
		if err != nil {
			return err
		}
		cursor.totalTokens = total
		cursor.offset = info.Size()
	}
	r.files[path] = cursor
	return nil
}

func (r *LiveUsageReader) consume(path string, cursor *rolloutCursor) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	cursor.lastModified = info.ModTime()
	if info.Size() < cursor.offset {
		total, latestErr := latestTokenTotal(path)
		if latestErr != nil {
			return latestErr
		}
		cursor.offset = info.Size()
		cursor.totalTokens = total
		turnID, turnErr := latestRolloutTurnID(path, cursor)
		if turnErr != nil {
			return turnErr
		}
		cursor.currentTurnID = turnID
		attention, attentionErr := latestSessionAttention(path, cursor)
		if attentionErr != nil {
			return attentionErr
		}
		cursor.attention = attention
		return nil
	}
	if info.Size() == cursor.offset {
		return nil
	}
	if _, err := file.Seek(cursor.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			cursor.offset += int64(len(line))
			if turnID, ordinal, at, ok := rolloutTurnIDRecord(line); ok &&
				tokenRecordIsOwned(ordinal, cursor.subagentHistoryStartOrdinal, at, cursor.nonRoot, cursor.startedAt) {
				cursor.currentTurnID = turnID
			}
			if model, ordinal, at, ok := rolloutModelRecord(line); ok &&
				tokenRecordIsOwned(ordinal, cursor.subagentHistoryStartOrdinal, at, cursor.nonRoot, cursor.startedAt) {
				cursor.currentModel = model
			}
			if attention, ordinal, at, ok := sessionAttentionRecord(line); ok &&
				tokenRecordIsOwned(ordinal, cursor.subagentHistoryStartOrdinal, at, cursor.nonRoot, cursor.startedAt) {
				cursor.attention = attention
			}
			if record, ok := tokenUsageRecord(line); ok {
				if !tokenRecordIsOwned(record.ordinal, cursor.subagentHistoryStartOrdinal, record.at, cursor.nonRoot, cursor.startedAt) {
					// Legacy child rollouts copy the parent's cumulative token
					// events. Preserve the latest inherited value as the child's
					// counter baseline without reporting it as new usage.
					cursor.totalTokens = record.total
					continue
				}
				if record.total >= cursor.totalTokens {
					delta := record.total - cursor.totalTokens
					r.totalTokens += delta
					cursor.observedTokens += delta
					if delta > 0 {
						if record.at.IsZero() {
							record.at = time.Now()
						}
						if record.at.After(r.lastActivity) {
							r.lastActivity = record.at
						}
						r.nextEventSequence++
						model := cursor.currentModel
						pricingUsage := record.usage
						usageMatchesDelta := record.usage.TotalTokens == delta
						_, daemonSubscribed := r.daemonSubscribedThreads[cursor.threadID]
						resolutionPending := daemonSubscribed && cursor.threadID != "" && cursor.currentTurnID != ""
						resolved := false
						if usageMatchesDelta {
							if observation, ok := r.takeResolvedModelObservation(cursor.threadID, cursor.currentTurnID, record.usage, record.total); ok {
								model, pricingUsage, resolved = observation.Model, observation.Usage, true
							}
						}
						resolutionDeferred := usageMatchesDelta && resolutionPending && !resolved
						call := LiveModelCall{
							Sequence: r.nextEventSequence, At: record.at, OutputTokens: record.outputTokens,
							OutputAvailable: record.outputKnown, Model: model,
						}
						if usageMatchesDelta && (!resolutionPending || resolved) {
							if cost, known, _ := EstimateStandardAPIEqCost(model, pricingUsage); known {
								call.APIEqUSD, call.APIEqKnown, call.apiEqFinalized = cost, true, true
								r.apiEqUSD += cost
								r.apiEqPricedCalls++
							} else {
								call.apiEqFinalized = true
								r.apiEqUnknownCalls++
							}
						} else if !resolutionDeferred {
							call.apiEqFinalized = true
							r.apiEqUnknownCalls++
						}
						cursor.modelCalls = appendBounded(cursor.modelCalls, call, telemetryHistoryMax)
						if resolutionDeferred {
							r.pendingModelResolutions = append(r.pendingModelResolutions, pendingModelResolution{
								callSequence: call.Sequence, threadID: cursor.threadID,
								turnID: cursor.currentTurnID, usage: record.usage, cumulativeTotal: record.total,
							})
						}
					}
				}
				cursor.totalTokens = record.total
			}
			if timing, available, ordinal, at, ok := turnTimingRecord(line); ok &&
				tokenRecordIsOwned(ordinal, cursor.subagentHistoryStartOrdinal, at, cursor.nonRoot, cursor.startedAt) {
				r.nextEventSequence++
				cursor.turnTimings = appendBounded(cursor.turnTimings, LiveTurnTiming{
					Sequence: r.nextEventSequence, At: at, TimeToFirstToken: timing, Available: available,
				}, telemetryHistoryMax)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func rolloutTurnIDRecord(line []byte) (string, *uint64, time.Time, bool) {
	if !bytes.Contains(line, []byte(`"turn_id"`)) {
		return "", nil, time.Time{}, false
	}
	var event rolloutTurnIdentityEvent
	if json.Unmarshal(line, &event) != nil {
		return "", nil, time.Time{}, false
	}
	if (event.Type != "event_msg" || (event.Payload.Type != "task_started" && event.Payload.Type != "turn_started")) &&
		event.Type != "turn_context" {
		return "", nil, time.Time{}, false
	}
	turnID := strings.TrimSpace(event.Payload.TurnID)
	return turnID, event.Ordinal, event.Timestamp, turnID != ""
}

func (r *LiveUsageReader) ingestResolvedModelObservations(observations []resolvedModelObservation) {
	for _, observation := range observations {
		if observation.Sequence <= r.daemonObservationSequence {
			continue
		}
		r.daemonObservationSequence = observation.Sequence
		r.resolvedObservations = appendBounded(r.resolvedObservations, observation, telemetryHistoryMax)
	}
}

func (r *LiveUsageReader) reconcilePendingModelResolutions() {
	for _, pending := range r.pendingModelResolutions {
		call := r.modelCall(pending.callSequence)
		if call == nil {
			// The call aged out of the bounded per-session history before it
			// could be finalized. Preserve fail-closed monotonic accounting.
			r.apiEqUnknownCalls++
			continue
		}
		if call.apiEqFinalized {
			continue
		}
		observation, ok := r.takeResolvedModelObservation(pending.threadID, pending.turnID, pending.usage, pending.cumulativeTotal)
		pricingUsage := pending.usage
		if ok {
			call.Model = observation.Model
			pricingUsage = observation.Usage
		}
		call.APIEqUSD, call.APIEqKnown, _ = EstimateStandardAPIEqCost(call.Model, pricingUsage)
		call.apiEqFinalized = true
		if call.APIEqKnown {
			r.apiEqUSD += call.APIEqUSD
			r.apiEqPricedCalls++
		} else {
			r.apiEqUnknownCalls++
		}
	}
	r.pendingModelResolutions = nil
}

func (r *LiveUsageReader) takeResolvedModelObservation(threadID, turnID string, usage BenchmarkUsage, cumulativeTotal int64) (resolvedModelObservation, bool) {
	for index, observation := range r.resolvedObservations {
		if observation.ThreadID != threadID || observation.TurnID != turnID ||
			observation.CumulativeTotal != cumulativeTotal || !sameBenchmarkUsage(observation.Usage, usage) {
			continue
		}
		r.resolvedObservations = append(r.resolvedObservations[:index], r.resolvedObservations[index+1:]...)
		return observation, true
	}
	return resolvedModelObservation{}, false
}

func (r *LiveUsageReader) modelCall(sequence uint64) *LiveModelCall {
	for _, cursor := range r.files {
		for index := range cursor.modelCalls {
			if cursor.modelCalls[index].Sequence == sequence {
				return &cursor.modelCalls[index]
			}
		}
	}
	return nil
}

func sameBenchmarkUsage(left, right BenchmarkUsage) bool {
	return left.TotalTokens == right.TotalTokens &&
		left.InputTokens == right.InputTokens &&
		left.CachedInputTokens == right.CachedInputTokens &&
		left.CacheWriteInputTokens == right.CacheWriteInputTokens &&
		left.OutputTokens == right.OutputTokens &&
		left.ReasoningOutputTokens == right.ReasoningOutputTokens
}

func latestSessionAttention(path string, cursor *rolloutCursor) (sessionAttentionState, error) {
	file, err := os.Open(path)
	if err != nil {
		return sessionAttentionUnknown, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return sessionAttentionUnknown, err
	}
	const chunkSize = int64(64 * 1024)
	position := info.Size()
	var carry []byte
	for position > 0 {
		readSize := min(chunkSize, position)
		position -= readSize
		chunk := make([]byte, int(readSize))
		if _, err := file.ReadAt(chunk, position); err != nil && !errors.Is(err, io.EOF) {
			return sessionAttentionUnknown, err
		}
		data := make([]byte, 0, len(chunk)+len(carry))
		data = append(data, chunk...)
		data = append(data, carry...)
		lines := bytes.Split(data, []byte{'\n'})
		firstComplete := 1
		if position == 0 {
			firstComplete = 0
		}
		for index := len(lines) - 1; index >= firstComplete; index-- {
			attention, ordinal, at, ok := sessionAttentionRecord(lines[index])
			if ok && tokenRecordIsOwned(ordinal, cursor.subagentHistoryStartOrdinal, at, cursor.nonRoot, cursor.startedAt) {
				return attention, nil
			}
		}
		carry = append(carry[:0], lines[0]...)
	}
	return sessionAttentionUnknown, nil
}

func sessionAttentionRecord(line []byte) (sessionAttentionState, *uint64, time.Time, bool) {
	if !bytes.Contains(line, []byte(`"event_msg"`)) && !bytes.Contains(line, []byte(`"response_item"`)) {
		return sessionAttentionUnknown, nil, time.Time{}, false
	}
	var event rolloutAttentionEvent
	if json.Unmarshal(line, &event) != nil {
		return sessionAttentionUnknown, nil, time.Time{}, false
	}
	if event.Type == "response_item" && event.Payload.Type == "function_call_output" {
		return sessionAttentionWorking, event.Ordinal, event.Timestamp, true
	}
	if event.Type != "event_msg" {
		return sessionAttentionUnknown, nil, time.Time{}, false
	}
	switch event.Payload.Type {
	case "request_user_input":
		if event.Payload.IsBlocking == nil || *event.Payload.IsBlocking {
			return sessionAttentionInput, event.Ordinal, event.Timestamp, true
		}
	case "exec_approval_request", "apply_patch_approval_request", "request_permissions", "elicitation_request":
		return sessionAttentionApproval, event.Ordinal, event.Timestamp, true
	case "task_complete", "turn_complete":
		return sessionAttentionIdle, event.Ordinal, event.Timestamp, true
	case "task_started", "turn_started", "user_message", "exec_command_begin", "dynamic_tool_call_response":
		return sessionAttentionWorking, event.Ordinal, event.Timestamp, true
	}
	return sessionAttentionUnknown, nil, time.Time{}, false
}

// appendBounded keeps insertion order while discarding the oldest values. The
// reslice reuses the existing backing array until its remaining capacity is
// exhausted, avoiding a full allocation and copy for every value after limit.
func appendBounded[T any](history []T, value T, limit int) []T {
	history = append(history, value)
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}
	return history
}

func latestTokenTotal(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	const chunkSize = int64(64 * 1024)
	position := info.Size()
	var carry []byte
	for position > 0 {
		readSize := min(chunkSize, position)
		position -= readSize
		chunk := make([]byte, int(readSize))
		if _, err := file.ReadAt(chunk, position); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		data := make([]byte, 0, len(chunk)+len(carry))
		data = append(data, chunk...)
		data = append(data, carry...)
		lines := bytes.Split(data, []byte{'\n'})
		firstComplete := 1
		if position == 0 {
			firstComplete = 0
		}
		for index := len(lines) - 1; index >= firstComplete; index-- {
			if total, _, _, ok := tokenTotalRecord(lines[index]); ok {
				return total, nil
			}
		}
		carry = append(carry[:0], lines[0]...)
	}
	return 0, nil
}

func tokenTotal(line []byte) (int64, time.Time, bool) {
	total, at, _, ok := tokenTotalRecord(line)
	return total, at, ok
}

func tokenTotalRecord(line []byte) (int64, time.Time, *uint64, bool) {
	record, ok := tokenUsageRecord(line)
	return record.total, record.at, record.ordinal, ok
}

type rolloutTokenRecord struct {
	total        int64
	outputTokens int64
	outputKnown  bool
	usage        BenchmarkUsage
	at           time.Time
	ordinal      *uint64
}

func tokenUsageRecord(line []byte) (rolloutTokenRecord, bool) {
	if !bytes.Contains(line, []byte("token_count")) {
		return rolloutTokenRecord{}, false
	}
	var event rolloutEvent
	if json.Unmarshal(line, &event) != nil || event.Type != "event_msg" ||
		event.Payload.Type != "token_count" || event.Payload.Info == nil {
		return rolloutTokenRecord{}, false
	}
	lastUsage := event.Payload.Info.LastTokenUsage.benchmarkUsage()
	outputKnown := event.Payload.Info.LastTokenUsage.OutputTokens != nil
	return rolloutTokenRecord{
		total:        event.Payload.Info.TotalTokenUsage.TotalTokens,
		outputTokens: lastUsage.OutputTokens, outputKnown: outputKnown,
		usage: lastUsage,
		at:    event.Timestamp, ordinal: event.Ordinal,
	}, true
}

func rolloutModelRecord(line []byte) (string, *uint64, time.Time, bool) {
	if !bytes.Contains(line, []byte(`"turn_context"`)) {
		return "", nil, time.Time{}, false
	}
	var event rolloutModelEvent
	if json.Unmarshal(line, &event) != nil {
		return "", nil, time.Time{}, false
	}
	if event.Type == "turn_context" && strings.TrimSpace(event.Payload.Model) != "" {
		return strings.TrimSpace(event.Payload.Model), event.Ordinal, event.Timestamp, true
	}
	return "", nil, time.Time{}, false
}

func latestRolloutModel(path string, cursor *rolloutCursor) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	const chunkSize = int64(64 * 1024)
	position := info.Size()
	var carry []byte
	for position > 0 {
		readSize := min(chunkSize, position)
		position -= readSize
		chunk := make([]byte, int(readSize))
		if _, err := file.ReadAt(chunk, position); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		data := make([]byte, 0, len(chunk)+len(carry))
		data = append(data, chunk...)
		data = append(data, carry...)
		lines := bytes.Split(data, []byte{'\n'})
		firstComplete := 1
		if position == 0 {
			firstComplete = 0
		}
		for index := len(lines) - 1; index >= firstComplete; index-- {
			if model, ordinal, at, ok := rolloutModelRecord(lines[index]); ok &&
				tokenRecordIsOwned(ordinal, cursor.subagentHistoryStartOrdinal, at, cursor.nonRoot, cursor.startedAt) {
				return model, nil
			}
		}
		carry = append(carry[:0], lines[0]...)
	}
	return "", nil
}

func latestRolloutTurnID(path string, cursor *rolloutCursor) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	const chunkSize = int64(64 * 1024)
	position := info.Size()
	var carry []byte
	for position > 0 {
		readSize := min(chunkSize, position)
		position -= readSize
		chunk := make([]byte, int(readSize))
		if _, err := file.ReadAt(chunk, position); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		data := make([]byte, 0, len(chunk)+len(carry))
		data = append(data, chunk...)
		data = append(data, carry...)
		lines := bytes.Split(data, []byte{'\n'})
		firstComplete := 1
		if position == 0 {
			firstComplete = 0
		}
		for index := len(lines) - 1; index >= firstComplete; index-- {
			if turnID, ordinal, at, ok := rolloutTurnIDRecord(lines[index]); ok &&
				tokenRecordIsOwned(ordinal, cursor.subagentHistoryStartOrdinal, at, cursor.nonRoot, cursor.startedAt) {
				return turnID, nil
			}
		}
		carry = append(carry[:0], lines[0]...)
	}
	return "", nil
}

func turnTimingRecord(line []byte) (time.Duration, bool, *uint64, time.Time, bool) {
	if !bytes.Contains(line, []byte("task_complete")) && !bytes.Contains(line, []byte("turn_complete")) {
		return 0, false, nil, time.Time{}, false
	}
	var event rolloutTurnEvent
	if json.Unmarshal(line, &event) != nil || event.Type != "event_msg" ||
		(event.Payload.Type != "task_complete" && event.Payload.Type != "turn_complete") {
		return 0, false, nil, time.Time{}, false
	}
	at := event.Timestamp
	if at.IsZero() && event.Payload.CompletedAt != nil {
		at = time.Unix(*event.Payload.CompletedAt, 0)
	}
	if event.Payload.TimeToFirstTokenMS == nil || *event.Payload.TimeToFirstTokenMS < 0 {
		return 0, false, event.Ordinal, at, true
	}
	return time.Duration(*event.Payload.TimeToFirstTokenMS) * time.Millisecond, true, event.Ordinal, at, true
}

func tokenRecordIsOwned(ordinal, boundary *uint64, at time.Time, nonRoot bool, startedAt time.Time) bool {
	if boundary == nil {
		return !nonRoot || startedAt.IsZero() || at.IsZero() || !at.Before(startedAt)
	}
	return ordinal != nil && *ordinal >= *boundary
}

func readRolloutMetadata(path string) (rolloutMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return rolloutMetadata{}, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for record := 0; record < 64; record++ {
		line, readErr := reader.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"session_meta"`)) {
			var event rolloutMetadataEvent
			if json.Unmarshal(bytes.TrimSpace(line), &event) == nil && event.Type == "session_meta" {
				var payload rolloutMetadataPayload
				if json.Unmarshal(event.Payload, &payload) == nil {
					parent, nonRoot := sessionSourceParent(payload.Source)
					if payload.ParentThreadID != "" {
						parent = payload.ParentThreadID
						nonRoot = true
					}
					if payload.ThreadSource == "subagent" {
						nonRoot = true
					}
					return rolloutMetadata{
						ID: payload.ID, ParentThreadID: parent,
						WorkingDirectory: payload.WorkingDirectory, NonRoot: nonRoot,
						StartedAt:                   firstNonZeroTime(payload.Timestamp, event.Timestamp),
						SubagentHistoryStartOrdinal: payload.SubagentHistoryStartOrdinal,
					}, nil
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return rolloutMetadata{}, readErr
		}
	}
	return rolloutMetadata{}, nil
}

func sessionSourceParent(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var sourceName string
	if json.Unmarshal(raw, &sourceName) == nil {
		return "", strings.HasPrefix(sourceName, "subagent") || strings.HasPrefix(sourceName, "internal")
	}
	var source struct {
		Subagent json.RawMessage `json:"subagent"`
		Internal json.RawMessage `json:"internal"`
	}
	if json.Unmarshal(raw, &source) != nil {
		return "", false
	}
	if len(source.Subagent) > 0 {
		var subagent struct {
			ThreadSpawn *struct {
				ParentThreadID string `json:"parent_thread_id"`
			} `json:"thread_spawn"`
		}
		if json.Unmarshal(source.Subagent, &subagent) == nil && subagent.ThreadSpawn != nil {
			return subagent.ThreadSpawn.ParentThreadID, true
		}
		return "", true
	}
	return "", len(source.Internal) > 0
}

const unattributedSessionID = "unattributed"

func (r *LiveUsageReader) liveWriterThreads() (map[string]struct{}, bool) {
	if !fileLockSupported {
		return nil, false
	}
	entries, err := os.ReadDir(r.WriterLocksRoot)
	if err != nil {
		return nil, false
	}
	live := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == ".coordination.lock" {
			continue
		}
		threadID, ok := strings.CutSuffix(entry.Name(), ".lock")
		if !ok || threadID == "" {
			continue
		}
		held, err := fileLockHeld(filepath.Join(r.WriterLocksRoot, entry.Name()))
		if err == nil && held {
			live[threadID] = struct{}{}
		}
	}
	return live, true
}

func (r *LiveUsageReader) sessionSnapshots(now time.Time, liveWriters map[string]struct{}, writerLocksSupported bool, exactStatuses map[string]sessionRuntimeStatus) ([]LiveUsageSession, int, bool) {
	byID := make(map[string]*rolloutCursor, len(r.files))
	for _, cursor := range r.files {
		if cursor.threadID != "" {
			byID[cursor.threadID] = cursor
		}
	}

	groups := make(map[string]*LiveUsageSession)
	groupHasFreshActivity := make(map[string]bool)
	groupWorking := make(map[string]bool)
	for _, cursor := range r.files {
		quietFor := now.Sub(cursor.lastModified)
		active := quietFor <= 5*time.Minute
		_, writerActive := liveWriters[cursor.threadID]
		exactStatus, exact := exactStatuses[cursor.threadID]
		exactWorking := exact && exactStatus == sessionRuntimeWorking
		localWorking := !exact && writerActive && cursor.attention == sessionAttentionWorking
		attention := sessionAttention(cursor.attention, writerActive, writerLocksSupported, quietFor, exactStatus, exact)
		if !active && !exactWorking && attention == SessionAttentionNone && cursor.observedTokens == 0 && len(cursor.modelCalls) == 0 && len(cursor.turnTimings) == 0 {
			continue
		}
		rootID, unattributed := rolloutRoot(cursor, byID)
		group := groups[rootID]
		if group == nil {
			group = &LiveUsageSession{ID: rootID, Unattributed: unattributed}
			groups[rootID] = group
		}
		group.TotalTokens += cursor.observedTokens
		group.ModelCalls = append(group.ModelCalls, cursor.modelCalls...)
		group.TurnTimings = append(group.TurnTimings, cursor.turnTimings...)
		group.Active = group.Active || active || exactWorking || attention != SessionAttentionNone
		group.Attention = mergeSessionAttention(group.Attention, attention)
		groupWorking[rootID] = groupWorking[rootID] || exactWorking || localWorking
		// CHECK SESSION is only an inactivity inference. A freshly writing
		// member—or an authoritatively working daemon thread—means the grouped
		// root is demonstrably active, even if a linked child was left quiet
		// overnight. Definite input and approval signals still propagate.
		groupHasFreshActivity[rootID] = groupHasFreshActivity[rootID] || quietFor < fallbackAttentionAfter || exactWorking
		if cursor.lastModified.After(group.LastActivity) {
			group.LastActivity = cursor.lastModified
		}
		if group.WorkingDirectory == "" {
			group.WorkingDirectory = cursor.workingDirectory
		}
		if group.StartedAt.IsZero() || !cursor.startedAt.IsZero() && cursor.startedAt.Before(group.StartedAt) {
			group.StartedAt = cursor.startedAt
		}
		if cursor.threadID == rootID {
			group.WorkingDirectory = cursor.workingDirectory
			group.StartedAt = cursor.startedAt
		} else if active || attention != SessionAttentionNone || cursor.observedTokens > 0 || len(cursor.modelCalls) > 0 || len(cursor.turnTimings) > 0 {
			group.AgentCount++
		}
	}

	sessions := make([]LiveUsageSession, 0, len(groups))
	activeCount := 0
	anyWorking := false
	for _, session := range groups {
		if session.Attention == SessionAttentionCheck && groupHasFreshActivity[session.ID] {
			session.Attention = SessionAttentionNone
		}
		sort.Slice(session.ModelCalls, func(i, j int) bool { return session.ModelCalls[i].Sequence < session.ModelCalls[j].Sequence })
		sort.Slice(session.TurnTimings, func(i, j int) bool { return session.TurnTimings[i].Sequence < session.TurnTimings[j].Sequence })
		if session.Active {
			activeCount++
		}
		anyWorking = anyWorking || groupWorking[session.ID]
		sessions = append(sessions, *session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Active != sessions[j].Active {
			return sessions[i].Active
		}
		if !sessions[i].LastActivity.Equal(sessions[j].LastActivity) {
			return sessions[i].LastActivity.After(sessions[j].LastActivity)
		}
		return sessions[i].ID < sessions[j].ID
	})
	return sessions, activeCount, anyWorking
}

func sessionAttention(state sessionAttentionState, writerActive, writerLocksSupported bool, quietFor time.Duration, exactStatus sessionRuntimeStatus, exact bool) SessionAttention {
	if exact {
		switch exactStatus {
		case sessionRuntimeApproval:
			return SessionAttentionApproval
		case sessionRuntimeInput:
			return SessionAttentionInput
		}
		// A status supplied by the shared daemon is authoritative. In
		// particular, an active thread without a waiting flag must not age into
		// the fallback inactivity warning while a local tool is still running.
		return SessionAttentionNone
	}
	if writerLocksSupported && !writerActive {
		return SessionAttentionNone
	}
	switch state {
	case sessionAttentionIdle:
		if writerActive {
			return SessionAttentionInput
		}
	case sessionAttentionInput:
		return SessionAttentionInput
	case sessionAttentionApproval:
		return SessionAttentionApproval
	}
	if writerLocksSupported && writerActive && quietFor >= fallbackAttentionAfter {
		return SessionAttentionCheck
	}
	return SessionAttentionNone
}

func mergeSessionAttention(current, next SessionAttention) SessionAttention {
	if sessionAttentionPriority(next) > sessionAttentionPriority(current) {
		return next
	}
	return current
}

func sessionAttentionPriority(attention SessionAttention) int {
	switch attention {
	case SessionAttentionApproval:
		return 3
	case SessionAttentionInput:
		return 2
	case SessionAttentionCheck:
		return 1
	default:
		return 0
	}
}

func firstNonZeroTime(times ...time.Time) time.Time {
	for _, candidate := range times {
		if !candidate.IsZero() {
			return candidate
		}
	}
	return time.Time{}
}

func rolloutRoot(cursor *rolloutCursor, byID map[string]*rolloutCursor) (string, bool) {
	if cursor.threadID == "" || cursor.nonRoot && cursor.parentThreadID == "" {
		return unattributedSessionID, true
	}
	current := cursor
	seen := make(map[string]bool)
	for current.parentThreadID != "" {
		if seen[current.threadID] {
			return unattributedSessionID, true
		}
		seen[current.threadID] = true
		parent := byID[current.parentThreadID]
		if parent == nil {
			return unattributedSessionID, true
		}
		current = parent
	}
	if current.nonRoot || current.threadID == "" {
		return unattributedSessionID, true
	}
	return current.threadID, false
}

func rolloutCreatedAfter(path string, start time.Time) bool {
	name := filepath.Base(path)
	const prefix = "rollout-"
	const layout = "2006-01-02T15-04-05"
	if !strings.HasPrefix(name, prefix) || len(name) < len(prefix)+len(layout) {
		return false
	}
	created, err := time.ParseInLocation(layout, name[len(prefix):len(prefix)+len(layout)], time.Local)
	return err == nil && !created.Before(start.Truncate(time.Second))
}

func isRolloutFile(name string) bool {
	return strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl")
}

func contextErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

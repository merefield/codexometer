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
	activeRolloutHorizon = 24 * time.Hour
	discoveryEvery       = 5 * time.Second
	fullDiscoveryEvery   = 5 * time.Minute
)

// LiveUsageSnapshot is a process-local, monotonically increasing count built
// from token-count events appended to locally persisted Codex rollouts.
type LiveUsageSnapshot struct {
	TotalTokens  int64
	LastActivity time.Time
	SessionCount int
	Sessions     []LiveUsageSession
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
	Unattributed     bool
}

// LiveUsageReader incrementally observes token telemetry written by local Codex
// sessions. It never interprets message, reasoning, command, or tool contents.
type LiveUsageReader struct {
	SessionsRoot string

	mu                sync.Mutex
	initialized       bool
	startedAt         time.Time
	lastDiscovery     time.Time
	lastFullDiscovery time.Time
	files             map[string]*rolloutCursor
	totalTokens       int64
	lastActivity      time.Time
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
	nonRoot                     bool
	subagentHistoryStartOrdinal *uint64
}

type rolloutEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Ordinal   *uint64   `json:"ordinal"`
	Type      string    `json:"type"`
	Payload   struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage struct {
				TotalTokens int64 `json:"total_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
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
	return &LiveUsageReader{SessionsRoot: filepath.Join(codexHome, "sessions")}, nil
}

// FetchTokenUsage discovers active rollout files and consumes only complete,
// newly appended JSONL records. The first call establishes baselines and never
// reports historical tokens as new activity.
func (r *LiveUsageReader) FetchTokenUsage(ctx context.Context) (LiveUsageSnapshot, error) {
	return r.fetchTokenUsage(ctx, false)
}

// FetchTokenUsageFresh forces a complete session discovery before consuming
// telemetry. The Monitor uses this for its final Stop reading so a recently
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

	sessions, activeSessions := r.sessionSnapshots(now)
	return LiveUsageSnapshot{
		TotalTokens: r.totalTokens, LastActivity: r.lastActivity,
		SessionCount: activeSessions, Sessions: sessions,
	}, nil
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
	if !r.initialized || !rolloutCreatedAfter(path, r.startedAt) {
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
			if total, at, ordinal, ok := tokenTotalRecord(line); ok {
				if !tokenRecordIsOwned(ordinal, cursor.subagentHistoryStartOrdinal, at, cursor.nonRoot, cursor.startedAt) {
					// Legacy child rollouts copy the parent's cumulative token
					// events. Preserve the latest inherited value as the child's
					// counter baseline without reporting it as new usage.
					cursor.totalTokens = total
					continue
				}
				if total >= cursor.totalTokens {
					delta := total - cursor.totalTokens
					r.totalTokens += delta
					cursor.observedTokens += delta
					if delta > 0 {
						if at.IsZero() {
							at = time.Now()
						}
						if at.After(r.lastActivity) {
							r.lastActivity = at
						}
					}
				}
				cursor.totalTokens = total
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
	if !bytes.Contains(line, []byte("token_count")) {
		return 0, time.Time{}, nil, false
	}
	var event rolloutEvent
	if json.Unmarshal(line, &event) != nil || event.Type != "event_msg" ||
		event.Payload.Type != "token_count" || event.Payload.Info == nil {
		return 0, time.Time{}, nil, false
	}
	return event.Payload.Info.TotalTokenUsage.TotalTokens, event.Timestamp, event.Ordinal, true
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

func (r *LiveUsageReader) sessionSnapshots(now time.Time) ([]LiveUsageSession, int) {
	byID := make(map[string]*rolloutCursor, len(r.files))
	for _, cursor := range r.files {
		if cursor.threadID != "" {
			byID[cursor.threadID] = cursor
		}
	}

	groups := make(map[string]*LiveUsageSession)
	for _, cursor := range r.files {
		active := now.Sub(cursor.lastModified) <= 5*time.Minute
		if !active && cursor.observedTokens == 0 {
			continue
		}
		rootID, unattributed := rolloutRoot(cursor, byID)
		group := groups[rootID]
		if group == nil {
			group = &LiveUsageSession{ID: rootID, Unattributed: unattributed}
			groups[rootID] = group
		}
		group.TotalTokens += cursor.observedTokens
		group.Active = group.Active || active
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
		} else if active || cursor.observedTokens > 0 {
			group.AgentCount++
		}
	}

	sessions := make([]LiveUsageSession, 0, len(groups))
	activeCount := 0
	for _, session := range groups {
		if session.Active {
			activeCount++
		}
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
	return sessions, activeCount
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

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
	"strings"
	"sync"
	"time"
)

const (
	activeRolloutHorizon = 24 * time.Hour
	fullDiscoveryEvery   = 30 * time.Second
)

// LiveUsageSnapshot is a process-local, monotonically increasing count built
// from token-count events appended to locally persisted Codex rollouts.
type LiveUsageSnapshot struct {
	TotalTokens  int64
	LastActivity time.Time
	SessionCount int
}

// LiveUsageReader incrementally observes token telemetry written by local Codex
// sessions. It never interprets message, reasoning, command, or tool contents.
type LiveUsageReader struct {
	SessionsRoot string

	mu                sync.Mutex
	initialized       bool
	startedAt         time.Time
	lastFullDiscovery time.Time
	files             map[string]*rolloutCursor
	totalTokens       int64
	lastActivity      time.Time
}

type rolloutCursor struct {
	offset       int64
	totalTokens  int64
	lastModified time.Time
}

type rolloutEvent struct {
	Timestamp time.Time `json:"timestamp"`
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
// telemetry. The Stopwatch uses this for its final Stop reading so a recently
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
	} else if err := r.discover(ctx, now, forceFullDiscovery || now.Sub(r.lastFullDiscovery) >= fullDiscoveryEvery); err != nil {
		return LiveUsageSnapshot{}, err
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

	sessions := 0
	for _, cursor := range r.files {
		if now.Sub(cursor.lastModified) <= 5*time.Minute {
			sessions++
		}
	}
	return LiveUsageSnapshot{
		TotalTokens: r.totalTokens, LastActivity: r.lastActivity, SessionCount: sessions,
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
	if full {
		r.lastFullDiscovery = now
	}
	return nil
}

func (r *LiveUsageReader) addFile(path string, info os.FileInfo) error {
	cursor := &rolloutCursor{lastModified: info.ModTime()}
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
			if total, at, ok := tokenTotal(line); ok {
				if total >= cursor.totalTokens {
					delta := total - cursor.totalTokens
					r.totalTokens += delta
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
			if total, _, ok := tokenTotal(lines[index]); ok {
				return total, nil
			}
		}
		carry = append(carry[:0], lines[0]...)
	}
	return 0, nil
}

func tokenTotal(line []byte) (int64, time.Time, bool) {
	if !bytes.Contains(line, []byte("token_count")) {
		return 0, time.Time{}, false
	}
	var event rolloutEvent
	if json.Unmarshal(line, &event) != nil || event.Type != "event_msg" ||
		event.Payload.Type != "token_count" || event.Payload.Info == nil {
		return 0, time.Time{}, false
	}
	return event.Payload.Info.TotalTokenUsage.TotalTokens, event.Timestamp, true
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

// Package diagnostics provides opt-in, process-local operational logging.
// Never pass prompts, replies, credentials, raw protocol data or upstream error
// text here. Errors are classified, not printed; explicit filesystem paths are
// retained to make local access failures actionable.
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
)

const maxBytes = 2 << 20

type Fields struct {
	Path, Session string
	Code          int
}

type repetition struct {
	at    time.Time
	count int
}

type logger struct {
	mu       sync.Mutex
	handler  slog.Handler
	verbose  bool
	closed   bool
	writeErr error
	seen     map[string]repetition
}

var active atomic.Pointer[logger]

// Install leaves logging completely off for an empty path. The returned close
// restores the previous logger and reports late write errors after the UI exits.
func Install(path string, verbose bool) (func() error, error) {
	if path == "" {
		if verbose {
			return nil, errors.New("--verbose requires --error-log PATH")
		}
		return func() error { return nil }, nil
	}
	output, err := openFile(path)
	if err != nil {
		return nil, err
	}
	level := slog.LevelError
	if verbose {
		level = slog.LevelDebug
	}
	l := &logger{verbose: verbose, seen: make(map[string]repetition)}
	l.handler = slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})
	previous := active.Swap(l)
	var once sync.Once
	var closeErr error
	return func() error {
		once.Do(func() {
			active.CompareAndSwap(l, previous)
			l.mu.Lock()
			defer l.mu.Unlock()
			l.closed = true
			closeErr = errors.Join(l.writeErr, output.Close())
		})
		return closeErr
	}, nil
}

func Error(source, operation string, err error, fields Fields) {
	record(slog.LevelError, source, operation, err, fields)
}

func Debug(source, operation string, fields Fields) {
	record(slog.LevelDebug, source, operation, nil, fields)
}

func record(level slog.Level, source, operation string, err error, fields Fields) {
	l := active.Load()
	if l == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		level = slog.LevelDebug
	}
	if level < slog.LevelError && !l.verbose {
		return
	}
	source, operation = bounded(source, 80), bounded(operation, 120)
	path, session := bounded(fields.Path, 2048), bounded(fields.Session, 12)
	cause := errorClass(err)
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d", level, source, operation, path, session, cause, fields.Code)
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	previous, exists := l.seen[key]
	if exists && now.Sub(previous.at) < time.Minute {
		previous.count++
		l.seen[key] = previous
		return
	}
	if len(l.seen) >= 128 {
		clear(l.seen)
	}
	l.seen[key] = repetition{at: now}
	r := slog.NewRecord(now, level, operation, 0)
	r.AddAttrs(slog.String("app", "codexometer"), slog.String("source", source))
	if path != "" {
		r.AddAttrs(slog.String("path", path))
	}
	if session != "" {
		r.AddAttrs(slog.String("session", session))
	}
	if fields.Code != 0 {
		r.AddAttrs(slog.Int("code", fields.Code))
	}
	if err != nil {
		r.AddAttrs(slog.String("error", cause), slog.String("error_type", fmt.Sprintf("%T", err)))
	}
	if previous.count > 0 {
		r.AddAttrs(slog.Int("suppressed_repeats", previous.count))
	}
	if err := l.handler.Handle(context.Background(), r); err != nil && l.writeErr == nil {
		l.writeErr = err
	}
}

// Only trusted OS/context sentinel text is emitted. In particular, fmt-wrapped,
// JSON/TOML and app-server errors can contain secrets or conversation content.
func errorClass(err error) string {
	if err == nil {
		return ""
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	for _, known := range []error{fs.ErrPermission, fs.ErrNotExist, fs.ErrExist, fs.ErrClosed, context.Canceled, context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "details omitted; inspect operation and error_type"
}

func bounded(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

// A single requested file is bounded: once full, older entries are discarded.
// No backup filenames are overwritten and no extra dependency is needed.
type boundedFile struct {
	*os.File
	size, limit int64
}

func openFile(path string) (*boundedFile, error) {
	before, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if before != nil && !before.Mode().IsRegular() {
		return nil, errors.New("error log must be a regular file, not a symlink or device")
	}
	// Windows append-only handles cannot truncate. One logger serialises its
	// writes; seek once on open to append across runs without O_APPEND.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	info, statErr := f.Stat()
	after, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, after) || after.Mode()&os.ModeSymlink != 0 {
		f.Close()
		return nil, errors.New("error log target changed or is not a regular file")
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, err
	}
	return &boundedFile{File: f, size: info.Size(), limit: maxBytes}, nil
}

func (f *boundedFile) Write(p []byte) (int, error) {
	if int64(len(p)) > f.limit {
		return 0, errors.New("diagnostic record exceeds log limit")
	}
	if f.size+int64(len(p)) > f.limit {
		if err := f.Truncate(0); err != nil {
			return 0, err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
		f.size = 0
	}
	n, err := f.File.Write(p)
	f.size += int64(n)
	return n, err
}

package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"time"
)

// Match the context bootstrap target. Extend through an overlapping record up
// to the existing 4 MiB baseline-record bound, never an unbounded history scan.
const rolloutActivityTailBytes = 256 * 1024

type rolloutContextReader struct {
	ctx context.Context
	io.Reader
}

func (r rolloutContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func (r *LiveUsageReader) activityNow() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Windows can retain an old modification time while the writer handle is open.
// Keep that metadata separate from observed growth and persisted event time.
func (c *rolloutCursor) activityTime() time.Time {
	latest := c.lastModified
	if c.lastEventAt.After(latest) {
		latest = c.lastEventAt
	}
	if c.lastGrowthAt.After(latest) {
		latest = c.lastGrowthAt
	}
	return latest
}

func (c *rolloutCursor) observeFile(info os.FileInfo, now time.Time) {
	c.lastModified = info.ModTime()
	if info.Size() != c.lastSize {
		c.lastGrowthAt = now
		c.lastSize = info.Size()
	}
}

func (c *rolloutCursor) observeEvent(line []byte, now time.Time) {
	var event struct {
		Timestamp time.Time `json:"timestamp"`
		Ordinal   *uint64   `json:"ordinal"`
		Type      string    `json:"type"`
	}
	if json.Unmarshal(line, &event) != nil || event.Type == "" || event.Type == "session_meta" ||
		event.Timestamp.IsZero() || event.Timestamp.After(now) ||
		!tokenRecordIsOwned(event.Ordinal, c.subagentHistoryStartOrdinal, event.Timestamp, c.nonRoot, c.startedAt) {
		return
	}
	if event.Timestamp.After(c.lastEventAt) {
		c.lastEventAt = event.Timestamp
	}
}

// Existing records establish activity age, never new token usage. Ignore an
// incomplete final record and future timestamps rather than keeping old logs
// active indefinitely. Only headers are decoded; no content is retained.
func (c *rolloutCursor) bootstrapActivity(ctx context.Context, path string, size int64, now time.Time) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	start := max(size-rolloutActivityTailBytes, 0)
	start, partial, err := rolloutActivityRecordStart(ctx, f, start)
	if err != nil {
		return err
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(f, start, size-start), 64*1024)
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk, readErr := reader.ReadSlice('\n')
		if !partial && len(line)+len(chunk) <= rolloutBaselineLineLimit {
			line = append(line, chunk...)
		} else {
			partial = true
			line = nil
		}
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			if !partial {
				c.observeEvent(line, now)
			}
			line = line[:0]
			partial = false
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil // An incomplete final record is not an event.
			}
			return readErr
		}
	}
}

// Find the beginning of the record overlapping the target tail. Fixed chunks
// avoid repeatedly copying a growing carry buffer. Read at most that record,
// not earlier history. If the overlap exceeds the bound, skip that record
// conservatively but still inspect complete records later in the tail.
func rolloutActivityRecordStart(ctx context.Context, f *os.File, start int64) (int64, bool, error) {
	const chunkBytes = 64 * 1024
	buffer := make([]byte, chunkBytes)
	floor := max(start-rolloutBaselineLineLimit, 0)
	for start > floor {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		size := min(start-floor, int64(len(buffer)))
		start -= size
		n, err := f.ReadAt(buffer[:size], start)
		if err != nil && err != io.EOF {
			return 0, false, err
		}
		if index := bytes.LastIndexByte(buffer[:n], '\n'); index >= 0 {
			return start + int64(index) + 1, false, nil
		}
	}
	return start, start > 0, nil
}

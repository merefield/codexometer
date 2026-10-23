package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"
)

// Match the context bootstrap target. Extend through one overlapping record
// when needed, so a large tool result or reasoning item is not discarded.
const rolloutActivityTailBytes = 256 * 1024

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
func (c *rolloutCursor) bootstrapActivity(path string, size int64, now time.Time) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	start := max(size-rolloutActivityTailBytes, 0)
	start, err = rolloutActivityRecordStart(f, start)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.NewSectionReader(f, start, size-start))
	if err != nil {
		return err
	}
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines[:max(len(lines)-1, 0)] {
		c.observeEvent(line, now)
	}
	return nil
}

// Find the beginning of the record overlapping the target tail. Fixed chunks
// avoid repeatedly copying a growing carry buffer. Read at most that record,
// not earlier history; memory for decoding follows the existing JSONL reader's
// largest-record behavior rather than imposing a truncating record limit.
func rolloutActivityRecordStart(f *os.File, start int64) (int64, error) {
	const chunkBytes = 64 * 1024
	buffer := make([]byte, chunkBytes)
	for start > 0 {
		size := min(start, int64(len(buffer)))
		start -= size
		n, err := f.ReadAt(buffer[:size], start)
		if err != nil && err != io.EOF {
			return 0, err
		}
		if index := bytes.LastIndexByte(buffer[:n], '\n'); index >= 0 {
			return start + int64(index) + 1, nil
		}
	}
	return 0, nil
}

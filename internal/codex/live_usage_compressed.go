package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const compressedHistoryLimit = 256 << 20
const rolloutBaselineLineLimit = 4 << 20

func isCompressedRolloutFile(name string) bool {
	return strings.HasSuffix(name, ".zst") && isRolloutFile(strings.TrimSuffix(name, ".zst"))
}

// Compressed histories are immutable baselines, never new spending. Keep their
// logical .jsonl identity and decoded offset so restoring them preserves both
// observed totals and the boundary between history and concurrent appends.
// No transcript copy is written to disk and retained memory is bounded.
func (r *LiveUsageReader) addCompressedFile(ctx context.Context, path string, info os.FileInfo) error {
	plain := strings.TrimSuffix(path, ".zst")
	if _, err := os.Stat(plain); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	old := r.files[plain]
	if old != nil && old.compressed && old.compressedInfo.Size() == info.Size() && old.compressedInfo.ModTime() == info.ModTime() {
		return nil
	}
	cursor, err := compressedRolloutBaseline(ctx, path, info, r.activityNow())
	if err != nil {
		return fmt.Errorf("read compressed rollout: %w", err)
	}
	if old != nil && old.threadID == cursor.threadID {
		cursor.observedTokens, cursor.modelCalls, cursor.turnTimings = old.observedTokens, old.modelCalls, old.turnTimings
		cursor.lastGrowthAt = old.lastGrowthAt
		if old.offset != cursor.offset {
			r.apiEqAccountingAdjustments++
		}
	}
	r.files[plain] = cursor
	return nil
}

func compressedRolloutBaseline(ctx context.Context, path string, info os.FileInfo, now time.Time) (*rolloutCursor, error) {
	if info.Size() > compressedHistoryLimit {
		return nil, errors.New("compressed history exceeds 256 MiB source limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder, err := zstd.NewReader(io.LimitReader(file, compressedHistoryLimit+1), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(128<<20), zstd.WithDecoderMaxMemory(128<<20))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	reader := bufio.NewReaderSize(io.LimitReader(decoder, compressedHistoryLimit+1), 64<<10)
	cursor := &rolloutCursor{lastModified: info.ModTime(), compressed: true, compressedInfo: info}
	var line, tail []byte
	var activityTail rolloutActivityTail
	digest := sha256.New()
	var decoded int64
	oversized := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunk, readErr := reader.ReadSlice('\n')
		decoded += int64(len(chunk))
		digest.Write(chunk)
		if decoded > compressedHistoryLimit {
			return nil, errors.New("decoded history exceeds 256 MiB baseline limit")
		}
		activityTail.append(chunk)
		tail = append(tail, chunk...)
		if len(tail) > 4096 {
			tail = append(tail[:0], tail[len(tail)-4096:]...)
		}
		if len(line)+len(chunk) <= rolloutBaselineLineLimit && !oversized {
			line = append(line, chunk...)
		} else {
			oversized = true
			cursor.exactUsageUncertain = true
			line = nil
		}
		if errors.Is(readErr, bufio.ErrBufferFull) {
			continue
		}
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			cursor.offset = decoded
			copy(cursor.compressedDigest[:], digest.Sum(nil))
			cursor.compressedTail = append(cursor.compressedTail[:0], tail...)
			if !oversized {
				baselineRolloutRecord(cursor, line)
			}
			line = line[:0]
			oversized = false
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, readErr
			}
			break
		}
	}
	cursor.exactUsageUncertain = cursor.exactUsageUncertain && !cursor.exactUsage
	cursor.lastSize = decoded // logical JSONL bytes, including an incomplete tail, not archive size
	if cursor.threadID == "" {
		return nil, errors.New("compressed history omitted session metadata")
	}
	size := int64(len(activityTail.data))
	if err := cursor.observeActivityTail(ctx, &activityTail, size, decoded > size, now); err != nil {
		return nil, err
	}
	return cursor, nil
}

func baselineRolloutRecord(c *rolloutCursor, line []byte) {
	if metadata, ok := rolloutMetadataRecord(line); ok && c.threadID == "" {
		c.threadID, c.parentThreadID, c.workingDirectory = metadata.ID, metadata.ParentThreadID, metadata.WorkingDirectory
		c.startedAt, c.nonRoot, c.subagentHistoryStartOrdinal = metadata.StartedAt, metadata.NonRoot, metadata.SubagentHistoryStartOrdinal
	}
	if settings, ordinal, at, ok := rolloutSessionSettingsRecord(line, c.modelSettings); ok && tokenRecordIsOwned(ordinal, c.subagentHistoryStartOrdinal, at, c.nonRoot, c.startedAt) {
		c.modelSettings = settings
	}
	if tier, ordinal, at, ok := rolloutTierRecord(line); ok && tokenRecordIsOwned(ordinal, c.subagentHistoryStartOrdinal, at, c.nonRoot, c.startedAt) {
		c.nextServiceTier = tier
	}
	if model, ordinal, at, ok := rolloutModelRecord(line); ok && tokenRecordIsOwned(ordinal, c.subagentHistoryStartOrdinal, at, c.nonRoot, c.startedAt) {
		c.currentModel, c.currentServiceTier = model, c.nextServiceTier
	}
	if turn, ordinal, at, ok := rolloutTurnIDRecord(line); ok && tokenRecordIsOwned(ordinal, c.subagentHistoryStartOrdinal, at, c.nonRoot, c.startedAt) {
		c.currentTurnID = turn
	}
	if attention, ordinal, at, ok := sessionAttentionRecord(line, c.currentTurnID); ok && tokenRecordIsOwned(ordinal, c.subagentHistoryStartOrdinal, at, c.nonRoot, c.startedAt) {
		c.attention = attention
	}
	if preview, ok := rolloutContextRecord(line, c); ok {
		c.preview = preview
	}
	if record, ok := tokenUsageRecord(line); ok {
		c.totalTokens = record.total
	}
	if record, ok := exactUsageRecord(line); ok && c.ownsExactRecord(record) {
		c.exactUsage = true
		usage := BenchmarkUsage{}
		if record.Payload.Usage != nil {
			usage = record.Payload.Usage.benchmarkUsage()
		}
		if id := strings.TrimSpace(record.Payload.ResponseID); id != "" {
			c.rememberExactResponse(id, usage)
		}
	}
}

func (r *LiveUsageReader) restoreCompressedCursor(ctx context.Context, path string, info os.FileInfo, cursor *rolloutCursor) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	tail := make([]byte, len(cursor.compressedTail))
	n, readErr := file.ReadAt(tail, cursor.offset-int64(len(tail)))
	// The tail is a quick mismatch check; hash the entire bounded prefix before
	// trusting its settings and offset. Reverts may change earlier context while
	// retaining an identical tail and file length.
	matches := false
	if readErr == nil && n == len(tail) && bytes.Equal(tail, cursor.compressedTail) && info.Size() >= cursor.offset {
		digest := sha256.New()
		_, err = io.Copy(digest, rolloutContextReader{ctx, io.NewSectionReader(file, 0, cursor.offset)})
		if err != nil {
			file.Close()
			return err
		}
		matches = bytes.Equal(digest.Sum(nil), cursor.compressedDigest[:])
	}
	file.Close()
	if matches {
		cursor.compressed = false
		cursor.compressedInfo = nil
		cursor.compressedTail = nil
		cursor.observeFile(info, r.activityNow())
		return nil
	}
	// A changed/truncated restoration cannot reuse an offset from different
	// history. Existing-thread discovery rebases it rather than billing copies.
	return r.addFile(ctx, path, info)
}

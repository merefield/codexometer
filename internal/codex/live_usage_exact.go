package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// Codex added durable, exact response records on 2026-08-31. Context token
// snapshots remain useful as baselines, but are not response billing records.
type rolloutExactUsage struct {
	Timestamp time.Time `json:"timestamp"`
	Ordinal   *uint64   `json:"ordinal"`
	Type      string    `json:"type"`
	Payload   struct {
		ThreadID   string             `json:"thread_id"`
		TurnID     string             `json:"turn_id"`
		ResponseID string             `json:"response_id"`
		Usage      *rolloutTokenUsage `json:"usage"`
	} `json:"payload"`
}

func exactUsageRecord(line []byte) (rolloutExactUsage, bool) {
	var record rolloutExactUsage
	if !bytes.Contains(line, []byte(`"token_usage_record"`)) || json.Unmarshal(line, &record) != nil || record.Type != "token_usage_record" {
		return record, false
	}
	return record, true
}

func (c *rolloutCursor) ownsExactRecord(record rolloutExactUsage) bool {
	return record.Payload.ThreadID == c.threadID && tokenRecordIsOwned(record.Ordinal, c.subagentHistoryStartOrdinal, record.Timestamp, c.nonRoot, c.startedAt)
}

func (c *rolloutCursor) rememberExactResponse(id string, usage BenchmarkUsage) bool {
	if c.exactResponses == nil {
		c.exactResponses = make(map[string]BenchmarkUsage)
	}
	if _, exists := c.exactResponses[id]; exists {
		return false
	}
	c.exactResponses[id] = usage
	c.exactResponseOrder = append(c.exactResponseOrder, id)
	if len(c.exactResponseOrder) > telemetryHistoryMax {
		delete(c.exactResponses, c.exactResponseOrder[0])
		delete(c.exactConflicts, c.exactResponseOrder[0])
		c.exactResponseOrder = c.exactResponseOrder[1:]
	}
	return true
}

// Establish the source and recent replay identities without counting history.
// Bound startup IO independently of the file's potentially large transcript.
func restoreExactUsageBaseline(path string, cursor *rolloutCursor, baselineOffset int64) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	end := min(info.Size(), baselineOffset)
	size := min(end, int64(4*1024*1024))
	start := end - size
	data := make([]byte, size)
	_, err = file.ReadAt(data, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	// A writer can be midway through a small exact record at discovery. Leave
	// its partial bytes for the incremental reader, rather than skipping them.
	if last := bytes.LastIndexByte(data, '\n'); last >= 0 && last < len(data)-1 {
		end = end - size + int64(last) + 1
		data = data[:last+1]
	}
	lines := bytes.Split(data, []byte{'\n'})
	if start > 0 {
		lines = lines[1:]
	}
	for _, line := range lines {
		record, ok := exactUsageRecord(line)
		if !ok || !cursor.ownsExactRecord(record) {
			continue
		}
		cursor.exactUsage = true
		if id := strings.TrimSpace(record.Payload.ResponseID); id != "" {
			usage := BenchmarkUsage{}
			if record.Payload.Usage != nil {
				usage = record.Payload.Usage.benchmarkUsage()
			}
			cursor.rememberExactResponse(id, usage)
		}
	}
	return end, nil
}

func (r *LiveUsageReader) consumeExactUsage(cursor *rolloutCursor, record rolloutExactUsage) {
	if !cursor.ownsExactRecord(record) {
		return
	}
	cursor.exactUsage = true
	id := strings.TrimSpace(record.Payload.ResponseID)
	usage := BenchmarkUsage{}
	if record.Payload.Usage != nil {
		usage = record.Payload.Usage.benchmarkUsage()
	}
	if previous, seen := cursor.exactResponses[id]; id != "" && seen {
		if previous != usage && !cursor.exactConflicts[id] {
			if cursor.exactConflicts == nil {
				cursor.exactConflicts = make(map[string]bool)
			}
			cursor.exactConflicts[id] = true
			r.apiEqInconsistentUsageCalls++
			r.rememberCallIssue(LiveModelCall{At: record.Timestamp, SessionID: cursor.threadID, Model: cursor.currentModel, RequestedServiceTier: cursor.currentServiceTier, ResponseID: id, UsageSource: "exact response", Effort: cursor.modelSettings.ReasoningEffort, issueKind: UsageIssueInconsistent, APIEqIssue: "response ID replayed with conflicting usage"})
		}
		return
	}
	if id != "" {
		cursor.rememberExactResponse(id, usage)
	}
	r.nextEventSequence++
	at := record.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	call := LiveModelCall{Sequence: r.nextEventSequence, At: at, Model: cursor.currentModel,
		SessionID: cursor.threadID, TurnID: record.Payload.TurnID, ResponseID: id,
		Effort: cursor.modelSettings.ReasoningEffort, UsageSource: "exact response", ModelBasis: "requested", RequestedServiceTier: cursor.currentServiceTier}
	call.OutputAvailable = record.Payload.Usage != nil && record.Payload.Usage.OutputTokens != nil
	call.OutputTokens = usage.OutputTokens
	if id == "" {
		call.issueKind = UsageIssueInconsistent
		call.APIEqIssue = "exact response record omitted response ID"
		call.apiEqFinalized = true
		r.apiEqInconsistentUsageCalls++
	} else if record.Payload.Usage == nil || record.Payload.Usage.InputTokens == nil || record.Payload.Usage.OutputTokens == nil || record.Payload.Usage.TotalTokens == nil || record.Payload.Usage.CachedInputTokens == nil || record.Payload.Usage.ReasoningOutputTokens == nil {
		call.issueKind = UsageIssueMissing
		call.APIEqIssue = "exact response record omitted required token breakdown fields"
		call.apiEqFinalized = true
		r.apiEqMissingUsageCalls++
	} else if issue := validateBenchmarkUsage(usage); issue != "" {
		call.issueKind = UsageIssueInconsistent
		call.APIEqIssue = issue
		call.apiEqFinalized = true
		r.apiEqInconsistentUsageCalls++
	} else {
		// Exact usage is counted independently of synthetic context-counter resets.
		r.totalTokens += usage.TotalTokens
		cursor.observedTokens += usage.TotalTokens
		if usage.TotalTokens > 0 && at.After(r.lastActivity) {
			r.lastActivity = at
		}
		_, subscribed := r.daemonSubscribedThreads[cursor.threadID]
		if subscribed && call.TurnID != "" {
			r.pendingModelResolutions = append(r.pendingModelResolutions, pendingModelResolution{
				callSequence: call.Sequence, threadID: call.SessionID, turnID: call.TurnID, usage: usage, cumulativeTotal: -1, exact: true})
		} else {
			r.finalizeCallPricing(&call, usage)
		}
	}
	if call.apiEqFinalized && call.APIEqIssue != "" {
		r.rememberCallIssue(call)
	}
	cursor.modelCalls = appendBounded(cursor.modelCalls, call, telemetryHistoryMax)
}

// Exact records precede the corresponding context snapshot. Attach that
// checkpoint when available; it is attribution evidence, not a billing gate.
func (r *LiveUsageReader) attachExactCheckpoint(cursor *rolloutCursor, record rolloutTokenRecord) {
	for i := len(r.pendingModelResolutions) - 1; i >= 0; i-- {
		pending := &r.pendingModelResolutions[i]
		if pending.exact && pending.threadID == cursor.threadID && (cursor.currentTurnID == "" || pending.turnID == cursor.currentTurnID) && pending.cumulativeTotal < 0 && sameBenchmarkUsage(pending.usage, record.usage) {
			pending.cumulativeTotal = record.total
			break
		}
	}
}

// Without a context checkpoint, accept only a unique matching live observation.
// Repeated identical responses must not inherit an ambiguous rerouted model.
func (r *LiveUsageReader) takeUniqueExactModelObservation(threadID, turnID string, usage BenchmarkUsage) (resolvedModelObservation, bool) {
	index := -1
	for i, observation := range r.resolvedObservations {
		if observation.ThreadID == threadID && observation.TurnID == turnID && sameBenchmarkUsage(observation.Usage, usage) {
			if index >= 0 {
				return resolvedModelObservation{}, false
			}
			index = i
		}
	}
	if index < 0 {
		return resolvedModelObservation{}, false
	}
	observation := r.resolvedObservations[index]
	r.resolvedObservations = append(r.resolvedObservations[:index], r.resolvedObservations[index+1:]...)
	return observation, true
}

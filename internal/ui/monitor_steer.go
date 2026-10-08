package ui

import (
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

// A steer is already sent, not a queued message that can be edited or cancelled.
// Retain its receipt across selection changes until live guidance catches up.
type monitorSteerReceipt struct {
	sentAt                                                time.Time
	id                                                    uint64
	session, thread, turn, text, baselineID, baselineText string
}

func (m *Model) recordMonitorSteer(receipt monitorSteerReceipt) {
	if receipt.text == "" || receipt.session == "" {
		return
	}
	m.monitorSteerSequence++
	receipt.id = m.monitorSteerSequence
	m.monitorSteers = append(append([]monitorSteerReceipt(nil), m.monitorSteers...), receipt)
	m.reconcileMonitorSteers()
}

func (m *Model) reconcileMonitorSteers() {
	if len(m.monitorSteers) == 0 {
		return
	}
	// Only a matching thread/turn can confirm a steer. A child approval, an
	// empty queue response, or temporarily missing telemetry cannot clear it.
	type target struct{ session, thread, turn string }
	confirmed := make(map[target]uint64)
	for _, receipt := range m.monitorSteers {
		for _, session := range m.monitorSessionData {
			c := session.preview
			if session.id != receipt.session || c.ThreadID != receipt.thread {
				continue
			}
			key := target{receipt.session, receipt.thread, receipt.turn}
			if c.TurnID != receipt.turn {
				// A positively newer turn supersedes its predecessor's guidance.
				if c.TurnID != "" && !receipt.sentAt.IsZero() && c.At.After(receipt.sentAt) {
					confirmed[key] = max(confirmed[key], receipt.id)
				}
				continue
			}
			changed := c.LatestGuidanceID != "" && c.LatestGuidanceID != receipt.baselineID ||
				c.LatestGuidanceID == "" && c.LatestGuidance != receipt.baselineText
			if changed && codex.SanitizeSessionContext(c.LatestGuidance) == receipt.text {
				confirmed[key] = max(confirmed[key], receipt.id)
			}
		}
	}
	remaining := make([]monitorSteerReceipt, 0, len(m.monitorSteers))
	for _, receipt := range m.monitorSteers {
		// Guidance shows only the latest steer; earlier receipts for that same
		// turn are superseded once a later submitted direction is visible.
		if receipt.id > confirmed[target{receipt.session, receipt.thread, receipt.turn}] {
			remaining = append(remaining, receipt)
		}
	}
	m.monitorSteers = remaining
}

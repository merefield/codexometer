package ui

import (
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

// A steer is already sent, not a queued message that can be edited or cancelled.
// Retain its receipt until a matching completed user-message event arrives.
type monitorSteerReceipt struct {
	sentAt                                time.Time
	id                                    uint64
	session, thread, turn, text, clientID string
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
	remaining := make([]monitorSteerReceipt, 0, len(m.monitorSteers))
	for _, receipt := range m.monitorSteers {
		confirmed := false
		for _, session := range m.monitorSessionData {
			if session.id != receipt.session {
				continue
			}
			progress := session.steerProgress
			if len(progress) == 0 {
				// Preserve compatibility with sources supplying only a context.
				progress = []codex.SessionSteerProgress{session.preview.SteerProgress()}
			}
			for _, c := range progress {
				if c.ThreadID != receipt.thread {
					continue
				}
				if c.TurnID != receipt.turn {
					// A positively newer turn makes this old receipt obsolete.
					confirmed = c.TurnID != "" && !receipt.sentAt.IsZero() && c.At.After(receipt.sentAt)
				} else {
					confirmed = c.HasCompletedSteer(receipt.clientID)
				}
				if confirmed {
					break
				}
			}
		}
		if !confirmed {
			remaining = append(remaining, receipt)
		}
	}
	m.monitorSteers = remaining
}

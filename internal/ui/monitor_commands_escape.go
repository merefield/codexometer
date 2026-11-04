package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const monitorCommandEscapeQuietPeriod = 250 * time.Millisecond

// Dismissing a command layer must not let the same Escape burst dismiss the
// composer/session underneath it. Modern terminals identify repeats/releases;
// legacy terminals need a short quiet period after the last Escape. Other
// deliberate input immediately restores ordinary navigation.
func (m *Model) consumeMonitorCommandEscape(msg tea.Msg) bool {
	p := &m.monitorCommands
	switch key := msg.(type) {
	case tea.KeyReleaseMsg:
		if key.String() == "esc" {
			p.escapeUntil = time.Time{}
		}
	case tea.MouseClickMsg, tea.PasteMsg:
		p.escapeUntil = time.Time{}
	case tea.KeyPressMsg:
		if key.String() != "esc" || m.meterView != viewMonitor || p.session != m.monitorContextTarget() {
			p.escapeUntil = time.Time{}
			return false
		}
		if key.IsRepeat && (p.open || !p.escapeUntil.IsZero()) || time.Now().Before(p.escapeUntil) {
			p.escapeUntil = time.Now().Add(monitorCommandEscapeQuietPeriod)
			return true
		}
		p.escapeUntil = time.Time{}
	}
	return false
}

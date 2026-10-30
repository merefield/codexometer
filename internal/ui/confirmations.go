package ui

import "github.com/merefield/codexometer/internal/confirmation"

// SetNoConfirm applies only to this launch, never to saved display preferences.
// Quota-reset redemption remains guarded regardless of this setting.
func (m *Model) SetNoConfirm(value bool) {
	m.confirmations = confirmation.Policy{NoConfirm: value}
}

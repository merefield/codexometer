package ui

import "github.com/merefield/codexometer/internal/confirmation"

// SetConfirmationMode applies only to this launch, never to saved preferences.
// Quota-reset redemption remains guarded in every mode.
func (m *Model) SetConfirmationMode(mode confirmation.Mode) {
	m.confirmations = confirmation.Policy{Mode: mode}
}

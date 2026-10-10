//go:build !darwin

package ui

import tea "charm.land/bubbletea/v2"

func localSessionWebLinkOpening() bool  { return false }
func openSessionWebLink(string) tea.Cmd { return nil }

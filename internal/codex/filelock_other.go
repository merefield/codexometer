//go:build !unix && !windows

package codex

import (
	"errors"
	"os"
)

const fileLockSupported = false

func fileLockHeld(string) (bool, error) {
	return false, errors.ErrUnsupported
}

func withHistoryFileLock(_ string, action func() error) error { return action() }

func replaceHistoryFile(from, to string) error { return os.Rename(from, to) }

func syncHistoryDirectory(string) error { return nil }

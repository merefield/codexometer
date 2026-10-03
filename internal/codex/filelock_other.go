//go:build !unix && !windows

package codex

import "errors"

const fileLockSupported = false

func fileLockHeld(string) (bool, error) {
	return false, errors.ErrUnsupported
}

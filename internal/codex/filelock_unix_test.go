//go:build !windows

package codex

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func holdTestFileLock(t *testing.T, path string) func() {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		file.Close()
		t.Fatal(err)
	}
	return func() {
		if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
			t.Errorf("unlock test writer: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Errorf("close test writer: %v", err)
		}
	}
}

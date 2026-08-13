//go:build windows

package codex

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func holdTestFileLock(t *testing.T, path string) func() {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		file.Close()
		t.Fatal(err)
	}
	return func() {
		if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped); err != nil {
			t.Errorf("unlock test writer: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Errorf("close test writer: %v", err)
		}
	}
}

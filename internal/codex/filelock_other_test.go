//go:build !unix && !windows

package codex

import "testing"

func holdTestFileLock(t *testing.T, _ string) func() {
	t.Helper()
	t.Skip("writer-lock probing is unsupported on this platform")
	return func() {}
}

func TestFileLockFallbackReportsUnsupported(t *testing.T) {
	if fileLockSupported {
		t.Fatal("fallback unexpectedly reports file-lock support")
	}
	if _, err := fileLockHeld("unused"); err == nil {
		t.Fatal("fallback file-lock probe unexpectedly succeeded")
	}
}

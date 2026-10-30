package codex

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/merefield/codexometer/internal/diagnostics"
)

type coverageReadCloser struct {
	io.Reader
	closeErr error
	closed   bool
}

func (f *coverageReadCloser) Close() error {
	f.closed = true
	return f.closeErr
}

func TestUsageCoverageReadAndCloseFailuresAreLoggedAndRetried(t *testing.T) {
	for _, failure := range []string{"read", "close", "read and close"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			path, logPath := filepath.Join(dir, "config.toml"), filepath.Join(dir, "error.log")
			const config = "[features]\nasync_compaction = true\n"
			if err := os.WriteFile(path, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			closeLog, err := diagnostics.Install(logPath, false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeLog() })
			file := &coverageReadCloser{Reader: strings.NewReader(config)}
			if strings.Contains(failure, "read") {
				file.Reader = iotest.ErrReader(os.ErrPermission)
			}
			if strings.Contains(failure, "close") {
				file.closeErr = os.ErrPermission
			}
			r := &LiveUsageReader{localFeatureChecks: make(map[string]localFeatureCheck)}
			enabled, issue := r.readLocalAsyncCompaction(file, info, diagnostics.Fields{Path: path, Session: "thread"})
			if enabled || issue != "background usage configuration could not be verified" || !file.closed {
				t.Fatal("filesystem failure did not block verification and close the file", enabled, issue, file.closed)
			}
			if len(r.localFeatureChecks) != 0 {
				t.Fatal("failed filesystem operation was cached", r.localFeatureChecks)
			}
			if err := closeLog(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"read", "close"} {
				if strings.Contains(failure, operation) && !strings.Contains(string(data), `"msg":"`+operation+`"`) {
					t.Fatal("filesystem failure was not logged", operation, string(data))
				}
			}
			// Retry the same path and unchanged metadata after the transient
			// failure. A successful read must now detect and cache the flag.
			if enabled, issue := r.localAsyncCompaction(path, "thread"); !enabled || issue != "" || !r.localFeatureChecks[path].enabled {
				t.Fatal("unchanged configuration was not successfully retried", enabled, issue)
			}
		})
	}
}

func TestUsageCoverageLogsFilesystemCauseWithoutChangingTheGuard(t *testing.T) {
	home := t.TempDir()
	logPath, config := filepath.Join(home, "error.log"), filepath.Join(home, "config.toml")
	close, err := diagnostics.Install(logPath, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close() })
	// A directory is unreadable as TOML on every target OS: either open or read
	// reports a real filesystem error without depending on root/ACL privileges.
	if err := os.Mkdir(config, 0700); err != nil {
		t.Fatal(err)
	}
	r := &LiveUsageReader{}
	_, issue := r.localAsyncCompaction(config, "session-123456789")
	if !strings.Contains(issue, "could not be") {
		t.Fatal("filesystem failure stopped blocking learning", issue)
	}
	if err := close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"source":"local-config"`, `"session":"session-1234`, `"error_type":`, `config.toml`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	if strings.Contains(string(data), "details omitted") {
		t.Fatal("OS error was hidden", string(data))
	}
}

func TestUsageCoverageLogsExactUnreadableConfigurationFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink requires optional Windows privileges")
	}
	home := t.TempDir()
	logPath, config := filepath.Join(home, "error.log"), filepath.Join(home, "config.toml")
	if err := os.Symlink(config, config); err != nil {
		t.Fatal(err)
	}
	close, err := diagnostics.Install(logPath, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close() })
	r := &LiveUsageReader{}
	_, issue := r.localAsyncCompaction(config, "session-one")
	if issue != "background usage configuration could not be read" {
		t.Fatal("exact access-failure diagnostic changed", issue)
	}
	if err := close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(logPath)
	for _, want := range []string{`"msg":"stat"`, `"path":`, `config.toml`, `"error":`, `"session":"session-one"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	if strings.Contains(string(data), "details omitted") {
		t.Fatal("actual filesystem cause not captured")
	}
}

func TestUsageCoverageNeverLogsConfigurationContents(t *testing.T) {
	home := t.TempDir()
	logPath, config := filepath.Join(home, "error.log"), filepath.Join(home, "config.toml")
	close, err := diagnostics.Install(logPath, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close() })
	if err := os.WriteFile(config, []byte("secret = PRIVATE_CONFIG_CONTENT ["), 0600); err != nil {
		t.Fatal(err)
	}
	r := &LiveUsageReader{}
	_, issue := r.localAsyncCompaction(config, "s")
	if issue != "background usage configuration could not be verified" {
		t.Fatal(issue)
	}
	if err := close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(logPath)
	if strings.Contains(string(data), "PRIVATE_CONFIG_CONTENT") || !strings.Contains(string(data), "parse TOML") {
		t.Fatal("unsafe or missing parse diagnostic", string(data))
	}
}

func TestUsageCoverageNonDirectoryOptionalConfigIsDebugNotError(t *testing.T) {
	home := t.TempDir()
	marker, logPath := filepath.Join(home, ".codex"), filepath.Join(home, "error.log")
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	close, err := diagnostics.Install(logPath, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close() })
	r := &LiveUsageReader{}
	if enabled, issue := r.localAsyncCompaction(filepath.Join(marker, "config.toml"), "thread"); enabled || issue != "" {
		t.Fatal("optional file under non-directory is not absent", enabled, issue)
	}
	if err := close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"level":"ERROR"`) || !strings.Contains(string(data), "configuration not present (normal)") {
		t.Fatal("non-directory optional config still logged as a failure", string(data))
	}
}

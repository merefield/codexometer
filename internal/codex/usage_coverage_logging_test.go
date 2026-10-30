package codex

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/merefield/codexometer/internal/diagnostics"
)

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

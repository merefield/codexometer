package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func records(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal("invalid JSON log record", err)
		}
		result = append(result, record)
	}
	return result
}

func TestLoggingIsOptInAndVerboseRequiresAFile(t *testing.T) {
	close, err := Install("", false)
	if err != nil {
		t.Fatal(err)
	}
	Error("test", "ignored", errors.New("secret"), Fields{})
	if close() != nil || active.Load() != nil {
		t.Fatal("default logging installed a sink")
	}
	if _, err = Install("", true); err == nil {
		t.Fatal("verbose logging without a destination was accepted")
	}
}

func TestFileErrorsAreActionableButArbitraryErrorTextIsNotLogged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error.log")
	close, err := Install(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close() })
	Debug("test", "hidden debug", Fields{})
	Error("local-config", "open", &os.PathError{Op: "open", Path: "/config", Err: syscall.EACCES}, Fields{Path: "/config", Session: "abcdefghijkl-secret"})
	Error("rpc", "turn/start", errors.New("bearer PRIVATE_TOKEN prompt PRIVATE_PROMPT reply PRIVATE_REPLY"), Fields{Code: -32000})
	if err := close(); err != nil {
		t.Fatal(err)
	}
	r := records(t, path)
	if len(r) != 2 || r[0]["level"] != "ERROR" || r[0]["path"] != "/config" || r[0]["error"] != syscall.EACCES.Error() || r[0]["session"] != "abcdefghijkl…" || r[1]["code"] != float64(-32000) {
		t.Fatal("diagnostic context missing", r)
	}
	data, _ := os.ReadFile(path)
	for _, forbidden := range []string{"PRIVATE_TOKEN", "PRIVATE_PROMPT", "PRIVATE_REPLY", "abcdefghijkl-secret", "hidden debug"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("unsafe or unrequested logging", forbidden)
		}
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("log permissions too broad", info.Mode())
	}
}

func TestVerboseDeduplicatesAndIsConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error.log")
	close, err := Install(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close() })
	Debug("coverage", "local fallback", Fields{Session: "one"})
	var workers sync.WaitGroup
	for range 50 {
		workers.Go(func() { Error("config", "stat", os.ErrPermission, Fields{Path: "/config"}) })
	}
	workers.Wait()
	if got := records(t, path); len(got) != 2 || got[0]["level"] != "DEBUG" {
		t.Fatal("missing verbose context or repeat spam", got)
	}
	l := active.Load()
	l.mu.Lock()
	for key, value := range l.seen {
		value.at = value.at.Add(-2 * time.Minute)
		l.seen[key] = value
	}
	l.mu.Unlock()
	Error("config", "stat", os.ErrPermission, Fields{Path: "/config"})
	if err := close(); err != nil {
		t.Fatal(err)
	}
	got := records(t, path)
	if len(got) != 3 || got[2]["suppressed_repeats"] != float64(49) {
		t.Fatal("suppressed-repeat summary missing", got)
	}
}

func TestLogSizeIsBoundedAndLatestEntrySurvives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error.log")
	f, err := openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.limit = 64
	for range 20 {
		if _, err := f.Write([]byte("{\"msg\":\"old record\"}\n")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.Write([]byte("{\"msg\":\"latest record\"}\n")); err != nil {
		t.Fatal(err)
	}
	info, _ := f.Stat()
	if info.Size() > f.limit {
		t.Fatal("log grew beyond its limit", info.Size())
	}
	got := records(t, path)
	if got[len(got)-1]["msg"] != "latest record" {
		t.Fatal("latest diagnostic was lost", got)
	}
}

func TestLogAppendsAcrossRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error.log")
	for _, operation := range []string{"first run", "second run"} {
		close, err := Install(path, false)
		if err != nil {
			t.Fatal(err)
		}
		Error("test", operation, os.ErrPermission, Fields{})
		if err := close(); err != nil {
			t.Fatal(err)
		}
	}
	got := records(t, path)
	if len(got) != 2 || got[0]["msg"] != "first run" || got[1]["msg"] != "second run" {
		t.Fatal("log was overwritten instead of appended", got)
	}
}

func TestInstallNormalizesExistingLogWithoutWritingRecords(t *testing.T) {
	for _, size := range []int{maxBytes - 1, maxBytes, maxBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "error.log")
			original := strings.Repeat("x", size)
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			close, err := Install(path, false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { close() })
			// No records are emitted: installation itself must enforce the cap.
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := original
			if size > maxBytes {
				want = ""
			}
			if string(got) != want {
				t.Fatalf("installed log size = %d; want %d with original content preserved below the cap", len(got), len(want))
			}
			if err := close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNormalizedLogTracksSizeAndAppendsAtStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error.log")
	if err := os.WriteFile(path, make([]byte, maxBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.size != 0 {
		t.Fatal("oversized log retained stale tracked size", f.size)
	}
	const record = "{\"msg\":\"after normalization\"}\n"
	if _, err := f.Write([]byte(record)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != record || f.size != int64(len(record)) {
		t.Fatal("normalized log did not restart at byte zero", len(got), f.size)
	}
}

func TestInvalidTargetsFailWithoutChangingThem(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, false); err == nil {
		t.Fatal("directory accepted as log")
	}
	if _, err := Install(filepath.Join(dir, "missing", "error.log"), false); err == nil {
		t.Fatal("inaccessible parent accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	target, link := filepath.Join(dir, "important"), filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(link, false); err == nil {
		t.Fatal("symlink accepted as log")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "keep me" {
		t.Fatal("symlink target changed")
	}
}

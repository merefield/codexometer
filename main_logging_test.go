package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/ui"
	"github.com/merefield/codexometer/internal/web"
)

func TestLoggingFlagsAndLaunchFailures(t *testing.T) {
	for _, browser := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "error.log")
		deps := dependencies{
			startUI: func(ui.Fetcher, time.Duration, bool, int, int, bool) error {
				return errors.New("PRIVATE_UPSTREAM_BODY")
			},
			startWeb: func(web.Source, time.Duration, int, io.Writer, bool, bool) error {
				return errors.New("PRIVATE_UPSTREAM_BODY")
			},
		}
		args := []string{"--demo", "--error-log", path, "--verbose"}
		if browser {
			args = append(args, "--web")
		}
		var out, stderr bytes.Buffer
		if code := run(args, &out, &stderr, deps); code != 1 {
			t.Fatal("launch failure hidden", code)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"msg":"started"`, `"level":"ERROR"`, `"msg":"stopped"`} {
			if !strings.Contains(string(data), want) {
				t.Fatal("launch diagnostics missing", string(data))
			}
		}
		if strings.Contains(string(data), "PRIVATE_UPSTREAM_BODY") {
			t.Fatal("upstream body leaked to log")
		}
	}
	for _, args := range [][]string{{"--verbose"}, {"--error-log", t.TempDir()}, {"--error-log", filepath.Join(t.TempDir(), "missing", "log")}} {
		var out, stderr bytes.Buffer
		if run(args, &out, &stderr, dependencies{}) == 0 || stderr.Len() == 0 {
			t.Fatal("invalid logging flags did not fail clearly", args)
		}
	}
}

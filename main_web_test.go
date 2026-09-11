package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/ui"
	"github.com/merefield/codexometer/internal/web"
)

func TestLaunchModesAreIsolated(t *testing.T) {
	for _, webMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal unchanged", true: "web read only"}[webMode], func(t *testing.T) {
			t.Setenv("DIGBENCH_API_TOKEN", "private-digbench")
			t.Setenv("OPENAI_API_KEY", "private-key")
			terminal, browser, discovery := 0, 0, 0
			deps := dependencies{
				startUI: func(_ ui.Fetcher, refresh time.Duration, inline bool, threshold, hours int) error {
					terminal++
					if refresh != time.Minute || inline || threshold != 80 || hours != 72 {
						t.Fatal("terminal defaults changed")
					}
					return nil
				},
				startWeb: func(source web.Source, refresh time.Duration, port int, output io.Writer) error {
					browser++
					if refresh != time.Minute || port != 54321 {
						t.Fatal("web options missing")
					}
					client, ok := source.(codex.Client)
					if !ok || client.BenchmarkAPIKey != "" || client.DigBenchToken != "" {
						t.Fatal("web received action credentials")
					}
					if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("DIGBENCH_API_TOKEN") != "" {
						t.Fatal("child credentials not scrubbed")
					}
					return nil
				},
				listDigBenchGames: func(context.Context, string) ([]string, error) { discovery++; return nil, nil },
			}
			args := []string{}
			if webMode {
				args = []string{"--web", "--web-port", "54321"}
			}
			var out, err bytes.Buffer
			if code := run(args, &out, &err, deps); code != 0 {
				t.Fatalf("%d %s", code, &err)
			}
			if webMode {
				if browser != 1 || terminal != 0 || discovery != 0 {
					t.Fatal("web invoked terminal/action discovery")
				}
			} else if terminal != 1 || browser != 0 || discovery != 1 {
				t.Fatal("normal launch changed")
			}
		})
	}
}

func TestWebFlagsAndErrors(t *testing.T) {
	for _, args := range [][]string{{"--web-port", "1"}, {"--web", "--web-port", "-1"}, {"--web", "--web-port", "65536"}, {"--web", "--inline"}, {"--web", "--check-auth"}, {"--web", "--digbench-game", "x"}} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err, dependencies{}); code != 2 {
			t.Fatalf("%v: %d %s", args, code, &err)
		}
	}
	var out, err bytes.Buffer
	deps := dependencies{startWeb: func(source web.Source, _ time.Duration, _ int, _ io.Writer) error {
		if _, ok := source.(*demoFetcher); !ok {
			t.Fatal("web demo not isolated")
		}
		return errors.New("listen failed")
	}}
	if code := run([]string{"--web", "--demo"}, &out, &err, deps); code != 1 || !strings.Contains(err.String(), "listen failed") {
		t.Fatalf("%d %s", code, &err)
	}
	out.Reset()
	err.Reset()
	if code := run([]string{"--web", "--version"}, &out, &err, dependencies{}); code != 0 {
		t.Fatal("version started a server")
	}
}

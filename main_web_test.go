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
	"github.com/merefield/codexometer/internal/confirmation"
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
				startUI: func(_ ui.Fetcher, refresh time.Duration, inline bool, threshold, hours int, _ confirmation.Mode) error {
					terminal++
					if refresh != time.Minute || inline || threshold != 80 || hours != 72 {
						t.Fatal("terminal defaults changed")
					}
					return nil
				},
				startWeb: func(source web.Source, refresh time.Duration, port int, output io.Writer, control bool, _ confirmation.Mode) error {
					browser++
					if refresh != time.Minute || port != 54321 || control {
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
	var flagOut, flagErr bytes.Buffer
	if code := run([]string{"--web-control"}, &flagOut, &flagErr, dependencies{}); code != 2 {
		t.Fatal("control allowed without web")
	}
	for _, args := range [][]string{{"--web-port", "1"}, {"--web", "--web-port", "-1"}, {"--web", "--web-port", "65536"}, {"--web", "--inline"}, {"--web", "--check-auth"}, {"--web", "--digbench-game", "x"}} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err, dependencies{}); code != 2 {
			t.Fatalf("%v: %d %s", args, code, &err)
		}
	}
	var out, err bytes.Buffer
	deps := dependencies{startWeb: func(source web.Source, _ time.Duration, _ int, _ io.Writer, _ bool, _ confirmation.Mode) error {
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

func TestWebControlExplicitOptIn(t *testing.T) {
	var out, err bytes.Buffer
	deps := dependencies{startWeb: func(_ web.Source, _ time.Duration, _ int, _ io.Writer, control bool, _ confirmation.Mode) error {
		if !control {
			t.Fatal("explicit control flag lost")
		}
		return nil
	}}
	if code := run([]string{"--web", "--web-control", "--demo"}, &out, &err, deps); code != 0 {
		t.Fatalf("%d %s", code, &err)
	}
}

func TestConfirmationFlagIsLaunchScopedAndDoesNotEnableWebWrites(t *testing.T) {
	for _, browser := range []bool{false, true} {
		for _, wanted := range []confirmation.Mode{confirmation.Balanced, confirmation.None, confirmation.Require} {
			seen := false
			deps := dependencies{
				startUI: func(_ ui.Fetcher, _ time.Duration, _ bool, _, _ int, mode confirmation.Mode) error {
					seen = true
					if browser || mode != wanted {
						t.Fatal("wrong terminal policy", mode, wanted)
					}
					return nil
				},
				startWeb: func(_ web.Source, _ time.Duration, _ int, _ io.Writer, writable bool, mode confirmation.Mode) error {
					seen = true
					if !browser || writable || mode != wanted {
						t.Fatal("confirmation policy enabled writes or lost mode", mode, wanted)
					}
					return nil
				},
			}
			args := []string{"--demo"}
			if browser {
				args = append(args, "--web")
			}
			if wanted != confirmation.Balanced {
				args = append(args, "--confirmation", string(wanted))
			}
			var out, stderr bytes.Buffer
			if code := run(args, &out, &stderr, deps); code != 0 || !seen {
				t.Fatal(code, stderr.String())
			}
		}
	}
}

func TestInvalidConfirmationFlagsFailBeforeLaunch(t *testing.T) {
	for _, args := range [][]string{{"--confirmation", "no"}, {"--confirmation", ""}, {"--no-confirm"}, {"--require-confirm"}} {
		var out, stderr bytes.Buffer
		if code := run(args, &out, &stderr, dependencies{}); code != 2 || stderr.Len() == 0 {
			t.Fatal(args, code, stderr.String())
		}
	}
}

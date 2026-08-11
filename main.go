package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/ui"
)

type demoFetcher struct{}

func (demoFetcher) Fetch(context.Context) (codex.Snapshot, error) {
	return codex.DemoSnapshot(), nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, defaultDependencies()))
}

type dependencies struct {
	checkAuth func(context.Context, string) (codex.Snapshot, error)
	startUI   func(ui.Fetcher, time.Duration, bool) error
}

func defaultDependencies() dependencies {
	return dependencies{
		checkAuth: func(ctx context.Context, binary string) (codex.Snapshot, error) {
			return (codex.Client{Binary: binary}).Fetch(ctx)
		},
		startUI: startUI,
	}
}

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	flags := flag.NewFlagSet("codexometer", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		codexPath    = flags.String("codex", "codex", "path to the Codex CLI")
		refresh      = flags.Duration("refresh", time.Minute, "quota refresh interval")
		demo         = flags.Bool("demo", false, "show the UI with simulated quota data")
		inline       = flags.Bool("inline", false, "render inline instead of using the alternate screen")
		checkAuth    = flags.Bool("check-auth", false, "verify access to the current Codex login and exit")
		printVersion = flags.Bool("version", false, "print the version and exit")
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *printVersion {
		fmt.Fprintln(stdout, "codexometer "+codex.Version)
		return 0
	}
	if *checkAuth {
		snapshot, err := deps.checkAuth(context.Background(), *codexPath)
		if err != nil {
			fmt.Fprintln(stderr, "Codex auth check failed:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Codex auth OK // %d quota window(s) online\n", len(snapshot.Meters()))
		return 0
	}

	var fetcher ui.Fetcher = codex.Client{Binary: *codexPath}
	if *demo {
		fetcher = demoFetcher{}
	}

	if err := deps.startUI(fetcher, *refresh, *inline); err != nil {
		fmt.Fprintln(stderr, "codexometer:", err)
		return 1
	}
	return 0
}

func startUI(fetcher ui.Fetcher, refresh time.Duration, inline bool) error {
	options := []tea.ProgramOption{tea.WithMouseAllMotion()}
	if !inline {
		options = append(options, tea.WithAltScreen())
	}
	_, err := tea.NewProgram(ui.New(fetcher, refresh), options...).Run()
	return err
}

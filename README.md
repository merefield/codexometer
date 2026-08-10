# Codexometer

Codexometer is a small, retro terminal dashboard for your current Codex quota.
Keep it open in a second terminal window or pane and you can see every active
usage window, its remaining capacity, and its reset time without repeatedly
opening `/status` in your working Codex session.

```text
█▀▀ █▀█ █▀▄ █▀▀ ▀▄▀ █▀█ █▀▄▀█ █▀▀ ▀█▀ █▀▀ █▀█
█▄▄ █▄█ █▄▀ ██▄ █ █ █▄█ █ ▀ █ ██▄  █  ██▄ █▀▄
◉ QUOTA TELEMETRY CONSOLE // CRT-01 // SIGNAL LOCKED
```

![Codexometer Hacker theme showing quota and reset-cycle gauges](assets/codexometer.png)

_Hacker theme with simulated quota data._

Codexometer refreshes once a minute by default. It is read-only: it displays
quota information and never starts a model turn or consumes a reset credit.

## Why use it?

Codex already exposes quota information through `/status`, but that view lives
inside the session you are using. Codexometer is designed as a companion
display:

```text
┌──────────────────────────────┬──────────────────────────┐
│ Codex session                │ Codexometer              │
│                              │                          │
│ Editing, reviewing, coding   │ 5-hour window      62%  │
│                              │ Weekly window      37%  │
│ No need to interrupt work    │ Next reset     02:17:00  │
└──────────────────────────────┴──────────────────────────┘
```

It works particularly well in:

- another Windows Terminal tab or split pane;
- a second Terminal/iTerm window on macOS;
- a tmux, Zellij, or terminal-multiplexer pane;
- an Ubuntu terminal beside the Codex CLI.

## What it shows

- Every rate-limit bucket and window returned by the current Codex account.
- Used and free percentages for each window.
- The duration of each window, such as five hours or one week.
- A live countdown and local clock time for each reset.
- On every style, a separate reset-cycle gauge comparing elapsed window time
  with quota consumed. It uses the same active colour as its quota meter.
- The current ChatGPT plan when Codex supplies it.
- Available earned reset credits when present.
- Online, refreshing, stale-data, and error states.
- A countdown to the next automatic refresh.

Codexometer does not assume that every account has the same windows. Some
accounts expose a shorter rolling window and a weekly window; plans and backend
configuration can differ. The UI renders whatever the current Codex account
actually returns.

## Requirements

- The `codex` CLI installed and available on `PATH`.
- A current ChatGPT login in Codex.
- A modern terminal with ANSI color and Unicode support.

Go is required only when installing from source. A compiled Codexometer binary
does not require a Go runtime.

## Install

Once a release has been published, Go users can install directly:

```sh
go install github.com/merefield/codexometer@latest
```

To build the current checkout:

```sh
git clone https://github.com/merefield/codexometer.git
cd codexometer
go build -trimpath -ldflags="-s -w" -o codexometer .
```

Then move `codexometer` somewhere on your `PATH`, or run it directly from the
project directory.

## Quick start

Start the dashboard:

```sh
codexometer
```

Confirm that Codexometer can use the prevailing Codex login without opening
the interface:

```sh
codexometer --check-auth
```

Preview all UI features using simulated quota data:

```sh
codexometer --demo
```

## Authentication and privacy

Codexometer starts `codex app-server` as a short-lived child process and asks
its account API for the current rate limits. The child inherits the prevailing
environment, including `CODEX_HOME`, so it uses the same ChatGPT account and
credential-refresh behavior as the installed Codex CLI.

Codexometer deliberately does not:

- read or copy Codex token files;
- implement a separate OAuth flow;
- store access or refresh tokens;
- send credentials to another service;
- invoke a model merely to discover quota information.

If you use a nonstandard Codex executable, pass it explicitly:

```sh
codexometer --codex /path/to/codex
```

## Controls

| Key | Action |
| --- | --- |
| `t` | Cycle color themes |
| `s` | Cycle meter styles |
| `r` | Refresh quota data immediately |
| `q` | Quit |
| `Esc` | Quit |
| `Ctrl+C` | Quit |

Theme and style changes are immediate and do not trigger a network refresh.

## Themes

Press `t` to cycle:

1. **Hacker** — the default green CRT telemetry console.
2. **Rust** — a warm amber monitor with weathered brown shadows.
3. **Blue Steel** — cool blue instruments on a dark slate background.
4. **Ultraviolet** — purple phosphor with magenta highlights and plum shadows.
5. **Nightshade** — vivid royal-purple instruments on a deep plum screen.

The default remains the original green hacker-terminal presentation.

## Meter styles

Press `s` to cycle:

1. **Bars** — chunky quota bars, with one full-width rate-limit window per row.
2. **Pie** — clockwise-filled circles rendered on a 2×4 sub-cell Braille canvas
   for clean curves at any size.
3. **Consumption Pace** — a signed horizontal scale comparing elapsed window
   time with quota consumed. Positive headroom means consumption is behind
   elapsed time; a negative deficit means quota is being used too quickly.
4. **Fuel Tank** — a reverse gauge whose bright segment shows remaining range
   and whose dark segment shows consumed capacity, labelled from Empty to Full;
   one full-width tank appears per row. Its reset-cycle comparison also drains
   backward and aligns exactly with the tank's first and last inner cells.

The layout responds to both terminal dimensions and the number of rate limits
returned by Codex. Header, status, errors, footer, and meter grid divide the
available rectangle proportionally. Bars, Consumption Pace, and Fuel Tank flow
one meter per row.
Meter rows always use identical heights; indivisible spare rows become quiet
space above the footer instead of stretching one quota block more than another.
Pie uses at least two columns when multiple limits exist, adding rows when that
preserves more radial detail and adding columns when the terminal is wide
enough. Consumption Pace calculates `elapsed window % - quota used %`, placing
under-budget consumption on the positive side and over-budget consumption on
the negative side. Every style also shows a `RESET CYCLE` comparison:
its label and countdown occupy one line, while its progress bar occupies a
separate line with the same width and active colour as the main visualization.
Its percentage is elapsed time from the calculated window start
(`reset - duration`) to the next reset. Every visualization
receives its card's remaining width and height, and resizing the terminal
immediately reflows and rescales it. The underlying values and reset information
never change with presentation.

## Options

```text
--codex PATH       path to the Codex CLI (default: codex)
--check-auth       verify the current Codex login and exit
--demo             use simulated quota data
--inline           render inline instead of using the alternate screen
--refresh DURATION refresh interval (default: 1m)
--version          print the version and exit
```

Examples:

```sh
# Refresh every 30 seconds
codexometer --refresh 30s

# Keep output in terminal scrollback rather than using a full-screen buffer
codexometer --inline

# Use a separately installed Codex build
codexometer --codex ~/bin/codex
```

## Platform builds

Codexometer uses pure Go and cross-compiles without CGo. Each operating system
and CPU architecture needs its own executable.

```sh
mkdir -p dist

GOOS=darwin  GOARCH=arm64 go build -trimpath -o dist/codexometer-darwin-arm64 .
GOOS=darwin  GOARCH=amd64 go build -trimpath -o dist/codexometer-darwin-amd64 .
GOOS=linux   GOARCH=arm64 go build -trimpath -o dist/codexometer-linux-arm64 .
GOOS=linux   GOARCH=amd64 go build -trimpath -o dist/codexometer-linux-amd64 .
GOOS=windows GOARCH=arm64 go build -trimpath -o dist/codexometer-windows-arm64.exe .
GOOS=windows GOARCH=amd64 go build -trimpath -o dist/codexometer-windows-amd64.exe .
```

The destination machine needs Codex installed and logged in, but it does not
need Go or Codexometer's source dependencies.

## How refresh works

At startup and on each refresh, Codexometer:

1. starts `codex app-server --stdio`;
2. performs the app-server initialization handshake;
3. requests `account/rateLimits/read`;
4. renders every returned limit bucket and window;
5. shuts down the short-lived app-server process.

Automatic refreshes occur once a minute unless `--refresh` changes the
interval. Pressing `r` refreshes immediately. If a refresh fails after valid
data has already been displayed, Codexometer retains the last snapshot and
marks it as stale instead of blanking the dashboard.

## Troubleshooting

### `Codex CLI not found`

Confirm that `codex --version` works in the same shell. Otherwise use
`--codex PATH`.

### Authentication check fails

Run:

```sh
codex login status
codexometer --check-auth
```

Codex rate-limit data requires a ChatGPT-backed Codex login. An API-key login
uses API billing and rate limits instead of ChatGPT subscription windows.

### Colors or symbols look wrong

Use a terminal with true-color and Unicode support, such as Windows Terminal,
the current macOS Terminal, iTerm2, or a modern Linux terminal. Ensure the
selected font includes block, arrow, and emoji glyphs.

### The terminal is too small

Codexometer adapts its header and meter widths, but rich gauges need enough
rows to display every quota window. Increase the pane height or return to the
compact default bar style with `s`.

## Development

Format, test, and vet the project:

```sh
gofmt -w .
go test ./...
go vet ./...
```

Measure test coverage:

```sh
go test -cover ./...
```

Codexometer uses:

- Go 1.22+
- Bubble Tea for the terminal event loop
- Lip Gloss for adaptive ANSI styling
- Codex app-server JSON-RPC for authenticated quota data

## License

Codexometer is available under the [MIT License](LICENSE).

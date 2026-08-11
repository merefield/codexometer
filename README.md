# Codexometer

Codexometer is a small, retro terminal dashboard for your current
[Codex](https://github.com/openai/codex) quota.
Keep it open in a second terminal window or pane and you can see every active
usage window, its remaining capacity, and its reset time without repeatedly
opening `/status` in your working Codex session.

```text
█▀▀ █▀█ █▀▄ █▀▀ ▀▄▀ █▀█ █▀▄▀█ █▀▀ ▀█▀ █▀▀ █▀█
█▄▄ █▄█ █▄▀ ██▄ █ █ █▄█ █ ▀ █ ██▄  █  ██▄ █▀▄
◉ QUOTA TELEMETRY CONSOLE // CRT-01 // SIGNAL LOCKED
```

![Codexometer Hacker theme showing quota and reset-cycle gauges](assets/codexometer.png)

_Hacker theme showing Codex and GPT-5.3-Codex-Spark quota windows._

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
- An optional Stopwatch view that measures token activity observed across local
  Codex sessions between Go and Stop, with a live 30-second bar chart.

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

The Stopwatch additionally reads locally persisted Codex rollout files under
`$CODEX_HOME/sessions` (normally `~/.codex/sessions`). It scans only for
`token_count` telemetry records and decodes only their token totals and
timestamps; message text, reasoning, commands, tool results, and credentials are
ignored and never retained by Codexometer.

If you use a nonstandard Codex executable, pass it explicitly:

```sh
codexometer --codex /path/to/codex
```

## Controls

| Key | Action |
| --- | --- |
| `t` | Cycle color themes |
| `Tab` | Select the next meter-style tab |
| `Shift+Tab` | Select the previous meter-style tab |
| `r` | Refresh quota data immediately |
| `s` | Start the Stopwatch recorder (Stopwatch view only) |
| `p` | Stop and take a final up-to-date reading (Stopwatch view only) |
| `q` | Quit |
| `Esc` | Quit |
| `Ctrl+C` | Quit |

The responsive tab rail below the account status selects meter styles by mouse,
`Tab`, or `Shift+Tab`; labels condense automatically as the terminal narrows.
The footer presents the remaining actions as clickable buttons. Move the pointer
over a tab or button to highlight it, or click it to activate it. Controls pulse
briefly when activated by mouse or keyboard. The keyboard assignments remain
available in terminals without mouse support. Theme and tab changes are
immediate and do not trigger a network refresh.

## Themes

Press `t` to cycle:

1. **Hacker** — the default green CRT telemetry console.
2. **Rust** — a warm amber monitor with weathered brown shadows.
3. **Blue Steel** — cool blue instruments on a dark slate background.
4. **Ultraviolet** — purple phosphor with magenta highlights and plum shadows.
5. **Nightshade** — vivid royal-purple instruments on a deep plum screen.

The default remains the original green hacker-terminal presentation.

## Meter styles

Select a tab with the mouse, `Tab`, or `Shift+Tab`:

1. **Bars** — chunky quota bars, with one full-width rate-limit window per row.
2. **Stopwatch** — establishes a zero baseline across locally active Codex
   sessions when you press Go. A large readout follows newly appended token
   telemetry once per second and shows total observed tokens, elapsed time, and
   average rate; clickable Go and Stop controls sit beside it. The full-width
   plot adds one thin vertical block bar every 30 seconds for tokens observed in
   that interval. New bars enter on the right and push older bars left, and the
   Y axis automatically expands or contracts to fit the visible samples. Stop performs
   an immediate final local read instead of relying on the latest graph sample.
3. **Pie** — clockwise-filled circles rendered on a 2×4 sub-cell Braille canvas
   for clean curves at any size.
4. **Consumption Pace** — a signed horizontal scale comparing elapsed window
   time with quota consumed. Positive headroom means consumption is behind
   elapsed time; a negative deficit means quota is being used too quickly.
5. **Fuel Tank** — a reverse gauge whose bright segment shows remaining range
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

The Stopwatch is deliberately separate from the percentage gauges: no token
ceiling is exposed for those quota windows, so a percentage-based bar would be
misleading. It follows the local token telemetry underlying Codex's live
[`thread/tokenUsage/updated`](https://developers.openai.com/codex/app-server)
data, rather than the delayed account activity
summary. A separate process cannot subscribe to another Codex process's
app-server connection, so Codexometer incrementally observes the equivalent
`token_count` records written to local rollout files.

This is local activity telemetry, not account-wide billing data. It can combine
multiple sessions using the same local `CODEX_HOME`, but it cannot see Codex
activity on another computer, in a different Codex home, or in a cloud session
that is not writing locally. Token totals normally appear when Codex emits usage
for a model response, not token-by-token while a response is streaming. Raw token
counts also do not reveal or reproduce the backend's quota-weighting rules, so
they should not be converted directly into the percentage gauges.

## Options

```text
--codex PATH       path to the Codex CLI (default: codex)
--check-auth       verify the current Codex login and exit
--demo             use simulated quota data
--inline           render inline instead of using the alternate screen
--refresh DURATION refresh interval (default: 1m)
-v, --version      print the version and exit
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

## Versioning

Codexometer follows semantic versioning; the current source version is `v0.1.2`. The Git tag is
the release source of truth. Go automatically embeds that tag in binaries built
with `go install github.com/merefield/codexometer@v0.1.2`; direct source builds
fall back to the maintained value in `internal/version/version.go`.

Both forms report the embedded version and exit without starting the interface:

```sh
codexometer -v
codexometer --version
```

Release automation can override the source-build fallback without editing code:

```sh
go build -ldflags="-s -w -X github.com/merefield/codexometer/internal/version.Fallback=0.1.2" .
```

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

While Stopwatch is running it checks appended local token telemetry once per
second and rolls the observed deltas into 30-second graph buckets. These reads do
not contact OpenAI or invoke a model. Pressing Stop performs one immediate final
local read and forces complete session discovery, including Codex sessions
resumed from older rollout directories.

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

### Stopwatch remains at zero

The Stopwatch observes rollout telemetry under the same `CODEX_HOME` visible to
the Codexometer process. Confirm that the Codex session doing work is local and
uses that home. A native Windows Codex session and a native Windows Codexometer
normally share the same user profile; WSL and native Windows have different
homes unless `CODEX_HOME` is deliberately shared. Cloud activity and sessions on
other machines are not visible. Usage is generally appended after a model
response reports its token totals, so a currently streaming response may not
appear until its next telemetry event.

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
- Local Codex rollout `token_count` records for live Stopwatch telemetry

## License

Codexometer is available under the [MIT License](LICENSE).

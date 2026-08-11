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

Codexometer refreshes once a minute by default. Its quota dashboard is
read-only. The optional Benchmark tab starts model turns only when you
explicitly click its run button; those trials consume Codex quota.

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
- An optional Monitor view that measures token activity observed across local
  Codex sessions between Go and Stop, with a live 30-second bar chart.
- An opt-in deterministic coding benchmark comparing every visible Codex model
  and supported reasoning effort by correctness, elapsed time, token use, and
  estimated standard API-equivalent cost.

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

The Monitor additionally reads locally persisted Codex rollout files under
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
| `s` | Start recording (Monitor view only) |
| `p` | Stop and take a final up-to-date reading (Monitor view only) |
| `b` | Run the selected challenge (Benchmark view only) |
| `a` | Arm, then confirm, Run All (Benchmark view only) |
| `[` / `]`, `Left` / `Right` | Select the previous or next benchmark challenge |
| `f` | Show all, passed, or failed benchmark results |
| `Page Up` / `Page Down` | Scroll through Benchmark result pages |
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
2. **Monitor** — establishes a zero baseline across locally active Codex
   sessions when you press Start. A large readout follows newly appended token
   telemetry once per second and shows total observed tokens, elapsed time, and
   average rate; clickable Start and Stop controls sit beside it. The full-width
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
6. **Benchmark** — runs a selected hermetic coding challenge, or the complete
   four-challenge suite, against every visible model and each reasoning effort
   that model advertises. Results arrive sequentially in a table with task,
   pass/fail, wall time, tokens, and estimated standard API-equivalent cost.
   Filter the table to all, passed, or failed trials. Scroll a long result matrix
   with Page Up, Page Down, or the mouse wheel. Click any column-heading button
   to sort by that field; click it again to reverse the order.

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

The Monitor is deliberately separate from the percentage gauges: no token
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

### Deterministic benchmark

The Benchmark tab discovers the current account's visible models and their
supported reasoning efforts through `model/list`. Select a challenge with the
arrow buttons, then press `b` or click **Run Selected** to run it against every
model/effort combination. **Run All** runs all four challenges against every
combination. Because that means `challenge count × model/effort count` model
turns, Codexometer displays the exact total and requires a second confirmation
within five seconds. A fresh, ephemeral, read-only app-server thread is used for
each trial, so benchmark history does not clutter normal Codex sessions. The
turns still consume the same account quota shown by Codexometer.

Each model/effort trial has a five-minute deadline. If an in-flight turn reaches
that deadline, Codexometer requests `turn/interrupt`, waits for the matching
`turn/completed` event, records that combination as `FAIL`, and continues with
the remaining combinations. Explicit user cancellation, app-server transport
failure, or failure to confirm interruption still stops the suite because the
server's state is then unsafe or unknown.

#### Challenges

Every trial asks the model to return one named Starlark function:

| Challenge | Required behavior | Verification set |
| --- | --- | --- |
| **Merge Ranges** | Sort inclusive integer ranges and merge every overlapping or adjacent pair into a canonical union. It must handle empty input, duplicates, nesting, negatives, and arbitrary order. | 8 hand-written edge cases + 48 reproducibly generated cases |
| **LRU Cache** | Process integer `put` and `get` operations, update recency, evict the least-recently-used entry, and return both get results and final entries in most-recently-used order. Capacity zero is valid. | 5 hand-written edge cases + 40 reproducibly generated cases |
| **Expression** | Evaluate tokenized non-negative integers with `+`, `-`, `*`, parentheses, normal precedence, and left associativity—without `eval`. | 8 hand-written edge cases + 40 reproducibly generated expressions |
| **Shortest Path** | Return the minimum four-direction move count through a rectangular blocked/open grid, or `-1` when no route exists. | 5 hand-written edge cases + 40 reproducibly generated mazes |

#### How PASS and FAIL are decided

There is no LLM judge and no subjective scoring. Codexometer loads the returned
function into its embedded Starlark interpreter, runs every case for that
challenge, computes the expected result with a separate Go reference
implementation, and compares the values exactly. It also snapshots the input
and rejects a solution that mutates it.

A row is `PASS` only when all of the following are true:

- the turn completes and returns the required strict JSON object containing
  Starlark source;
- the source loads, defines the correctly named callable, and stays within the
  64 KiB source and 250,000 execution-step limits;
- every hand-written and generated case returns the exact reference answer with
  the required type and bounded shape; and
- the turn does not emit a tool-use item; and
- none of the supplied inputs are mutated.

Any syntax/runtime error, timeout, malformed response, wrong type or value,
mutation, safety/size-limit violation, or failed case produces `FAIL`. The first
failure is retained as the row's diagnostic. The test data is deterministic, so
the same Codexometer version judges every model/effort combination identically.

Starlark is a deliberately small, Python-like embedded language. Every prompt
includes the same compact language contract—available statements and built-ins,
plus the absence of `while`, recursion, imports, `load`, and `eval`—to reduce
advantage from prior syntax familiarity. It cannot remove that advantage
entirely: these results measure algorithmic coding through Starlark and may
favor models stronger at Python-like languages. They are not a language-neutral
measure of general model quality.

Codexometer links the Starlark interpreter into the standalone binary and
exposes no filesystem, process, network, clock, or environment capabilities to
submitted code. Restricted return types and bounded result sizes add further
containment.

#### Interpreting token and API-equivalent figures

Codexometer asks the local app-server for its opt-in `rawResponse/completed`
telemetry and, when a complete valid ledger is available, sums the exact usage
reported for each upstream response in the turn. Because that event is an
internal experimental Codex interface, older app-servers may reject or omit it;
Codexometer then falls back automatically to the final cumulative
`thread/tokenUsage/updated` value for the fresh one-turn thread. Both event types
are matched to the expected thread and turn IDs so activity from another trial
or Codex session is not included.

Before displaying either source, Codexometer checks that all token fields are
non-negative, cached plus cache-write input does not exceed total input,
reasoning output does not exceed output, total equals input plus output, and
cumulative updates never regress. When both complete raw-response and cumulative
telemetry are present, their totals must agree. A valid cumulative total can
stand in for an omitted raw usage payload; otherwise missing, duplicate, or
inconsistent telemetry displays `N/A`. It is never silently converted to zero
or clamped into a plausible value, and the status panel retains the reason.

The displayed total includes all reported input tokens—including cached input
and cache-write input—and all reported output tokens. Reasoning tokens are
already included in the output-token total and are not added a second time.

`API EQ` is an estimated **standard, short-context, text-token API equivalent**,
not a bill, a ChatGPT subscription charge, or a prediction of how much account
quota the turn consumed. Codexometer separates ordinary input, cached input,
cache-write input, and output, then applies the per-million-token prices known
to this Codexometer release. Usage availability and price availability are
tracked separately: a valid token total can still have `API EQ` shown as `N/A`
for an unknown model or a token class whose price was not published when the
release was built. Codexometer does not inherit or guess such a price. Pricing
can change after a binary is released; consult the
[official OpenAI API pricing page](https://developers.openai.com/api/docs/pricing)
for current values.

The figures are useful for comparing these particular observed trials, but
they have important limitations:

- They do not reveal the private quota-weighting rules used by ChatGPT plans,
  and should not be converted into quota percentages or treated as dollars
  actually charged.
- Prompt-cache state can depend on earlier activity and benchmark order. A
  later trial may receive cheaper cached input or incur a cache write that an
  otherwise identical trial would not, so observed API-equivalent cost is not
  a cache-neutral ranking.
- Current costing applies short-context rates to the trial's aggregate usage.
  It does not implement long-context price thresholds. Although supported Codex
  versions provide per-response usage, the event does not associate a distinct
  model price with each response.
- If Codex reroutes a turn, usage is priced using the final reported model. A
  turn that actually spans differently priced models cannot be reconstructed
  exactly without a response-to-model association.
- Tool use is prohibited for these hermetic trials. If a tool-use item is
  observed, the row is forced to `FAIL` and `API EQ` is `N/A`, even when valid
  text-token telemetry was also reported.
- Exact raw-response telemetry is an internal experimental app-server facility
  and may change independently of Codexometer. The validated cumulative path is
  retained for compatibility, but it does not preserve a response-by-response
  ledger.
- Model-specific Codex instructions and tool descriptions are part of reported
  input usage. That is appropriate when comparing the real Codex experience,
  but it is not a measurement of the challenge prompt in isolation.

PASS/FAIL evaluation is independent of these measurements: incomplete or
ambiguous token telemetry does not make an incorrect program pass, and a valid
program can still have an unavailable or approximate cost.

#### Measurement hardening status and guidance

The measurement path is deliberately fail-closed. Its current hardening status
is:

| Priority | Safeguard | Status |
| --- | --- | --- |
| **P0** | Distinguish missing telemetry from a genuine observed zero | Complete |
| **P0** | Reject negative, inconsistent, regressing, or overflowing token data | Complete |
| **P0** | Track token availability independently from price availability and retain the reason for `N/A` | Complete |
| **P1** | Prefer a validated per-response ledger, with a validated cumulative compatibility fallback | Complete |
| **P1** | Detect prohibited tool-use items, force the trial to `FAIL`, and invalidate `API EQ` | Complete |
| **P1** | Apply the correct pricing tier to long-context responses | Deferred |
| **P2** | Reduce cache-order bias with balanced warm-ups, randomized ordering, or repeated trials | Open |
| **P2** | Report a cache-neutral comparison alongside the observed cached cost | Open |
| **P2** | Price mixed-model reroutes from a response-to-model association | Open; the current raw event does not expose that association |
| **P2** | Record pricing-table provenance and make stale compiled pricing conspicuous | Open |
| **P2** | Add explicit compatibility diagnostics for future experimental-event schema changes | Partial; automatic cumulative fallback is already implemented |

Future accounting changes should preserve these rules:

- Never treat absent or invalid telemetry as zero, and never clamp malformed
  fields into a plausible value.
- Validate individual responses, overflow-safe aggregates, cumulative
  monotonicity, and raw-versus-cumulative agreement before setting usage as
  available.
- Keep correctness, usage availability, and cost availability as independent
  states. An unavailable price must not erase a valid token count, and a
  measurement problem must not change the deterministic Starlark verdict.
- Prefer exact response telemetry only when response IDs are present and unique;
  retain the cumulative path for compatible older app-servers.
- Do not infer prices for unknown models or unpublished token classes. Update
  the compiled table only from published OpenAI pricing and record its source
  and effective date when provenance support is added.
- Treat any tool-use item as a benchmark protocol violation. Text-token pricing
  alone cannot represent separately priced or externally executed work.
- Cover missing fields, invalid invariants, integer overflow, duplicate events,
  event regression, source disagreement, tool use, and experimental-protocol
  fallback in tests. Keep race-enabled CI green on Linux, macOS, and Windows.

Correct long-context and reroute costing will require each upstream response to
be associated with the model and pricing tier that actually served it, including
the exact threshold semantics. Cache-neutral or repeated-trial reporting would
improve comparison quality without changing the deterministic PASS/FAIL
verifier.

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

Codexometer follows semantic versioning; the current source version is `v0.2.1`. The Git tag is
the release source of truth. Go automatically embeds that tag in binaries built
with `go install github.com/merefield/codexometer@v0.2.1`; direct source builds
fall back to the maintained value in `internal/version/version.go`.

Both forms report the embedded version and exit without starting the interface:

```sh
codexometer -v
codexometer --version
```

Release automation can override the source-build fallback without editing code:

```sh
go build -ldflags="-s -w -X github.com/merefield/codexometer/internal/version.Fallback=0.2.1" .
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

While the Monitor is recording it checks appended local token telemetry once per
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
rows to display every quota window. Increase the pane height or use `Tab` to
return to the compact default bar style.

### Monitor remains at zero

The Monitor observes rollout telemetry under the same `CODEX_HOME` visible to
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

- Go 1.25+
- Bubble Tea for the terminal event loop
- Lip Gloss for adaptive ANSI styling
- Starlark for deterministic, hermetic benchmark-code evaluation
- Codex app-server JSON-RPC for authenticated quota data
- Local Codex rollout `token_count` records for live Monitor telemetry

## License

Codexometer is available under the [MIT License](LICENSE).

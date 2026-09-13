# Codexometer

[![Version](https://img.shields.io/github/v/tag/merefield/codexometer?sort=semver&label=version)](https://github.com/merefield/codexometer/tags)
[![CI](https://github.com/merefield/codexometer/actions/workflows/ci.yml/badge.svg)](https://github.com/merefield/codexometer/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/merefield/codexometer)](go.mod)
[![License](https://img.shields.io/github/license/merefield/codexometer)](LICENSE)

Codexometer is a retro terminal dashboard and **session command centre** for
[Codex](https://github.com/openai/codex). Keep it open beside your working
sessions to follow their activity, see which need attention, and keep quota
and reset timing visible without repeatedly opening `/status`.

The **Sessions** tab brings your sessions into one place: read their latest context,
compare token activity, and—with the [shared local app-server setup](#recommended-codex-cli-setup)—
answer supported questions, review and confirm command approvals, and send
follow-ups to idle sessions. Quota views, account usage history and optional
model benchmarks round out the dashboard. Ordinary CLI sessions retain
best-effort local monitoring; interactive controls require live server support.

```text
█▀▀ █▀█ █▀▄ █▀▀ ▀▄▀ █▀█ █▀▄▀█ █▀▀ ▀█▀ █▀▀ █▀█
█▄▄ █▄█ █▄▀ ██▄ █ █ █▄█ █ ▀ █ ██▄  █  ██▄ █▀▄
◉ QUOTA TELEMETRY CONSOLE · VERSION <CURRENT>
```

![Codexometer Hacker theme showing quota and reset-cycle gauges](assets/codexometer.png)

_Hacker theme showing Codex and GPT-5.3-Codex-Spark quota windows._

Codexometer refreshes quota once a minute by default. Passive quota monitoring
and the Usage history tab are read-only and do not start model turns. Sessions
previews are passive too; explicitly sending a follow-up starts a Codex turn
and consumes model usage. Redeeming
a banked quota reset is an explicit, separately confirmed action. Benchmark
runs also require an explicit action and consume Codex quota or API-billed tokens,
depending on the selected authentication.

The four primary tabs are **Quota**, **Sessions**, **Usage**, and **Benchmark**.
The interface supports mouse controls, keyboard navigation, five colour themes,
and [16 languages with 17 locale options](#language), with the original UK English presentation
unchanged by default.

An opt-in [experimental browser interface](#experimental-browser-interface)
provides read-only Quota, Sessions and Usage views with `codexometer --web`.
Add `--web-control` explicitly to enable supported session approvals and prompts
from full-page session detail; other browser views remain read-only.
The terminal remains the default and the full-featured command centre.

## Why use it?

Codex already exposes quota information through `/status`, but that view lives
inside the session you are using. Codexometer adds a companion display and a
single place to check on several sessions—then respond to the selected session
without searching through terminal tabs when live interaction is available:

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

## Language

UK English (`en-GB`) is the default. Its existing labels, numbers, spacing,
hotkeys and layouts are preserved; this feature does not redesign the English UI.
To select another interface language, set `CODEXOMETER_LANG` before starting:

| Language | Code |
| --- | --- |
| English (UK) | `en-GB` |
| Dutch | `nl` |
| German | `de` |
| French | `fr` |
| Italian | `it` |
| Spanish | `es` |
| Russian | `ru` |
| Japanese | `ja` |
| Chinese (Simplified) | `zh-Hans` |
| Swedish | `sv` |
| Norwegian (Bokmål) | `nb` |
| Turkish | `tr` |
| Estonian | `et` |
| Finnish | `fi` |
| Portuguese (Brazil) | `pt-BR` |
| Portuguese (Portugal) | `pt-PT` |
| Danish | `da` |

Codes use BCP 47 language tags. Regional variants such as `de-DE`, `fr-CA`,
`ja-JP`, `zh-CN`, `sv-SE`, `nb-NO`, `tr-TR`, `et-EE` and `fi-FI` match the
corresponding supported language. `no` and `no-NO` also select Norwegian Bokmål.
Bare `pt` selects Brazilian Portuguese (`pt-BR`). Use `pt-PT` for European
Portuguese, with its own wording, number formatting and plural rules.
Regional tags such as `pt-AO` and `pt-MZ` match the European catalogue.
`da-DK` and other Danish variants select Danish.
Other English variants use the existing UK English presentation. An unset, invalid or
unsupported code falls back to UK English. `LANG` and `LC_ALL` are deliberately
not used to choose the UI language: the default stays English unless you opt in.

Try a language for one launch:

```sh
CODEXOMETER_LANG=fr codexometer
CODEXOMETER_LANG=ja codexometer --demo
CODEXOMETER_LANG=sv codexometer
CODEXOMETER_LANG=nb codexometer --demo
CODEXOMETER_LANG=pt-BR codexometer
CODEXOMETER_LANG=pt-PT codexometer
CODEXOMETER_LANG=da codexometer --demo
```

### Retain the language setting

The recommended persistent configuration is a user environment variable. No
extra Codexometer config file or language pack is needed, and upgrades retain
your choice. Codexometer does not edit your shell profile or persist an override
from a one-off launch.

**Bash / Zsh (Linux and macOS):** add the following line to `~/.bashrc` or
`~/.zshrc`, then open a new terminal:

```sh
export CODEXOMETER_LANG=de
```

**Fish:** `set -Ux CODEXOMETER_LANG de`

**Windows PowerShell:** set a user environment variable for future terminals,
and optionally set it in the current session too:

```powershell
[Environment]::SetEnvironmentVariable('CODEXOMETER_LANG', 'de', 'User')
$env:CODEXOMETER_LANG = 'de'
```

**Windows cmd:** use `setx CODEXOMETER_LANG de` for future terminals and
`set CODEXOMETER_LANG=de` for the current terminal. Restart Windows Terminal
if an existing terminal process still supplies the old environment. WSL has
its own shell environment: configure it using the Linux instructions.

Set `en-GB` explicitly (or remove the variable) to restore English. Language is
selected at startup, so restart Codexometer after changing it.

### Translation boundaries and maintenance

The interface uses Go's `golang.org/x/text/language`, `message`, `catalog` and
CLDR plural rules, with JSON catalogues embedded using `go:embed`. The compiled
binary remains standalone and does not download translations. Text is translated
before measuring terminal-cell widths, so translated buttons share their layout
with mouse hit targets. Existing keyboard shortcuts remain unchanged; compact
tab/button abbreviations may retain Latin letters to keep those keys recognisable.

Localisation covers the dashboard's navigation, controls, labels and status
messages. Product/model/theme names, protocol IDs, CLI flag names and technical
units are not renamed. Benchmark challenges, verifier rules and model transcripts
are not translated: changing prompts could bias benchmark comparisons. Raw
backend/diagnostic messages and messages without a translation remain in English.
Compact API-EQ restart-reason codes (such as `ACCOUNT`, `RESET` and `REBASED`)
also remain consistently English; estimate labels and full confidence labels are
translated, while compact confidence codes remain `L`/`M`.
API-equivalent amounts remain USD, regardless of language. These are initial
translations; native-language corrections are welcome.
Non-English history views use unambiguous ISO dates; the established English
date formatting, wording, colours and spacing are unchanged. CLI help and
diagnostic output retain English in this initial localisation pass.

To maintain translations, edit `internal/i18n/locales/<code>.json`. Keys are the
original English messages; preserve formatting placeholders and parenthesised
hotkeys. New presentation strings should use `i18n.Text` (literal text) or
`i18n.Format` (formatted messages) before layout. Quota-window plural forms live
in `units.json`. The tests check catalogue parity, placeholder/hotkey integrity,
language matching, cross-language layouts and click targets, and a byte-for-byte
English rendering baseline captured from v0.12.0.

## What it shows

- Every rate-limit bucket and window returned by the current Codex account,
  plus the effective monthly credit limit when supplied.
- Used and free percentages for each window.
- The duration of each window, such as five hours or one week.
- A live countdown and local clock time for each reset.
- On every Quota view, a separate reset-cycle gauge comparing elapsed window time
  with quota consumed. It uses the same active colour as its quota meter.
- A learned, Fast-aware API-equivalent estimate for primary Codex windows of both
  quota consumed and inferred 100% capacity, including a range, sample count,
  and deliberately conservative confidence level.
- The current ChatGPT plan when Codex supplies it.
- Spend-control hard stops and available account-credit balance when supplied.
- Available earned reset credits when present.
- Online, refreshing, stale-data, and error states, with the limiting window
  named in warning states and a celebratory fresh-reset signal at 0% usage.
- A countdown to the next automatic refresh.
- Confirmed redemption of available banked quota resets, normally offered only
  when a displayed window is at least 80% consumed (configurable), or a known
  available reset expires in less than 72 hours (configurable). A dedicated Quota → Resets view
  remains accessible below the usage threshold.
- Account token history in a daily activity grid, weekly bars, or a cumulative
  graph, with a 6/12-month range and lifetime, peak-day, and streak summaries
  when supplied. This server-side history can lag live local telemetry.
- An always-on Sessions view that measures local token activity while Codexometer
  is running, with a Reset button and a `p` hotkey for Pause/Resume.
  Each independent local root session gets its own metrics and 30-second graph;
  explicitly linked spawned agents are included with their root.
- Dismissible session rows that automatically return on fresh activity,
  without closing or modifying the underlying Codex session.
- Highlighted per-session attention badges. A shared Codex app-server supplies
  exact `INPUT NEEDED` and `APPROVAL NEEDED` states. Without it, a completed
  open turn is definite `INPUT NEEDED`; an otherwise active session with no
  rollout activity for three minutes is cautiously labelled `CHECK SESSION`.
  For a root with linked agents, fresh activity from any member suppresses that
  uncertain fallback; definite input or approval signals still propagate from
  the member that raised them.
- An opt-in deterministic coding benchmark comparing a selectable scope of
  visible Codex models and supported reasoning efforts by correctness, elapsed
  time, token use, and estimated standard API-equivalent cost. The current trial appears immediately
  as an `IN PROGRESS` row; select or click any row to inspect its benchmark-only
  prompt, structured response, verifier outcome, and telemetry.

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

The release installer downloads the pre-built binary for the current operating
system and architecture, verifies its published SHA-256 checksum, confirms the
binary reports the requested version, and then installs it. Go is not required.

On macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/merefield/codexometer/main/install-release.sh | sh
```

The default destination is `/usr/local/bin`; the installer uses `sudo` only
when that directory is not writable. To install without elevation:

```sh
curl -fsSL https://raw.githubusercontent.com/merefield/codexometer/main/install-release.sh | \
  CODEXOMETER_BIN_DIR="$HOME/.local/bin" sh
```

To install a specific release, replace `vX.Y.Z` with its tag and add
`CODEXOMETER_VERSION=vX.Y.Z` beside the bin-directory setting, or download the
script and pass `--version vX.Y.Z`.

On Windows, download and run the PowerShell installer:

```powershell
$installer = Join-Path ([IO.Path]::GetTempPath()) "install-codexometer.ps1"
Invoke-WebRequest https://raw.githubusercontent.com/merefield/codexometer/main/install-release.ps1 -OutFile $installer
Set-ExecutionPolicy -Scope Process Bypass -Force
& $installer
Remove-Item $installer
```

The execution-policy override applies only to that PowerShell process. Inspect
the downloaded script before running it if required by your security policy.

It installs into `%LOCALAPPDATA%\Programs\codexometer\bin` by default. Override
that with `CODEXOMETER_BIN_DIR` or `-BinDir`; use `-Version vX.Y.Z` to select a
release. Both installers print a reminder if the destination is not already on
`PATH`. Re-running the same command safely upgrades or reinstalls Codexometer.

The installer scripts are ordinary text files in this repository and can be
downloaded and inspected before execution.

### Install from source

Developers with a current Go toolchain can build and install from source:

```sh
go install github.com/merefield/codexometer@latest
```

`go install` builds locally and places the executable in `GOBIN`, or in the
`bin` directory under `GOPATH` when `GOBIN` is empty. That directory must be on
`PATH`.

To build the current checkout:

```sh
git clone https://github.com/merefield/codexometer.git
cd codexometer
make build
```

Use `go build -trimpath -o codexometer .` directly if Make is unavailable; on
Windows, use `-o codexometer.exe`. Confirm any installation with:

```sh
codexometer --version
```

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

## Recommended Codex CLI setup

Codexometer works with an ordinary Codex CLI process, but connecting your CLI
sessions through one shared app-server unlocks its most accurate live
telemetry on macOS, Linux, and WSL:

- **Definite attention states** — Sessions can distinguish `INPUT NEEDED` from
  `APPROVAL NEEDED` using live per-thread status instead of eventually showing
  the cautious `CHECK SESSION` inactivity fallback.
- **Command approval details and controls** — supported live requests show the
  command, directory and reason in the Sessions tab's full detail page, with
  buttons matching Codex's offered decisions, with confirmation for grants. Incomplete or
  unsupported requests still need answering in Codex.
- **Resolved-model API equivalents** — live model-reroute and response-usage
  events let Codexometer price a positively matched call using the model that
  actually served it, rather than relying only on the requested model saved in
  the rollout.
- **Better multi-session visibility** — every connected CLI tab or pane remains
  a separate session while sharing the same accurate status source;
  explicitly linked subagents are still folded into their root session.
- **No extra Codexometer authentication** — the server, CLI clients, and
  Codexometer continue to use the prevailing Codex login under the same
  `CODEX_HOME`.
- **Safe degradation** — sessions not connected to the server continue to use
  all core quota features with requested-model pricing, rollout lifecycle
  signals, and writer-lock attention detection.

Set up the recommended arrangement as follows.

1. Confirm that the current Codex CLI and Codexometer see the same
   login and `CODEX_HOME`:

   ```sh
   codex --version
   codexometer --check-auth
   ```

2. Choose **one** way to start the shared server. Both use the default Unix
   socket that Codexometer detects; do not run both against that socket.

   **Option A: use your existing installation (no reinstall).** In a dedicated
   terminal, run:

   ```sh
   codex app-server --listen unix://
   ```

   Leave that terminal and server running while using connected sessions.
   This runs in the foreground; it does not need the managed standalone install.

   **Option B: managed background daemon.** This requires the installer-managed
   standalone binary at `$CODEX_HOME/packages/standalone/current/codex`
   (normally `~/.codex/packages/standalone/current/codex`). An npm, Homebrew or
   manually built executable alone does not satisfy that requirement. If you
   want this option, install the managed binary first:

   ```sh
   curl -fsSL https://chatgpt.com/codex/install.sh | sh
   ```

   Then start the daemon and confirm that it is ready:

   ```sh
   codex app-server daemon start
   codex app-server daemon version
   ```

   If you see **managed standalone Codex install not found**, either complete
   that installation or use Option A instead.

3. Launch each working Codex CLI terminal against the server's default Unix
   control socket:

   ```sh
   codex --remote unix://
   ```

   Run this client command separately in every terminal tab or pane that
   Codexometer should monitor through the shared server. Use `/resume` inside
   each connected CLI to reopen an existing conversation.

   Starting the server does not interrupt or migrate existing ordinary CLI
   sessions. To move a conversation, let its current task finish, exit that
   ordinary CLI, then resume it through the connected client. Avoid opening
   the same conversation in both modes at once. Unmigrated sessions remain
   available to Codexometer through local observation, without live approvals.

4. Start Codexometer in an adjacent window or split pane before beginning work
   that you want attributed:

   ```sh
   codexometer
   ```

   Starting it first matters because transient model-reroute events cannot be
   reconstructed after the fact. No additional Codexometer option is required;
   it automatically probes the same default socket under `CODEX_HOME`.

   If the latest Codexometer build is already running, leave it open: it retries
   the connection and discovers newly connected sessions automatically. Allow
   a refresh cycle. Restart it if you have replaced its binary or changed its
   `CODEX_HOME`. A context detail marked **LIVE** came from the shared server;
   **LOCAL** means that excerpt came from rollout logs, which do not persist
   the live approval-request events. An older local excerpt can remain until
   new live context arrives.

5. Leave Codexometer running while you work. Session monitoring starts automatically;
   use Reset when you want a fresh measured interval, and keep unrelated Codex
   activity quiet while running Benchmarks if you want the cleanest comparisons.

Codexometer subscribes only to thread IDs that are already loaded by the
shared server; it does not load unrelated historical sessions.

For Option B, manage or stop the daemon with:

```sh
codex app-server daemon restart
codex app-server daemon stop
```

For Option A, stop the foreground server with `Ctrl+C` in its dedicated
terminal. Stopping or restarting either server disconnects its clients and
can interrupt their work; finish active tasks first.

The managed daemon lifecycle is currently experimental, Unix-only, and expects
the standalone Codex installation. Native Windows and other ordinary CLI
sessions remain fully usable through the fallback described above. The
app-server also supports a local WebSocket listener, including on Windows:

```sh
codex app-server --listen ws://127.0.0.1:4500
codex --remote ws://127.0.0.1:4500
```

Plain WebSockets should be used only on localhost or through an SSH tunnel.
Codexometer currently auto-detects only the default Unix control socket, so
WebSocket-connected sessions use its fallback attention detection for now. See
the official [Codex app-server documentation](https://developers.openai.com/codex/app-server)
for custom socket paths, secure remote connections, and authentication.

## Authentication and privacy

Codexometer starts `codex app-server` as a short-lived child process and asks
its account API for the current rate limits and account identity. The identity
is immediately reduced to the process-local fingerprint described below. The
child inherits the prevailing environment, including `CODEX_HOME`, so it uses
the same ChatGPT account and credential-refresh behavior as the installed Codex
CLI.

Codexometer deliberately does not:

- read or copy Codex credential/token files;
- implement a separate OAuth flow;
- store access or refresh tokens;
- send Codex credentials to another service;
- invoke a model merely to discover quota information.

Sessions and the observed quota estimator additionally read locally persisted
Codex rollout files under `$CODEX_HOME/sessions` (normally
`~/.codex/sessions`). They decode `token_count` totals, each last response's
input/cache/cache-write/output counts, requested model name, timestamps, and
content-free turn timing,
plus the minimum session metadata needed for grouping: thread ID, parent thread
ID, source classification, working directory, and the inherited-history
boundary. It also reads lifecycle event names and blocking flags to identify an
explicit unresolved input or approval request. To distinguish an open CLI
waiting at its prompt from a closed historical session, it inspects the lock
state—not the contents—of Codex's per-thread writer lock. Sessions also extracts
bounded assistant replies/commentary, input questions and choices, approval
reasons, and command descriptions for its session-context previews. Excerpts
stay in process memory, never in preferences or a summarisation service. User
prompts, reasoning and arbitrary tool results are not retained by that reader.
Text may contain sensitive material the assistant already displayed: terminal
sanitisation is not secret redaction. Press `h` in Sessions to hide previews and
close the detail view; this saved preference controls display, not collection.

Benchmark turns explicitly started by Codexometer have separate content capture.
Their Codexometer-authored policy and prompt, visible
structured response, and—only for DigBench—sanitized game-tool requests and
responses are kept in bounded process memory for the Benchmark run detail view.
Reasoning events, platform instructions, credentials, request headers,
temporary paths, local scratch-work commands, and internal thread, turn, call,
or response IDs are not captured.

When the managed shared daemon is available, Codexometer also keeps a local
app-server subscription for already-loaded thread IDs. From that stream it
retains runtime flags, model-reroute/token-usage correlations, and bounded
context from assistant messages and input/approval requests. Resolved requests
and new turns clear stale pending context. Observation alone never starts a turn
or answers a question. Explicit actions in Sessions can submit follow-up turns
to idle sessions, answer supported blocking questions, and send selected command
decisions (with confirmation for permission grants). Drafts stay in memory;
submitted text enters Codex's normal session history and follow-ups consume
model usage. Nothing is sent automatically; see the Sessions controls below.
Without a shared daemon, previews use
available local rollout text rather than fetching full thread histories.
Missing request details are left unavailable; there is no extra model call.

If you use a nonstandard Codex executable, pass it explicitly:

```sh
codexometer --codex /path/to/codex
```

## Controls

| Key | Action |
| --- | --- |
| `t` | Cycle color themes |
| `Tab` | Select the next top-level tab: Quota, Sessions, Usage, or Benchmark |
| `Shift+Tab` | Select the previous top-level tab |
| `r` | Refresh account history in Usage; otherwise refresh quota data |
| `v` | Cycle the active Quota view |
| `s` | Reset the Sessions baseline, or open Benchmark Scope |
| `p` | Pause or resume live monitoring (Sessions view only) |
| `h` | Reset all Sessions rows to graph-only / split detail-and-graph, closing full detail and clearing individual row choices |
| `Left` / `Right` | In Sessions, less / more detail for the selected session: graph ↔ split ↔ wide ↔ full screen; stops at either end |
| `b` | Run the selected benchmark scope (Benchmark view only) |
| `a` | Arm, then confirm, Run All (Benchmark view only) |
| `x` | Dismiss the selected Sessions row; close Benchmark detail/Scope, or stop an active suite and retain its incomplete trial |
| `d` | Select Daily in Usage, or close the Benchmark Scope screen |
| `6` / `1` | Select 6/12 months of Usage history |
| `[` / `]` | Select the previous or next benchmark suite |
| `Left` / `Right` | Page Usage history, or select the previous or next benchmark suite |
| `f` | Show all, passed, or failed benchmark results |
| `w` | Select Weekly in Usage, or cycle Cost, Balanced, and Speed benchmark ranking weights |
| `Up` / `Down` | Select a session row or Benchmark row, or scroll open Benchmark detail |
| `Enter` / `Space` | Open a selected Benchmark result, or toggle a Scope checkbox; in Sessions full detail, `Enter` focuses an available reply editor or submits its text |
| `c` | Select Cumulative in Usage, copy the Benchmark result matrix as Markdown, or copy the complete open run detail |
| `l` | Clear accumulated Benchmark results while no suite is running |
| `Page Up` / `Page Down` | Page Usage history, session rows, or Benchmark results |
| `q` | Quit |
| `Esc` | Step back one Sessions context level, cancel quota-reset confirmation/dismiss its notice, return from Benchmark detail or Scope; otherwise quit |
| `Ctrl+C` | Quit |

The responsive top rail below the account status selects Quota, Sessions, Usage, or
Benchmark by mouse, `Tab`, or `Shift+Tab`. Quota adds a second rail for Bars,
Consumption Pace, Pie, Fuel Tank, and Resets; select these with the mouse or cycle them
with `v`. Codexometer remembers the selected Quota view when you leave
and return. Both rails condense automatically as the terminal narrows.
The footer presents the remaining actions as clickable buttons, including View
only while Quota is active. Move the pointer over a tab or button to highlight
it, or click it to activate it. Controls pulse briefly when activated by mouse
or keyboard. The keyboard assignments remain available in terminals without
mouse support. Theme and tab changes are immediate and do not trigger a network
refresh. Theme, Quota view, benchmark result filter, and benchmark ranking
weight are restored on the next launch.

Click the Codexometer title/logo to return directly to **Quota → Bars** (this
also becomes the remembered Quota view). The title has no external hyperlink.
The version number links to its release highlights
(development builds link to their base release; prerelease tags are preserved).
Use your terminal's hyperlink gesture (usually
Ctrl-click; some terminals use Cmd-click or a context menu). The version is
underlined on hover and uses a standard OSC 8 hyperlink; Codexometer does not
launch a browser process. Your terminal must support hyperlinks, including
when running over SSH or in WSL. Existing tab and button shortcuts are unchanged.

### Quota health signal

The top-right signal keeps `ONLINE` in the selected theme color while its dot
and quota-health label use fixed semantic colors. Warning labels identify the
meter responsible, for example `5 HOURS // WATCH`, `MONTHLY // NEAR`, or
`SPEND // EXHAUSTED`:

- green `RESET FRESH // GO!` — every returned meter currently reports 0% used;
- green `QUOTA CLEAR` — consumption is keeping pace with, or trailing, elapsed
  window time;
- blue `QUOTA WATCH` — the current average burn rate would exhaust at least one
  quota window before it resets;
- amber `LIMIT NEAR` — excess pace projects exhaustion within the first quarter
  of the time still remaining, or no more than 5% remains while over pace;
- red `QUOTA EXHAUSTED` — a window is at 100%, Codex explicitly reports its
  rate limit reached, or account spend control reports a hard stop.

Codexometer reports the worst state among all returned rolling windows and the
effective monthly credit limit. If reset timing or cycle duration is
unavailable, it conservatively falls back to remaining capacity: Watch at 20%
and Near at 5%. The monthly limit supplies a reset time but no cycle start, so
Codexometer never invents monthly elapsed progress. Scanning and stale-signal
states take precedence while quota health cannot be evaluated reliably.

For each window with valid duration and reset data, Codexometer calculates:

- `U`, the used quota fraction (`usedPercent / 100`);
- `E`, the elapsed window fraction, using `reset time - window duration` as the
  window start;
- remaining quota `1 - U` and remaining time `1 - E`;
- projected time to exhaustion, as a fraction of the complete window:
  `E × (1 - U) / U`.

It then applies these rules in severity order:

1. **Exhausted** if `usedPercent` is 100, Codex explicitly reports that a rate
   limit has been reached, or `spendControlReached` is true.
2. **Fresh** if every returned used percentage is zero.
3. **Clear** if `U <= E`: quota consumption is no further advanced than the
   reset cycle.
4. **Limit Near** when over pace and either no more than 5% quota remains, or
   projected exhaustion is within the first 25% of the time remaining to reset.
5. **Watch** for every other over-pace window (`U > E`).

This means 10% remaining can correctly stay Clear when less than 10% of the
window remains: quota is low, but reset is closer than exhaustion at the
observed average pace. Conversely, a window with plenty remaining can reach
Watch or Limit Near if it is being consumed very early and projects exhaustion
well before reset. Percentages are clamped to 0–100 before classification. If
duration or reset data is missing or invalid, pace cannot be calculated, so the
fallback is Clear above 20% remaining, Watch at 20% or less, Limit Near at 5%
or less, and Exhausted at 0%.

### Observed quota API equivalent

Every Quota presentation also learns an **observed API-equivalent**, including
the requested Fast-mode premium where supported,
for the primary `codex` rate-limit windows. Additional/model-specific limits
show `LIMIT ATTRIBUTION UNKNOWN`, because the rate-limit API does not say which
local model calls consumed those buckets. The primary windows show two estimates:

- `SPEND` / `NOW` — the inferred API-equivalent value of the percentage
  consumed in the current window;
- `100%` / `FULL` — the inferred API-equivalent value represented by an entire
  window under the workload Codexometer observed.

These figures are not an account balance, subscription valuation, token
allowance, invoice, or claim about OpenAI's private quota formula. They answer a
narrower question: “At published API text-token prices, roughly what
would this observed mix of model work cost when mapped onto the movement in my
quota meter?”

#### Fast mode and service-tier uncertainty

Codexometer prices each newly observed response separately. Standard token cost
and the requested-tier premium are maintained independently; quota learning uses
their sum. For example, $1 at standard prices plus a $1 Fast premium over **5
percentage points** of quota movement implies roughly **$40 per 100%**, compared
with **$20 at standard prices**. It never multiplies the observed quota percentage
or applies today's `/fast` setting retroactively.

- `TIER*` means the estimate includes a **requested**, not billing-confirmed,
  Fast premium. The current Codex sources expose persisted
  `thread_settings_applied` settings, but neither local token events nor the
  shared app-server's response-usage notifications expose the actual billed tier.
  Settings are kept per session and captured for each turn; an in-flight turn
  retains its original setting when the next turn's settings change. Linked
  sub-agents keep their own attribution before their totals are combined.
- `STD?` means tier information was unavailable for some observations: those
  responses use explicitly qualified **standard-price estimates**. `TIER*?`
  means a mixture of requested Fast premiums and this unknown-tier fallback.
  Missing tier coverage caps confidence at `LOW`. Older Codex logs, inherited
  child histories without an owned settings event, and subscription benchmark
  results may lack tier evidence. Once startup finds the latest model's chunk,
  it searches up to 4 MiB of older chunks for tier settings, keeping discovery
  responsive for large histories. Records after that context do not consume
  this budget; settings outside the lookback are also unknown until a new
  settings event is observed. No global configuration is used to guess it.
- When the line is wide enough, `STD 100%` retains the **standard-price midpoint
  comparison** for the same sample windows. Narrower layouts omit that comparison.
- A known default setting has no premium. An explicitly unsupported tier/model
  combination is unpriced and interrupts clean learning rather than silently
  applying an invented multiplier. Standard benchmark rankings are unchanged.

Fast rates were verified on **10 September 2026**: **GPT-6 Astra and GPT-5.6
(Sol, Terra, Luna)** use **2× applicable standard API prices** for `fast` / the
legacy `priority` alias. The premium is applied after the existing per-response
long-context and cache-read/cache-write calculations; reasoning output is not
counted twice. Other models' Fast prices are not yet maintained. See the official
[Astra pricing](https://developers.openai.com/api/docs/models/gpt-6-astra),
[Codex speed guidance](https://learn.chatgpt.com/docs/agent-configuration/speed),
and [API Fast-mode guide](https://developers.openai.com/api/docs/guides/fast-mode).
The footer's standard-price retrieval date remains separate from this tier review.

**This is still an estimate, not a bill.** A requested Fast response can be served
at standard tier, and the observed interfaces cannot confirm that downgrade.
Codex's **2.5× subscription credit multiplier** is deliberately **not** used as
an API-dollar multiplier. Even with the Fast premium included, 100% can therefore
have a different equivalent value than a standard-only workload. Samples blend
the observed workload; changing model/tier mixtures can move the estimate while
new evidence accumulates. Future improvement: consume the actual response
`service_tier` if Codex exposes it with matching per-response token usage, then
prefer that evidence to requested settings.

Codexometer normally prices each newly completed local model call using the
requested model durably recorded in its turn context. Current Codex rollout
files do not persist transient model-reroute events, so a reroute cannot be
reconstructed from that source alone. When Codexometer and the CLI share the
managed daemon described above, Codexometer keeps a live subscription and
matches reroute and token-usage events to rollout calls by thread ID, turn ID,
cumulative token checkpoint, and the complete response token breakdown. For a
subscribed thread, costing waits for one refresh when that exact match is not
yet available; a matching notification uses the resolved model, otherwise the
call safely returns to requested-model pricing. A call awaiting that decision
is kept in a separate pending count and does not enter the cumulative priced or
unpriced totals. This makes finalized accounting monotonic; a quota observation
that overlaps pending or newly finalized accounting is deferred instead of
silently moving its learning baseline. Late attachment, disconnects,
ordinary non-daemon clients, and already historical reroutes also retain
requested-model pricing rather than being guessed; these remaining coverage
limits are one reason confidence never rises above Medium. Ordinary input, cached input,
cache-write input, and output are priced separately; requests above the
published 272,000-input-token threshold use the corresponding long-context
rates where OpenAI publishes them. Unknown models or missing price classes fail
closed as `UNPRICED MODEL MIX` rather than being guessed or treated as free.
Core, Extended, and DigBench trials use ephemeral threads that intentionally do
not appear in normal persisted session telemetry. While a subscription-funded
benchmark suite is active, Quota views replace the numeric API-equivalent
estimate with `SUBSCRIPTION BENCHMARK ACTIVE` instead of presenting partial
accounting. When each trial finishes, its authoritative benchmark usage is
folded into the same process-local accounting used for quota learning; missing
or unpriceable benchmark usage fails closed and restarts the learning anchor.
Benchmarks funded by `CODEXOMETER_BENCHMARK_API_KEY` are excluded because they
do not consume the displayed subscription quota. Benchmark threads remain
hidden from Sessions; hiding presentation does not exclude their aggregate
subscription impact from Quota views.
The embedded rates come from the
[official OpenAI API pricing page](https://developers.openai.com/api/docs/pricing)
and were retrieved on **2026-09-04**.
Every Quota presentation repeats that retrieval date and a terminal hyperlink
to the source in its footer when the terminal is wide enough, matching the
Benchmark view and making stale compiled pricing conspicuous wherever a priced
figure appears.

Each refresh brackets the account quota request with local accounting reads.
If their cost, finalized-call, or pending-call counters differ—or a call is
still pending—`OBSERVATION DEFERRED` is shown and no sample is taken, preventing
a response completed during the request from being paired with the wrong quota
snapshot. Learning starts with a stable quota percentage and cumulative local
API-equivalent cost anchor. A pause in activity does not expire or reduce clean
movement. The backend may revise an upcoming rolling-window reset timestamp;
Codexometer retains the earliest observed boundary and does not restart merely
because that future timestamp moved. Once the same window advances by at least
five displayed percentage points without a reset or an unpriced call, a sample
is calculated:

```text
central 100% estimate = observed API-equivalent cost × 100 / percentage-point movement
lower bound           = observed API-equivalent cost × 100 / (movement + 1)
upper bound           = observed API-equivalent cost × 100 / (movement - 1)
current spend range   = 100% range × current used percentage
```

The `±1` denominator reflects the integer granularity of the quota percentage.
For multiple samples the UI reports the median lower and upper bounds. One or
two clean samples are `LOW` confidence; at least three samples spanning 15 or
more percentage points can reach `MED` only when both their rounding ranges and
their central capacity estimates agree within conservative spread limits.
Confidence is intentionally capped at Medium because local telemetry cannot
prove that no other machine, cloud task, unobserved client, or server-side model
reroute outside the shared-daemon subscription also affected the account quota.

An interval is discarded and re-anchored if its earliest reset boundary passes,
used quota falls, the account or window definition changes, finalized counters
regress, the quota moves five points without any matching priced local call, or
an unknown/unpriced model occurs. The learning readout retains the reason, for
example `RESTARTED: WINDOW RESET`, `WINDOW DEFINITION CHANGED`, `LOCAL
ACCOUNTING REBASED`, `UNPRICED MODEL MIX`, or `LOCAL COVERAGE GAP`, while new
clean movement accumulates. It never silently returns to `0/5PP`. Even a valid
estimate can still vary with reasoning effort, model mix, caching, prompt
shape, and backend quota weighting, so compare ranges and sample counts rather
than treating the midpoint as a fixed entitlement.

Samples remain process-local and are never written to the preferences file, so
evidence cannot leak from one login into another on a later run. During a run,
Codexometer requests the current account email from the same local app-server,
immediately reduces it to an in-memory one-way fingerprint, and uses that only
to separate account observations. The email and fingerprint are not persisted.
If an older app-server cannot provide an account identity, the estimate fails
closed as `ACCOUNT ATTRIBUTION UNKNOWN` rather than mixing indistinguishable
accounts.

The privacy trade-off is that quitting Codexometer discards every learned
sample and quota anchor. On restart it can reconstruct cumulative priced usage
from local rollout telemetry, but the current quota percentage and cost become
a new baseline: the display returns to `LEARNING` and needs another five clean
percentage points of movement before producing an estimate. Medium confidence
must also be earned again from three qualifying samples spanning at least 15
percentage points. Frequent restarts can therefore delay an estimate
substantially, especially for a slowly moving weekly window.

At most 12 samples per account/window are retained in memory and samples older
than 45 days are ignored. No token event, model-call record, prompt, response,
session ID, email, or account ID is stored. Run `codexometer --demo`, then
refresh once with `r`, to preview a learned estimate without consuming quota.

## Themes

Press `t` to cycle:

1. **Hacker** — the default green CRT telemetry console.
2. **Rust** — a warm amber monitor with weathered brown shadows.
3. **Blue Steel** — cool blue instruments on a dark slate background.
4. **Ultraviolet** — purple phosphor with magenta highlights and plum shadows.
5. **Nightshade** — vivid royal-purple instruments on a deep plum screen.

The default remains the original green hacker-terminal presentation.

## Views and quota presentations

The top-level tabs are **Quota**, **Sessions**, **Usage**, and **Benchmark**. Within Quota,
choose one of these five views with its sub-tab or `v`:

1. **Bars** — chunky quota bars, with one full-width rate-limit window per row.
2. **Consumption Pace** — a signed horizontal scale comparing elapsed window
   time with quota consumed. Positive headroom means consumption is behind
   elapsed time; a negative deficit means quota is being used too quickly. A
   clearly labelled linear projection reports `SAFE THROUGH RESET` or estimates
   how long remains until exhaustion and how early that is relative to reset.
3. **Pie** — clockwise-filled circles rendered on a 2×4 sub-cell Braille canvas
   for clean curves at any size.
4. **Fuel Tank** — a reverse gauge whose bright segment shows remaining range
   and whose dark segment shows consumed capacity, labelled from Empty to Full;
   one full-width tank appears per row. Its reset-cycle comparison also drains
   backward and aligns exactly with the tank's first and last inner cells.
5. **Resets** — available reset credits, grant dates, expiry dates and backend
   descriptions. Expiring credits appear first, non-expiring credits last.
   Scroll with Up/Down or Page Up/Page Down when necessary.

The reset shortcut opens Resets and asks for confirmation before redeeming.
When individual credit details are supplied, Codexometer sends the ID of the
soonest-expiring available quota-reset credit it can identify. That ID remains
fixed through confirmation and any retry of an uncertain request; it never
silently switches credits. A credit that expires or disappears before a new
request is submitted requires a fresh confirmation.

An amber warning such as `⚠  RESET EXPIRES IN 2D 4H` appears immediately before
the normal `[ RESET // N ]` button when a known available reset has less than
72 hours left by default, even below `--reset-threshold`. Clicking the warning opens
**Quota → Resets** without arming confirmation or submitting a reset. It
underlines on hover, shortens on narrower terminals and moves onto an extra
row when necessary. This is an expiry
warning, not a recommendation to reset unused quota. Redemption still requires
fresh account data and explicit confirmation. The Resets view always permits
review regardless of usage percentage.

The Resets page repeats the actual countdown, for example
`FIRST EXPIRATION IN 2D 4H // within 72 hours`: the first value is the remaining time
until the earliest known expiry; the second is your configured warning lead time.

The confirmation explicitly warns that unused allowance does not carry over or
stack and that the weekly reset schedule changes. Treat an expiry warning as a
prompt to **review**, not a recommendation to redeem immediately. When expiry
information is missing or incomplete, both the inventory and confirmation say
so: no warning does not prove there is no upcoming expiry, and undisclosed
credits may expire sooner than the selected known credit.

Set the lead time with `--reset-warning-hours HOURS`: for example,
`./codexometer --reset-warning-hours 24` warns one day ahead, while
`./codexometer --reset-warning-hours 168` warns a week ahead (useful for testing
with a known later expiry). `--reset-warning-hours 0` disables expiry warnings
and their threshold bypass, without disabling the consumption-based reset
button or the Resets view. Use whole, non-negative hours. This setting does not
invent credit details or change expiry dates; it applies to the current launch.
Keep the option in your usual shell alias or launch command to retain it.

Credit details are optional and may be capped by the backend. The view shows
how many of the available credits have usable details; earliest expiry means
**earliest known**, not a guarantee about undisclosed credits. If only a count
is available, expiry is unknown and the backend chooses the credit; its default
selection order is not guaranteed by the public protocol. A supplied null expiry
means “does not expire”; an omitted expiry field means “unknown”. Credits with
unknown expiry remain visible with a disclosure, but are not selected as the
earliest known expiry. With no comparable expiries, the backend selects the credit.
Reset-credit expiry is separate from the
automatic quota-window reset date.

### Usage: account token history

**Usage** is a read-only companion to Codex CLI's `/usage` command. It fetches
`account/usage/read` through a short-lived local Codex app-server using your
prevailing ChatGPT login. No model turn is started and no reset credit is used.
It does not require the shared-daemon configuration used for live Sessions events.

- **Daily** (`d`): a GitHub-style activity grid, with seven weekday rows and
  one column per week. More tokens mean brighter theme-coloured blocks.
- **Weekly** (`w`): tokens summed into Sunday–Saturday weeks; the current week is partial.
- **Cumulative** (`c`): a running total of those weeks within the selected window,
  not the account's lifetime total.

Choose **6 months** (`6`) or **12 months** (`1`) with the range buttons. These
represent 26 or 52 Sunday-based weeks including the current partial week,
not exact calendar-month boundaries. The default is 12 months. Selection applies
instantly to all three views without refetching, resets paging, and is remembered
while switching tabs for the current run. Cumulative restarts from the beginning
of the selected range; the separately labelled lifetime summary does not change.

Daily uses five intensity levels (zero and four positive levels), scaled to
the highest daily token count across the selected range, so colours remain
comparable when paging. Future days stay blank. A Less/More legend and the peak
daily count explain the scale. All Usage views prioritise showing the full
26 or 52 weeks: compact cells/bars and reduced gaps on narrower terminals, expanding
when space allows. Daily fits a year at 60 terminal columns; Weekly/Cumulative
need 64 columns, including their vertical axis. Daily cells grow to approximately
square blocks when both width and height allow; very short terminals request
more height rather than dropping weekday rows.

Weekly and Cumulative use theme-coloured fractional block bars, automatically
scale their vertical axes to the visible values, and fit as many periods as the
terminal allows. Only terminals too narrow for the selected number of weeks need paging; these
open on the newest periods. Click **Older/Newer** or use
`←`/`→` or `Page Up`/`Page Down` to browse the rest. Daily pages by whole weeks.
Dates beneath the graph
identify daily dates or weekly start dates. Widening the terminal reveals more
periods; resizing does not change the underlying data. Period buttons support
mouse clicks and hover highlighting, and the period remains selected when
switching tabs. `Tab` and `Shift+Tab` still navigate the main tabs.

As in Codex's chart, the available display range is 52 Sunday-based weeks ending
in the current UTC week. Missing dates in a supplied history count as zero;
invalid dates, negative values, future dates, and dates outside that range are
ignored. Duplicate dates are summed. The compact summary shows the server's
separately reported lifetime tokens, peak daily tokens, and current streak when
available (`—` otherwise).

History refreshes when you enter Usage, on the normal refresh interval while
Usage is selected, or with `r` / the Refresh button. These are server-side account
statistics, **not live Sessions telemetry**: updates may lag ongoing work.
Older CLI versions or unsupported accounts can return an unavailable/error state;
missing history is never silently presented as zero. A failed refresh labels
previously fetched data **STALE**, and a detected account change discards it.

The endpoint currently exposes daily **total tokens**, not historical per-model,
input/output/cache splits, quota percentages, or dollar spend. Consequently this
tab does not infer historical API-equivalent cost or combine these totals with
the Sessions tab's local counters. History is held in memory only; restarting fetches it
again from Codex. `--demo` includes sample history for previewing the charts.

### Other top-level views

The other top-level views are:

- **Sessions** — the session command centre: inspect context, track activity,
  and respond through eligible live approval and reply controls for the selected
  session. It automatically establishes a zero baseline across locally active
  Codex sessions when Codexometer starts. The **SESSION TOTALS** readout follows newly appended
  token telemetry in a compact summary strip: **TOKENS**, visible **SESSIONS**,
  **WORKING**, **APPROVAL**, **INPUT**, and **CHECK*** counts. CHECK* means inferred
  inactivity, not a confirmed request. Linked agents are already included in
  parent sessions and are not counted again. Tokens retain the existing measurement
  baseline (including previously dismissed sessions); the session/state counts
  describe currently visible rows. Elapsed time and average rate
  remain underneath when space permits; a clickable Reset control sits beside it.
  Account-wide quota details live in Quota, not Sessions.
  During paused/unavailable observation, live state counts show **—**, not zero.
  Pause/Resume remains available
  through the `p` hotkey only, without a large Pause button taking space from
  the readout. This pauses measurement, not Codex sessions. Active sessions
  are checked once per second and the idle cadence relaxes to five seconds.
  The Sessions tab light and status label pulse between bright and dim amber
  whenever any session needs input, approval, or a check. With nothing waiting,
  they pulse green while at least one session is working and remain steady green
  while the Codex runtime is healthy but idle. They turn red only when local
  runtime health is observable and Codex is down, and remain dim while paused
  or when runtime health cannot be established. Below,
  every independent root session has a metrics box and its own graph.
  Spawned-agent descendants with an explicit Codex parent link are recursively
  aggregated into the root row and reported as `ROOT + n AGENTS`. Each row compactly shows
  model calls and latest activity, latest/peak time to first token, and
  latest/peak output size. All graphs add one thin vertical block bar on the
  same 30-second tick, after a fresh boundary read. The companion readout
  records each account quota window at the current baseline and tracks its
  observed change while monitoring. Every session row shows its exact share of
  locally observed tokens and an explicitly labelled, local-only estimate of the first quota
  window's movement, apportioned by that share. A root discovered part-way
  through an interval gets an honestly labelled partial first bar and its rate
  uses that root's own observed lifetime. New bars enter on the right, older
  bars move left, and each Y axis automatically rescales to its visible samples.
  An open root or linked child whose latest durable lifecycle event says its
  turn completed receives an amber `INPUT NEEDED` badge and border until a new
  turn starts or the CLI closes. When Codexometer finds the default shared
  Codex app-server socket, it reads the server's per-thread runtime status and
  uses the exact `waitingOnApproval` and `waitingOnUserInput` flags for
  `APPROVAL NEEDED` and `INPUT NEEDED`. If no shared server is available, an
  otherwise active open session with no new token or rollout activity for three
  minutes receives `CHECK SESSION`: a deliberately uncertain prompt that can
  also mean a long-running local tool. Subsequent activity from the root or any
  linked agent clears the uncertain group-level warning.
  Codexometer never guesses `APPROVAL NEEDED` from inactivity. Closing the CLI
  releases its per-thread writer lock and clears every attention badge.
  A session already included in the current Sessions recording can remain as an
  `IDLE` historical row so its completed metrics and graph are not discarded.
  Click the themed `[×]` in a session metrics box to hide that row for the
  current run without closing or altering the Codex session. Codexometer keeps
  collecting its telemetry while hidden and restores the row automatically
  when tokens, model calls, turn timing, durable activity, or attention moves
  forward, or when an inactive session becomes active again. An alert already
  visible when `[×]` is clicked is dismissed with its row; a later new or
  changed alert restores it. Resetting session monitoring also restores every dismissed
  row. With the keyboard, `Down` initially selects the top row, `Up` initially
  selects the bottom row, subsequent arrow presses move the highlight, and `x`
  closes the selected row.
  When the terminal cannot fit every root, use Page Up, Page Down, or the mouse
  wheel to page through the rows. Pause performs an immediate final local read
  instead of relying on the latest graph sample. Resume preserves the recorded
  totals while excluding tokens and elapsed time from the paused interval.
- **Benchmark** — runs the selected scope from the active Core, Extended, or
  conditional DigBench suite, or the active suite's complete catalog, against
  the selected or complete set of compatible model/reasoning-effort pairs.
  Results arrive sequentially in a ranked table with task, outcome, wall time,
  tokens, and estimated standard API-equivalent cost.
  Filter the table to all, passed, or failed trials. Scroll a long result matrix
  with Page Up, Page Down, or the mouse wheel. Click any column-heading button
  to sort by that field; click it again to reverse the order.

The layout responds to both terminal dimensions and the number of rate limits
returned by Codex. Header, status, errors, footer, and meter grid divide the
available rectangle proportionally. Bars, Consumption Pace, and Fuel Tank flow
one meter per row. Codexometer does not hardcode the currently returned window
set: it renders every primary and secondary window from every limit bucket,
including a 300-minute window as `5 HOURS`, plus an effective monthly credit
limit when present. When more limits arrive,
horizontal views remove decorative row gaps before compressing the cards, while
Pie adds rows or columns only when each radial card retains a useful width.
Meter rows always use identical heights; indivisible spare rows become quiet
space above the footer instead of stretching one quota block more than another.
Pie uses at least two columns when multiple limits exist, adding rows when that
preserves more radial detail and adding columns when the terminal is wide
enough. Consumption Pace calculates `elapsed window % - quota used %`, placing
under-budget consumption on the positive side and over-budget consumption on
the negative side. Its linear projection assumes the average burn observed
since the calculated cycle start continues unchanged: remaining time is
`elapsed time × (1 - U) / U`. It reports safe when the resulting exhaustion
time falls at or after reset, and hides the projection when timing is
insufficient. This is a trend estimate, not a backend forecast. Every Quota view
also shows a `RESET CYCLE` comparison:
its label and countdown occupy one line, while its progress bar occupies a
separate line with the same width and active colour as the main visualization.
Its percentage is elapsed time from the calculated window start
(`reset - duration`) to the next reset. When Codex supplies a monthly reset but
not a cycle start, the card says `CYCLE START UNAVAILABLE`, shows the known
countdown, and leaves the comparison bar unfilled. Every visualization
receives its card's remaining width and height, and resizing the terminal
immediately reflows and rescales it. The underlying values and reset information
never change with presentation.

Session monitoring is deliberately separate from the percentage gauges: no token
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

Session rows represent recently active rollout roots plus open CLI sessions
waiting for input, not a guaranteed list of every terminal process. Two
independent CLI tabs have different root thread IDs and therefore remain
separate rows.
`thread_spawn` descendants,
including nested descendants, are folded into their root by following persisted
parent IDs. Review, compact, or other internal work that lacks an explicit
parent is never guessed onto a root; if observed, it appears in an
`UNATTRIBUTED // INTERNAL` row. When Codex records inherited child history, the
Sessions honors its ownership boundary so copied parent telemetry is not counted
twice. Legacy spawned-agent rollouts without an ordinal boundary are separated
at the child session timestamp: inherited cumulative totals establish the child
counter baseline but are not reported as new usage.

Attention detection reads only content-free lifecycle metadata, per-thread
writer-lock state, and—when available—the shared app-server's runtime thread
status. A held writer lock plus a completed turn reliably identifies an open
CLI waiting at its prompt. Without a shared server, three minutes without any
new rollout-file activity produces only `CHECK SESSION`, because persisted data
cannot distinguish an approval wait from every long-running local tool. The
context preview does not infer attention from prose. A linked child's attention
state is folded into its root
so one remote Sessions row identifies the CLI session that needs intervention.
A definite approval signal takes precedence, then definite input, then the
inferred check state when linked members have mixed states. Because `CHECK
SESSION` is only an inactivity inference, fresh activity anywhere in the group
suppresses a stale sibling's check; definite input and approval are never
suppressed this way.

`CALLS` counts upstream model-response cycles observed after the current Sessions
baseline, not complete user turns. A single Codex turn can make several calls while using
tools or progressing through an agent loop. `LAST OUT` is the provider-reported
output-token count for the latest such call. `TTFT` comes from the completed
turn's persisted time-to-first-token measurement; older Codex rollouts that do
not contain it display `N/A`. Spawned descendants contribute these pulses to
the same root row as their token activity.

The per-session quota figure in Sessions is an estimate, not API attribution.
Codex exposes account-level quota percentages and local per-session token
telemetry separately; it does not report which session consumed each percentage
point. Codexometer therefore multiplies the observed account-wide change by a
session's share of locally observed tokens. This assumes that no activity from
another computer, cloud session, different `CODEX_HOME`, or otherwise invisible
client changes the account quota during the recording; if it does, its movement
cannot be separated and will contaminate the estimate. Model choice, reasoning
effort, cache behavior, and private backend weighting can also make equal token
counts affect quota differently.

The UI calls this `EST LOCAL-ONLY`, uses percentage points (`PP`), and never
presents finer precision than the whole-number quota percentage returned by
Codex. `NO INTEGER Δ` means no whole-point movement was observed, not necessarily
zero consumption; a smaller apportioned estimate is shown as `<1PP`. Stale,
missing, late-baseline, and reset-crossing windows do not produce a per-session
number.
Starting, resuming, or resetting reads quota before establishing the local token
baseline, while Pause reads local tokens before the final quota snapshot, so the
account observation brackets each monitored segment. These operations are not
atomic, so unrelated account activity during either short boundary read remains
another source of uncertainty.

#### Session context previews

Previews are visible by default in the split presentation: the space to the
right of session metrics is shared roughly equally between detail and the token
graph. The preview holds at most two text rows plus the source session and age
when height permits. When that right-hand section is too narrow to split, detail
takes priority over the graph. An empty preview shows `NO CONTEXT`; it does not
automatically change the row's chosen presentation. Each row can independently
switch between graph-only, split, expanded, and full detail as described below.

- **LAST REPLY** is the last completed assistant reply, not a new question. An
  observed local completed-turn prompt is labelled **TURN COMPLETE**.
  For shared-server sessions, **TURN COMPLETE** comes from an observed successful
  `turn/completed` event followed by idle status, independently of whether reply
  text is available or previews are hidden. Failed/interrupted turns and idle
  threads without an observed completion do not receive this label. Starting
  the next turn clears it; a working linked agent suppresses root completion,
  and actual input/approval requests take priority. Attaching after a turn
  finished may miss its live completion event; the next completed turn qualifies.
- **QUESTION** contains an observed blocking input request and any choices.
- **APPROVAL REQUEST** contains an observed approval reason/command when available.
- **LAST ACTIVITY** is observed commentary or a command, not proof that input
  is required. Ages describe the last observed event; paused readings can be stale.

Select a session with `Up`/`Down`, then use `Left` for less detail or `Right` for
more. Click the left/right half of that row's combined detail/graph area for the
same action on that session. The presentations are:

1. **Graph only:** the token graph fills the row's right-hand section.
2. **Split:** a short detail preview and token graph share that section equally;
   very narrow terminals prioritise readable detail. No approval buttons.
3. **Expanded:** that session's context occupies the full space previously shared
   by its preview and token graph, while telemetry and the other sessions stay
   visible. Its border shows **LAST REPLY**, **LAST ACTIVITY**, **QUESTION**,
   **APPROVAL REQUEST**, or **NO CONTEXT**, just like the compact preview.
   The body retains source/session details and text without repeating that heading.
   Session state belongs in the left telemetry box: its prominent
   **WORKING** badge blinks only its ball, keeping the text and colour steady,
   and uses the same observed-work evidence as the animated dots,
   not merely recent activity. Completion and attention badges take priority;
   paused/transitioning monitoring, observation errors, or inactive sessions
   suppress **WORKING**. Compact previews keep their content-type title.
   Eligible approval buttons sit below the complete command/request
   and source session. If the complete request plus controls cannot fit, a
   highlighted **APPROVAL — OPEN DETAIL →** warning appears beside the navigation
   arrows. Click it, or press `Right`, to review the request in full detail; the
   warning itself never approves anything. Narrow boxes shorten it to
   **APPROVAL →** or **!→**, prioritising the warning if the arrow pair cannot fit.
   Layout is recalculated on every render: dismissing another session or enlarging
   the terminal restores inline approval buttons as soon as the complete request
   fits, and the warning disappears. Shrinking the space hides the buttons and
   restores the warning. A live request that is no longer pending does not
   regain the warning while its old preview awaits a refresh. Unsupported requests
   can still require action in Codex.
   The selected session also offers a **FOLLOW-UP** composer here when the shared
   server confirms it is ready for input and the complete context plus composer
   fit. Only one inline composer is available at a time. Click its input line or
   press `Enter` to focus, then `Enter` to send; `Esc` leaves the editor without
   changing the row view. Drafts wrap and scroll within spare space, preserving
   the context above. Resizing to hide the composer releases keyboard focus;
   enlarge the window and refocus to continue the draft. Approval requests and
   structured questions do not use this inline composer; use full detail instead.
4. **Full detail:** the existing scrollable Sessions-area view with pinned buttons.
   Its box title mirrors the left telemetry badge (including the blinking
   **WORKING** ball), or falls back to **SESSION CONTEXT** when no badge applies.
   The body keeps content-type headings and source details, but does not repeat
   the session status. The left telemetry box is hidden in this view.
   Use Up/Down, Page Up/Down, or the mouse wheel to read long requests.
   `Left` or `Esc` returns to the Sessions overview with the same session showing
   full-width detail. When a reply editor is available,
   `Enter` focuses it instead of changing presentation.

`Left` / `Right` move along **graph ↔ split ↔ expanded ↔ full detail** without
wrapping. Each row retains its own presentation; adjusting one does not resize
other rows. The left half of the combined detail/token area reduces detail and
the right half increases it, including its borders. In full-screen detail the
left half of the background steps back; the right half stays at maximum detail.
Approval buttons and reply editors keep their own actions; clicking the telemetry
box does not change detail. `Esc`, `[×]` or `x` in full detail returns to that
expanded row. Subsequent `Left` presses step back through split and graph.
The old `i` and Enter detail-navigation shortcuts have been removed.

Clickable **[←] [→]** buttons sit at the top-right of each row's rightmost box
(graph or detail), and on the full-detail box. They mirror the cursor keys:
Left is dimmed and inactive at graph-only; Right is dimmed and inactive at full
detail. The **SESSION TOTALS** header also has **[↑] [↓]** session-selection buttons,
disabled at the first/last visible session. Dismissed sessions are skipped.
These up/down session-selection buttons disappear in full detail, where Up/Down scroll the text.
On narrow boxes, arrow buttons are omitted if they cannot fit; keyboard and
background half-click navigation still work. Hide/Show and Close retain priority.
The global `h`/Show Detail/Hide Detail control resets **all** rows to split or
graph-only, closes full detail and clears per-row overrides. Only that global
default is persisted across launches. While typing, arrows move the text cursor,
Enter submits, and `Esc` leaves the editor first. Approval controls remain
exclusive to the current target.

An amber/theme-warning **attention strip** beneath the summary provides direct
links to visible sessions with observed approval or input requests, approvals
first. Click a button to select that exact session and open full detail, including
when another session's detail is already open. It only navigates: it never sends
a decision or prompt. Switching clears the previous draft, armed confirmation
and scroll position. Clicking the session already open in full detail leaves its
draft, confirmation and scroll position intact. CHECK* sessions do not appear as confirmed requests here.
Labels include a list number (not a keyboard shortcut) and shortened directory; the full detail identifies
the target. A single navigation row is reserved even when there are no requests,
so attention arriving or clearing does not shift the session list.
**[+N →]** pages through additional sessions. Very short terminals reclaim this
row to protect session content. Paused or failed observation
suppresses these links until live readings return.

On tall terminals, full detail retains the summary and attention strip. When
space becomes limited, the summary disappears first, then the attention strip.
The budget reserves the actual approval/composer control rows and useful text
space; long drafts, wrapped controls and translated labels are taken into account.
The overview keeps its compact summary and reserves one attention row to preserve
session content. Existing footer, approval, dismiss and editor click targets use
the same layout as rendering. Keyboard navigation and approval confirmations
remain unchanged.
The initial keyboard target is the explicitly selected session, otherwise the
most recent approval-gated session with context, otherwise the first session
with context. Once expanded, the target is pinned and kept on screen: newer
approvals cannot silently switch the session behind the buttons. Clicking
another row's detail/graph area explicitly changes the target. Switching context levels
clears unsubmitted approval confirmations. Hiding previews or dismissing the
target clears its expansion.
When a row is explicitly selected, `Left`/`Right` targets that row even if it has
no context yet, rather than falling back to another session.
Moving the selection preserves other rows' chosen presentations, but clears the
previous target's pending confirmation. Only the current target can expose
approval controls, even when several rows have full-width detail. With no
selected row, the default targeting described above applies.
Outstanding requests take priority over ordinary activity when linked agents
share a root row, and the source ID identifies the actual member.

**Replies and follow-ups (shared app-server only):** the selected wide inline
row offers a follow-up composer when the complete context fits, as described
above; structured questions use full detail. Full detail reserves a
compact prompt at the bottom when a loaded session is idle (including **TURN
COMPLETE**), or has a supported blocking question. Click the input line or press
`Enter` to focus it; type your text, then press `Enter` to submit. Dashboard
hotkeys become ordinary letters while typing. `Esc` leaves the editor without
sending; press it again to return to the expanded row. Cursor editing, Unicode
and bracketed paste are supported; pasted newlines are preserved and never
submit. Text wraps automatically and the editor grows upward, reducing the
scrollable context area above it. Once it reaches the available height, the
editor scrolls internally without truncating the draft; deleting text shrinks
it again. Enter still submits rather than inserting a newline. This is a text
editor, not the Codex slash-command UI. Secret answers retain a single-line
masked password field.

For a question with multiple parts, `Enter` records each answer locally; only
after the last answer is the complete response sent. Use `↑`/`↓` to select offered
choices. Custom text is accepted only if the question permits it, and secret
answers are masked. Unsupported or ambiguous simultaneous requests remain
**REPLY IN CODEX**. Approval requests retain their explicit decision buttons;
typing is not a substitute for approval.

An idle follow-up starts a new turn in the displayed root CLI session using its
existing configuration, without changing its model, working directory or
permission policy. A pending question is answered on its exact source thread,
which can be a linked agent. The editor shows the target thread or question.
Drafts are memory-only, bounded to 4,096 characters per answer, and discarded
when leaving detail, hiding context, or when the request/connection changes.
Submitted text is sent to Codex and can become part of its normal session history.
No text is sent automatically; pressing Enter while focused is the send action.

Capabilities are one-use and connection-bound. Follow-ups recheck the live idle
state immediately before `turn/start`, and pending answers use their original
JSON-RPC request and question IDs. The protocol has no atomic “start only if this
completed turn is still current” condition, so avoid submitting in Codex and
Codexometer simultaneously. Failed or ambiguous sends are never automatically
retried: check Codex first. Local-only, disconnected, busy and very small views
do not offer an editor; use Codex itself in those cases. Codexometer does not
detect the CLI's visual keyboard-focus state or type into its terminal.

**Command approvals (shared app-server only):** the detail page shows the reason,
command and working directory. The full detail view presents distinct justification, command,
working-directory and persistent-rule sections, with themed headings, muted
metadata and blank separators. Commands retain their line breaks and are
visually marked with a vertical rail. Formatting uses validated structured
fields rather than guessing command boundaries from the justification. Legacy
or incomplete requests retain their original bounded text.

When the approval event omits the command or
directory, Codexometer associates it with the preceding command item from the
same thread, turn and item. A complete ordinary command request offers clickable
buttons matching the supported decisions Codex actually offers, in its order:

- **APPROVE ONCE** grants this command after **CONFIRM APPROVAL**.
- **ALLOW FOR SESSION** grants session-scoped approval after **CONFIRM SESSION
  GRANT**; future prompts covered by Codex's session approval cache may run
  without asking again.
- **ALWAYS ALLOW PREFIX** applies the exact displayed persistent command-prefix
  rule after **CONFIRM PERSISTENT RULE**. Future matching commands may run
  without asking again; review the displayed prefix carefully.
- **DECLINE** rejects the command but lets the agent continue the turn.
- **REJECT & STOP TURN** sends Codex's `cancel` decision: reject the command and
  interrupt the turn. This is distinct from declining or closing the detail page.

Each button appears independently; a normal Accept/Cancel prompt no longer
requires a Decline option to enable approval. Unknown decision types are not
actionable. The **OFFERED DECISIONS** line shows bounded, canonical decision
names/types for troubleshooting, not conversation content. Structured
command-prefix grants are validated and their exact payload is returned; no
decision absent from the request can be submitted. For older servers omitting
the decision list, only the legacy Accept/Cancel pair is offered.

Closing the detail page cancels an unsubmitted confirmation. Each visible
approval button has a numbered shortcut (`1`–`8`, following the offered order).
A grant shortcut selects the choice. For **APPROVE ONCE**, terminals supporting
key-release events through the [Kitty keyboard protocol](https://sw.kovidgoyal.net/kitty/keyboard-protocol/)
allow the same number again after releasing it (`1`, release,
`1` for the first option). The confirmation button displays that number; held-key
repeats cannot confirm. `C` remains an alternative, whether selected by keyboard
or mouse. Terminals without key-release support retain the displayed `C`
confirmation. Session-wide and persistent grants always require `C` or clicking
the confirmation button, not repeating their number. Armed confirmations expire
after five seconds and are cancelled by changing the target or leaving its view;
a new request requires a new confirmation. Decline
and reject/stop shortcuts act immediately, just like their buttons. Only one
session—the expanded row or full-detail target—can display approval controls
at a time. Hidden, clipped and compact controls have no active shortcuts, and
typing in the reply editor never triggers approval shortcuts.

This enhanced shortcut depends on the **terminal application and detected
keyboard protocol**, not just the operating system:

- **Windows Terminal 1.24 with Ubuntu/WSL:** uses `1` then `C`. Microsoft
  introduced Kitty keyboard-protocol support in
  [Windows Terminal Preview 1.25](https://github.com/microsoft/terminal/releases/tag/v1.25.622.0).
  Preview can be installed alongside Stable; open Ubuntu inside Preview to try
  the enhanced shortcut. No particular Stable rollout date is assumed here.
- **Apple's built-in Terminal.app:** uses `1` then `C`; it lacks Kitty
  keyboard-protocol support. The separate [kitty terminal app](https://sw.kovidgoyal.net/kitty/)
  supports the protocol on macOS. See also this
  [terminal compatibility guide](https://silvery.dev/guide/kitty-protocol#terminal-support).
- **Other terminals or intermediary layers:** follow the confirmation button's
  displayed shortcut. Codexometer only offers repeat-number confirmation after
  detecting event-type support and requires a key release between presses.

Kitty protocol support is **not required to run Codexometer or approve commands**.
If the confirmation button shows `C`, use `C` or click it; this is the safe
fallback in both inline and full-screen detail, not a missing approval feature.

Buttons wrap into rows when needed, reserve their
confirmation widths so other targets do not move, and sit in a pinned footer
below the scrolling request text, separated by one blank line when space allows.
Very short terminals omit that spacer first. Controls are hidden if the terminal
cannot fit them with room to read the request. All permission grants require
their own explicit second confirmation; confirming one choice cannot confirm another.

Actions are bound to a single pending request on the current connection. They
become unavailable when resolved elsewhere, the turn ends, or the connection
closes. The server arbitrates simultaneous responses from multiple clients.
Sending a decision is not proof that the command ran: check Codex for the outcome.
Failed or ambiguous sends are not automatically retried.

In full Sessions detail only, successful **Text sent ...** and **Decision sent ...**
notices use a subtle dot wave until the session context or attention state changes.
Fast activity updates retain the sent wording for at least three seconds before
switching to dots alone; new stop or attention-needed states take priority.
After new context arrives, the wave continues on its own while the session is
observed as working with no attention flag. Recent activity alone is not enough:
the reader must observe a working shared-server thread or a live local writer
with an ongoing turn, including linked agents. It stops on completion, input/approval
or check-session flags, inactive sessions, paused monitoring or observation errors.
Stopping the animation retains the plain successful-send acknowledgement for the
same context (or the remainder of its three-second minimum), rather than losing
delivery confirmation. New context then replaces it normally.
This is a best-effort activity indicator, not proof of execution or a progress
percentage; ordinary local observation can lag behind the session.
On the main Sessions screen, visible compact and expanded session-context boxes
also show the same activity dots at the bottom left, independently for each
session. Short boxes prioritise readable context and approval controls; the
compact view omits the dots when fewer than three body rows fit. Sent-message
acknowledgement animations remain confined to full detail.

Local rollout logs do **not** persist Codex's approval-request events, so a local
preview can show only the message preceding an approval. `INPUT NEEDED` or
`CHECK SESSION` alone never enables these controls. Requests with missing,
truncated or sanitised-away details, network approvals, file changes, permission
grants and other unsupported requests remain **REPLY IN CODEX**. The complete
eligible request is available in the scrollable detail, not just the compact
two-line preview. Unsupported requests may have only a bounded excerpt.

When controls are unavailable, the detail page explains why beside
**REPLY IN CODEX**: for example, missing command/directory or request identity,
additional permissions, network/file-change approval, no supported decisions,
truncated or sanitised text, local-only observation, or a resolved/disconnected
request. Eligible requests on small terminals instead explain that the terminal
must be enlarged. These diagnostics do not relax any approval safeguards.

The readout's `[ H: HIDE DETAIL ]`/`[ H: SHOW DETAIL ]` button or `h` toggles all
previews (shortened to `[H:HIDE]`/`[H:SHOW]` in narrow layouts); the choice
survives restarts. No excerpt is saved. Each retained excerpt is capped at 4,096
Unicode characters; startup reads only a bounded 256 KiB rollout tail, so older
context can be unavailable. Terminal escapes and control/bidirectional-formatting
characters are stripped. Previews remain in their original language and are not
LLM-generated summaries. `--demo` includes example context and a simulated
command approval choices without accessing a real conversation or executing
any command; restart the demo to reset its approval.

### Saved presentation preferences

Codexometer stores only the selected theme, main tab, Quota view, benchmark filter,
benchmark ranking weight, and the Sessions context hide/show preference.
No quota estimate or snapshot, raw session telemetry,
benchmark result, message content, credential, session ID, email, account
fingerprint, or account ID is written. The small JSON file uses the
platform-standard user configuration directory:

- Linux: `$XDG_CONFIG_HOME/codexometer/preferences.json`, normally
  `~/.config/codexometer/preferences.json`;
- macOS: `~/Library/Application Support/codexometer/preferences.json`;
- Windows: `%AppData%\codexometer\preferences.json`.

Missing, unreadable, or malformed preferences never prevent startup;
Codexometer falls back to its safe defaults. Restarting returns to your last main
tab and remembers your Quota view separately. First launch defaults to Quota →
Bars; older preferences without a main tab reopen the saved Quota view.
Restoring a tab does not resume a benchmark run or reopen an approval dialog.

### Benchmark authentication and usage boundary

> [!IMPORTANT]
> Codexometer's benchmarks are intended for a person running the local client
> with their own Codex authentication and quota. They are not a way to convert
> a ChatGPT subscription into general API traffic, re-serve model access, or
> share one person's included usage with other users. Do not deploy a shared
> benchmark service backed by one person's subscription. You remain responsible
> for complying with the terms and policies of OpenAI and each external
> benchmark provider; this project cannot guarantee that guidance or enforcement
> will remain unchanged. If you choose to run a benchmark with **Sign in with
> ChatGPT** subscription authentication, you do so at your own risk.

The justification for supporting the prevailing Codex login is that OpenAI's
[authentication documentation](https://learn.chatgpt.com/docs/auth) expressly
distinguishes **Sign in with ChatGPT** for subscription access from API-key
authentication for usage-based access. OpenAI also documents
[Codex app-server](https://learn.chatgpt.com/docs/app-server) as the interface
for embedding Codex into another product, including its ChatGPT login flows. A
[public clarification from OpenAI's Codex and ChatGPT lead](https://x.com/thsottiaux/status/2090675027670978569)
further says that using included subscription usage through **Sign in with
ChatGPT** is fine in official or OSS clients; it identifies converting a
subscription into API traffic for re-serving or sharing across users as the
unsupported pattern. That post is useful operational guidance, not a
contractual guarantee.

Codexometer stays on the client side of that boundary: it invokes the official
local Codex app-server for the authenticated user, runs bounded user-triggered
trials, and reports results locally. It does not expose an inference API,
forward ChatGPT credentials, or offer another user access to the account. For
unattended automation, CI, or a centrally hosted benchmark service, use an
appropriately owned API-key-authenticated setup instead; OpenAI recommends API
key authentication for programmatic Codex CLI workflows and the
[Codex SDK](https://learn.chatgpt.com/docs/codex-sdk) for automated jobs and CI.
The separate DigBench token described below authorizes only DigBench and does
not change how Codex itself is authenticated.

For a clearer billing boundary, supply your own OpenAI API key. A dedicated key
takes precedence over the prevailing ChatGPT login for **all** benchmark model
discovery and runs, including DigBench:

```sh
export CODEXOMETER_BENCHMARK_API_KEY="<openai-api-key>"
codexometer
```

`OPENAI_API_KEY` is accepted as a fallback when the dedicated variable is not
set. Codexometer removes both variables from its normal child environment and
passes the selected value only in the documented `account/login/start` request
to a benchmark-only app-server. That server uses a temporary `CODEX_HOME` and
ephemeral in-memory credential storage, so it neither replaces the normal Codex
login nor persists the key. API-authenticated trials use standard usage-based
API billing; quota monitoring continues to use the prevailing Codex login.
Environment variables may still be visible to other processes running as the
same operating-system user, so supply secrets only on a trusted local machine.
Neither key is written to preferences or accepted as a command-line argument.

### Experimental DigBench agentic run

Codexometer includes an experimental integration for
[DigBench](https://digbench.ai/), an external benchmark of discovering unknown
rules in interactive text games. Supplying `DIGBENCH_API_TOKEN` adds
**DIGBENCH** to the Benchmark tab's **Suite** selector alongside the always
available **CODEXOMETER CORE** and **CODEXOMETER EXTENDED** suites. At launch,
Codexometer calls
authenticated `GET /games` and uses every game name returned by DigBench; no
`P-x` catalog is compiled into Codexometer. Select the DigBench suite, open
**Scope**, and use its game, model, and reasoning-level checkboxes to check all,
clear all, or choose any subset. **Run Scope** executes every selected game
against every selected compatible model/reasoning-effort pair. **Run All**
executes every discovered game against every compatible pair. Both controls
display the exact remote-session count and require a second confirmation before
creating those sessions, because every game/pair invocation creates a persisted
remote session with a random seed.

Create a token from the [DigBench token page](https://digbench.ai/account/tokens)
(sign-in is a passwordless email link) and place it in the environment:

```sh
export DIGBENCH_API_TOKEN="<token>"
codexometer
```

The existing headless form remains available for a named exploratory game:

```sh
codexometer --digbench-game P-1
```

The published Codex condition is the default: `gpt-5.6-sol` with `high`
reasoning effort. DigBench Scope labels both sides of that exact pairing, while
`xhigh` is identified as an enhanced Codexometer experiment and not a paper
condition. Codexometer allows two hours per game by default because
discovery runs can be substantially longer than ordinary coding benchmarks.
The headless form can set another finite boundary explicitly; for example:

```sh
codexometer --digbench-game P-3 \
  --digbench-model gpt-5.6-terra \
  --digbench-effort medium \
  --digbench-timeout 12h
```

The command creates a fresh remote DigBench session, so invoking it is an
external write as well as a model run that consumes either Codex subscription
quota or usage-billed API tokens, according to the authentication choice above.
DigBench persists sessions and currently exposes no deletion endpoint. The
token is read only
from `DIGBENCH_API_TOKEN`, is never written to Codexometer preferences, and is
removed from the process environment before Codex is spawned. It is used only
by Codexometer's native HTTPS client to start the session. Codex sees only
session-scoped `get_session` and `step` tools, not the account token.

The game runs in one ephemeral Codex app-server thread with a writable temporary
workspace and no approval prompts. The model may use its normal local tools for
notes, matching DigBench's agentic-harness design, but its only game access is
the scoped dynamic-tool bridge. Step calls are safely retried using DigBench's
idempotent `step_index` protocol; session creation is never automatically
retried because that endpoint does not document idempotency.

The model-facing instructions retain Codexometer's session-isolation and useful
scratch-note guidance while following the agentic prompt published in the
[DigBench paper](https://arxiv.org/abs/2608.12593). Codex receives the API's task
description, including any objective or special-action guidance that does not
reveal the rules, plus `creative_toggle` when the state supplies it. Every move
is explicitly an observe → reason → one-action cycle: Codex is told to wait for
and inspect the authoritative result before choosing another move, including
when deliberately testing a sequence. Tool results sent back into the model are
compact game-state slices so repeated session metadata and schemas do not crowd
out useful history. This compaction does not discard game observations, legal
actions, limits, transitions, progress, or creative-mode state. The derived
`levels_beaten` score stays in the operator-facing transcript and UI rather than
being supplied to the model.

In the TUI, each selected game run appears immediately in the existing result
table and can be opened while it is in progress. Its benchmark-only detail view
shows every Codexometer-authored developer instruction, the complete game prompt
with the session ID redacted, and the dynamic-tool definitions. It then presents
each game exchange as a sanitized **Tool Request** and full **Tool Response**,
followed by the concise **Move** and authoritative **State** representation, so
the solving workflow remains readable without losing protocol detail. The full
recorded response intentionally remains richer than the compact state returned
to the model. The final
model response completes the visible
`Policy → Prompt → Tools → Tool Request → Tool Response → Move → State → Final Response`
workflow. The **Copy** control exports that complete captured view. DigBench
session IDs, credentials, request headers,
temporary paths, local scratch-work commands, internal app-server context, Codex
platform instructions, and Codex reasoning are never included. **Stop** requests
interruption of the active Codex turn, retains the captured transcript, and marks
the row stopped.

The headless command prints the selected game, model, effort, and billing source
before connecting. It then reports remote-session creation, Codex-turn startup,
authoritative level/step/status updates after successful game-tool calls, and a
content-free elapsed-time heartbeat every 15 seconds while Codex is working.

The final line reports `WIN`, `LOSS`, or `INCOMPLETE`, levels beaten, total game
steps, duration, tokens, and estimated standard API-equivalent cost. A win
requires the authoritative terminal combination `done: true` and
`state.status: "completed"`; `game_over` is a loss. Contradictory terminal
fields fail as a protocol error. DigBench assigns a random game seed and does
not currently accept a requested seed. Codexometer still assigns an
**observed-run rank** to completed DigBench rows, calculated independently from
Core and Extended results with the same correctness-first cost/time formula
described below. A win counts as a pass; a loss or incomplete attempt counts as
a failure; stopped rows remain unranked. Levels beaten, steps, and token counts
remain visible diagnostics but do not affect the rank.

This rank compares the attempts Codexometer actually observed; it is not a
controlled or definitive model ranking. Different model/effort combinations
may receive game instances whose difficulty varies with their server-assigned
seed, and the API cannot currently pair combinations on the same seed. Treat a
single run as exploratory, and use repeated runs with aggregate outcomes when
drawing broader conclusions. Rankings shown while a suite is running are also
provisional because combinations may temporarily have different numbers of
completed games.

### Deterministic benchmark

The Benchmark tab groups its built-in deterministic challenges into two
always-available suites: **CODEXOMETER CORE** contains the original Easy and
Moderate set, while **CODEXOMETER EXTENDED** contains the later Hard set. The
**Suite** arrows therefore always switch between at least two choices. A third
**DIGBENCH** choice appears when its credentials and launch-time catalog are
available. The selector reserves a stable responsive track for the longest
suite name, so its controls do not move as the selection changes.

Codexometer discovers models visible to the active benchmark authentication and
their supported reasoning efforts through `model/list`. Initially every
benchmark in each built-in suite and every compatible model/effort pair is
selected. Press `s` or click **Scope** to open a separate selection screen:
every benchmark in the active suite, model, and reasoning level has its own
checkbox, and each group has a **Check All** control that changes to **Clear
All** when the whole group is selected. Core and Extended retain independent
benchmark selections when you switch between them.

The supported reasoning levels shown beside each model dim immediately when
they fall outside the selected scope. Click a row, or use `Up`/`Down` and
`Space`/`Enter`, then click **Done** or press `d`/`Esc` to return. Unsupported
model/effort intersections are never counted or run.

Press `b` or click **Run Scope** to execute each selected benchmark against
every selected compatible model/effort pair. The button is enabled whenever
that scope contains at least one benchmark and one compatible pair, and its
label shows the resulting turn count. **Run All** deliberately ignores the
scope and executes every benchmark in the active suite against every compatible
model/effort pair. Codexometer displays that exact total and requires a second
confirmation within five seconds. A fresh, ephemeral, read-only app-server
thread is used for each built-in trial, so benchmark history does not clutter
normal Codex sessions. The turns still consume the same account quota shown by
Codexometer unless a benchmark API key is supplied; with a key, they use
standard usage-based API billing instead.

Each model/effort trial has a five-minute deadline. If an in-flight turn reaches
that deadline, Codexometer requests `turn/interrupt`, waits for the matching
`turn/completed` event, records that combination as `FAIL`, and continues with
the remaining combinations. App-server transport failure or failure to confirm
timeout interruption still stops the suite because the server's state is then
unsafe or unknown.

The **Stop** control remains visible but disabled until a suite starts. Press
`x` or click **Stop** to request `turn/interrupt` for the current trial and wait
for its matching completion before the temporary app-server is closed.
Completed results are retained, the current row becomes `STOPPED`, remaining
trials are not started, and the status reports how many planned trials were
complete. A stopped row is incomplete rather than failed, so it appears under
**All** but not the **Fail** filter or rankings. If remote interruption cannot
be confirmed before cleanup, the stopped result reports that uncertainty as a
**Stop Issue**.

The current trial appears in the existing Result Matrix immediately as an
`IN PROGRESS` row, before any result has completed. Click that or any completed
row, or use `Up`/`Down` followed by `Enter`, to replace the matrix with the
run's scrollable detail. An open live detail updates as safe benchmark events
arrive, then changes in place to the final `PASS`, `FAIL`, or `STOPPED` result. It shows the
requested and actual model, effort, task, outcome, live duration, token classes,
API-equivalent cost, exact benchmark prompt, visible structured response
(including the submitted Starlark), policy events, and deterministic-verifier
result. In the matrix, press `c` or click **Copy** at the lower right to export
a clean Markdown table containing only the eight column headings and every data
row. The export includes filtered and off-screen rows, retains the current sort
and rank weighting, and excludes the Show and Rank controls.

`Esc` returns to the same selected matrix row. Page keys, arrow keys, and the
mouse wheel scroll a long detail. With a detail open, press `c` or click
**Copy** at the lower right to copy the complete unstyled detail, including
interactions below the visible scroll window. Both copy actions use the
terminal's OSC 52 clipboard support, which is not available in every terminal.

This transcript is deliberately benchmark-only. It is populated directly by
the ephemeral thread that Codexometer created for that trial, bounded to 4,096
entries of at most 64 KiB each and 1 MiB total, retained only in process
memory, and discarded when a new suite starts or Codexometer exits. It does not
subscribe to or read ordinary Codex conversations, and it excludes reasoning
events, credentials, request headers, and internal app-server IDs.
Codexometer sends result data or that bounded transcript to the system clipboard
only when you explicitly press `c` or click **Copy**; the clipboard then falls
under your operating system and terminal's normal retention behavior.

#### Challenges and difficulty

Every trial asks the model to return one named Starlark function:

| Suite | Challenge | Difficulty | Required behavior | Verification set |
| --- | --- | --- | --- | --- |
| Core | **Merge Ranges** | Easy | Sort inclusive integer ranges and merge every overlapping or adjacent pair into a canonical union. It must handle empty input, duplicates, nesting, negatives, and arbitrary order. | 8 hand-written edge cases + 48 reproducibly generated cases |
| Core | **LRU Cache** | Moderate | Process integer `put` and `get` operations, update recency, evict the least-recently-used entry, and return both get results and final entries in most-recently-used order. Capacity zero is valid. | 5 hand-written edge cases + 40 reproducibly generated cases |
| Core | **Expression** | Moderate | Evaluate tokenized non-negative integers with `+`, `-`, `*`, parentheses, normal precedence, and left associativity—without `eval`. | 8 hand-written edge cases + 40 reproducibly generated expressions |
| Core | **Shortest Path** | Moderate | Return the minimum four-direction move count through a rectangular blocked/open grid, or `-1` when no route exists. | 5 hand-written edge cases + 40 reproducibly generated mazes |
| Extended | **Dependency Scheduler** | Hard | Find the minimum makespan for a small dependency DAG with job durations and a limited number of identical workers. Correct solutions must reason about precedence, concurrency, and cases where immediately starting every available job is not optimal. | 6 hand-written edge cases + 8 reproducibly generated DAGs |
| Extended | **Version Resolver** | Hard | Select one version per package while satisfying inclusive dependency ranges and exact conflicts, then return the lexicographically greatest valid solution. | 5 hand-written edge cases + 12 reproducibly generated catalogs |
| Extended | **Event Processor** | Hard | Reorder ledger events by sequence and apply idempotency, transfers, freezes, reversals, failure precedence, and a canonical audit result. | 5 hand-written edge cases + 10 reproducibly generated event streams |

The difficulty labels are documentation rather than part of the terminal names.
Hard challenges deliberately combine more rules or require bounded search, which
should create more separation between models and reasoning efforts than simply
making the easier inputs larger.

#### Ranking

The `RANK` column is an overall ranking for each model/reasoning-effort
combination across every completed row currently in the result matrix. It uses
ordinal cost and time positions—not raw dollars and seconds—so the distance
between first and second place on either axis is always one rank position,
regardless of the difference in the underlying measurements.

Rankings are isolated by benchmark provider: deterministic Core/Extended
results and randomized DigBench results are never combined. For DigBench,
`PASS` and `FAIL` in the algorithm below mean `WIN` and `LOSS`/`INCOMPLETE`, and
the seed disclaimer in the DigBench section applies.

The algorithm is:

1. Group completed rows by requested model ID and reasoning effort. A reported
   model reroute remains part of the requested combination that produced it.
2. For each combination, count passes and failures, sum non-negative wall time,
   and sum API-equivalent cost. Cost is complete only when every row in that
   combination has a finite, non-negative cost measurement.
3. Partition combinations into correctness tiers with identical pass and
   failure counts. Within each tier, rank combinations independently by
   ascending total cost and ascending total wall time. Equal measurements share
   a competition rank: for example, `1, 2, 2, 4`. Every incomplete-cost
   combination ties on the cost axis after every cost-complete peer in its tier.
4. Calculate a lower-is-better weighted score from cost rank `C` and time rank
   `T` according to the selected mode:

   | Mode | Formula | Equivalent weighting |
   | --- | --- | ---: |
   | **Cost** | `3C + T` | 75% cost / 25% time |
   | **Balanced** | `C + T` | 50% cost / 50% time |
   | **Speed** | `C + 3T` | 25% cost / 75% time |

5. Produce the final order lexicographically: more passes first, then fewer
   failures, then lower weighted score. Correctness therefore always dominates
   efficiency; a cheap, fast failure cannot outrank a combination with more
   passes. Combinations equal on all three comparisons share a competition rank.

Click **Cost**, **Bal**, or **Speed** in the Result Matrix control row, or press
`w` to cycle them. The ranking is recomputed immediately without rerunning any
trial. Token counts remain visible diagnostics but do not affect rank. The same
overall rank is repeated on each task row for that combination, and the rank
heading is clickable like the other sortable headings.

Missing cost is penalized on the cost axis, but it does not automatically force
the combination to the bottom of the final table. A sufficiently strong time
rank can still compensate in Balanced or Speed mode. If every combination lacks
cost, they all tie on the cost axis and the final efficiency order is determined
by time in every mode. If only part of a combination's cost ledger is missing,
its known costs are not used to infer a partial position: the whole combination
is treated as cost-incomplete.

Rankings update as results arrive, so they are provisional until a run finishes.
Cost and time axes are recalculated among peers in the same current correctness
tier. During a task-major Run All execution, one combination can temporarily
have one more completed row than the others, so an in-progress rank should not
be compared with the final result.
They inherit the API-equivalent caveats below; in particular, unknown prices
sort behind complete measurements on the cost axis and prompt-cache order can
affect that axis. Correctness remains the dominant criterion.

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
  64 KiB source limit and the difficulty-appropriate per-case execution budget:
  250,000 steps for Easy/Moderate challenges or 2,000,000 steps for Hard challenges;
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

`API EQ` is an estimated **standard text-token API equivalent**,
not a bill, a ChatGPT subscription charge, or a prediction of how much account
quota the turn consumed. Codexometer separates ordinary input, cached input,
cache-write input, and output, then applies the per-million-token prices known
to this Codexometer release. Usage availability and price availability are
tracked separately: a valid token total can still have `API EQ` shown as `N/A`
for an unknown model or a token class whose price was not published when the
release was built. Codexometer does not inherit or guess such a price. Pricing
can change after a binary is released; consult the
[official OpenAI API pricing page](https://developers.openai.com/api/docs/pricing)
for current values. The rates compiled into this version were retrieved from
that page on **2026-09-04**; every pricing-bearing Quota or Benchmark footer
displays both the retrieval date and a terminal hyperlink to the source when
space permits, so stale embedded pricing is visible while interpreting results.
The maintained price table covers GPT-6 Astra, GPT-5.6 Sol, GPT-5.6 Terra,
GPT-5.6 Luna, GPT-5.5, GPT-5.4, GPT-5.4 Mini, and GPT-5.3 Codex.

The figures are useful for comparing these particular observed trials, but
they have important limitations:

- They do not reveal the private quota-weighting rules used by ChatGPT plans,
  and should not be converted into quota percentages or treated as dollars
  actually charged.
- Prompt-cache state can depend on earlier activity and benchmark order. A
  later trial may receive cheaper cached input or incur a cache write that an
  otherwise identical trial would not, so observed API-equivalent cost is not
  a cache-neutral ranking.
- When exact per-response usage is present, long-context price thresholds are
  applied to each response independently. A cumulative-only turn beyond the
  threshold displays `N/A`, because its response boundaries cannot be proven.
  The raw event still does not associate a distinct model with each response.
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
| **P1** | Apply the correct pricing tier to long-context responses | Complete for exact per-response telemetry; ambiguous cumulative-only long contexts fail closed |
| **P2** | Reduce cache-order bias with balanced warm-ups, randomized ordering, or repeated trials | Open |
| **P2** | Report a cache-neutral comparison alongside the observed cached cost | Open |
| **P2** | Price mixed-model reroutes from a response-to-model association | Open; the current raw event does not expose that association |
| **P2** | Record pricing-table provenance and make stale compiled pricing conspicuous | Complete; every pricing-bearing Quota or Benchmark footer shows its source and retrieval date when space permits |
| **P2** | Add explicit compatibility diagnostics for future experimental-event schema changes | Complete for usage objects; unknown token fields fail closed and older servers retain the cumulative fallback |

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
- Do not infer prices for unknown models or unpublished token classes. Unknown
  usage fields must make costing unavailable. Update the compiled table only
  from published OpenAI pricing, and update its source retrieval date at the
  same time.
- Treat any tool-use item as a benchmark protocol violation. Text-token pricing
  alone cannot represent separately priced or externally executed work.
- Cover missing fields, invalid invariants, integer overflow, duplicate events,
  event regression, source disagreement, tool use, and experimental-protocol
  fallback in tests. Keep race-enabled CI green on Linux, macOS, and Windows.

Correct mixed-model reroute costing will require each upstream response to be
associated with the model that actually served it. Cache-neutral or
repeated-trial reporting would improve comparison quality without changing the
deterministic PASS/FAIL verifier.

## Options

```text
--codex PATH       path to the Codex CLI (default: codex)
--check-auth       verify the current Codex login and exit
--demo             preview simulated quota, Sessions, Usage, and benchmark data
--inline           render inline instead of using the alternate screen
--web              experimental read-only browser interface (loopback only)
--web-control      opt into browser session approvals/prompts (requires --web)
--web-port PORT    local browser port (default: 0/automatic; requires --web)
--refresh DURATION refresh interval (default: 1m)
--reset-threshold PERCENT show available resets at this consumption level (0-100; default: 80)
--reset-warning-hours HOURS expiry warning lead time (default: 72; 0 disables)
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

## Experimental browser interface

Keep the terminal experience, or opt into a local Svelte browser dashboard:

```sh
codexometer --web

# Explore with entirely simulated data
codexometer --web --demo

# Optional stable port; no configurable remote bind address
codexometer --web --web-port 8765
```

1. Run one of these commands and keep that terminal open.
2. Open the private `http://127.0.0.1:PORT/#pair=...` link printed in the terminal
   within five minutes. The application does not automatically launch a browser.
3. Pairing exchanges the one-use secret for a temporary browser capability and
   removes the secret from the visible URL. Do not share the original link.
4. Browse **Quota**, **Sessions**, and **Usage**. Refresh and browser Back/Forward
   work; session-detail URLs support direct navigation in the paired tab.
5. Press Ctrl+C in the launching terminal to stop the server and invalidate access.

This preview is **read-only by default and UK-English-only**, not feature parity with
the terminal. It includes Bars, Consumption Pace, Consumption Zone, Pie and Fuel Tank quota
presentations; reset inventory with disclosed expiry information; local session
telemetry with expandable/full-page context and synchronised activity graphs;
and account history with a daily heatmap, monthly/cumulative bars, a 6/12-month
selector and an accessible data table. Five browser themes are available.
`CODEXOMETER_LANG` continues to configure the terminal, not this preview.

**Consumption Zone** plots each window's elapsed quota period horizontally and
0–100% consumption vertically. The bottom-left to top-right diagonal represents
steady consumption: above it means usage is outpacing elapsed time, below it means
headroom. The background fades from red at the top left through amber to green at
the bottom right, and a high-contrast dot marks the current position (last known
consumption against the current elapsed time). A white trail connects successful
quota observations, with an open circle marking the first observation. This is
the quota window's observed path, **not usage attributed to an individual Codex
session**, a reconstruction of earlier history, or a prediction of future use.
Windows without a known duration and reset date cannot be plotted.
An expandable, keyboard-accessible **OBSERVATION TABLE** supplies the same
retained history as text: observation time, elapsed period, consumed percentage
and breaks between segments. It updates alongside the plotted trail.

Trails are held only in the web server's memory, survive browser reloads and tab
changes, and restart when the server stops. A changed account, reset date or
window duration, a lower consumption reading, a backwards clock or a removed
window starts a fresh trail. Failed reads add no points and leave a break before
the next observation. Each window retains at most 720 points: the original start
plus the latest 719; the omitted interval is shown as a gap. Readings between
polls are not known. Account-change isolation depends on the account identity
available from the existing reader.

### Browser Sessions command centre

- **SESSION TOTALS** shows observed tokens and the number of currently listed
  sessions, plus separate working, awaiting-approval, awaiting-input and inferred
  check counts. Parent counters already include linked agents; they are summed
  once. These are not account-wide or permanent cumulative totals: removing a
  session from the observation can reduce the sum. On stale data, token/session
  totals are labelled last-known and live state counts become unavailable (—).
- Select a session by clicking its directory/name, or use **↑ / ↓**.
- Use **← / →** or the row's arrow buttons to move through **graph only → split
  detail and graph → wide detail → full-page detail**. Each row has its own layout.
- **Escape** or **← ALL SESSIONS** returns from full-page detail to that session's
  wide detail row. **SHOW ALL DETAILS / HIDE ALL DETAILS** switches all current
  rows between split detail and graph-only, clearing individual overrides and
  setting the default for newly observed sessions too. This global default is
  independent of the 100-entry saved per-session history. Narrow screens stack panels.
- Attention links jump to the relevant session. Approval indicators remain
  outside scrollable reply text; available commands are separated from their
  justification. Missing commands are explicitly labelled, never reconstructed.
- **INPUT NEEDED / APPROVAL NEEDED** represent observed signals; **CHECK SESSION**
  is labelled inferred inactivity and is not proof that a reply is required.
  **TURN COMPLETE** is informational. Disconnection or failed session refresh
  suppresses live attention indicators and marks the display stale.
- **CONTEXT SOURCE** describes the reply/command source, not necessarily the
  provenance of the grouped session's status. No additional daemon certainty is
  inferred from a LOCAL or app-server context label. The existing reader's
  shared-daemon/fallback limitations still apply.

The browser remembers its theme, last primary tab, quota view, selected session
and up to 100 per-session row layouts. Pairing opens the saved primary tab;
explicit deep links take precedence. Full-page detail returns via its URL on
reload, but a fresh pairing opens the Sessions list. Clicking CODEXOMETER still
returns to Quota → Bars. These preferences use browser localStorage on the same
origin; **use a fixed `--web-port` to retain them across server restarts** because
the automatically selected port may change. Blocked storage falls back to
in-memory preferences. Clear this site's browser data to forget them (this also
removes browser pairing access). No replies, commands, usage trails or Codex
credentials are stored in localStorage; the saved selection/layouts do contain
session IDs. Navigation and layout controls never send actions to Codex.

The browser uses a compact dashboard layout: quota plots share the available
width and height below the tabs. Pie charts retain their circular shape, while
Consumption Zone scales each axis independently and keeps text legible. On short
windows or with many quota windows, content scrolls without hiding the footer
controls or shrinking plots below a readable minimum.

Benchmark execution and quota-reset redemption are **not exposed by the web
server**. Session approvals and prompts require the explicit opt-in described
below. Use Codex or the terminal interface for other actions. Quota API-equivalent
learning, status scoring, pricing readouts and other terminal-only controls
are intentionally deferred; the web interface does not invent replacements.

The same existing Codex readers provide the data and the same local/shared-daemon
limitations apply. The browser cannot reveal an approval command missing from
the source. Session counters measure tokens observed since this **web process**
started, not session lifetime or account totals; linked child agents are grouped
by the existing reader. Graphs collect up to 120 samples, approximately one hour,
at 30-second intervals with automatic scaling. Initial samples establish a
baseline, not historical usage. Delayed polls after sleep can cover longer
intervals. Session text can contain private source code, paths or secrets.

Quota refresh defaults to one minute (`--refresh`, minimum ten seconds in web
mode); sessions poll every two seconds and account history every five minutes.
Polls wait for each read to finish before scheduling the next, avoiding overlap.
One collector set serves all connected browser views; changing routes does not
start additional Codex readers. Updates use an authenticated event stream with
full snapshots on reconnect. Refresh errors and disconnections are labelled;
cached observations are not proof that a session is still working. Account
history is hidden unless its account matches the current successful quota read.

### Optional browser session control

```sh
# Read-only remains the default
codexometer --web

# Explicitly grant the paired browser session-control access for this launch
codexometer --web --web-control

# Safely try the simulated approval; no command runs
codexometer --web --web-control --demo
```

The header and launching terminal clearly identify **SESSION CONTROL** mode.
The flag is not saved as a preference and cannot be enabled by a browser request.
Without it, the control endpoints do not exist, even if Codex supports actions.
This permission applies to the paired browser capability for that server launch;
there is no separate read-only pairing link on a control-enabled server.

1. Follow [Recommended Codex CLI setup](#recommended-codex-cli-setup): install the
   managed standalone Codex CLI, start its local app-server daemon and connect
   the CLI sessions to that daemon. Merely starting a daemon does not migrate
   already-running local CLI sessions. Existing local-only sessions still supply
   best-effort telemetry, but cannot supply browser approval/prompt capabilities.
2. Launch with `--web --web-control` and open its private pairing link.
3. Open **Sessions → FULL DETAIL** for the intended session. Controls appear only
   for a supported, currently live request or an idle thread offering a follow-up.
   Inferred inactivity, old reply text and a TURN COMPLETE label alone do not
   grant permission to send anything. Unsupported requests remain **reply in Codex**.
4. Check the target thread (including a linked agent where applicable), working
   directory and exact command. Select one of the actual supported decisions,
   answer the input questions, or write a follow-up. Session-wide and persistent
   prefix grants are explicitly marked and show the supplied prefix details.
5. Click **REVIEW BEFORE SENDING**, then the separate **CONFIRM** button within
   30 seconds. Cancel returns to editing; Enter in a text area inserts a newline,
   never submits. All decisions, including declines, use this two-step browser flow.

The server binds preparation to the exact session, source request, chosen option
and answers. Confirmation rechecks the offer against current reader/provider
state; missing/failed observations or observations older than ten seconds fail
closed. The existing Codex clients also validate connection-local, one-use
capabilities; follow-ups recheck the thread is idle before starting a turn.
The server consumes confirmation before dispatch. Double-clicks/replays cannot
resend it, and failed or ambiguous sends **are never automatically retried**:
check Codex before deciding what to do next. An acknowledgement means a decision
or text was sent, not that Codex finished successfully. Sending a follow-up may
start work and consume quota; approving a command can let that work proceed.

Only full-page detail has write controls in this tranche. Drafts and confirmations
are scoped to that session/request, cleared on navigation or request changes, and
never saved in display preferences. Secret question answers use masked inputs.
Go retains at most one prepared action per server for up to 30 seconds; a new
preparation replaces the previous one. Raw Codex request tokens stay in Go;
the browser receives opaque web offer/confirmation identifiers, not reusable
Codex credentials. No actions run merely by opening a page or reconnecting.

### Browser security and local access

See [the local interface security checklist](SECURITY.md) for maintainer rules,
request/resource limits, terminal control-sequence handling and CI-only Go/npm
vulnerability checks. These checks add no application startup or runtime cost.

- The server binds **only `127.0.0.1`**. Remote/LAN hosting, reverse proxies,
  tunnelling and exposing the port publicly are unsupported. There is no web
  listener or browser collector during a normal terminal launch.
- The one-use pairing link expires after five minutes. Its browser capability
  expires after eight hours or on server restart. Restart `--web` for a fresh
  pairing link if it expires or you need to pair a different browser.
- The temporary capability is kept in the paired tab's **sessionStorage** so
  reload works; it is sent in an Authorization header, never a cookie or URL
  query. It is not a Codex credential. A fresh independent tab does not
  automatically inherit access. Browser tab/session restoration may retain
  sessionStorage, so closing a tab is not a reliable revocation mechanism;
  stopping the server is. If browser storage is blocked, access is memory-only
  and reloading will require a new pairing link.
- Codex authentication, account fingerprints and raw approval/input capabilities
  stay in Go. The browser receives explicit presentation/action fields, not raw client
  objects or upstream error strings. No session content is saved by the web
  server on disk. Display preferences and session layout/selection IDs use
  localStorage as described above; observation trails remain in server memory.
- Exact Host, Origin and Fetch Metadata checks reject rebinding and cross-origin
  access, including other localhost ports. Pairing requires same-origin JSON;
  protected reads require the bearer capability. Control requests also require
  same-origin JSON and bearer authorization. CORS is not enabled; control routes
  exist only when launched with `--web-control`.
- Responses use `no-store`, `no-referrer`, `nosniff` and a restrictive Content
  Security Policy with framing disabled. Session replies and commands render as
  text, never HTML/Markdown execution. No CDN assets, analytics or external reply
  images are loaded. Inline CSS is allowed for responsive gauges; inline scripts
  and JavaScript evaluation are not.

Local HTTP is deliberate for loopback delivery; it does not offer HTTPS transport
protection if forwarded elsewhere. These protections do not defend against
malware running as your user, a compromised browser, or extensions with access
to the page. Keep sensitive sessions out of screenshots and shared displays.

**Good practice for opt-in write mode:**

- Enable `--web-control` only when you need it; use ordinary `--web` for viewing.
  Restart without the control flag when finished rather than leaving it enabled.
- Use an updated browser and a **dedicated profile with no extensions**. Extensions
  allowed to access the page may read session text or act through your paired
  access. Private/incognito mode is not an equivalent guarantee: extensions can
  be enabled there too. This reduces exposure, not all browser risk.
- Keep the server local: no LAN sharing, tunnels, reverse proxies or public port
  forwarding. Keep the original pairing link private and lock your computer when
  away. A normal website must not be given your pairing link or bearer token.
- Before confirming, verify the session, working directory, exact command and
  permission scope. Prefer a one-time grant over session-wide or persistent
  permission when appropriate. Generated explanations are **not proof of safety**.
- Keep Codex's sandbox and approval protections enabled; do not weaken them to
  make browser controls appear. If an action is unavailable, inspect it in Codex.
- Stop the server with Ctrl+C to revoke access. Closing the browser tab alone
  is insufficient. On an uncertain send outcome, inspect Codex before trying again.

In plain English: opting in lets the paired browser send instructions and
approval decisions to your Codex sessions, which may run commands or change files
within their permissions. A compromised browser/profile or malicious code executing
inside this application's origin could misuse that access; cross-site checks and
confirmations cannot protect against code already running as the application.
The terminal avoids that browser-specific attack surface, but its risk is not
zero: local malware, unsafe approvals and compromised dependencies still matter.

On Ubuntu/WSL, try the exact printed `127.0.0.1` URL in your Windows browser;
this depends on your WSL localhost-forwarding configuration. Native Windows,
macOS and Linux use the same loopback design. Do not substitute a LAN address or
disable the host checks as a workaround. Windows/WSL and Safari should still be
treated as experimental until verified on your particular setup.

### Web development and terminal regression protection

The browser uses **Svelte + TypeScript + Vite**, a small hash router and CSS/SVG
visualisations. Production assets are embedded in the Go executable. Release
archives and `go install` remain standalone: end users need no Node runtime,
frontend server or separate asset directory. The committed production bundle
also means ordinary Go builds do not require an npm install.

Frontend contributors need Node 24 and npm:

```sh
make web-build           # npm ci, Svelte/TypeScript checks, regenerate assets
make build               # embed the new assets in the local executable
cd web
npx playwright install chromium
npm test                 # real browser tests against ../codexometer --web --demo
```

Commit `web/package-lock.json` and regenerated `internal/web/dist` together with
source changes. CI rebuilds the frontend and rejects stale generated assets,
then runs browser tests against the production Go server, including its CSP.
Frontend tests use simulated data only. Never point test traces or screenshots
at real private sessions.

The terminal UI and Codex reader implementation are unchanged by this initial
web layer. Existing regression tests cover English presentation across themes
and sizes, localisation, responsive layouts, mouse hit regions, session
navigation, approvals, reset confirmation and quota learning. Additional launch
tests ensure `--web` cannot start the terminal or benchmark discovery and normal
launches never start web mode. CI retains `go test -race -cover ./...`, vet and
build checks on Linux, macOS and Windows; browser tests currently run Chromium
on Linux. These checks provide regression evidence, not a guarantee that every
terminal emulator or OS/browser combination is covered.

## Redeeming a banked quota reset

On the Quota tab, `[ RESET // N ]` appears at the top right when a recent
quota reading reports available banked resets, the account is verified, and
at least one displayed quota window is **80% consumed or higher**, or a known
available reset expires within the warning lead time (**72 hours** by default;
configure with `--reset-warning-hours`). In the dedicated **Resets**
view, the usage threshold does not apply.
Set another threshold with `./codexometer --reset-threshold 60` (whole percentages
from 0 to 100). For testing, `./codexometer --reset-threshold 0` bypasses the
consumption threshold; an available credit and verified, fresh account data
are still required. The default is 80 when the flag is omitted.
It moves below the main tabs on narrow terminals. Click once to open Resets and reveal
`[ CONFIRM RESET ]`, then click again within ten seconds to redeem one reset.
`Esc` or changing tabs cancels confirmation. Redemption refreshes eligible
quota windows and changes the weekly reset schedule; it does not add quota
on top of the existing allowance.

Codexometer uses the prevailing Codex login and the
[official app-server](https://learn.chatgpt.com/docs/app-server#8-earned-rate-limit-resets-chatgpt)
`account/rateLimitResetCredit/consume` method. A shared daemon is not required.
Older servers that do not report reset availability leave the button hidden.
The button is disabled during submission, and quota/count data is fetched
again afterward. If the result is uncertain, `[ RETRY RESET ]` repeats the
same attempt identifier and selected credit ID, with another confirmation, to avoid consuming a second
reset. Keep Codexometer open to retain that retry identifier. Check Codex's
Usage page before attempting another reset after restarting the application.

## Release builds

GoReleaser builds Linux, macOS, and Windows archives for AMD64 and ARM64 with
CGo disabled. Unix releases are `.tar.gz`; Windows releases are `.zip`; every
release also includes `checksums.txt`.

```sh
make release-snapshot
```

That local snapshot requires GoReleaser. Publishing is deliberately confined to
the `Release` GitHub Actions workflow: a semantic `v*` tag must resolve to a
commit reachable from `main`, pass the full Linux/macOS/Windows test matrix, and
remain unchanged between validation and publication.

## Versioning

Codexometer follows semantic versioning. The maintained source version lives in
[`internal/version/VERSION`](internal/version/VERSION), and the release workflow
requires its Git tag to match. Go automatically embeds an exact tag in binaries
built with `go install github.com/merefield/codexometer@vX.Y.Z`.

The resolver uses the first version available in this order:

1. a link-time value, such as the nearest Git description injected by
   `make build`;
2. Go's embedded module version—an exact tag for a release build or, when Go
   supplies one, a pseudo-version such as
   `X.Y.Z-0.<timestamp>-<commit>[+dirty]`;
3. for a local checkout whose module version is `(devel)`, a VCS fallback in
   the explicit form `<source-version>-dev+<commit>[.dirty]`;
4. the maintained value in `internal/version/VERSION` when no build or VCS
   identity is available.

The leading `v` used by Git tags and Go module versions is removed in every
case. The VCS fallback is a Codexometer development identity, not a Go
pseudo-version.

The dashboard masthead, app-server client metadata, and both CLI flags all use
that one resolved value. The flags report it and exit without starting the
interface:

```sh
codexometer -v
codexometer --version
```

Release automation can override the source-build fallback without editing code:

```sh
go build -ldflags="-s -w -X github.com/merefield/codexometer/internal/version.buildVersion=vX.Y.Z" .
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

Session monitoring starts with Codexometer and checks appended local token telemetry
once per second while sessions are active, relaxing to once every five seconds
when none are active. It groups explicit agent descendants under their root and
rolls each root's observed deltas into synchronized graph buckets. It also updates the
three compact response-cycle statistics without retaining response content. A
bucket closes only after the boundary telemetry read completes; its heading
reports the actual observed duration when scheduling or first-session detection
makes it shorter or longer than 30 seconds. These reads do not contact OpenAI or
invoke a model.
On Unix systems, these reads also probe the default shared app-server control
socket. Exact thread-status results are cached for five seconds to avoid
repeating the same per-thread requests on every active poll. When present, its
loaded-thread runtime statuses make attention badges exact; when absent or
unreachable, Codexometer silently uses the local rollout and writer-lock
fallback described above. Graph history is bounded to the latest 4,096 samples.
Pressing Pause performs one immediate final local read and forces complete
session discovery, including Codex sessions resumed from older rollout
directories. Resume rebases counters so activity during the pause is excluded;
Reset clears the measurement and graphs without changing the paused/running
state.

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
rows to display every quota window. Increase the pane height or press `v` in
Quota to return to the compact default Bars view.

### Sessions remains at zero

Sessions observes rollout telemetry under the same `CODEX_HOME` visible to
the Codexometer process. Confirm that the Codex session doing work is local and
uses that home. A native Windows Codex session and a native Windows Codexometer
normally share the same user profile; WSL and native Windows have different
homes unless `CODEX_HOME` is deliberately shared. Cloud activity and sessions on
other machines are not visible. Usage is generally appended after a model
response reports its token totals, so a currently streaming response may not
appear until its next telemetry event.

## Roadmap

Potential Sessions follow-ups (not implemented):

- **Remember session visibility across restarts.** Persist dismissed rows using
  durable activity markers, not connection-local counters or approval tokens.
  Restore them on genuine new activity or verified pending approval, with a
  conservative fallback that favours showing a session needing attention.
- **Remember the Sessions workspace.** Restore the selected session and each
  session's detail level, alongside visibility. Handle missing sessions and
  smaller terminals gracefully; restoring the layout must not restore editor
  focus, unsent drafts, or armed approval confirmations. Session identifiers
  would become persisted data and the saved-preferences documentation would
  need updating accordingly.

Currently, the main tab, Quota view and global Sessions hide/show preference are
saved, but session selection, per-session detail levels and dismissals remain
temporary.

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

- Go 1.26.6+
- Bubble Tea v2 for the terminal event loop and declarative terminal modes
- Lip Gloss v2 for adaptive ANSI styling and layout
- Starlark for deterministic, hermetic benchmark-code evaluation
- Codex app-server JSON-RPC for authenticated quota data
- Local Codex rollout `token_count` records for live Sessions telemetry

## License

Codexometer is available under the [MIT License](LICENSE).

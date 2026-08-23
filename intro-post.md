# Codexometer: a retro companion dashboard for Codex

[Codexometer](https://github.com/merefield/codexometer) is a small, retro terminal dashboard for monitoring Codex quota, local session activity, and benchmark runs. Leave it open in a second terminal window or pane to see every active usage window, its remaining capacity, and its reset time without repeatedly opening `/status` in the Codex session where you are working.

![codexometer|690x454](upload://xcWugpZYodk0zuFVYsDzluTemtf.png)

## Why use it?

`/status` is useful, but it lives inside the session where you are working. Codexometer turns the same quota information into an always-visible companion display, then adds local session monitoring and a benchmark runner around it.

It is useful when you want to:

- keep quota and reset timing visible without interrupting your current task;
- see whether local Codex sessions are active, waiting for input, or awaiting approval;
- understand locally observed token activity and API-equivalent cost;
- compare available models and reasoning levels on the same checked tasks; or
- inspect how a benchmark was solved rather than seeing only a final score.

Everything runs locally in a terminal, with mouse controls, keyboard shortcuts, responsive layouts, five colour themes, and support for additional rate-limit windows when Codex returns them.

## What does it show?

The interface has three main tabs:

- **Quota** — switch between Bars, Consumption Pace, Pie, and Fuel Tank presentations. A pace-aware health signal distinguishes quota that is safely tracking the reset cycle from quota burning too quickly. Codexometer can also learn an observed standard API-equivalent estimate for primary Codex windows, with conservative confidence and visible pricing provenance.
- **Monitor** — record locally observed token activity by root Codex session, with scrolling graphs and compact call, activity, output, and time-to-first-token telemetry. It estimates each session’s local share of observed account activity, highlights sessions needing input or approval when Codex exposes that state, and folds explicitly linked spawned agents into their parent session.
- **Benchmark** — run programmatically checked challenges across selected model and reasoning-level combinations, then compare outcomes, wall time, tokens, estimated API-equivalent cost, and rankings.

## Benchmarking

The Benchmark tab is a scoped runner with live, inspectable run details.

The always-available benchmark catalogue is divided into:

- **Codexometer Core** — the original easy and moderate deterministic coding tasks; and
- **Codexometer Extended** — the later, harder deterministic tasks.

Open **Scope** to select individual tasks, models, and reasoning levels. Compatibility cues show which efforts each model supports, while bulk controls make it easy to check or clear a complete group. **Run Scope** executes the selected intersections; **Run All** runs the active suite’s complete catalogue across every compatible model and reasoning level.

Each trial appears in the result matrix as soon as it starts and remains clickable while in progress. The detail view includes:

- the benchmark prompt and safe context supplied to the model;
- model responses and verifier results;
- live progress, elapsed time, tokens, and API-equivalent cost;
- tool requests and responses where the benchmark uses them;
- move and state transitions for interactive games; and
- a complete copy-to-clipboard action for sharing or further analysis.

The result matrix itself can also be copied as a clean Markdown table containing its headings and every result row, without the interactive controls. You can enter and leave a live detail view without interrupting the run, navigate long transcripts with the keyboard, and stop an active suite while retaining completed work and the captured incomplete result.

Deterministic suites can be ranked with cost-, speed-, or balanced weighting. DigBench uses the same controls for a separate observed-run ranking, with the clear caveat that its server-assigned random seeds make individual attempts exploratory rather than controlled model comparisons.

## Optional DigBench integration

Codexometer also includes an experimental integration with [DigBench](https://digbench.ai/), or “Discovery in Games”. DigBench is a scientific-discovery benchmark containing 70 interactive games with undisclosed rules. Humans and AI agents receive the same states, available actions, and step budgets, and must discover each game’s mechanics through experimentation.

Supplying a `DIGBENCH_API_TOKEN` adds **DigBench** to the suite selector. Codexometer fetches the current game catalogue at launch rather than compiling a fixed list, so Scope can expose every game returned by DigBench alongside the model and reasoning controls.

The exact published `gpt-5.6-sol`/`high` condition is identified on both Scope controls; `xhigh` is marked as an enhanced, non-paper experiment. Each game receives a two-hour default allowance, with a different finite timeout available to headless runs. The model gets DigBench's task description and creative-mode metadata, a compact state-focused response after every action, and explicit guidance to inspect that response before choosing its next single move. The derived level score remains operator-facing, and Codexometer keeps the richer sanitized API exchange in the benchmark detail transcript.

Before a run, Codexometer shows the planned number of persisted remote sessions and asks for confirmation. During each game, the result table reports live game and level progress. Its detail view documents the solving workflow as safe prompts and tool definitions followed by each tool request, tool response, move, authoritative state, and final response. Win detection comes from the remote DigBench state rather than from interpreting the model’s prose.

Without a DigBench token, the integration remains hidden and the normal Codexometer experience is unchanged. Tokens can be created from the [DigBench account page](https://digbench.ai/account/tokens).

## Privacy, authentication, and cost

Quota monitoring uses the prevailing Codex login and does not inspect ordinary conversation content. Detailed interaction capture is restricted to isolated benchmark turns created by Codexometer; ordinary Codex session messages remain excluded.

Benchmark transcripts are bounded and sanitized. Credentials, request headers, known runtime identifiers, temporary paths, terminal controls, Codex reasoning, and unrelated local session content are not retained in the detail view.

By default, benchmark model calls use the prevailing Codex login and quota. If you choose to benchmark with **Sign in with ChatGPT** subscription authentication, you remain responsible for the applicable terms and policies and do so at your own risk. Codexometer is a local client for user-triggered trials; it is not intended to re-serve or share one person’s subscription access.

For a clearer usage-based billing boundary, set `CODEXOMETER_BENCHMARK_API_KEY` to your own OpenAI API key. `OPENAI_API_KEY` is accepted as a fallback. The selected key is isolated in a benchmark-only Codex app-server and takes precedence for all benchmark model discovery and runs, while quota monitoring continues to use the prevailing login.

The DigBench token authorizes only the external game service. Codexometer removes it from the environment before spawning Codex and gives the model only session-scoped game tools. DigBench sessions are persisted remotely, and benchmark model calls consume either Codex subscription quota or usage-billed API tokens according to the authentication choice above.

## Where does it fit?

Codexometer works particularly well in:

- another Windows Terminal tab or split pane;
- a second Terminal or iTerm window on macOS;
- a tmux, Zellij, or other terminal-multiplexer pane; or
- an Ubuntu terminal beside the Codex CLI.

It is written in Go and builds as a standalone binary for macOS, Windows, and Linux.

## Install and upgrade

The release installer is also the easiest way to upgrade. It downloads the pre-built binary for your platform, verifies the published SHA-256 checksum, confirms the release version, and installs it without requiring Go.

On macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/merefield/codexometer/main/install-release.sh | sh
```

The default destination is `/usr/local/bin`. For a user-local installation, set `CODEXOMETER_BIN_DIR="$HOME/.local/bin"` when running the installer.

On Windows PowerShell:

```powershell
$installer = Join-Path ([IO.Path]::GetTempPath()) "install-codexometer.ps1"
Invoke-WebRequest https://raw.githubusercontent.com/merefield/codexometer/main/install-release.ps1 -OutFile $installer
& $installer
Remove-Item $installer
```

Re-run the relevant installer to upgrade or reinstall Codexometer. It replaces the executable only after the downloaded artifact passes its checksum and version checks. Developers who prefer to build from source can still use `go install github.com/merefield/codexometer@latest`.

Full installation, authentication, privacy, monitoring, and benchmarking guidance is available in the [README](https://github.com/merefield/codexometer#readme).

**GitHub:** https://github.com/merefield/codexometer

If you enjoy it, please give the project a ⭐ on GitHub!

# Codexometer: a retro companion dashboard for Codex

[Codexometer](https://github.com/merefield/codexometer) is a small, retro terminal dashboard for [Codex](https://github.com/openai/codex): live quota, session monitoring, token-usage history, and model benchmarks. Leave it open in a second terminal window or pane to see every active quota window, its remaining capacity, and its reset time without repeatedly opening `/status` in the Codex session where you are working.

![codexometer|690x454](upload://xcWugpZYodk0zuFVYsDzluTemtf.png)

## Why use it?

`/status` is useful, but it lives inside the session where you are working. Codexometer turns that information into an always-visible companion display, with a `/usage`-inspired history tab, local session monitoring, and an optional benchmark runner alongside it.

It is useful when you want to:

- keep quota and reset timing visible without interrupting your current task;
- see whether local Codex sessions are active, waiting for input, or awaiting approval;
- understand locally observed token activity and API-equivalent cost;
- explore up to a year of account token history, peak days, and activity streaks;
- compare available models and reasoning levels on the same checked tasks; or
- inspect how a benchmark was solved rather than seeing only a final score.

The interface runs locally in a terminal, with clickable and hover-highlighted controls, keyboard shortcuts, responsive layouts, five colour themes, and sixteen interface languages. Extra rate-limit windows are accommodated as Codex returns them. Theme, Quota view, benchmark filter, and ranking preferences survive restarts.

## What does it show?

The interface has four main tabs:

- **Quota** — switch between Bars, Consumption Pace, Pie, and Fuel Tank presentations. Compare consumption directly with elapsed reset-cycle time, see countdowns and reset dates, and get a pace-aware health signal or an early-exhaustion projection. Learned API-equivalent estimates show both current spend and what 100% of a primary quota window might represent, with conservative confidence and a pricing-source/date footer when space permits. Eligible banked resets can be redeemed with a separate confirmation click.
- **Monitor** — see one metrics box and scrolling, auto-scaling graph per root Codex session, all sampled on the same 30-second tick. Compare local token shares, model calls, activity, output size, and time to first token; explicitly linked subagents are folded into their parent session. Pause/resume or reset the measurement, page through sessions, and dismiss finished rows with `[×]` without closing the session: fresh activity brings them back.
- **Usage** — explore account token history in a GitHub-style daily activity grid, weekly bars, or a cumulative graph. Choose 6 or 12 months (26/52 weeks), browse older periods, and see reported lifetime tokens, peak daily usage, and the current streak. This is server-side history, not the Monitor’s live local counters, so it can lag ongoing work; it does not invent historical dollar costs or per-model breakdowns.
- **Benchmark** — run programmatically checked challenges across selected model and reasoning-level combinations, then compare outcomes, wall time, tokens, estimated API-equivalent cost, and rankings.

Use `Tab` / `Shift+Tab` or the mouse to navigate. Quota refreshes every minute by default. Passive monitoring and history views do not start model turns; benchmark runs consume subscription quota or API-billed tokens, depending on your chosen authentication.

Click the **Codexometer title** to jump back to Quota’s Bars view. The **version label** links to that version’s release highlights through your terminal’s hyperlink support—typically Ctrl-click.

## Your terminal command centre

Give each Monitor session as much space as it needs. Press `i` or click anywhere in that row’s detail/graph area to cycle **graph only → split detail/graph → full-width detail → full-screen detail**, then back through the same views. Only that session changes; approval buttons and reply editors keep their own actions. From full-screen detail, `i` or Escape returns to the same expanded row.

Global **Show Detail / Hide Detail** (`h`) resets all rows to split detail/graph or graph-only. That global preference survives restarts; individual row layouts are temporary.

Subtle animated dots in inline and full-screen detail indicate observed work, disappearing when the session is no longer observed working or needs attention. “Text sent ...” and “Decision sent ...” confirmations linger briefly so fast updates do not swallow the acknowledgement. Local activity signals remain best-effort, not proof that a model is still executing.

## Quota estimates, resets, and session attention

API-equivalent figures are workload-dependent estimates, not your subscription’s cash value or a statement of OpenAI’s private quota formula. They need clean observed quota movement to learn, show uncertainty, and restart learning when Codexometer is relaunched. Pricing uses published input, cached-input, and output rates where the model and usage are known; missing data is not treated as free.

The reset button normally appears only when Codex reports an available reset and a quota window is at least **80% consumed**. Change that threshold with `--reset-threshold 60`. Click once to reveal confirmation, then again within ten seconds to redeem; `Esc` cancels. A reset refreshes eligible quota and changes the weekly reset schedule—it does not stack additional allowance.

Ordinary Codex CLI sessions work out of the box. For the best Monitor feedback, connect your CLI sessions through a shared local Codex app-server: Codexometer can distinguish **INPUT NEEDED** from **APPROVAL NEEDED**, show command-approval details with controls matching Codex's supported offered choices, and use positively matched resolved-model events for more accurate pricing. Permission grants require confirmation; session-wide and persistent-prefix choices are clearly labelled. Without that setup, it falls back to local session signals and a cautious **CHECK SESSION** inactivity prompt, not a guessed approval alert. See the [recommended setup](https://github.com/merefield/codexometer#recommended-codex-cli-setup).

## Make it yours

Choose Hacker, Rust, Blue Steel, Ultraviolet, or Nightshade with `t`. UK English remains the default and retains the original presentation. Set `CODEXOMETER_LANG` to opt into Dutch (`nl`), German (`de`), French (`fr`), Italian (`it`), Spanish (`es`), Russian (`ru`), Japanese (`ja`), Simplified Chinese (`zh-Hans`), Swedish (`sv`), Norwegian Bokmål (`nb`, also `no`), Turkish (`tr`), Estonian (`et`), Finnish (`fi`), Brazilian Portuguese (`pt-BR`), or Danish (`da`):

```sh
CODEXOMETER_LANG=fr codexometer
CODEXOMETER_LANG=ja codexometer --demo
CODEXOMETER_LANG=fi codexometer
CODEXOMETER_LANG=pt-BR codexometer
CODEXOMETER_LANG=da codexometer --demo
```

On PowerShell, use `$env:CODEXOMETER_LANG = 'fr'` before launching. Retain the setting in your shell profile or Windows user environment; restart Codexometer after changing it. Set `en-GB` or remove the variable to restore English. `pt` and other Portuguese variants, including `pt-PT`, currently use the Brazilian translation; `da-DK` selects Danish. Translations are embedded in the binary; existing hotkeys, model names, and benchmark prompts stay unchanged. These are initial UI translations, and native-language corrections are welcome.

## Benchmarking

The Benchmark tab is a scoped runner with live, inspectable run details.

The built-in coding challenges return Starlark functions, evaluated in a restricted interpreter against fixed edge cases and reproducibly generated tests. PASS/FAIL comes from those checks, not another LLM judging the answer. Harder challenges include dependency scheduling, version resolution, and event processing.

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

Filter passed/failed results, click column headings to sort, or switch instantly between cost-, speed-, and balanced ranking weights. Deterministic-suite rankings prioritise correctness before measured efficiency. DigBench uses the same controls for a separate observed-run ranking, with the clear caveat that its server-assigned random seeds make individual attempts exploratory rather than controlled model comparisons.

## Optional DigBench integration

Codexometer also includes an experimental integration with [DigBench](https://digbench.ai/), or “Discovery in Games”. DigBench is a scientific-discovery benchmark containing 70 interactive games with undisclosed rules. Humans and AI agents receive the same states, available actions, and step budgets, and must discover each game’s mechanics through experimentation.

Supplying a `DIGBENCH_API_TOKEN` adds **DigBench** to the suite selector. Codexometer fetches the current game catalogue at launch rather than compiling a fixed list, so Scope can expose every game returned by DigBench alongside the model and reasoning controls.

The exact published `gpt-5.6-sol`/`high` condition is identified on both Scope controls; `xhigh` is marked as an enhanced, non-paper experiment. Each game receives a two-hour default allowance, with a different finite timeout available to headless runs. The model gets DigBench's task description and creative-mode metadata, a compact state-focused response after every action, and explicit guidance to inspect that response before choosing its next single move. The derived level score remains operator-facing, and Codexometer keeps the richer sanitized API exchange in the benchmark detail transcript.

Before a run, Codexometer shows the planned number of persisted remote sessions and asks for confirmation. During each game, the result table reports live game and level progress. Its detail view documents the solving workflow as safe prompts and tool definitions followed by each tool request, tool response, move, authoritative state, and final response. Win detection comes from the remote DigBench state rather than from interpreting the model’s prose.

Without a DigBench token, the integration remains hidden and the normal Codexometer experience is unchanged. Tokens can be created from the [DigBench account page](https://digbench.ai/account/tokens).

Prefer a non-interactive run? `codexometer --digbench-game P-1` runs a named game headlessly; model, reasoning, and finite timeout options are documented in the README. This creates a persisted remote game session and consumes model usage just like the interactive runner.

## Privacy, authentication, and cost

Quota monitoring uses the prevailing Codex login. Monitor can show a bounded last-reply, activity, question, or approval preview beside each session. Excerpts remain in memory and do not trigger another model call; global Hide Detail hides them until you choose to show them again, including by expanding an individual row. With a shared app-server, full detail lets you answer supported blocking questions or explicitly send a follow-up to an idle session. Unsupported questions and approvals remain in Codex. Supported command decisions use clickable buttons or numbered shortcuts; grants require a separately labelled confirmation (`C` or a second click), and persistent-prefix rules are shown for review. Approval controls belong to only the current target session. Drafts stay in memory; submitted text becomes part of Codex's normal session history, and follow-up turns consume model usage. Full interaction capture remains restricted to isolated benchmark turns created by Codexometer; Monitor does not harvest user prompts, reasoning, or arbitrary tool output.

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

The default destination is `/usr/local/bin`. For a user-local installation, set `CODEXOMETER_BIN_DIR="$HOME/.local/bin"` when running the installer. Keep your existing installation directory when upgrading and ensure it is on `PATH`.

On Windows PowerShell:

```powershell
$installer = Join-Path ([IO.Path]::GetTempPath()) "install-codexometer.ps1"
Invoke-WebRequest https://raw.githubusercontent.com/merefield/codexometer/main/install-release.ps1 -OutFile $installer
Set-ExecutionPolicy -Scope Process Bypass -Force
& $installer
Remove-Item $installer
```

The execution-policy override applies only to that PowerShell process; inspect the downloaded script before running it if required by your security policy.

Quit the running dashboard and re-run the relevant installer to upgrade or reinstall Codexometer, then check `codexometer --version`. It replaces the executable only after the downloaded artifact passes its checksum and version checks. Windows defaults to `%LOCALAPPDATA%\Programs\codexometer\bin`; add that to `PATH` if needed. Developers who prefer to build from source can use `go install github.com/merefield/codexometer@latest` with Go 1.26.6 or later, ensuring `GOBIN` (or the default Go bin directory) is on `PATH`.

Full installation, authentication, privacy, monitoring, and benchmarking guidance is available in the [README](https://github.com/merefield/codexometer#readme).

**GitHub:** https://github.com/merefield/codexometer

If you enjoy it, please give the project a ⭐ on GitHub!

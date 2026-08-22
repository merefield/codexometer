# Codexometer: a retro companion dashboard for Codex

[Codexometer](https://github.com/merefield/codexometer) is a small, retro terminal dashboard for monitoring Codex quota, local session activity, and benchmark runs. Leave it open in a second terminal window or pane to see every active usage window, its remaining capacity, and its reset time without repeatedly opening `/status` in the Codex session where you are working.

![codexometer|690x454](upload://xcWugpZYodk0zuFVYsDzluTemtf.png)

## What does it show?

The responsive interface has three main tabs:

- **Quota** — switch between Bars, Consumption Pace, Pie, and Fuel Tank presentations. A pace-aware health signal distinguishes quota that is safely tracking the reset cycle from quota burning too quickly. Codexometer can also learn an observed standard API-equivalent estimate for primary Codex windows, with conservative confidence and visible pricing provenance.
- **Monitor** — record locally observed token activity by root Codex session, with scrolling graphs and compact call, activity, output, and time-to-first-token telemetry. Explicitly linked spawned agents are folded into their parent session.
- **Benchmark** — run programmatically checked challenges across selected model and reasoning-level combinations, then compare outcomes, wall time, tokens, estimated API-equivalent cost, and rankings.

There are five colour themes, mouse-clickable controls, keyboard shortcuts, responsive layouts, and automatic support for additional rate-limit windows when Codex returns them.

## A much richer benchmarking system

The last two releases have developed the Benchmark tab from a result matrix into a scoped runner with live, inspectable run details.

[Codexometer v0.8.0](https://github.com/merefield/codexometer/releases/tag/v0.8.0) added:

- clickable details for completed and in-progress benchmark runs;
- live progress, model responses, moves, and benchmark results;
- one-key copying of the complete run detail;
- the ability to stop an active suite while retaining completed work; and
- model and reasoning-level scope controls with bulk selection and compatibility cues.

[Codexometer v0.9.0](https://github.com/merefield/codexometer/releases/tag/v0.9.0) adds:

- separate **Codexometer Core** and **Codexometer Extended** suites;
- per-task selection alongside model and reasoning-level scope;
- clearer **Run Scope** and **Run All** workflows;
- richer benchmark transcripts covering safe prompts, tool exchanges, moves, states, final responses, token usage, and API-equivalent cost; and
- stronger timeout, interruption, redaction, and credential-isolation safeguards.

Detailed interaction capture is restricted to benchmark-created turns. Ordinary Codex session message content remains excluded.

## Optional DigBench integration

Version 0.9.0 also adds an optional suite powered by [DigBench](https://digbench.ai/), or “Discovery in Games”. DigBench is a scientific-discovery benchmark containing 70 interactive games with undisclosed rules. Humans and AI agents receive the same states, available actions, and step budgets, and must discover each game’s mechanics through experimentation.

When a `DIGBENCH_API_TOKEN` is supplied, Codexometer discovers the current game catalogue at launch and exposes a dedicated DigBench suite. You can select particular `P-x` games, models, and reasoning levels, then follow the agent through each tool request, move, resulting state, and final response. Runs report live level and step progress and use the remote game state for win detection.

Without a DigBench token, the integration remains hidden and the normal Codexometer experience is unchanged. The token is kept out of spawned Codex environments, and captured benchmark content is bounded and sanitized to remove credentials, runtime identifiers, temporary paths, and terminal controls.

## Install or upgrade

Codexometer is written in Go and builds as a standalone binary for macOS, Windows, and Linux. The easiest installation or upgrade is:

```sh
go install github.com/merefield/codexometer@latest
codexometer --version
```

Go installs the executable into `GOBIN`, or normally `$HOME/go/bin`. Ensure that directory is on `PATH`. If the version command still finds an older installation, `command -v codexometer` on macOS/Linux or `Get-Command codexometer` in PowerShell will show which executable is being run.

Codexometer uses the prevailing Codex login for quota monitoring and, by default, benchmark turns. Users who prefer usage-based API billing can provide a dedicated benchmark API key, which Codexometer isolates from the normal Codex environment.

Full installation, authentication, privacy, and benchmarking guidance is available in the [README](https://github.com/merefield/codexometer#readme).

**GitHub:** https://github.com/merefield/codexometer

If you enjoy it, please give the project a ⭐ on GitHub!

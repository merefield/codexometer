# Codexometer: a retro companion dashboard for Codex

[Codexometer](https://github.com/merefield/codexometer) is a small, retro terminal dashboard for your current Codex quota. Leave it open in a second terminal window or pane to see every active usage window, its remaining capacity, and its reset time—without repeatedly opening `/status` in the Codex session where you are working.

![codexometer|690x454](upload://xcWugpZYodk0zuFVYsDzluTemtf.png)

## Why use it?

`/status` is useful, but it lives inside your active session. Codexometer turns the same quota information into an always-visible companion display that refreshes automatically and uses your prevailing Codex login—without handling your credentials itself.

The interface has three responsive tabs:

- **Quota** — switch between Bars, Consumption Pace, Pie, and Fuel Tank presentations. A pace-aware health signal distinguishes quota that is safely tracking the reset cycle from quota burning too quickly. Codexometer can also learn an observed standard API-equivalent estimate for primary Codex windows, showing both current consumption and an inferred 100% value with conservative confidence and visible pricing provenance.
- **Monitor** — record locally observed token activity by root Codex session, with scrolling graphs and compact call, activity, output, and time-to-first-token telemetry. It estimates each session's local share of observed account quota movement and highlights sessions needing input or approval when Codex exposes that state. Explicitly linked spawned agents are folded into their parent session.
- **Benchmark** — run deterministic, programmatically checked coding challenges across the available model and reasoning-level combinations. Compare pass/fail results, wall time, tokens, estimated API-equivalent cost, and rankings weighted towards cost, speed, or a balance of both.

There are five colour themes, mouse-clickable controls, keyboard shortcuts, responsive layouts, and automatic support for additional rate-limit windows when Codex returns them.

It works particularly well in:

- another Windows Terminal tab or split pane;
- a second Terminal or iTerm window on macOS;
- a tmux, Zellij, or other terminal-multiplexer pane;
- an Ubuntu terminal beside the Codex CLI.

Codexometer is written in Go and builds quickly as a standalone binary for macOS, Windows, and Linux. Install it with:

```sh
go install github.com/merefield/codexometer@latest
```

Make sure Go's binary directory—`GOBIN`, or normally `$HOME/go/bin`—is on your `PATH`. The README includes complete macOS, Linux, and Windows installation instructions.

**GitHub:** https://github.com/merefield/codexometer

If you enjoy it, please give the project a ⭐ on GitHub!

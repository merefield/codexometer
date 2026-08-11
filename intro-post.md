# Codexometer: a retro quota dashboard for Codex

[Codexometer](https://github.com/merefield/codexometer) is a small, retro terminal dashboard for your current Codex quota. Leave it open in a second terminal window or pane to see every active usage window, its remaining capacity, and its reset time—without repeatedly opening `/status` in the Codex session where you are working.

![codexometer|690x454](upload://xcWugpZYodk0zuFVYsDzluTemtf.png)

## Why use it?

`/status` is useful, but it lives inside your active session. Codexometer turns the same quota information into an always-visible companion display that refreshes automatically and uses your prevailing Codex login.

You can choose from five colour themes and several responsive displays, including a consumption-pace meter that shows whether you are using quota faster or slower than the current window is elapsing. There is also a local token-activity monitor and an opt-in deterministic model benchmark.

It works particularly well in:

- another Windows Terminal tab or split pane;
- a second Terminal or iTerm window on macOS;
- a tmux, Zellij, or other terminal-multiplexer pane;
- an Ubuntu terminal beside the Codex CLI.

Codexometer is written in Go and builds as a standalone binary for macOS, Windows, and Linux.

**GitHub:** https://github.com/merefield/codexometer

If you enjoy it, please give the project a ⭐ on GitHub!

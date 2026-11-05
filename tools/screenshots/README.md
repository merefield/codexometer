# README terminal screenshots

The README hero and main feature images are **real TUI renders**, rasterised
at 2× resolution. They are not the experimental web interface and are not
AI-generated mockups. Refresh the whole set for **every release**, even when
the visible change is just the version number.

## One-time setup

Use the Go version in `go.mod`, Node 24, and the existing frontend development
dependencies. From the repository root:

```sh
npm ci --prefix web
cd web
npx playwright install chromium
cd ..
```

On a minimal Linux machine, use `npx playwright install --with-deps chromium`
from `web` instead. This may require permission to install browser system
libraries. Normal users and release binaries need none of these extra tools.

## Capture and check

```sh
make screenshots
make screenshots-check
```

Without Make (including PowerShell):

```sh
node web/scripts/screenshots.mjs
node web/scripts/screenshots.mjs --check
```

The capture command exports test-only scenes through `Model.View()`, decodes
their ANSI colours and character widths with Charm's existing terminal screen
buffer, and uses Playwright/Chromium **only to draw those cells to PNG**. It
does not launch the application, read a Codex home, start a web server, retrieve
credentials, run a benchmark or send a command. Fixture write methods panic if
accidentally called. All network requests in the rasteriser are blocked.

The clock is frozen at 5 November 2026, 14:30 UTC; English, terminal dimensions,
animation phase and font files are fixed. The **version comes from
`internal/version/VERSION`**, not the developer's Git description. Vendored
DejaVu Sans Mono regular/bold fonts keep box drawing and Braille independent
of the host's installed fonts; their redistribution licence is in
[`fonts/LICENSE.txt`](fonts/LICENSE.txt). The browser is pinned by the existing
frontend lockfile. PNG rasterisation can still differ slightly across operating
systems or browser builds; generate on Linux for the most consistent results.

`make screenshots-check` needs Go and Node, but **no browser or npm install**.
It compares newly rendered terminal-cell hashes, the maintained version,
capture-source/font/lockfile hashes, and each committed PNG's hash against
[`assets/screenshots/manifest.json`](../../assets/screenshots/manifest.json).
It never rewrites images. Ordinary Go tests also verify that every scene fits
its terminal, renders deterministically and retains its key feature labels;
they do not write files unless export is explicitly requested.

## Release checklist

1. Finish the release changes and update `internal/version/VERSION`.
2. Run `make screenshots` from that checkout.
3. Open **every image** at readable size. Check clipping, missing glyphs,
   readable labels, meaningful states, synthetic-only content and the version.
4. Check that README captions and feature placement are still accurate. Keep
   the set focused; not every theme, language or minor option needs an image.
5. Run `make screenshots-check`, then commit the images and manifest with the
   release preparation. Do not edit, crop or annotate the generated PNGs by hand.
6. Follow the normal tagged release process. Tag validation checks the version,
   source fingerprints and PNG hashes without installing a browser or compiling
   Go again. PR CI additionally compares the current TUI frames.

On a capture failure, fix it and rerun the command before committing; generation
is not an atomic transaction across all seven PNGs. The check detects a partial
refresh. Temporary JSON frames are removed automatically. The previous manually
supplied `assets/codexometer.png` is preserved, but is no longer the README hero.

## Scenes

Edit `internal/ui/documentation_screenshots_test.go` to change the synthetic
scenes. Never substitute a real account, rollout file, path, session identifier,
approval capability or benchmark result.

| File | Feature | Theme |
| --- | --- | --- |
| `sessions.png` | README hero: sessions, activity, approval and completion | Hacker |
| `quota.png` | Quota Bars and aligned reset cycles | Hacker |
| `pace.png` | Consumption against elapsed time and projected trends | Nightshade |
| `approval.png` | Approval detail with commentary and command controls | Blue Steel |
| `usage.png` | Annual activity heatmap and account summary | Hacker |
| `benchmark.png` | Illustrative result matrix, not measured comparisons | Rust |
| `scheduler.png` | Follow-up scheduling within session detail | Ultraviolet |

Use `web/scripts/screenshots.mjs` only for rasterisation and freshness checks;
all UI layout belongs to the existing Go renderer. No new screenshot code,
fonts, Node dependencies or PNGs are embedded into the shipped terminal binary.

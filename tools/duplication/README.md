# Duplication ratchet

The separate **Duplication ratchet** CI job measures tracked, maintained Go and
browser source with pinned jscpd 5.4.2. It runs alongside, not inside, the Go and
web jobs. jscpd is development tooling only: the application does not include or
run it. No LLM, embeddings, network inference, API key or Codex quota is used.
Scanner "tokens" are source-code tokens, not billable model tokens.

## Local checks

From the repository root, using Node 24:

```sh
npm ci --prefix tools/duplication --ignore-scripts
npm --prefix tools/duplication test
npm --prefix tools/duplication run check
```

The local check compares with the committed baseline. To reproduce CI's extra
comparison with the actual PR base, check out that commit in another worktree
and use the **base's** checker once it contains this tooling:

```sh
git worktree add --detach /tmp/codexometer-duplication-base origin/main
npm ci --prefix /tmp/codexometer-duplication-base/tools/duplication --ignore-scripts
node /tmp/codexometer-duplication-base/tools/duplication/check.mjs \
  --root "$PWD" --base /tmp/codexometer-duplication-base \
  --output /tmp/codexometer-duplication-report
```

Use an unused worktree path, and replace `origin/main` with the PR's exact base
SHA when diagnosing an older CI run. Only tracked files are scanned: `git add`
new sources before checking them. A scanner/installation/configuration error
fails the job; bootstrap never hides operational failures.

## What is gated?

Each production scan must independently stay equal or improve in **all three**
absolute metrics: clone count, duplicated lines and duplicated tokens.

| Scan          | Minimum tokens / lines | Matching                                                    |
| ------------- | ---------------------- | ----------------------------------------------------------- |
| `exact-50`    | 50 / 5                 | Token matches, ignoring comments                            |
| `renamed-50`  | 50 / 5                 | Also normalises consistent identifier renaming and literals |
| `exact-100`   | 100 / 10               | Larger token matches                                        |
| `renamed-100` | 100 / 10               | Larger normalised matches                                   |

Percentages are informational: removing non-duplicated code can increase a
percentage without adding duplication. A lower metric cannot offset an increase
in another one. This is a net-metrics gate, not a ban on every new clone pair;
new pairs are still highlighted for review.

Production means maintained `.go` files excluding `_test.go`, plus browser
source under `web/src` (TypeScript, JavaScript, Svelte and CSS).
Generated files, embedded build output, dependencies and vendored source are
excluded. Go and browser tests are measured separately at the smaller threshold
but are **report-only**, so useful regression fixtures do not fail the gate.
The tooling itself, documentation and other file types are outside this policy.
Missing, oversized, symlinked or outside-repository selected sources fail instead
of silently disappearing from the measurement.

Normalised matches are refactoring candidates, not proof of identical behaviour.
Palettes, glyph tables and similar data can legitimately resemble one another.
Do not consolidate them automatically or weaken tests just to reduce a metric.

## Baseline lifecycle

`baseline.json` is versioned CI policy, unrelated to the application's model
benchmarks or usage history. `policy.json` pins the scanner version, selection
rules, matching settings and thresholds.

1. The first PR introduces a `pending-bootstrap` sentinel. Because its base has
   no duplication policy yet, that first measurement establishes the benchmark
   without applying an invented ceiling. The duplication gate passes regardless
   of those initial counts, provided the scanner and tooling tests succeed.
2. After all ordinary CI jobs pass on `main`, **Update duplication baseline**
   measures again and commits the initial numerical baseline with provenance.
3. Later PRs use the checker, policy and baseline from their main-branch base
   commit, not the PR's copy. They compare against both that committed ceiling
   and fresh measurements of the same base, taking the lower bound for each
   metric. A delayed baseline update therefore cannot hide a regression. The
   inherited baseline file must remain byte-for-byte unchanged in PRs; only
   successful main CI writes it. Ordinary application PRs use this one
   authoritative comparison, not a duplicate candidate run.
4. After another successful main build, the main-only writer commits smaller
   limits if any improved. Equality leaves the baseline and provenance unchanged.
   Missing, corrupt or incompatible established baselines fail closed unless
   an explicit policy migration has passed the old/new checks below.

PR and fork jobs have read-only repository permissions. Only the main-only
writer receives `contents: write`, waits for Go/web/duplication success, and
stages `baseline.json` alone. It skips an outdated main SHA and never force-pushes
over concurrent work. Its normal `GITHUB_TOKEN` commit does not recursively
start another CI run.

Repository administrators should require **Duplication ratchet** in branch
rules and require an up-to-date branch (or equivalent merge-queue validation)
to prevent merges that bypass the check. Adding a workflow alone does not enable
branch protection. The PR comparison is pinned to its base SHA, not a moving
`main` fetched halfway through the job. Push CI compares with the pre-push main
commit. Actions and repository rules must permit the main-only writer's baseline
commit; otherwise that job fails and preserves its measured artifact for
diagnosis. Fix the repository configuration before rerunning it; do not edit a
PR's baseline or grant broad protection bypass just to make the check green.

## Checker and policy maintenance

When maintained duplication tooling changes (runtime `.mjs`, policy, package or
lock files), the trusted verifier also runs the candidate checker against the
same source/base, using **main's scanner, policy and baseline**. Its metrics,
limits and provenance must match the authoritative result. It also verifies
that the candidate's installed scanner matches its declared version. This
targeted compatibility check catches a broken/no-op/mismeasuring proposed
checker before it becomes main's checker. Documentation/test-only changes do
not trigger additional production scans.

For an intentional scanner-version or policy change:

1. Create a worktree at the exact main comparison SHA as shown above. Update the
   candidate policy, package and lock as needed, but **do not edit the baseline**.
2. Record an explicit migration with the exact old policy, new policy hash and a
   meaningful reason:

   ```sh
   node tools/duplication/verify.mjs --prepare-migration \
     --root "$PWD" --base /tmp/codexometer-duplication-base \
     --reason "Explain the scanner or measurement-rule change"
   ```

3. Commit the resulting `tools/duplication/migration.json` with the tooling
   changes. CI still gates production under main's **old** rules, verifies the
   candidate checker under those rules, then separately compares head and base
   under the **proposed new** rules/scanner. Both comparisons must pass. Preserve
   compatibility with the previous checker CLI for this validation.
4. Review both measurements in the summary/artifact. The old baseline remains
   untouched throughout the PR. After merge and successful main CI, the writer
   migrates it to measurements under the new policy. The migration record may
   remain as provenance; it is ignored once the baseline matches the policy.

A missing/mismatched migration record fails; it cannot silently reset an
ordinary feature PR's ceiling. Do not raise limits to make an application
change green. Any change to the CI machinery itself still needs code review;
the duplication check is not a substitute for protecting workflow changes.

## Actionable output for humans and Codex

Download the **duplication-report** artifact from the standalone CI job:

The GitHub job summary contains the actual/limit statistics table, including
PASS/FAIL/BOOTSTRAP status. The separate test-source table explicitly labels its
statistics REPORT ONLY. CI logs contain a concise result and failure diagnostics,
not a duplicate table. Local checks print the summary directly.

- `summary.md`: actual/limit table, exact increases and a shortlist of new pairs.
- `result.json`: policy/commit provenance, per-scan ceilings, actual-base metrics,
  effective limits, explicit regression deltas, and clone pairs with repository-
  relative paths, both line ranges, match kind, source fragments and `isNew`.
- `production/<scan>/jscpd-report.json`: full production clone reports.
- `base/<scan>/jscpd-report.json`: comparison-base reports.
- `tests/<scan>/jscpd-report.json`: report-only test duplication.
- `*/<scan>/inputs.json` and `config.json`: exact source manifests and scanner
  settings, so exclusions and scope are inspectable.
- `baseline.json`: the measured candidate, not permission to loosen a ceiling.
- `verification.json`: whether targeted checker validation was needed, and which
  tooling files changed. Tooling-maintenance PRs additionally include
  `candidate-validation/` and, for policy changes, `proposed-policy/` reports.

Paths in `result.json` resolve relative to the artifact root. Source locations
inside clone pairs resolve relative to the repository root. Up to 20 GitHub error
annotations point at candidate source locations on a failing scan. Reports are
retained for 30 days; code fragments are repository source, so respect the same
access restrictions as the repository when sharing them.

Codex can read `result.json`, identify the regressed metric and both code ranges,
inspect the fragments, apply a behaviour-preserving shared implementation, and
rerun the check plus the relevant application tests. Reports guide refactoring;
they do not authorise automated changes or prove semantic equivalence.

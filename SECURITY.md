# Local interface security

Codexometer is a local companion, not a remotely hosted service. The terminal
is the default; without `--web` no HTTP listener or browser collectors start.
The experimental browser interface remains read-only. These controls reduce
exposure; they do not defend against a compromised OS account or browser.

## Maintainer checklist

- Bind only IPv4 loopback. Do not add proxy-header trust, remote bind addresses
  or permissive CORS as convenience fixes. Tunnels/public hosting are unsupported.
- Keep Host, Origin and Fetch Metadata checks outside routing so failures and
  unknown routes receive the same security headers. They complement, not replace,
  bearer authentication: non-browser programs can forge browser headers.
- Every data endpoint must use the shared authorization wrapper. Test missing,
  wrong, expired, cookie-only and query-only credentials. New Codex mutations
  require separate review and explicit control permissions; this PR adds none.
- Pair only with same-origin JSON, a bounded strict body, and a short-lived
  one-use secret. Invalid attempts must not consume the legitimate pairing secret
  or create a permanent lockout. Never log tokens or put bearer tokens in URLs.
- Keep temporary browser capabilities in sessionStorage, not persistent display
  preferences. Browser session restoration can retain them; stopping the server
  revokes them. Same-origin script injection can access browser storage.
- Keep display DTOs explicit: never serialize raw Codex credentials, approval/input
  capabilities, account identities or upstream error objects.
- Render untrusted replies, commands, names and metadata as escaped text. Do not
  introduce raw HTML, executable Markdown, remote scripts or remote images without
  a new security review. CSP complements escaping; it does not replace it.
- In the terminal, sanitize untrusted display strings before styling, wrapping,
  truncating or extracting names. Preserve the original IDs, paths and command
  requests for routing/execution. The sanitiser removes terminal controls and
  bidi formatting; it is not secret redaction or a full visual-spoofing defence.
- Preserve responsive click geometry, English presentation snapshots and existing
  approval tests when changing terminal display sanitisation.

## Resource bounds

The HTTP server limits headers to 8 KiB, pairing bodies to 1 KiB, header reads to
5 seconds, request reads to 10 seconds, ordinary writes to 15 seconds and idle
connections to 30 seconds. SSE has 16 live slots and renews its own five-second
write deadline, allowing healthy streams to remain connected. HEAD cannot reserve
a streaming slot. Background collectors and retained history also remain bounded.

These are not comprehensive denial-of-service protection against local malware:
there is no overall connection cap or client quota. Unauthenticated failures are
cheap, and adding a shared attempt lockout could itself lock out the user. The
current threat model does not justify an additional rate-limiting dependency.

Camera, microphone, geolocation, payment and USB browser features are disabled by
Permissions Policy, framing is denied, and resources have a same-origin policy.
Dynamic chart styles remain allowed; tightening CSP must be browser-tested.

## Dependency checks (CI only)

The Security workflow runs for PRs, pushes to main, weekly and on manual dispatch.
It has read-only repository permissions, requires no secrets, and does not upgrade
dependencies automatically.

- `govulncheck` is pinned to v1.8.0 and uses the current Go vulnerability database.
  It checks reachable vulnerabilities for the **Linux build** with the Go version
  in go.mod. CI disables automatic toolchain switching so a newer toolchain cannot
  hide issues in the version actually maintained here. This is not an exhaustive
  analysis of Windows/macOS-only code or a guarantee against undisclosed flaws.
- npm audits the committed lockfile without installing packages or running their
  scripts. Runtime dependencies and the full tree (including build tooling) have
  separate checks. Both block on high/critical advisories; lower severities remain
  visible for triage. A tooling advisory is not automatically a shipped browser
  vulnerability, but build tools can affect the release supply chain.
- Advisory-service failures fail the check rather than reporting a clean scan.
  Investigate failures and update dependencies deliberately; do not use blind
  `npm audit fix --force` or blanket suppressions.

To repeat locally, use the project's Go toolchain and run:

```sh
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
cd web
npm audit --omit=dev --audit-level=high
npm audit --audit-level=high
```

If your Go launcher auto-selects toolchains, set `GOTOOLCHAIN` to the exact version
in go.mod for the scan. No scanner runs at app startup or ships as a runtime
dependency. Tests include hostile web access, malicious rendering, terminal
control payloads and sanitizer fuzz seeds; optional fuzzing can run with
`go test ./internal/codex -run '^$' -fuzz FuzzSessionContextSanitization -fuzztime 10s`.

Do not include real pairing links, tokens, private session text or credentials in
public bug reports. Use synthetic reproductions; use GitHub private vulnerability
reporting when available for sensitive disclosures.

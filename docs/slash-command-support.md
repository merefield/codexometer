# Slash-command support audit

Upstream inventory checked against **Codex CLI 0.170.0** on **2026-11-19**: [command registry](https://github.com/openai/codex/blob/rust-v0.170.0/codex-rs/tui/src/slash_command.rs), [native command dispatch](https://github.com/openai/codex/blob/rust-v0.170.0/codex-rs/tui/src/chatwidget/slash_dispatch.rs), [app-server method registry](https://github.com/openai/codex/blob/rust-v0.170.0/codex-rs/app-server-protocol/src/protocol/common.rs), [thread settings types](https://github.com/openai/codex/blob/rust-v0.170.0/codex-rs/app-server-protocol/src/protocol/v2/thread.rs), and [native directory-change workflow](https://github.com/openai/codex/blob/rust-v0.170.0/codex-rs/tui/src/app/working_directory.rs). This is not a claim of compatibility with every newer Codex release.

The tables cover all **65 registered command variants** and the three additional accepted aliases `/cwd`, `/pet` and `/clean`: **68 upstream command names** in total. `/presets` and `/config` were absent from the earlier inventory and are now included as gaps, not newly implemented features. Codexometer’s local `/help` and terminal `/commands` entries, and model-advertised speed aliases, are outside that count. Platform, feature and debug-build filters affect actual visibility. The [published reference](https://learn.chatgpt.com/docs/developer-commands#built-in-slash-commands) is supplementary; the pinned registry determines this inventory. Codexometer has explicit adapters/local UI implementations; it does not send slash commands as model prompts. There is no generic server endpoint to discover or execute the CLI's slash commands.

“Missing” below means no Codexometer slash adapter, **not** that the server lacks the capability. Live commands require a loaded session and shared app-server; browser control commands require writable mode. Windows' existing file-based monitoring path does not gain live server commands from this documentation update. Registry coverage and current adapters are checked against the pinned source and local implementation; proposed API mechanisms still need workflow-specific validation before implementation.

## Priority and complexity scale

These are proposed priorities for Codexometer's session-monitoring/control role, informed by the recent directory and approval issues and opportunities to reuse existing UI. They are judgments, not measured demand or delivery commitments. **Complexity rates the proposed next increment below**, across terminal and browser interfaces where applicable, including state/reconnect handling, confirmation and meaningful regression coverage. It does not rate the implementation already completed, or promise full native CLI parity.

| Priority | Meaning |
| --- | --- |
| **P1 — next** | Common workflow, diagnostics or an operation that unblocks a session; address before optional expansion. |
| **P2 — later** | Useful capability after the core session controls and lifecycle foundation. |
| **P3 — optional** | Niche, cosmetic or broader account/configuration integration; schedule when demand justifies it. |
| **Defer** | A substantial expansion beyond the current product; no near-term implementation recommended. |
| **Done** | Current scoped implementation is sufficient; no additional increment proposed. |
| **Exclude** | Internal/debug, retired or unregistered names; no product implementation proposed. |

| Complexity | Meaning |
| --- | --- |
| **Low** | Reuse an existing action/view, add an alias or a small local preference; little new state. |
| **Medium** | Add a bounded API adapter, editor/picker or progress view with target/state validation. |
| **High** | Add a stateful lifecycle, history/repository, authentication, configuration or input workflow spanning several components. |
| **Very high** | Add project-context/trust transitions, multimodal transport or an external integration with substantial new infrastructure. |
| **—** | Not applicable: no further implementation is proposed for Done/Exclude entries. |

Aliases on one row share the rating and implementation. Lifecycle commands share a thread-handoff foundation, so their costs should not be added as independent projects. P1 is a value/need ranking, not a claim that every P1 is a quick win. Work blocked on a foundation comes after that foundation even if it has the same priority.

### Delivered through v0.31.0

- Native `/permissions` profiles, approval policies and Human/Auto-review reviewer selection, including quoted inline arguments and managed-policy checks.
- Native `/approve` exact-denial retry consent, full inspection even with extra confirmations disabled, and root-thread-scoped assessment telemetry in terminal/web.
- Provider-aware model/speed discovery and threshold validation on Codex 0.169+, plus thread/environment-aware `/hooks` discovery on 0.166+.
- In the terminal and writable web composer, a space after a supported command opens its parameter list. Type `/goal resume` to narrow it to **Resume goal**, or type through the model and reasoning choices. Browsing does not run a command.
- Reversible settings normally need no extra confirmation. Choose `/model` during work to set the model for the next turn without interrupting the current one. The launch confirmation policy does not override Codex's permissions or mandatory human checkpoints.

Local display commands remain daemon-independent. Server-backed menus use a bounded, memory-only catalogue tree; execution always revalidates fresh server options. These delivered items are not future roadmap work.

## Implemented and partial support

| Command | Support | Priority | Complexity | Implementation and limits |
| --- | --- | --- | --- | --- |
| `/rename` | Implemented | Done | — | `thread/name/set`; explicit selection applies directly under the balanced default, with optional extra confirmation. Works during active turns and does not send a prompt. |
| `/goal` | Session adapter | Done | — | Session-scoped `thread/goal/get`, `set`, `clear`; native objective/edit/pause/resume/clear forms plus a token-budget editor. **Resume after quota recovery** is a local, one-shot trigger for an active or usage-limited goal, not a scheduled slash message. It requires observed goal/quota state and a live idle shared app-server session before dispatch. Fresh review/verification, default mutation confirmation, live goal notifications and stale-safe snapshot fallback shared by TUI/web. Goal state follows row statusline fields and precedes full-detail controls. Requires a goal-enabled shared app-server. |
| `/cd <path>` | Implemented with narrower semantics | P1 | Very high | Confirmed `thread/settings/update`, sending only `threadId` and `cwd`. Requires idle/local session, empty queue, no pending approval and an existing directory. Resolves relative paths from the running session directory, expands `~`/`~/`, rechecks symlinks and verifies the result from running configuration. No write retries. See the semantics below. Proposed next increment: destination project-context reload with trust/permission validation and a verified conversation handoff. Reuse the lifecycle foundation below; native-client cooperation may still be necessary. |
| `/pwd`, `/cwd` | Implemented aliases | Done | — | Read-only `thread/resume` **without overrides** reports the running directory, including during active turns. Captured `thread/read.cwd` metadata is not used as the answer. The session directory/footer also follows live directory reads and settings notifications. |
| `/model` | Session adapter | Done | — | Live model/reasoning catalogue; send one verified `thread/settings/update` immediately on idle or working sessions, for subsequent turns only. No Codexometer-side queue or automatic retry. Fresh provider/settings/catalogue checks precede the write. The balanced default needs no extra confirmation. Selecting a model clears its explicit speed override. Session-only scope is intentional; no native persistent-default editor is proposed. |
| `/plan` | Partial | P2 | Medium | Advertised collaboration-mode selection; no native optional prompt argument. Proposed next increment: explicit review of an inline plan prompt and confirmed mode/prompt submission without losing the draft. |
| `/permissions` | Session adapter | Done | — | Native allowed profiles, approval policies and Human/Auto-review reviewer selection. Supports `/permissions <profile-id> [--approval-policy=<policy>] [--reviewer=user\|auto_review]`, shell-style quoting and native granular policy JSON. May update subsequent-turn settings during work, but retained/overflowed human requests and native approval/input waits block writes. Managed requirements and the `guardian_approval` feature govern availability. Requested settings are read back; uncertain writes are not retried. Browser edits invalidate prepared/in-flight inline reviews. Older servers without reviewer metadata retain profile-only controls; no global configuration editor is implied. |
| `/approve` | Session adapter | Done | — | `thread/approveGuardianDeniedAction` records consent for one exact recent native denial, not direct execution. The retry still passes through Auto-review; critical-risk and mandatory human checks are not bypassed. Full action/rationale inspection precedes consent even with `--confirmation none`. Inventories and assessment display are root-thread-scoped, memory-only and connection-local: up to ten denials per thread and 256 tracked threads, with spent-consent replay protection. Unknown/incomplete actions stay in Codex. Read-only web displays assessments but cannot send consent. |
| `/skills` | Browse only | P2 | Medium | `skills/list` inventory/help. No invocation or configuration flow; `skills/config/write` exists but needs scope/review UI. Proposed next increment: invoke an advertised skill through structured input after review. Configuration/enablement writes are a separate High-complexity extension. |
| `/apps` | Browse only | P3 | High | `app/list` inventory/help. Installation, linking and authentication workflows are absent. Proposed next increment: a guided linking/authentication workflow with return/cancellation handling; reuse upstream flows rather than inventing installation semantics. |
| `/mcp` | Browse only | P2 | High | `mcpServerStatus/list` and tool help. No `/mcp login`, resource browser or arbitrary tool invocation. Related APIs exist but need dedicated authentication/execution UI. Proposed next increment: server-specific login/status and verbose inspection. Arbitrary tool execution remains a separate reviewed workflow. |
| `/hooks` | Browse only | P3 | High | `hooks/list`; on 0.166+ discovery is scoped to the loaded thread and its execution environments, with supplied environment attribution. Older servers retain directory-scoped discovery; genuine server errors do not become a different inventory. No lifecycle-hook editor or configuration-write review. Proposed next increment: a scope-aware editor with preview and reload handling. |
| `/experimental` | Browse only | P3 | Medium | `experimentalFeature/list`. Enablement changes process/configuration scope, rather than a single session; no mutation UI. Proposed next increment: review an advertised enablement change with its process/configuration scope and refresh/restart effects made clear. |
| `/usage` | Local equivalent | Done | — | Opens the existing Codexometer usage-history view. Existing quota/reset views and their confirmation flows are retained. No account mutation is sent by the alias. |
| `/copy` | Local equivalent | Done | — | Reuses the selected session’s Copy action and complete observed reply/activity payload. Missing or ineligible text gives feedback. Historical-fragment selection remains outside this increment. |
| `/theme` | Local equivalent | Done | — | Terminal theme picker and browser’s existing theme selector; persists Codexometer display preferences. Native syntax-theme configuration is outside this scope. |
| `/exit`, `/quit` | Local equivalents | Done | — | Terminal aliases reuse local Quit. Browser aliases leave full session detail for Sessions; the web server and Codex sessions remain running. |
| `/statusline` | Local equivalent | Done | — | Codexometer's own footer picker; also available in read-only web mode. Does not edit the native CLI's status line. |
| `/help` | Local catalogue | Done | — | Codexometer catalogue entry; absent from the upstream registry. |
| `/commands` | Local catalogue shortcut | Done | — | Terminal-only shortcut opening the existing command catalogue; the browser has the **/ COMMANDS** button. Not an upstream registry command. |
| Advertised speed aliases, e.g. `/fast` | Session adapter | Done | — | Show all live model-advertised speeds plus the default in one picker. Up/Down highlights; Enter APPLY or a click sends a verified session override under the balanced default. Strict confirmation mode adds review. Works during a turn or pending approval; applies to subsequent turns, not an inference request in flight. On 0.169+, discovery uses the session's saved provider. No local queue or global-default changes; the composer retains the public alias rather than the internal tier path. |

The local `/usage`, `/copy`, `/theme`, `/exit`, `/quit` and `/statusline` entries are merged into the interface catalogue and do not become server RPCs. They work without a shared Codex daemon; browser full detail offers the local catalogue in read-only mode. Slash names are reserved against model-advertised speed aliases.

`/help` is Codexometer's catalogue entry, absent from this upstream registry. Speed aliases such as `/fast` appear only when the current model advertises that tier; IDs are not guessed. They change session overrides rather than native persistent defaults.

### Directory-change semantics

Codexometer's `/cd` keeps the selected thread ID/history and sends only `threadId` and `cwd` to `thread/settings/update`. It sends no permission/model/global configuration overrides; the native server handles directory/environment and permission validation. Directory changes are restricted to local execution environments; `/cwd` can display a remote session's reported directory. This is not a promise that a directory update reproduces every native project-context transition.

Native `/cd` is a larger client workflow: destination project-configuration reload, trust checks, checks for subagents/background terminals, and potentially a conversation fork into a new thread. Codexometer **does not perform that workflow**, reload project configuration/instruction sources, or move subagents or already-running terminals. Other clients attached to the same thread share its server settings, but may retain their own local project/UI context and send their own directory overrides. The confirmation explains this. Use native `/cd` for its full project-context transition. Success is reported only after running configuration reflects the new directory; ignored fields, server rejection, timeout or reconnection remain unconfirmed.

## Missing commands with server capabilities or conversation workflows

These are adapter/workflow gaps. Where an API exists it is listed.

| Command | Priority | Complexity | Mechanism / why missing |
| --- | --- | --- | --- |
| `/new` | P1 | High | `thread/start` exists; needs creation options and selection/handoff to the new thread. Proposed next increment: build the common thread-creation/handoff foundation, including subscriptions, drafts and selection. |
| `/clear` | P2 | Medium | Native clear starts a new chat and clears its terminal; no combined local clear/`thread/start` workflow. Hiding monitor rows is different. Proposed next increment: a local clear/new-chat action after the /new foundation exists; Medium assumes that dependency is complete. |
| `/resume` | Implemented | Done | Search current-account saved sessions, preview completed turns, and explicitly load a selected thread into Sessions detail without sending a turn. The browser only enables the Resume action in write mode. |
| `/fork` | P1 | High | `thread/fork` exists; needs options, confirmation and handoff while preserving source drafts. Proposed next increment: a reviewed fork and verified new-thread selection, retaining source history/draft and correct telemetry accounting. |
| `/worktree` | P2 | Very high | Needs actual Git worktree creation/ownership/cleanup plus thread creation/fork. No worktree manager; a cwd update is insufficient. Proposed next increment: managed worktree creation and conversation transition after thread handoff and project-context changes are supported. |
| `/archive` | P2 | Medium | `thread/archive` exists; no archive confirmation/handoff. Close/Close All only hide rows. Proposed next increment: confirm the exact session, observe archive completion and select another row without discarding unrelated drafts. |
| `/delete` | P3 | Medium | `thread/delete` exists; no destructive-delete confirmation and history/selection cleanup. Proposed next increment: explicit permanent-delete confirmation and verified removal, with stale-target and selection/history cleanup. |
| `/compact` | P1 | Medium | `thread/compact/start` exists; needs state checks and completion/error progress rather than reporting a queued operation as done. Proposed next increment: confirmed start plus completion/error tracking, keeping polling and approvals responsive throughout. |
| `/recap` | P2 | High | Native summarization workflow has no adapter/progress UI; verify that workflow before choosing a server operation or prompt. Provisional estimate: first establish the native summarization semantics, then add a reviewed summary workflow without changing the source conversation unexpectedly. |
| `/review` | P2 | High | `review/start` exists; no review-target picker, delivery mode or progress UI. Proposed next increment: target/delivery selection, confirmed start and a dedicated review-progress/result view. |
| `/side`, `/btw` | P3 | High | No slash-driven side-conversation creation/return workflow. Codexometer already observes persisted attached side chats as independent rows, preserving their attachment separately from spawned-agent lineage; observation is not native creation/navigation parity. Proposed next increment: reviewed creation, exact thread routing, separate drafts and source-thread return using the lifecycle foundation. |
| `/ps` | Implemented | Done | Lists background terminals for the selected connected session using `thread/backgroundTerminals/list`, with refresh. Read-only in both interfaces; does not stop processes. |
| `/stop`, `/clean` | P1 | Medium | `thread/backgroundTerminals/clean` exists; no session-scoped stop-all review and verification. Turn interruption is a different action. Proposed next increment: confirm and verify stop-all for the selected session, using the /ps inventory and leaving other sessions alone. |
| `/status` | P1 | Medium | Settings/usage reads exist; detail/footer show related data, but no consolidated status slash view. Proposed next increment: a consolidated read-only view of running configuration, permission scope, directory and usage; live configuration adds work beyond a navigation alias. |
| `/debug-config` | P2 | Medium | `config/read` with layers and `configRequirements/read` exist; no directory-aware configuration-source/requirements inspector. Proposed next increment: a read-only effective-layer/requirements inspector with bounded display and sensitive-value handling. |
| `/daybreak` | P3 | Medium | `thread/metadata/update` can save Daybreak choice; no dedicated feature-aware toggle/state adapter. Do not invent an experimental flag ID. Proposed next increment: show availability/current choice and confirm the per-thread metadata update; it must not imply access is granted. |
| `/memories` | P3 | High | `memory/status`, `thread/memoryMode/set` and configuration APIs exist; no scope-specific settings UI. Thread mode and global generation settings differ. Proposed next increment: display current memory state, then review thread-mode changes separately from persistent generation/configuration changes. |
| `/import` | P3 | Very high | `externalAgentConfig/detect`/`import` exist; no selection, preview, progress or configuration/history confirmation. Proposed next increment: preview/import selected configuration and history with conflict handling, progress and recovery; optional cross-tool scope. |
| `/plugins` | P3 | High | Listing/search/read/install/uninstall APIs exist; no lifecycle/authentication and installation/configuration-review workflow. Proposed next increment: read installed/available details, then reviewed installation/removal with dependency, authentication and effective-scope handling. |
| `/logout` | P3 | Medium | `account/logout` exists; needs account-level confirmation and recovery for consequences to shared clients. Proposed next increment: account-level identity/scope confirmation and a clear disconnected/re-authentication state for shared clients. |
| `/feedback` | P3 | High | `feedback/upload` exists; no log-selection and data-sharing preview/consent workflow. Proposed next increment: select and preview the actual diagnostic payload before consent/upload, with bounded collection and failure/cancellation handling. |
| `/setup-default-sandbox` | P3 | High | `windowsSandbox/setupStart`/`readiness` exist; no Windows elevated-sandbox setup workflow. Not applicable to macOS local sessions. Proposed next increment: the Windows setup/readiness/elevation workflow and platform-specific recovery; requires real Windows verification. |
| `/voice` | Defer | Very high | `thread/realtime/*`, including voice listing, exist; no microphone/audio transport, device settings or realtime conversation UI. Proposed scope if demanded: audio-device permissions, transport, realtime state, cancellation and reconnection in both interfaces. This is a major product expansion. |
| `/daemon` | P2 | Medium | Native daemon management and `server/diagnostics` exist; no lifecycle-management command. Stopping/restarting affects other clients. Proposed next increment: read-only daemon diagnostics/status. Start/stop/restart management is a separate P3/High workflow with shared-client confirmation and disconnection/recovery handling. |
| `/warnings` | P1 | Medium | Server/config warning notifications exist; no retained warning/diagnostic browser. Ordinary error notices are only partial analogues. Proposed next increment: a bounded retained-warning view with source/time, details and freshness; useful for explaining stalled or rejected actions. |

## Missing native UI commands

These require Codexometer UX, not forwarding the command name to a server.

| Command | Priority | Complexity | Why missing / existing analogue |
| --- | --- | --- | --- |
| `/presets` | P3 | High | Native saved prompt/slash-command presets use the CLI's quick-dial picker and local settings; no Codexometer adapter or preset editor exists. Proposed next increment: explicitly scoped save/edit/delete and invocation of reusable prompts/commands, with separate persistence, target validation and confirmation of any resulting action. Never treat a saved slash command as a model prompt. |
| `/config` | P3 | High | Native preferences browser launches editors for CLI-local presentation, providers and other settings. Codexometer has local theme/statusline preferences but no equivalent configuration hub or native global-settings editor. Proposed next increment: begin with the read-only `/debug-config` foundation; design any persistent editor separately with effective-scope, ownership and reload/restart handling. |
| `/agents` | P2 | High | Native agent command center has no adapter; linked-session grouping is not a matching management center. Proposed next increment: a dedicated agent state/management view with capability-aware controls and exact child/root routing. |
| `/subagents` | Implemented | Done | Opens a read-only picker for linked agents and attributable child transcripts; the parent remains the target for composing and approvals. |
| `/export` | P2 | High | No Markdown export; needs complete paginated history plus a file/download destination. Proposed next increment: complete paginated transcript retrieval and Markdown export with bounded streaming/file/download handling; existing excerpts are insufficient. |
| `/diff` | P2 | High | No repository diff browser with tracked/untracked handling and explicit repository context. Proposed next increment: a directory-aware tracked/untracked diff view with large/binary-file handling and no inferred repository scope. |
| `/mention` | P2 | High | No structured file-mention picker or associated attachment/input metadata routing. Proposed next increment: file search/selection plus structured input metadata through review, send/steer and draft handling. |
| `/init` | P2 | Medium | Native command asks Codex to create `AGENTS.md`; no explicit generation/overwrite review. It is deliberately not silently converted to a prompt. Proposed next increment: explicitly review a request to generate AGENTS.md in the selected directory; report request/agent result rather than claiming the file was created immediately. |
| `/ide` | Defer | Very high | No native IDE-context integration for selection/open files. Proposed scope if demanded: discover/connect an editor and securely route selections/open-file context, including stale/different-workspace handling. |
| `/app` | P3 | Medium | No desktop-app handoff/deep-link action; upstream visibility is platform-dependent. Proposed next increment: a platform-aware desktop-app handoff with validated thread/directory identity and an unavailable-app fallback. |
| `/raw` | P3 | High | Native raw-scrollback toggle cannot control Codexometer's separate rendering; no equivalent local switch. Proposed next increment: a Codexometer-owned scrollback/copy-friendly rendering mode that preserves live input and state; it is not a server toggle. |
| `/tui` | P3 | Medium | Chooses native CLI mode for its next launch; no corresponding Codexometer launch-mode picker. Proposed next increment: local next-launch mode preferences with explicit precedence over flags and terminal capability checks. |
| `/title` | P3 | Medium | No local title-field picker or native title configuration editor. Proposed next increment: a local title-field picker with persistence and safe terminal/browser title updates, reusing statusline field selection. |
| `/pets`, `/pet` | Defer | High | Native terminal-pet picker has no counterpart here. Proposed scope if demanded: terminal/browser pet assets, animation/state and preference UI; cosmetic expansion outside core monitoring. |
| `/keymap` | P3 | High | No shortcut-remapping editor; native keybindings belong to another interface. Proposed next increment: local remapping with composer/modal precedence, conflict checks and accurate help across all views. |
| `/vim` | P3 | High | No Vim-mode composer. Proposed next increment: composer modes/editing with Unicode, paste, shortcuts, focus and review-modal interactions verified in both interfaces. |

## Debug entries and older documented names

| Command | Priority | Complexity | Treatment |
| --- | --- | --- | --- |
| `/rollout` | Exclude | — | Debug-build registry entry; no rollout-path slash inspector. |
| `/test-approval` | Exclude | — | Debug-build synthetic approval request; not a user command here. Real approvals already have dedicated controls. |
| `/debug-m-drop`, `/debug-m-update` | Exclude | — | Registry descriptions say “DO NOT USE”; intentionally not exposed. |
| `/agent` | Exclude | — | Older published name; this registry uses `/agents`, not a separate `/agent` command. Track /agents above instead; add a compatibility alias only if there is demonstrated need. |
| `/personality` | Exclude | — | Published but absent from this registry; protocol personality fields are deprecated. No current CLI style selector to reproduce. |
| `/sandbox-add-read-dir` | Exclude | — | Published/platform-specific older entry, absent from this registry. Do not assume availability in every installed version. Reassess only against a version/platform that actually advertises this operation. |

## Recommended delivery order

1. **P1 read-only additions:** `/status` and `/warnings` views. `/ps`, `/usage` and `/copy` are implemented. They build on existing controls and improve visibility before adding more mutations.
2. **P1 session controls:** `/compact` and reviewed `/stop`/`clean` built on `/ps`. `/subagents`, `/approve` and the native reviewer controls are delivered; exact child-thread review routing would be a separate extension, not a reason to list `/approve` as missing.
3. **P1 lifecycle foundation:** `/new` and `/fork`, reusing the subscription, selection, draft and history handoff used by `/resume`. Then pursue full `/cd` project-context parity; it depends on this foundation and remains Very high complexity.
4. **P2 extensions:** `/plan` inline prompts, `/archive`, `/debug-config`, daemon diagnostics and reviewed `/init`; then `/review`, `/export`, `/diff`, `/mention`, skill invocation and MCP login/inspection. Put `/clear` after `/new`, agent management after linked-agent navigation, and `/worktree` after lifecycle/project-context support. `/goal` is now implemented.
5. **P3/Defer:** optional global/account, `/presets`, `/config`, import, installation, sandbox, daemon lifecycle, keymap and composer-mode work require explicit scope and demand. Treat daemon diagnostics as the smaller first step to its P2 workflow. Voice, IDE integration and pets should not displace core monitoring/control work. Exclude internal debug and retired/unregistered names.

The next recommended feature PRs are **`/status`** and **`/warnings`** as separate read-only increments. Subsequent PRs can add compaction and reviewed background-terminal stopping. Full directory-context transitions need a separate design covering project configuration/trust loading, conversation handoff and attached-client behavior.

For future updates, compare both the next installed version's registry and generated protocol. Omission from the published list is not evidence that a command is unsupported.

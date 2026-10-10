# Slash-command support audit

Audited on 2026-10-10 against **Codex CLI 0.162.1** (`rust-v0.162.1`), using its [command registry](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/tui/src/slash_command.rs), [native directory-change workflow](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/tui/src/app/working_directory.rs), and protocol types generated with `codex app-server generate-ts --experimental`. Server directory behavior is defined in the matching [turn processor](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server/src/request_processors/turn_processor.rs).

The tables cover all **63 registered command variants** and the three additional accepted aliases `/cwd`, `/pet` and `/clean`: **66 upstream command names** in total. Codexometer’s local `/help` entry and model-advertised speed aliases are outside that count. Platform, feature and debug-build filters affect actual visibility. The [published reference](https://learn.chatgpt.com/docs/developer-commands#built-in-slash-commands) omits some commands in this registry and also lists older names. Codexometer has explicit adapters/local UI implementations; it does not send slash commands as model prompts. There is no generic server endpoint to discover or execute the CLI's slash commands.

“Missing” below means no Codexometer slash adapter, **not** that the server lacks the capability. Live commands require a loaded session and shared app-server; browser control commands require writable mode. Windows' existing file-based monitoring path does not gain live server commands from this change.

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

## Implemented and partial support

| Command | Support | Priority | Complexity | Implementation and limits |
| --- | --- | --- | --- | --- |
| `/rename` | Implemented | Done | — | Confirmed `thread/name/set`; works during active turns. |
| `/cd <path>` | Implemented with narrower semantics | P1 | Very high | Confirmed `thread/settings/update`, sending only `threadId` and `cwd`. Requires idle/local session, empty queue, no pending approval and an existing directory. Resolves relative paths from the running session directory, expands `~`/`~/`, rechecks symlinks and verifies the result from running configuration. No write retries. See the semantics below. Proposed next increment: destination project-context reload with trust/permission validation and a verified conversation handoff. Reuse the lifecycle foundation below; native-client cooperation may still be necessary. |
| `/pwd`, `/cwd` | Implemented aliases | Done | — | Read-only `thread/resume` **without overrides** reports the running directory, including during active turns. Captured `thread/read.cwd` metadata is not used as the answer. The session directory/footer also follows live directory reads and settings notifications. |
| `/model` | Session adapter | Done | — | Live model/reasoning catalogue, confirmed while idle. Clears explicit speed override when selecting a model; does not reproduce native CLI persistence. Session-only scope is intentional; no global-default editor is proposed. |
| `/plan` | Partial | P2 | Medium | Advertised collaboration-mode selection; no native optional prompt argument. Proposed next increment: explicit review of an inline plan prompt and confirmed mode/prompt submission without losing the draft. |
| `/permissions` | Session adapter | Done | — | Live allowed named profiles for the directory, confirmed while idle. Not every native permission-dialog action is implemented. The named-profile session adapter is the proposed scope; broader native dialogs are not scheduled. |
| `/skills` | Browse only | P2 | Medium | `skills/list` inventory/help. No invocation or configuration flow; `skills/config/write` exists but needs scope/review UI. Proposed next increment: invoke an advertised skill through structured input after review. Configuration/enablement writes are a separate High-complexity extension. |
| `/apps` | Browse only | P3 | High | `app/list` inventory/help. Installation, linking and authentication workflows are absent. Proposed next increment: a guided linking/authentication workflow with return/cancellation handling; reuse upstream flows rather than inventing installation semantics. |
| `/mcp` | Browse only | P2 | High | `mcpServerStatus/list` and tool help. No `/mcp login`, resource browser or arbitrary tool invocation. Related APIs exist but need dedicated authentication/execution UI. Proposed next increment: server-specific login/status and verbose inspection. Arbitrary tool execution remains a separate reviewed workflow. |
| `/hooks` | Browse only | P3 | High | `hooks/list`. No editor/review for lifecycle-hook configuration or its command execution scope. Proposed next increment: a scope-aware hook configuration editor with preview and reload handling. |
| `/experimental` | Browse only | P3 | Medium | `experimentalFeature/list`. Enablement changes process/configuration scope, rather than a single session; no mutation UI. Proposed next increment: review an advertised enablement change with its process/configuration scope and refresh/restart effects made clear. |
| `/statusline` | Local equivalent | Done | — | Codexometer's own footer picker; also available in read-only web mode. Does not edit the native CLI's status line. |
| `/help` | Local catalogue | Done | — | Codexometer catalogue entry; absent from the upstream registry. |
| Advertised speed aliases, e.g. `/fast` | Session adapter | Done | — | Show all live model-advertised speeds plus the default in one picker; Up/Down selects and C Apply commits a verified session override. Global defaults remain out of scope. |

`/help` is Codexometer's catalogue entry, absent from this upstream registry. Speed aliases such as `/fast` appear only when the current model advertises that tier; IDs are not guessed. They change session overrides rather than native persistent defaults.

### Directory-change semantics

Codexometer's `/cd` keeps the selected thread ID/history. In 0.162.1, the server replaces the previous directory in default workspace roots, retains other roots and applies existing permission validation. Codexometer sends no permission/model/global configuration overrides. Directory changes are restricted to local execution environments; `/cwd` can display a remote session's reported directory.

Native `/cd` is a larger client workflow: destination project-configuration reload, trust checks, checks for subagents/background terminals, and potentially a conversation fork into a new thread. Codexometer **does not perform that workflow**, reload project configuration/instruction sources, or move subagents or already-running terminals. Other clients attached to the same thread share its server settings, but may retain their own local project/UI context and send their own directory overrides. The confirmation explains this. Use native `/cd` for its full project-context transition. Success is reported only after running configuration reflects the new directory; ignored fields, server rejection, timeout or reconnection remain unconfirmed.

## Missing commands with server capabilities or conversation workflows

These are adapter/workflow gaps. Where an API exists it is listed; this PR only adds directory commands.

| Command | Priority | Complexity | Mechanism / why missing |
| --- | --- | --- | --- |
| `/new` | P1 | High | `thread/start` exists; needs creation options and selection/handoff to the new thread. Proposed next increment: build the common thread-creation/handoff foundation, including subscriptions, drafts and selection. |
| `/clear` | P2 | Medium | Native clear starts a new chat and clears its terminal; no combined local clear/`thread/start` workflow. Hiding monitor rows is different. Proposed next increment: a local clear/new-chat action after the /new foundation exists; Medium assumes that dependency is complete. |
| `/resume` | P1 | High | `thread/list`/`thread/resume` exist; no saved-session picker. Monitor selection observes loaded sessions. Proposed next increment: a paginated saved-session picker with verified load/handoff; share the lifecycle foundation with /new and /fork. |
| `/fork` | P1 | High | `thread/fork` exists; needs options, confirmation and handoff while preserving source drafts. Proposed next increment: a reviewed fork and verified new-thread selection, retaining source history/draft and correct telemetry accounting. |
| `/worktree` | P2 | Very high | Needs actual Git worktree creation/ownership/cleanup plus thread creation/fork. No worktree manager; a cwd update is insufficient. Proposed next increment: managed worktree creation and conversation transition after thread handoff and project-context changes are supported. |
| `/archive` | P2 | Medium | `thread/archive` exists; no archive confirmation/handoff. Close/Close All only hide rows. Proposed next increment: confirm the exact session, observe archive completion and select another row without discarding unrelated drafts. |
| `/delete` | P3 | Medium | `thread/delete` exists; no destructive-delete confirmation and history/selection cleanup. Proposed next increment: explicit permanent-delete confirmation and verified removal, with stale-target and selection/history cleanup. |
| `/compact` | P1 | Medium | `thread/compact/start` exists; needs state checks and completion/error progress rather than reporting a queued operation as done. Proposed next increment: confirmed start plus completion/error tracking, keeping polling and approvals responsive throughout. |
| `/recap` | P2 | High | Native summarization workflow has no adapter/progress UI; verify that workflow before choosing a server operation or prompt. Provisional estimate: first establish the native summarization semantics, then add a reviewed summary workflow without changing the source conversation unexpectedly. |
| `/review` | P2 | High | `review/start` exists; no review-target picker, delivery mode or progress UI. Proposed next increment: target/delivery selection, confirmed start and a dedicated review-progress/result view. |
| `/goal` | P2 | Medium | `thread/goal/get`, `set`, `clear` exist; no goal editor, budget/clear confirmation or goal state. Proposed next increment: read/edit/clear goal and budget with session targeting, confirmation and live state refresh. |
| `/side`, `/btw` | P3 | High | Ephemeral forks need a temporary conversation/draft view, thread routing and cleanup. Proposed next increment: an isolated ephemeral conversation UI with separate drafts, lifecycle cleanup and source-thread return. |
| `/approve` | P1 | High | `thread/approveGuardianDeniedAction` exists; needs exact targeting of a recent automatic-review denial. Ordinary approval buttons are a different action. Proposed next increment: identify the exact retained denied action, review its original scope, confirm the retry and track the result. A generic approval shortcut is insufficient. |
| `/ps` | P1 | Medium | `thread/backgroundTerminals/list` exists; no list/detail adapter. Telemetry process/session views do not reproduce it. Proposed next increment: a read-only, session-scoped terminal list with command/state detail and refresh; foundation for /stop. |
| `/stop`, `/clean` | P1 | Medium | `thread/backgroundTerminals/clean` exists; no session-scoped stop-all review and verification. Turn interruption is a different action. Proposed next increment: confirm and verify stop-all for the selected session, using the /ps inventory and leaving other sessions alone. |
| `/usage` | P1 | Low | `account/usage/read`, rate-limit/reset-credit APIs exist. Usage/reset-token UI already exists; no slash alias opens it. Account reset credits are different from Sessions Zero. Proposed next increment: open the existing usage/reset views through a slash alias; reuse their current confirmation flow rather than adding new account mutations. |
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
| `/agents` | P2 | High | Native agent command center has no adapter; linked-session grouping is not a matching management center. Proposed next increment: a dedicated agent state/management view with capability-aware controls and exact child/root routing. |
| `/subagents` | P1 | Medium | Linked agents are visible in grouping; no slash picker switches their native CLI views. Proposed next increment: a linked-agent picker with separate child detail and preserved root/draft state; native-client switching is not implied. |
| `/copy` | P1 | Low | Copy controls exist; no slash alias/picker for the last response or selected transcript parts. Proposed next increment: open the existing session-copy action from a slash alias; selection of arbitrary historical fragments can follow separately. |
| `/export` | P2 | High | No Markdown export; needs complete paginated history plus a file/download destination. Proposed next increment: complete paginated transcript retrieval and Markdown export with bounded streaming/file/download handling; existing excerpts are insufficient. |
| `/diff` | P2 | High | No repository diff browser with tracked/untracked handling and explicit repository context. Proposed next increment: a directory-aware tracked/untracked diff view with large/binary-file handling and no inferred repository scope. |
| `/mention` | P2 | High | No structured file-mention picker or associated attachment/input metadata routing. Proposed next increment: file search/selection plus structured input metadata through review, send/steer and draft handling. |
| `/init` | P2 | Medium | Native command asks Codex to create `AGENTS.md`; no explicit generation/overwrite review. It is deliberately not silently converted to a prompt. Proposed next increment: explicitly review a request to generate AGENTS.md in the selected directory; report request/agent result rather than claiming the file was created immediately. |
| `/ide` | Defer | Very high | No native IDE-context integration for selection/open files. Proposed scope if demanded: discover/connect an editor and securely route selections/open-file context, including stale/different-workspace handling. |
| `/app` | P3 | Medium | No desktop-app handoff/deep-link action; upstream visibility is platform-dependent. Proposed next increment: a platform-aware desktop-app handoff with validated thread/directory identity and an unavailable-app fallback. |
| `/raw` | P3 | High | Native raw-scrollback toggle cannot control Codexometer's separate rendering; no equivalent local switch. Proposed next increment: a Codexometer-owned scrollback/copy-friendly rendering mode that preserves live input and state; it is not a server toggle. |
| `/tui` | P3 | Medium | Chooses native CLI mode for its next launch; no corresponding Codexometer launch-mode picker. Proposed next increment: local next-launch mode preferences with explicit precedence over flags and terminal capability checks. |
| `/title` | P3 | Medium | No local title-field picker or native title configuration editor. Proposed next increment: a local title-field picker with persistence and safe terminal/browser title updates, reusing statusline field selection. |
| `/theme` | P2 | Low | Codexometer theme preferences exist; no slash entry reproduces the native syntax-highlighting picker. Proposed next increment: open Codexometer’s existing theme preferences; native syntax-theme parity is a separate, larger feature. |
| `/pets`, `/pet` | Defer | High | Native terminal-pet picker has no counterpart here. Proposed scope if demanded: terminal/browser pet assets, animation/state and preference UI; cosmetic expansion outside core monitoring. |
| `/keymap` | P3 | High | No shortcut-remapping editor; native keybindings belong to another interface. Proposed next increment: local remapping with composer/modal precedence, conflict checks and accurate help across all views. |
| `/vim` | P3 | High | No Vim-mode composer. Proposed next increment: composer modes/editing with Unicode, paste, shortcuts, focus and review-modal interactions verified in both interfaces. |
| `/exit`, `/quit` | P2 | Low | Existing local Quit controls exit Codexometer, without slash aliases. Native CLI exit/server shutdown has different scope. Proposed next increment: aliases for local Codexometer exit/leave behavior, clearly labelled; never treat them as shared-daemon shutdown or native CLI exit. |

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

1. **Small P1 additions:** `/usage` and `/copy` aliases, then `/status`, `/warnings` and `/ps` read-only views. They build on existing controls and improve visibility before adding more mutations.
2. **P1 session controls:** `/compact`, the linked-agent `/subagents` picker, and reviewed `/stop`/`clean` built on `/ps`. Add `/approve` as its own High-complexity workflow with exact denied-action targeting.
3. **P1 lifecycle foundation:** `/new`, `/resume` and `/fork`, sharing subscription, selection, draft and history-handoff handling. Then pursue full `/cd` project-context parity; it depends on this foundation and remains Very high complexity.
4. **P2 extensions:** inexpensive local `/theme` and `/exit`/`quit` aliases; `/plan` inline prompts, `/goal`, `/archive`, `/debug-config`, daemon diagnostics and reviewed `/init`; then `/review`, `/export`, `/diff`, `/mention`, skill invocation and MCP login/inspection. Put `/clear` after `/new`, agent management after linked-agent navigation, and `/worktree` after lifecycle/project-context support.
5. **P3/Defer:** optional global/account, import, installation, sandbox, daemon lifecycle, keymap and composer-mode work require explicit scope and demand. Treat daemon diagnostics as the smaller first step to its P2 workflow. Voice, IDE integration and pets should not displace core monitoring/control work. Exclude internal debug and retired/unregistered names.

The next small PR should cover **`/usage`, `/copy`, `/status`, `/warnings` and `/ps`**. Subsequent PRs can add compaction and background-terminal stopping using those views. Full directory-context transitions need a separate design and PR covering project configuration/trust loading, conversation handoff and attached-client behavior.

For future updates, compare both the next installed version's registry and generated protocol. Omission from the published list is not evidence that a command is unsupported.

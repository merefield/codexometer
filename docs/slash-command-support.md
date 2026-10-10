# Slash-command support audit

Audited on 2026-10-10 against **Codex CLI 0.162.1** (`rust-v0.162.1`), using its [command registry](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/tui/src/slash_command.rs), [native directory-change workflow](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/tui/src/app/working_directory.rs), and protocol types generated with `codex app-server generate-ts --experimental`. Server directory behavior is defined in the matching [turn processor](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server/src/request_processors/turn_processor.rs).

The tables cover all **63 registered commands**, including aliases. Platform, feature and debug-build filters affect actual visibility. The [published reference](https://learn.chatgpt.com/docs/developer-commands#built-in-slash-commands) omits some commands in this registry and also lists older names. Codexometer has explicit adapters/local UI implementations; it does not send slash commands as model prompts. There is no generic server endpoint to discover or execute the CLI's slash commands.

“Missing” below means no Codexometer slash adapter, **not** that the server lacks the capability. Live commands require a loaded session and shared app-server; browser control commands require writable mode. Windows' existing file-based monitoring path does not gain live server commands from this change.

## Implemented and partial support

| Command | Support | Implementation and limits |
| --- | --- | --- |
| `/rename` | Implemented | Confirmed `thread/name/set`; works during active turns. |
| `/cd <path>` | Implemented with narrower semantics | Confirmed `thread/settings/update`, sending only `threadId` and `cwd`. Requires idle/local session, empty queue, no pending approval and an existing directory. Resolves relative paths from the running session directory, expands `~`/`~/`, rechecks symlinks and verifies the result from running configuration. No write retries. See the semantics below. |
| `/pwd`, `/cwd` | Implemented aliases | Read-only `thread/resume` **without overrides** reports the running directory, including during active turns. Captured `thread/read.cwd` metadata is not used as the answer. The session directory/footer also follows live directory reads and settings notifications. |
| `/model` | Session adapter | Live model/reasoning catalogue, confirmed while idle. Clears explicit speed override when selecting a model; does not reproduce native CLI persistence. |
| `/plan` | Partial | Advertised collaboration-mode selection; no native optional prompt argument. |
| `/permissions` | Session adapter | Live allowed named profiles for the directory, confirmed while idle. Not every native permission-dialog action is implemented. |
| `/skills` | Browse only | `skills/list` inventory/help. No invocation or configuration flow; `skills/config/write` exists but needs scope/review UI. |
| `/apps` | Browse only | `app/list` inventory/help. Installation, linking and authentication workflows are absent. |
| `/mcp` | Browse only | `mcpServerStatus/list` and tool help. No `/mcp login`, resource browser or arbitrary tool invocation. Related APIs exist but need dedicated authentication/execution UI. |
| `/hooks` | Browse only | `hooks/list`. No editor/review for lifecycle-hook configuration or its command execution scope. |
| `/experimental` | Browse only | `experimentalFeature/list`. Enablement changes process/configuration scope, rather than a single session; no mutation UI. |
| `/statusline` | Local equivalent | Codexometer's own footer picker; also available in read-only web mode. Does not edit the native CLI's status line. |

`/help` is Codexometer's catalogue entry, absent from this upstream registry. Speed aliases such as `/fast` appear only when the current model advertises that tier; IDs are not guessed. They change session overrides rather than native persistent defaults.

### Directory-change semantics

Codexometer's `/cd` keeps the selected thread ID/history. In 0.162.1, the server replaces the previous directory in default workspace roots, retains other roots and applies existing permission validation. Codexometer sends no permission/model/global configuration overrides. Directory changes are restricted to local execution environments; `/cwd` can display a remote session's reported directory.

Native `/cd` is a larger client workflow: destination project-configuration reload, trust checks, checks for subagents/background terminals, and potentially a conversation fork into a new thread. Codexometer **does not perform that workflow**, reload project configuration/instruction sources, or move subagents or already-running terminals. Other clients attached to the same thread share its server settings, but may retain their own local project/UI context and send their own directory overrides. The confirmation explains this. Use native `/cd` for its full project-context transition. Success is reported only after running configuration reflects the new directory; ignored fields, server rejection, timeout or reconnection remain unconfirmed.

## Missing commands with server capabilities or conversation workflows

These are adapter/workflow gaps. Where an API exists it is listed; this PR only adds directory commands.

| Command | Mechanism / why missing |
| --- | --- |
| `/new` | `thread/start` exists; needs creation options and selection/handoff to the new thread. |
| `/clear` | Native clear starts a new chat and clears its terminal; no combined local clear/`thread/start` workflow. Hiding monitor rows is different. |
| `/resume` | `thread/list`/`thread/resume` exist; no saved-session picker. Monitor selection observes loaded sessions. |
| `/fork` | `thread/fork` exists; needs options, confirmation and handoff while preserving source drafts. |
| `/worktree` | Needs actual Git worktree creation/ownership/cleanup plus thread creation/fork. No worktree manager; a cwd update is insufficient. |
| `/archive` | `thread/archive` exists; no archive confirmation/handoff. Close/Close All only hide rows. |
| `/delete` | `thread/delete` exists; no destructive-delete confirmation and history/selection cleanup. |
| `/compact` | `thread/compact/start` exists; needs state checks and completion/error progress rather than reporting a queued operation as done. |
| `/recap` | Native summarization workflow has no adapter/progress UI; verify that workflow before choosing a server operation or prompt. |
| `/review` | `review/start` exists; no review-target picker, delivery mode or progress UI. |
| `/goal` | `thread/goal/get`, `set`, `clear` exist; no goal editor, budget/clear confirmation or goal state. |
| `/side`, `/btw` | Ephemeral forks need a temporary conversation/draft view, thread routing and cleanup. |
| `/approve` | `thread/approveGuardianDeniedAction` exists; needs exact targeting of a recent automatic-review denial. Ordinary approval buttons are a different action. |
| `/ps` | `thread/backgroundTerminals/list` exists; no list/detail adapter. Telemetry process/session views do not reproduce it. |
| `/stop`, `/clean` | `thread/backgroundTerminals/clean` exists; no session-scoped stop-all review and verification. Turn interruption is a different action. |
| `/usage` | `account/usage/read`, rate-limit/reset-credit APIs exist. Usage/reset-token UI already exists; no slash alias opens it. Account reset credits are different from Sessions Zero. |
| `/status` | Settings/usage reads exist; detail/footer show related data, but no consolidated status slash view. |
| `/debug-config` | `config/read` with layers and `configRequirements/read` exist; no directory-aware configuration-source/requirements inspector. |
| `/daybreak` | `thread/metadata/update` can save Daybreak choice; no dedicated feature-aware toggle/state adapter. Do not invent an experimental flag ID. |
| `/memories` | `memory/status`, `thread/memoryMode/set` and configuration APIs exist; no scope-specific settings UI. Thread mode and global generation settings differ. |
| `/import` | `externalAgentConfig/detect`/`import` exist; no selection, preview, progress or configuration/history confirmation. |
| `/plugins` | Listing/search/read/install/uninstall APIs exist; no lifecycle/authentication and installation/configuration-review workflow. |
| `/logout` | `account/logout` exists; needs account-level confirmation and recovery for consequences to shared clients. |
| `/feedback` | `feedback/upload` exists; no log-selection and data-sharing preview/consent workflow. |
| `/setup-default-sandbox` | `windowsSandbox/setupStart`/`readiness` exist; no Windows elevated-sandbox setup workflow. Not applicable to macOS local sessions. |
| `/voice` | `thread/realtime/*`, including voice listing, exist; no microphone/audio transport, device settings or realtime conversation UI. |
| `/daemon` | Native daemon management and `server/diagnostics` exist; no lifecycle-management command. Stopping/restarting affects other clients. |
| `/warnings` | Server/config warning notifications exist; no retained warning/diagnostic browser. Ordinary error notices are only partial analogues. |

## Missing native UI commands

These require Codexometer UX, not forwarding the command name to a server.

| Command | Why missing / existing analogue |
| --- | --- |
| `/agents` | Native agent command center has no adapter; linked-session grouping is not a matching management center. |
| `/subagents` | Linked agents are visible in grouping; no slash picker switches their native CLI views. |
| `/copy` | Copy controls exist; no slash alias/picker for the last response or selected transcript parts. |
| `/export` | No Markdown export; needs complete paginated history plus a file/download destination. |
| `/diff` | No repository diff browser with tracked/untracked handling and explicit repository context. |
| `/mention` | No structured file-mention picker or associated attachment/input metadata routing. |
| `/init` | Native command asks Codex to create `AGENTS.md`; no explicit generation/overwrite review. It is deliberately not silently converted to a prompt. |
| `/ide` | No native IDE-context integration for selection/open files. |
| `/app` | No desktop-app handoff/deep-link action; upstream visibility is platform-dependent. |
| `/raw` | Native raw-scrollback toggle cannot control Codexometer's separate rendering; no equivalent local switch. |
| `/tui` | Chooses native CLI mode for its next launch; no corresponding Codexometer launch-mode picker. |
| `/title` | No local title-field picker or native title configuration editor. |
| `/theme` | Codexometer theme preferences exist; no slash entry reproduces the native syntax-highlighting picker. |
| `/pets`, `/pet` | Native terminal-pet picker has no counterpart here. |
| `/keymap` | No shortcut-remapping editor; native keybindings belong to another interface. |
| `/vim` | No Vim-mode composer. |
| `/exit`, `/quit` | Existing local Quit controls exit Codexometer, without slash aliases. Native CLI exit/server shutdown has different scope. |

## Debug entries and older documented names

| Command | Treatment |
| --- | --- |
| `/rollout` | Debug-build registry entry; no rollout-path slash inspector. |
| `/test-approval` | Debug-build synthetic approval request; not a user command here. Real approvals already have dedicated controls. |
| `/debug-m-drop`, `/debug-m-update` | Registry descriptions say “DO NOT USE”; intentionally not exposed. |
| `/agent` | Older published name; this registry uses `/agents`, not a separate `/agent` command. |
| `/personality` | Published but absent from this registry; protocol personality fields are deprecated. No current CLI style selector to reproduce. |
| `/sandbox-add-read-dir` | Published/platform-specific older entry, absent from this registry. Do not assume availability in every installed version. |

## Follow-up priorities

1. Add slash entry points to existing read-only/local views: `/status`, `/usage`, `/copy`, `/subagents`, plus a background-terminal `/ps` view.
2. Add `/compact`, `/review` and `/goal` with completion/state tracking; then `/new`, `/resume` and `/fork` with thread handoff.
3. Add `/diff`, `/export` and background-terminal stop controls with explicit target scope.
4. Keep account/global configuration, import, installation, sandbox and daemon actions in dedicated reviewed workflows. Their omission is mainly absent UX/scope handling, not absent APIs.

For future updates, compare both the next installed version's registry and generated protocol. Omission from the published list is not evidence that a command is unsupported.

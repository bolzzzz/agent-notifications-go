# Agent Notifications (plugin)

> **Fork notice** — this repository is a personal fork of
> [claude-notifications-go](https://github.com/777genius/claude-notifications-go)
> by 777genius. It adds extra plugin hosts (opencode, the Codex CLI, CodeBuddy
> Code, and the Cursor CLI) and a set of personal patches on top of upstream.

## What this fork adds

- **opencode support** — `.opencode/plugins/notifications.ts` forwards opencode
  events (`session.idle`, `question.asked`, `permission.updated`,
  `session.error`) to the same Go binary via `handle-hook`, so opencode gets
  the full notification pipeline (config, sounds, click-to-focus, webhooks).
- **Codex CLI support** — `.codex-plugin/` registers the same binary as a Codex
  plugin (`Stop`/`SubagentStop`/`PermissionRequest`/`request_user_input`
  events), detected from the payload's `turn_id`/`model` fields or `PLUGIN_ROOT`.
- **CodeBuddy Code support** — `.codebuddy-plugin/` registers the same binary as
  a CodeBuddy Code plugin (`Stop`/`SubagentStop`/`Notification`/`PreToolUse`/
  `SessionStart`/`TeammateIdle` events). CodeBuddy Code is hook-compatible with
  Claude Code (identical event names and stdin payload), so it is identified by
  its `CODEBUDDY_*` environment variables rather than payload fields; the hooks
  pin `--product codebuddy` explicitly. When a `transcript_path` is present the
  plugin normalizes CodeBuddy's own transcript schema (top-level `content[]`,
  `output_text`/`function_call`, epoch-millisecond timestamps) into the shared
  analyzer; otherwise it classifies from the payload's `last_assistant_message`
  exactly like Codex. Configuration is read from
  `~/.codebuddy/agent-notifications-go/config.json` only (not Claude's path).
- **Cursor CLI support** — the Cursor CLI (`agent` / `cursor-agent`) runs hooks
  from `~/.cursor/hooks.json`. Run `agent-notifications install-cursor-hooks` to
  register them; the entries invoke the binary directly and pin
  `--product cursor`. Cursor is detected from its `CURSOR_*` environment
  variables (and the explicit `--product cursor` pin), which win over the Codex
  `model` heuristic. On `stop`, an `aborted` turn is silent, `error` maps to an
  API-error notification, and a completed turn notifies as task-complete — using
  the transcript (`CURSOR_TRANSCRIPT_PATH` / `transcript_path`) for a richer
  summary when transcripts are enabled, or the status alone otherwise. Remove
  the hooks with `agent-notifications uninstall-cursor-hooks`. Configuration is
  read from `~/.cursor/agent-notifications-go/config.json`. This targets the
  Cursor **CLI** only (not the Cursor IDE agent).
- **Cursor approval-wait notifications** — Cursor has no hook for "the agent is
  now waiting for you to approve this command", and its
  `beforeShellExecution` / `beforeMCPExecution` gates fire *before* Cursor
  decides whether a call needs you, so notifying from them directly would fire
  on every auto-approved command. Instead the gate records the pending call and
  a detached watcher re-checks it after a grace period
  (`cursor.approvalWaitSeconds`, default 8). Two signals mean the call was
  approved and nothing is waiting: the matching `afterShellExecution` /
  `afterMCPExecution` event arrived, or the command is still running (`/proc` on
  Linux, `ps` on macOS; unavailable on Windows, where the grace period is the
  only guard). Only a call that is still unresolved notifies. The gates answer
  `{"permission":"allow"}`, meaning this hook has no objection — Cursor's own
  approval policy still decides. Set `cursor.notifyOnApprovalWait` to `false` to
  turn this off.
- **VS Code window-focus fixes** — workspace/cwd-folder-aware window matching
  so the right VS Code window is focused when several are open; the session
  cwd is preserved for focus.
- **Project folder in the notification title** — desktop notifications show
  the project folder name.

---

Everything else — features, installation, configuration, sounds,
click-to-focus and troubleshooting — is identical to upstream, see:

- [Upstream README](https://github.com/777genius/claude-notifications-go#readme)
- [Upstream docs/](https://github.com/777genius/claude-notifications-go/tree/main/docs)

> One difference to keep in mind: in this fork the plugin and its slash
> commands are named `agent-notifications-go`
> (`/agent-notifications-go:init`, `/agent-notifications-go:settings`).

## License

GPL-3.0 — See [LICENSE](LICENSE) file for details.

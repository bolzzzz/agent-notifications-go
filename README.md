# Agent Notifications (plugin)

> **Fork notice** — this repository is a personal fork of
> [claude-notifications-go](https://github.com/777genius/claude-notifications-go)
> by 777genius. It adds extra plugin hosts (opencode and the Codex CLI) and
> a set of personal patches on top of upstream.

## What this fork adds

- **opencode support** — `.opencode/plugins/notifications.ts` forwards opencode
  events (`session.idle`, `question.asked`, `permission.updated`,
  `session.error`) to the same Go binary via `handle-hook`, so opencode gets
  the full notification pipeline (config, sounds, click-to-focus, webhooks).
- **Codex CLI support** — `.codex-plugin/` registers the same binary as a Codex
  plugin (`Stop`/`SubagentStop`/`PermissionRequest`/`request_user_input`
  events), detected from the payload's `turn_id`/`model` fields or `PLUGIN_ROOT`.
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

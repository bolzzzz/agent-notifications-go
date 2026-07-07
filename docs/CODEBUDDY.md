# CodeBuddy Code support

`claude-notifications-go` works with **CodeBuddy Code** as well as Claude Code.
The two CLIs share an identical hooks contract (JSON over stdin, the same event
names, and a `hooks/hooks.json` plugin mechanism), so almost nothing had to
change — only the environment variables each tool exports for its plugin hooks.

## How detection works

The binary auto-detects which tool invoked it from the environment:

| Signal | Claude Code | CodeBuddy Code |
| --- | --- | --- |
| Plugin root env var | `CLAUDE_PLUGIN_ROOT` | `CODEBUDDY_PLUGIN_ROOT` |
| Judge-mode env var | `CLAUDE_HOOK_JUDGE_MODE` | `CODEBUDDY_HOOK_JUDGE_MODE` |

When `CODEBUDDY_PLUGIN_ROOT` is present the caller is treated as CodeBuddy
(`internal/product`). All other behavior (desktop notifications, sounds,
click-to-focus, webhooks, transcript analysis) is shared.

## Wiring into CodeBuddy

CodeBuddy reads hook configuration from `~/.codebuddy/settings.json` (user
scope) or `<project>/.codebuddy/settings.json`. The repository ships a ready-made
hook definition at [`hooks/codebuddy-hooks.json`](../hooks/codebuddy-hooks.json)
that mirrors `hooks/hooks.json` but expands `${CODEBUDDY_PLUGIN_ROOT}`.

Merge the `hooks` block from `hooks/codebuddy-hooks.json` into your
`~/.codebuddy/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "ExitPlanMode|AskUserQuestion",
        "hooks": [ { "type": "command",
                     "command": "sh",
                     "args": ["${CODEBUDDY_PLUGIN_ROOT}/bin/hook-wrapper.sh", "handle-hook", "PreToolUse"],
                     "timeout": 30 } ] }
    ],
    "Notification": [
      { "matcher": "permission_prompt",
        "hooks": [ { "type": "command",
                     "command": "sh",
                     "args": ["${CODEBUDDY_PLUGIN_ROOT}/bin/hook-wrapper.sh", "handle-hook", "Notification"],
                     "timeout": 30 } ] },
      { "matcher": "idle_prompt",
        "hooks": [ { "type": "command",
                     "command": "sh",
                     "args": ["${CODEBUDDY_PLUGIN_ROOT}/bin/hook-wrapper.sh", "handle-hook", "Notification"],
                     "timeout": 30 } ] }
    ],
    "SessionStart": [
      { "hooks": [ { "type": "command",
                     "command": "sh",
                     "args": ["${CODEBUDDY_PLUGIN_ROOT}/bin/hook-wrapper.sh", "handle-hook", "SessionStart"],
                     "timeout": 30 } ] }
    ],
    "Stop": [
      { "hooks": [ { "type": "command",
                     "command": "sh",
                     "args": ["${CODEBUDDY_PLUGIN_ROOT}/bin/hook-wrapper.sh", "handle-hook", "Stop"],
                     "timeout": 30 } ] }
    ],
    "SubagentStop": [
      { "hooks": [ { "type": "command",
                     "command": "sh",
                     "args": ["${CODEBUDDY_PLUGIN_ROOT}/bin/hook-wrapper.sh", "handle-hook", "SubagentStop"],
                     "timeout": 30 } ] }
    ],
    "TeammateIdle": [
      { "hooks": [ { "type": "command",
                     "command": "sh",
                     "args": ["${CODEBUDDY_PLUGIN_ROOT}/bin/hook-wrapper.sh", "handle-hook", "TeammateIdle"],
                     "timeout": 30 } ] }
    ]
  }
}
```

`bin/hook-wrapper.sh` self-heals `CLAUDE_PLUGIN_ROOT` from its own location, so
all existing `${CLAUDE_PLUGIN_ROOT}` asset paths (icon, sounds) continue to
resolve under CodeBuddy.

## Configuration

Settings are **shared** with Claude Code: both tools read
`~/.claude/claude-notifications-go/config.json`. No separate config file is
required for CodeBuddy. See [config docs](ARCHITECTURE.md) for the full schema.

## Notes

- CodeBuddy's `Notification` payload uses `message` + `notification_type`
  (no `title`). The dispatcher does not read `title`/`message` from the payload
  — notifications are generated from the session transcript — so this difference
  is transparent.
- `idle_prompt` (CodeBuddy waiting > 60s for input) maps to the same
  `question` status as `permission_prompt`, so it triggers the question sound and
  notification.
- Transcript analysis assumes the CodeBuddy transcript format is compatible with
  the Claude-style JSONL transcript. If a future CodeBuddy transcript diverges,
  the `Stop`/`SubagentStop` analysis may need adjustment.

# Plugin Compatibility

Compatible with other Claude Code plugins that spawn background Claude instances.

## double-shot-latte

**[double-shot-latte](https://github.com/obra/double-shot-latte)** — auto-continue plugin that uses a background Claude instance for context evaluation. Notifications are automatically suppressed for the background judge process (via `CLAUDE_HOOK_JUDGE_MODE=true` environment variable).

## For plugin developers

If you're developing a plugin that spawns background Claude instances and want to suppress notifications, set `CLAUDE_HOOK_JUDGE_MODE=true` in the environment before invoking Claude.

To disable this behavior and receive notifications even in judge mode, set in `~/.claude/agent-notifications-go/config.json`:

```json
{
  "notifications": {
    "respectJudgeMode": false
  }
}
```

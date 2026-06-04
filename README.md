# bo-personalized — changes on top of main

This branch contains Bo's personal patches applied on top of the upstream
[claude-notifications-go](https://github.com/777genius/claude-notifications-go) `main` branch.

## Changes

### `CLAUDE_NOTIFICATIONS_BIN` support on Linux

The `CLAUDE_NOTIFICATIONS_BIN` environment variable is now honoured in both
the hook wrapper script and the daemon's binary lookup, so a custom or
platform-specific binary path is respected at runtime instead of always
falling back to the `bin/claude-notifications` symlink.

Commits: `e4c3d1d`, `5c8dad1`

---

### GNOME Wayland: correct VS Code window focus when multiple windows are open

**Problem.** On GNOME Wayland, `activateByWmClass("code")` was tried first.
Because all VS Code windows share the same WM class, the wrong window was
focused when more than one was open. The folder-specific search (step 2)
was never reached.

**Fix.**
- `GetSearchTermWithFolder` now returns `"<folder> — Visual Studio Code"`
  (with em dash) for VS Code. The em dash suffix is present in VS Code
  window titles but absent from browser tab titles, so the search term
  precisely matches the right VS Code window without false-matching a
  Chrome/Firefox tab that contains the project folder name.
- `TryActivateWindowByTitle` tries the folder-specific substring search
  **first**; WM class is now the fallback for when no folder name is
  available or the search finds nothing.

Commit: `2a51717`

---

### D-Bus urgency set to Critical for all notifications

Linux desktop notifications are sent with urgency `2` (Critical) so they
break through Focus / Do-Not-Disturb and persist in the notification centre
until dismissed, matching the behaviour of macOS time-sensitive alerts.

Commit: `7044539`

---

### Notification replacement: new notification replaces the previous one from the same window

When Claude finishes a second task in the same VS Code window / terminal tab
before the user has dismissed the previous notification, the new notification
now **replaces** the old one in-place rather than stacking a second banner.

**Linux.** The daemon tracks the last D-Bus notification ID per source window
(keyed by X11 window ID on X11, or `terminal:folder` on Wayland). The next
`Notify` call passes that ID as `replaces_id`, causing the compositor to
update the existing notification in place. When the user dismisses a
notification the entry is removed, so the next notification from that window
creates a fresh banner.

**macOS.** `terminal-notifier -group` is now set to `claude-notif:<cwd>`
(stable per project directory) instead of a random timestamp. Notifications
with the same group ID replace each other.

Commit: `bf18a50`

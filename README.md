# bo-personalized — changes on top of fix/gnome-focus-chrome-false-match

## Changes

### D-Bus urgency set to Critical

Linux desktop notifications are sent with urgency `2` (Critical) so they
break through Focus / Do-Not-Disturb and persist in the notification centre
until dismissed.

Commit: `7044539`

---

### Notification replacement: new notification replaces the previous one from the same window

When Claude finishes a second task in the same VS Code window / terminal tab
before the user has dismissed the previous notification, the new notification
replaces the old one in-place rather than stacking a second banner.

**Linux.** The daemon tracks the last D-Bus notification ID per source window
(keyed by X11 window ID on X11, or `terminal:folder` on Wayland). The next
`Notify` call passes that ID as `replaces_id`. When the user dismisses a
notification the entry is removed, so the next one from that window starts
fresh.

**macOS.** `terminal-notifier -group` is now set to `claude-notif:<cwd>`
(stable per project directory) instead of a random timestamp. Notifications
with the same group ID replace each other.

Commit: `bf18a50`

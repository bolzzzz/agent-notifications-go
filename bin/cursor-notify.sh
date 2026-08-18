#!/bin/sh
# cursor-notify.sh — adapt Cursor CLI agent hooks to agent-notifications
# handle-hook. Only needed when wiring hooks by hand from a plugin checkout;
# `agent-notifications install-cursor-hooks` invokes the binary directly and
# does not use this wrapper.
#
# Cursor uses camelCase events and a different stdin schema (conversation_id,
# model, workspace_roots, status, …). This wrapper maps the event name and
# pins --product cursor so the Go binary does not mis-detect Cursor's model
# field as Codex.
#
# Usage (from hooks/hooks-cursor.json or ~/.cursor/hooks.json):
#   bin/cursor-notify.sh sessionStart
#   bin/cursor-notify.sh stop
#   bin/cursor-notify.sh subagentStop
#   bin/cursor-notify.sh beforeShellExecution
#   bin/cursor-notify.sh afterShellExecution
#   bin/cursor-notify.sh beforeMCPExecution
#   bin/cursor-notify.sh afterMCPExecution
#
# Binary / plugin root resolution (first hit wins):
#   1. $AGENT_NOTIFICATIONS_BIN
#   2. $AGENT_NOTIFICATIONS_PLUGIN_ROOT/bin/hook-wrapper.sh
#   3. <this-script>/hook-wrapper.sh (plugin checkout)
#   4. agent-notifications on PATH

set -eu

EVENT_RAW="${1:-}"
if [ -z "$EVENT_RAW" ]; then
  echo "cursor-notify.sh: missing event name" >&2
  exit 1
fi

# The before* gates must print a permission decision on stdout. The Go binary
# may write other noise there, so its stdout is discarded and the decision is
# emitted here instead.
NEED_PERMISSION_ALLOW=0

case "$EVENT_RAW" in
  sessionStart|SessionStart) HOOK_EVENT="SessionStart" ;;
  stop|Stop) HOOK_EVENT="Stop" ;;
  subagentStop|SubagentStop) HOOK_EVENT="SubagentStop" ;;
  beforeShellExecution|BeforeShellExecution|beforeMCPExecution|BeforeMCPExecution)
    # Cursor has no event for "auto-review decided this needs you". These gates
    # are the closest signal, but they fire before that decision, so the binary
    # only records the call and a detached watcher notifies if it stays
    # unresolved. Allow states this hook has no objection; Cursor's own approval
    # policy still applies.
    HOOK_EVENT="$EVENT_RAW"
    NEED_PERMISSION_ALLOW=1
    ;;
  afterShellExecution|AfterShellExecution|afterMCPExecution|AfterMCPExecution)
    # Cancels the watcher started by the matching before* gate.
    HOOK_EVENT="$EVENT_RAW"
    ;;
  *)
    echo "cursor-notify.sh: unsupported event: $EVENT_RAW" >&2
    exit 0
    ;;
esac

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PLUGIN_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"

resolve_target() {
  if [ -n "${AGENT_NOTIFICATIONS_BIN:-}" ] && [ -x "$AGENT_NOTIFICATIONS_BIN" ]; then
    printf '%s\n' "direct:$AGENT_NOTIFICATIONS_BIN"
    return 0
  fi
  if [ -n "${AGENT_NOTIFICATIONS_PLUGIN_ROOT:-}" ] && [ -x "$AGENT_NOTIFICATIONS_PLUGIN_ROOT/bin/hook-wrapper.sh" ]; then
    printf '%s\n' "wrapper:$AGENT_NOTIFICATIONS_PLUGIN_ROOT/bin/hook-wrapper.sh"
    return 0
  fi
  if [ -x "$SCRIPT_DIR/hook-wrapper.sh" ]; then
    printf '%s\n' "wrapper:$SCRIPT_DIR/hook-wrapper.sh"
    return 0
  fi
  if command -v agent-notifications >/dev/null 2>&1; then
    printf '%s\n' "direct:$(command -v agent-notifications)"
    return 0
  fi
  return 1
}

RESOLVED="$(resolve_target)" || {
  echo "cursor-notify.sh: agent-notifications binary not found; set AGENT_NOTIFICATIONS_BIN or AGENT_NOTIFICATIONS_PLUGIN_ROOT" >&2
  # Still let the agent proceed when the permission gates cannot be handled.
  if [ "$NEED_PERMISSION_ALLOW" -eq 1 ]; then
    printf '%s\n' '{"permission":"allow"}'
  fi
  exit 0
}

TARGET="${RESOLVED#*:}"

export AGENT_NOTIFICATIONS_PLUGIN_ROOT="${AGENT_NOTIFICATIONS_PLUGIN_ROOT:-$PLUGIN_ROOT}"

# Never block the Cursor agent loop on notification failures.
set +e
if [ "$NEED_PERMISSION_ALLOW" -eq 1 ]; then
  "$TARGET" handle-hook "$HOOK_EVENT" --product cursor >/dev/null
  printf '%s\n' '{"permission":"allow"}'
else
  "$TARGET" handle-hook "$HOOK_EVENT" --product cursor
fi
exit 0

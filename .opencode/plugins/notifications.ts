// opencode plugin for claude-notifications-go.
//
// opencode has no JSON-command hooks (unlike Claude Code/Codex), so this
// plugin forwards opencode events to the Go binary's `handle-hook` subcommand
// over stdin. All notification logic (config, dedup, sounds, click-to-focus,
// webhooks) lives in the Go binary and is shared with the Claude Code/Codex
// integrations.
//
// Event mapping:
//   session.idle          -> handle-hook Stop          (or SubagentStop for
//                                                       child/subagent sessions)
//   question.asked        -> handle-hook Notification  (opencode question tool)
//   permission.updated    -> handle-hook Notification  (approval prompt)
//   session.error         -> handle-hook Stop          (API/auth errors)
//
// Binary resolution order:
//   1. $CLAUDE_NOTIFICATIONS_BIN
//   2. $CLAUDE_PLUGIN_ROOT/bin/<binary> (existing Claude Code plugin install)
//   3. <plugin file>/../../bin/<binary> (plugin checkout, e.g. this repo)
//   4. claude-notifications on PATH
import type { Plugin } from "@opencode-ai/plugin"
import { spawn } from "node:child_process"
import { existsSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const PRODUCT = "opencode"
const HOOK_TIMEOUT_MS = 25_000

// Error names (session.error properties.error.name) worth notifying about.
// MessageAbortedError (user pressed abort), MessageOutputLengthError
// (auto-compaction) and UnknownError are deliberately skipped.
const NOTIFY_ERROR_NAMES = new Set(["APIError", "ProviderAuthError"])

type EventProperties = Record<string, unknown>

type HookPayload = {
  session_id: string
  cwd: string
  product: string
  last_assistant_message?: string
  message?: string
  error_type?: string
}

function pluginFileDir(): string {
  try {
    return path.dirname(fileURLToPath(import.meta.url))
  } catch {
    return process.cwd()
  }
}

// platformBinaryNames returns the binary names for this host, e.g.
// ["claude-notifications", "claude-notifications-darwin-arm64"] (with .exe on
// Windows, matching install.sh's claude-notifications-<os>-<arch> assets).
function platformBinaryNames(): string[] {
  const names = ["claude-notifications"]
  const osMap: Record<string, string> = { darwin: "darwin", linux: "linux", win32: "windows" }
  const archMap: Record<string, string> = { x64: "amd64", arm64: "arm64" }
  const os = osMap[process.platform]
  const arch = archMap[process.arch]
  if (os && arch) {
    names.push(`claude-notifications-${os}-${arch}`)
  }
  return process.platform === "win32" ? names.map((n) => `${n}.exe`) : names
}

function candidateBins(root: string): string[] {
  return platformBinaryNames().map((name) => path.join(root, "bin", name))
}

function resolveBinary(): { bin: string; pluginRoot: string | null; found: boolean } {
  const candidates: Array<{ bin: string; root: string | null }> = []

  const explicit = process.env.CLAUDE_NOTIFICATIONS_BIN
  if (explicit) candidates.push({ bin: explicit, root: null })

  const claudeRoot = process.env.CLAUDE_PLUGIN_ROOT
  if (claudeRoot) {
    for (const bin of candidateBins(claudeRoot)) candidates.push({ bin, root: claudeRoot })
  }

  // Plugin checkout layout: <root>/.opencode/plugins/notifications.ts with the
  // binary at <root>/bin/ (e.g. running opencode inside this repository).
  const checkoutRoot = path.resolve(pluginFileDir(), "..", "..")
  for (const bin of candidateBins(checkoutRoot)) candidates.push({ bin, root: checkoutRoot })

  for (const candidate of candidates) {
    if (existsSync(candidate.bin)) return { ...candidate, found: true }
  }

  // Last resort: rely on PATH.
  return { bin: "claude-notifications", root: claudeRoot, found: false }
}

function runHook(hookEvent: string, payload: HookPayload, onMissingBinary?: () => void): void {
  const resolved = resolveBinary()
  if (!resolved.found) {
    onMissingBinary?.()
    return
  }
  const env: NodeJS.ProcessEnv = { ...process.env }
  if (resolved.pluginRoot) {
    env.CLAUDE_NOTIFICATIONS_PLUGIN_ROOT = resolved.pluginRoot
  }

  let child: ReturnType<typeof spawn>
  try {
    child = spawn(resolved.bin, ["handle-hook", hookEvent], {
      stdio: ["pipe", "ignore", "ignore"],
      env,
      windowsHide: true,
    })
  } catch {
    return
  }

  const timer = setTimeout(() => child.kill(), HOOK_TIMEOUT_MS)
  child.on("error", () => clearTimeout(timer))
  child.on("exit", () => clearTimeout(timer))

  try {
    child.stdin?.write(JSON.stringify(payload))
    child.stdin?.end()
  } catch {
    // Payload write failure: nothing to do, the hook simply won't fire.
  }
}

function eventProperties(event: { type: string; properties?: EventProperties }): EventProperties {
  return event.properties ?? {}
}

// lastAssistantText fetches the session's last assistant message and returns
// its text content ("" when the session never produced an assistant message).
type AssistantMessage = { info: { role: string }; parts: Array<{ type: string; text?: string }> }

async function lastAssistantText(
  client: { session: { messages: (opts: unknown) => Promise<AssistantMessage[] | { data?: AssistantMessage[] }> } },
  sessionID: string,
): Promise<string> {
  const result = await client.session.messages({ path: { id: sessionID } })
  // The SDK (hey-api client) resolves to a { data, error, ... } envelope;
  // tolerate a bare array too in case a future client unwraps it.
  const messages = Array.isArray(result) ? result : (result?.data ?? [])
  for (let i = messages.length - 1; i >= 0; i--) {
    const message = messages[i]
    if (!message || message.info?.role !== "assistant") continue
    const parts = message.parts ?? []
    const text = parts
      .filter((part) => part && part.type === "text" && typeof part.text === "string")
      .map((part) => part.text)
      .join("\n")
      .trim()
    if (text) return text
  }
  return ""
}

async function isChildSession(
  client: { session: { get: (opts: unknown) => Promise<{ parentID?: string } | { data?: { parentID?: string } } | undefined> } },
  sessionID: string,
): Promise<boolean> {
  try {
    const result = await client.session.get({ path: { id: sessionID } })
    const info = result && "data" in (result as object) ? (result as { data?: { parentID?: string } }).data : (result as { parentID?: string } | undefined)
    return Boolean(info?.parentID)
  } catch {
    return false
  }
}

export const Notifications: Plugin = async ({ client, directory }) => {
  const cwd = directory || process.cwd()
  let warnedMissingBinary = false

  const log = (level: "info" | "warn" | "debug", message: string, extra?: Record<string, unknown>) => {
    if (level === "debug" && process.env.CLAUDE_NOTIFICATIONS_DEBUG !== "1") return
    client.app
      .log({ body: { service: "claude-notifications-go", level, message, ...(extra ? { extra } : {}) } })
      .catch(() => {})
  }

  const resolved = resolveBinary()
  log("info", "opencode plugin initialized", {
    directory: cwd,
    binary: resolved.bin,
    binaryFound: resolved.found,
    pluginRoot: resolved.pluginRoot,
  })

  let firstEventLogged = false
  const seenEventTypes = new Set<string>()

  const handleIdle = async (sessionID: string, source: string) => {
    if (!sessionID) return
    const text = await lastAssistantText(client, sessionID)
    if (!text) {
      log("info", "session.idle skipped: no assistant text", { sessionID, source })
      return // nothing was produced (empty session, shell, cancel)
    }
    const isChild = await isChildSession(client, sessionID)
    const hookEvent = isChild ? "SubagentStop" : "Stop"
    log("info", "session.idle fired", { sessionID, source, isChild, textLength: text.length })
    await fire(hookEvent, { session_id: sessionID, last_assistant_message: text })
  }

  const fire = async (hookEvent: string, payload: Partial<HookPayload>): Promise<void> => {
    try {
      runHook(hookEvent, { cwd, product: PRODUCT, ...payload } as HookPayload, () => {
        if (warnedMissingBinary) return
        warnedMissingBinary = true
        // One-time warning; the binary is usually at CLAUDE_PLUGIN_ROOT/bin
        // or CLAUDE_NOTIFICATIONS_BIN.
        log("warn", "claude-notifications binary not found. Set CLAUDE_NOTIFICATIONS_BIN or CLAUDE_PLUGIN_ROOT.")
      })
    } catch {
      // Never let notification failures break the opencode session.
    }
  }

  return {
    event: async ({ event }) => {
      const properties = eventProperties(event)
      const sessionID = typeof properties.sessionID === "string" ? properties.sessionID : ""

      if (!firstEventLogged) {
        firstEventLogged = true
        log("info", "opencode plugin receiving events", { firstEventType: event.type })
      }
      if (!seenEventTypes.has(event.type)) {
        seenEventTypes.add(event.type)
        log("info", "event type seen", { type: event.type })
      }

      try {
        switch (event.type) {
          case "session.idle": {
            await handleIdle(sessionID, "session.idle")
            return
          }

          case "session.status": {
            // Fallback: some opencode versions deliver idle via session.status.
            const status = properties.status as { type?: string } | undefined
            if (status?.type === "idle") {
              await handleIdle(sessionID, "session.status:idle")
            }
            return
          }

          case "question.asked": {
            // opencode's `question` tool (AskUserQuestion equivalent).
            if (!sessionID) return
            const questions = Array.isArray(properties.questions) ? (properties.questions as Array<{ question?: string }>) : []
            const questionText = questions.map((q) => q?.question).find((q) => typeof q === "string" && q.trim() !== "")
            log("info", "question.asked fired", { sessionID })
            await fire("Notification", { session_id: sessionID, message: questionText })
            return
          }

          case "permission.updated": {
            // Approval prompt waiting for the user.
            if (!sessionID) return
            const title = typeof properties.title === "string" ? properties.title : ""
            log("info", "permission.updated fired", { sessionID })
            await fire("Notification", { session_id: sessionID, message: title })
            return
          }

          case "session.error": {
            if (!sessionID) return
            const error = properties.error as { name?: unknown } | undefined
            const name = typeof error?.name === "string" ? error.name : ""
            if (!NOTIFY_ERROR_NAMES.has(name)) return
            log("info", "session.error fired", { sessionID, errorName: name })
            await fire("Stop", { session_id: sessionID, error_type: name })
            return
          }
        }
      } catch (err) {
        log("warn", "event handler error", { eventType: event.type, error: String(err) })
      }
    },
  }
}

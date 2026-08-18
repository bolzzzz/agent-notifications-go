// Package product identifies which AI CLI (Claude Code, Codex, opencode,
// CodeBuddy Code, or the Cursor CLI) is invoking the hook. Claude Code, Codex,
// CodeBuddy and Cursor deliver hook events as JSON over stdin, but opencode has
// no JSON-command hooks — its TS plugin forwards events to this binary and
// carries an explicit "product": "opencode" field.
//
// Codex adds Codex-specific extension fields (turn_id, model) to every event
// payload and exports PLUGIN_ROOT for plugin-bundled hooks, while Claude Code
// exports only CLAUDE_PLUGIN_ROOT.
//
// CodeBuddy Code is hook-compatible with Claude Code: identical event names and
// an identical stdin payload shape, with no distinguishing payload fields. It
// is therefore identified by its own environment variables (CODEBUDDY_*), which
// it injects into every hook subprocess. Note that CodeBuddy also exports the
// CLAUDE_* names as compatibility aliases, so CLAUDE_* must never be used as a
// Claude Code signal.
//
// The Cursor CLI (`agent`/`cursor-agent`) and Cursor IDE agent both run hooks
// from ~/.cursor/hooks.json and inject CURSOR_* environment variables into
// each hook subprocess. The stop payload also carries a model field that would
// otherwise match the Codex heuristic, so CURSOR_* env (and the explicit
// "product": "cursor" / --product cursor pin used by the install command) must
// win over Codex detection.
package product

import "os"

const (
	// Claude is Anthropic's Claude Code CLI.
	Claude = "claude"
	// Codex is OpenAI's Codex CLI.
	Codex = "codex"
	// OpenCode is the opencode CLI (plugin forwards events to handle-hook).
	OpenCode = "opencode"
	// CodeBuddy is Tencent's CodeBuddy Code CLI.
	CodeBuddy = "codebuddy"
	// Cursor is the Cursor CLI agent hooks host.
	Cursor = "cursor"
)

// codeBuddyEnvVars are environment variables CodeBuddy Code injects into hook
// subprocesses. CODEBUDDY_PLUGIN_ROOT is set only for plugin-bundled hooks,
// so the project/session variables are needed to also cover hooks configured
// directly in settings.json.
var codeBuddyEnvVars = []string{
	"CODEBUDDY_PLUGIN_ROOT",
	"CODEBUDDY_PROJECT_DIR",
	"CODEBUDDY_SESSION_ID",
}

// cursorEnvVars are environment variables Cursor injects into hook subprocesses
// (documented in Cursor's hooks reference). CURSOR_PROJECT_DIR and
// CURSOR_VERSION are always present for Agent hooks; CURSOR_TRANSCRIPT_PATH is
// added when transcripts are enabled.
var cursorEnvVars = []string{
	"CURSOR_PROJECT_DIR",
	"CURSOR_VERSION",
	"CURSOR_TRANSCRIPT_PATH",
}

// Detect returns the product currently invoking the hook.
//
// CodeBuddy Code is checked first: its payload is indistinguishable from Claude
// Code's, so only the environment can tell them apart. The check must also
// precede the PLUGIN_ROOT heuristic below, because PLUGIN_ROOT may be inherited
// from an outer shell and would otherwise misidentify CodeBuddy as Codex.
//
// Cursor is checked next: every Cursor stop payload carries a model field that
// would otherwise match the Codex heuristic, so CURSOR_* env must win before
// turn_id/model are consulted.
//
// Codex is then identified by either:
//   - Codex-specific extension fields in the hook input JSON (turn_id is
//     present on all turn-scoped events, model on every event), or
//   - the PLUGIN_ROOT environment variable, which Codex exports for
//     plugin-bundled hooks (Claude Code exports only CLAUDE_PLUGIN_ROOT).
//
// Everything else is treated as Claude Code, preserving the historical
// default.
func Detect(turnID, model string) string {
	if isCodeBuddyEnv() {
		return CodeBuddy
	}
	if isCursorEnv() {
		return Cursor
	}
	if turnID != "" || model != "" {
		return Codex
	}
	if os.Getenv("PLUGIN_ROOT") != "" {
		return Codex
	}
	return Claude
}

// IsCursorEnv reports whether any Cursor-specific environment variable is set
// to a non-empty value. The Cursor IDE and Cursor CLI both inject these into
// hook subprocesses.
func IsCursorEnv() bool {
	return isCursorEnv()
}

// isCursorEnv reports whether any Cursor-specific environment variable is set
// to a non-empty value.
func isCursorEnv() bool {
	for _, name := range cursorEnvVars {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}

// isCodeBuddyEnv reports whether any CodeBuddy-specific environment variable is
// set to a non-empty value.
func isCodeBuddyEnv() bool {
	for _, name := range codeBuddyEnvVars {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}

// FromPayload returns the product for a hook payload, honoring an explicit
// product field (set by the opencode plugin, which controls the entire JSON
// payload, and by the CodeBuddy hooks via the --product flag) before falling
// back to the heuristic signals.
func FromPayload(product, turnID, model string) string {
	switch product {
	case OpenCode, Codex, CodeBuddy, Claude, Cursor:
		return product
	}
	return Detect(turnID, model)
}

// FromPayloadWithDefault behaves like FromPayload but substitutes defaultProduct
// when the payload carries no explicit product field and no Codex payload
// signals (turn_id/model). This lets callers pin a base product — tests running
// inside a CodeBuddy session inherit CODEBUDDY_* env that would otherwise make
// product.Detect report CodeBuddy for every Claude-path payload. When an
// explicit product field or Codex signals are present, they still win; an empty
// defaultProduct disables the override entirely (production behavior).
func FromPayloadWithDefault(product, turnID, model, defaultProduct string) string {
	switch product {
	case OpenCode, Codex, CodeBuddy, Claude, Cursor:
		return product
	}
	if defaultProduct != "" && turnID == "" && model == "" {
		return defaultProduct
	}
	return Detect(turnID, model)
}

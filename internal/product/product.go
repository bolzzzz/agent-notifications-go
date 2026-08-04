// Package product identifies which AI CLI (Claude Code, Codex, opencode, or
// CodeBuddy Code) is invoking the hook. Claude Code, Codex and CodeBuddy
// deliver hook events as JSON over stdin, but opencode has no JSON-command
// hooks — its TS plugin forwards events to this binary and carries an explicit
// "product": "opencode" field.
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

// Detect returns the product currently invoking the hook.
//
// CodeBuddy Code is checked first: its payload is indistinguishable from Claude
// Code's, so only the environment can tell them apart. The check must also
// precede the PLUGIN_ROOT heuristic below, because PLUGIN_ROOT may be inherited
// from an outer shell and would otherwise misidentify CodeBuddy as Codex.
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
	if turnID != "" || model != "" {
		return Codex
	}
	if os.Getenv("PLUGIN_ROOT") != "" {
		return Codex
	}
	return Claude
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
	case OpenCode, Codex, CodeBuddy, Claude:
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
	case OpenCode, Codex, CodeBuddy, Claude:
		return product
	}
	if defaultProduct != "" && turnID == "" && model == "" {
		return defaultProduct
	}
	return Detect(turnID, model)
}

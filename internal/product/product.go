// Package product identifies which AI CLI (Claude Code or Codex) is invoking
// the hook. Both tools deliver hook events as JSON over stdin, but Codex adds
// Codex-specific extension fields (turn_id, model) to every event payload and
// exports PLUGIN_ROOT for plugin-bundled hooks, while Claude Code exports only
// CLAUDE_PLUGIN_ROOT.
package product

import "os"

const (
	// Claude is Anthropic's Claude Code CLI.
	Claude = "claude"
	// Codex is OpenAI's Codex CLI.
	Codex = "codex"
)

// Detect returns the product currently invoking the hook.
//
// Codex is identified by either:
//   - Codex-specific extension fields in the hook input JSON (turn_id is
//     present on all turn-scoped events, model on every event), or
//   - the PLUGIN_ROOT environment variable, which Codex exports for
//     plugin-bundled hooks (Claude Code exports only CLAUDE_PLUGIN_ROOT).
//
// Everything else is treated as Claude Code, preserving the historical
// default.
func Detect(turnID, model string) string {
	if turnID != "" || model != "" {
		return Codex
	}
	if os.Getenv("PLUGIN_ROOT") != "" {
		return Codex
	}
	return Claude
}

// Name returns a human-friendly product name for user-visible text
// (notifications, webhook footers, logs).
func Name(product string) string {
	if product == Codex {
		return "Codex"
	}
	return "Claude Code"
}

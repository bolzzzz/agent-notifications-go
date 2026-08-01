// Package product identifies which AI CLI (Claude Code, Codex, or opencode)
// is invoking the hook. Claude Code and Codex deliver hook events as JSON over
// stdin, but opencode has no JSON-command hooks — its TS plugin forwards
// events to this binary and carries an explicit "product": "opencode" field.
// Codex adds Codex-specific extension fields (turn_id, model) to every event
// payload and exports PLUGIN_ROOT for plugin-bundled hooks, while Claude Code
// exports only CLAUDE_PLUGIN_ROOT.
package product

import "os"

const (
	// Claude is Anthropic's Claude Code CLI.
	Claude = "claude"
	// Codex is OpenAI's Codex CLI.
	Codex = "codex"
	// OpenCode is the opencode CLI (plugin forwards events to handle-hook).
	OpenCode = "opencode"
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

// FromPayload returns the product for a hook payload, honoring an explicit
// product field (set by the opencode plugin, which controls the entire JSON
// payload) before falling back to the Codex/Claude Code heuristic signals.
func FromPayload(product, turnID, model string) string {
	switch product {
	case OpenCode, Codex:
		return product
	}
	return Detect(turnID, model)
}
